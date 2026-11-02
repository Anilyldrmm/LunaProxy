package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func openRunKey(write bool) (registry.Key, error) {
	access := uint32(registry.QUERY_VALUE)
	if write {
		access = registry.SET_VALUE
	}
	return registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`, access)
}

const appName = "LunaProxy"

type app struct {
	mu         sync.Mutex
	running    bool
	localIP    string
	proxySrv   *http.Server
	pacSrv     *http.Server
	pacPort    int                // aktif PAC port'u takip — port değişirse sunucu yeniden başlar
	dpiSource  string             // aktif DPI kaynağı: "service"|"process"|"manual"|"bundle"|"disabled"|"none"|""
	stopCancel context.CancelFunc // stop goroutine'ini iptal eder (hızlı yeniden başlatma için)
	stopDone   chan struct{}      // stop goroutine'i Shutdown'ı tamamladığında kapanır
	gen        uint64             // her start'ta artar; eski DPI başlatma goroutine'i kendini iptal eder
}

// current — gen hâlâ aktif çalışmaya mı ait? a.mu tutulurken çağrılır.
func (a *app) current(gen uint64) bool { return a.running && a.gen == gen }

var g *app

// appExiting — tray "Çıkış" tıklandığında true; Closing handler gerçek kapanmaya izin verir.
var appExiting bool

func main() {
	if relaunchElevated() {
		return
	}
	if !ensureSingleInstance() {
		bringExistingToFront()
		return
	}

	SentinelCheck()
	loadConfig()
	g = &app{localIP: getLocalIP()}
	// Eski sürümlerin Run anahtarı kaydı yükseltilmiş uygulamayı açılışta başlatmıyor: zamanlanmış göreve taşı.
	if windows.GetCurrentProcessToken().IsElevated() && runKeyHasApp() {
		go setStartup(true)
	}

	// PAC sunucusunu proxy'den bağımsız, hemen başlat.
	// Başlangıçta DIRECT döndürür; proxy açılınca otomatik güncellenir.
	c := getConfig()
	addFirewallRules(c.ProxyPort, c.PACPort)
	if pac, err := startPAC(g.localIP, c.PACPort); err == nil {
		g.pacSrv = pac
		g.pacPort = c.PACPort
	}

	initTray()

	StartUpdateChecker(func(tag, url string) {
		pendingUpdateTag.Store(tag)
		pendingUpdateURL.Store(url)
		showToast("LunaProxy Güncellemesi", "Yeni sürüm mevcut: "+tag+" — uygulamayı açıp güncelleyin")
		pushStatus()
	})

	if c.ProxyAutoStart {
		if err := g.start(); err != nil {
			logError("Otomatik başlatma hatası: " + err.Error())
		}
	}

	initWindow() // WebView2 mesaj döngüsü — çıkana kadar bloklar
}

// ── Uygulama yaşam döngüsü ────────────────────────────────────────────────────

func (a *app) start() error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil
	}
	// Pending stop goroutine varsa iptal et ve portun serbest kalmasını bekle
	if a.stopCancel != nil {
		cancel := a.stopCancel
		done := a.stopDone
		a.stopCancel = nil
		a.stopDone = nil
		a.mu.Unlock()
		cancel()
		<-done
		a.mu.Lock()
		if a.running {
			a.mu.Unlock()
			return nil
		}
	}
	defer a.mu.Unlock()

	c := getConfig()

	// Açılışta ağ hazır değilse localIP 127.0.0.1 kalır; PAC telefona loopback verir.
	if ip := getLocalIP(); ip != a.localIP && !strings.HasPrefix(ip, "127.") {
		a.localIP = ip
	}

	// HTTP proxy
	ps, err := startProxy(c.ProxyPort)
	if err != nil {
		if strings.Contains(err.Error(), "bind") || strings.Contains(err.Error(), "Only one usage") || strings.Contains(err.Error(), "address already in use") {
			return fmt.Errorf("Port %d başka bir uygulama tarafından kullanılıyor — Ayarlar → Ağ Portlarından değiştirin", c.ProxyPort)
		}
		return fmt.Errorf("proxy başlatılamadı (port %d): %w", c.ProxyPort, err)
	}

	// PAC sunucusu — port değişmediyse mevcut sunucuyu yeniden kullan.
	// Proxy durdurulduğunda PAC çalışmaya devam eder (DIRECT modunda),
	// böylece mobil cihazlar interneti kesmez.
	if a.pacSrv == nil || a.pacPort != c.PACPort {
		if a.pacSrv != nil {
			a.pacSrv.Shutdown(context.Background())
			a.pacSrv = nil
		}
		cs, err := startPAC(a.localIP, c.PACPort)
		if err != nil {
			ps.Close()
			if strings.Contains(err.Error(), "bind") || strings.Contains(err.Error(), "Only one usage") || strings.Contains(err.Error(), "address already in use") {
				return fmt.Errorf("PAC portu %d kullanımda — Ayarlar → Ağ Portlarından değiştirin", c.PACPort)
			}
			return fmt.Errorf("PAC başlatılamadı (port %d): %w", c.PACPort, err)
		}
		a.pacSrv = cs
		a.pacPort = c.PACPort
	}

	// PAC içeriğini proxy moduna al; router'a da bildir
	setPACRunning(a.localIP, c.ProxyPort)
	go pushRouterPAC(a.localIP, "proxy", c.ProxyPort)
	startRouterHeartbeat(guessGatewayIP(a.localIP))

	if c.DNSMode != "unchanged" && c.DNSMode != "" {
		go func() {
			if err := ApplyDNS(c.DNSMode); err != nil {
				logWarn("DNS uygulanamadı: " + err.Error())
			}
		}()
	}

	if c.SetSystemProxy {
		BackupSystemProxy()
		go SetSystemProxy(fmt.Sprintf("%s:%d", a.localIP, c.ProxyPort))
	}

	a.gen++
	gen := a.gen
	go func() {
		result, err := ResolveDPI(c)
		a.mu.Lock()
		if !a.current(gen) { // bu arada stop/restart oldu
			a.mu.Unlock()
			return
		}
		if err != nil {
			logWarn("DPI kaynağı belirlenemedi: " + err.Error())
			a.dpiSource = "none"
			a.mu.Unlock()
			return
		}
		a.dpiSource = result.Source
		a.mu.Unlock()
		if result.Source == "internal" {
			logInfo("DPI: dahili motor aktif (kapsam: " + c.DPIScope + "), strateji test ediliyor…")
			if c.DPIScope != "pc" {
				selectStrategy()
			}
			if c.DPIScope == "pc" || c.DPIScope == "apps" {
				if IsGDPIServiceRunning() || FindGDPIProcess() {
					logWarn("DPI: PC'de GoodbyeDPI çalışıyor — dahili motorla aynı anda paket işleyebilir, sorun olursa GoodbyeDPI'ı durdurun")
				}
				a.mu.Lock() // stop() de a.mu altında stopDivert çağırır → başlatma ile durdurma yarışmaz
				if a.current(gen) {
					if err := startDivert(c.DPIScope, c.DPIApps); err != nil {
						logError("DPI: WinDivert başlatılamadı, sadece proxy girişi çalışıyor: " + err.Error())
						evalJS(fmt.Sprintf(`showError(%s)`, jsonEscape(err.Error())))
					}
				}
				a.mu.Unlock()
			}
		} else if result.ExePath != "" {
			if err := gdpi.Start(result.ExePath, activeGDPIFlags()); err != nil {
				logError("GoodbyeDPI başlatılamadı: " + err.Error())
			}
		} else {
			logInfo("GoodbyeDPI kaynağı: " + result.Source + " (harici, dokunulmuyor)")
		}
	}()

	a.proxySrv = ps
	a.running = true

	stats.reset()
	watchdog.Stop()
	watchdog.Start()

	logInfo(fmt.Sprintf("LunaProxy başlatıldı | IP:%s Proxy:%d PAC:%d DPIMode:%s ISP:%s DNS:%s DPISrc:%s",
		a.localIP, c.ProxyPort, c.PACPort, c.DPIMode, c.ISP, c.DNSMode, c.DPISource))
	return nil
}

func (a *app) stop() {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		return
	}

	// 1. Heartbeat durdur + PAC'ı DIRECT yap
	stopRouterHeartbeat()
	setPACDirect()

	// 2. Durumu hemen stopped'a al — watchdog ve UI güncellenir.
	proxySrv := a.proxySrv
	localIP := a.localIP
	a.proxySrv = nil
	a.running = false

	c := getConfig()
	if c.DNSMode != "unchanged" && c.DNSMode != "" {
		go RestoreDNS()
	}
	if c.SetSystemProxy {
		RestoreSystemProxy()
	}
	if gdpi.IsRunning() {
		gdpi.Stop()
	}
	stopDivert()
	a.dpiSource = ""

	// Önceki stop goroutine'i varsa iptal et (aynı proxy iki kez kapatılmasın)
	if a.stopCancel != nil {
		a.stopCancel()
	}
	stopCtx, stopCancel := context.WithCancel(context.Background())
	a.stopCancel = stopCancel
	done := make(chan struct{})
	a.stopDone = done

	a.mu.Unlock()

	// 3. Router PAC'ı senkron güncelle, ardından proxy'yi kapat.
	// pushRouterPAC önce tamamlanır (max ~6s), sonra 5s daha bekle → iOS yeni PAC'ı çeker.
	// start() çağrılırsa stopCtx iptal edilir → hemen Shutdown().
	go func() {
		defer close(done)
		pushRouterPAC(localIP, "direct", 0)
		select {
		case <-time.After(5 * time.Second):
		case <-stopCtx.Done(): // start() veya restart() çağrıldı — hemen kapat
		}
		if proxySrv != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			proxySrv.Shutdown(ctx)
		}
		logInfo("LunaProxy durduruldu — PAC sunucusu DIRECT modunda çalışıyor")
	}()
}

// shutdown — uygulama tamamen kapanırken çağrılır.
// PAC'ı DIRECT'e alır, sonra PAC sunucusunu da kapatır.
func (a *app) shutdown() {
	a.stop()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pacSrv != nil {
		a.pacSrv.Shutdown(context.Background())
		a.pacSrv = nil
	}
}

func (a *app) restart() {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		if err := a.start(); err != nil {
			logError("Yeniden başlatma başarısız: " + err.Error())
		}
		return
	}

	// Pending stop goroutine'i iptal et
	if a.stopCancel != nil {
		a.stopCancel()
		a.stopCancel = nil
		a.stopDone = nil
	}

	stopRouterHeartbeat()
	setPACDirect()

	proxySrv := a.proxySrv
	a.proxySrv = nil
	a.running = false

	c := getConfig()
	if c.DNSMode != "unchanged" && c.DNSMode != "" {
		go RestoreDNS()
	}
	if c.SetSystemProxy {
		RestoreSystemProxy()
	}
	if gdpi.IsRunning() {
		gdpi.Stop()
	}
	stopDivert()
	a.dpiSource = ""
	a.mu.Unlock()

	// Grace period yok — restart'ta portu hemen serbest bırak
	if proxySrv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		proxySrv.Shutdown(ctx)
		cancel()
	}

	if err := a.start(); err != nil {
		logError("Yeniden başlatma başarısız: " + err.Error())
	}
}

// ── Windows kayıt defteri — otomatik başlangıç ────────────────────────────────

// Yükseltilmiş (yönetici) çalışan uygulamaları Windows, Run anahtarından açılışta başlatmaz.
// Bu yüzden başlangıç kaydı "en yüksek yetkiyle" zamanlanmış görevdir; görev oluşturulamazsa
// (yükseltilmemiş çalışıyorsa) Run anahtarına düşülür.
func startupEnabled() bool {
	if _, err := hiddenOutput("schtasks", "/Query", "/TN", appName); err == nil {
		return true
	}
	return runKeyHasApp()
}

// underDir — path, dir'in altında mı (büyük/küçük harf duyarsız; "C:\Program FilesX" eşleşmez).
func underDir(path, dir string) bool {
	if dir == "" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(filepath.Clean(path)), strings.ToLower(filepath.Clean(dir))+`\`)
}

// relaunchElevated — yönetici hesabı yetkisiz (filtrelenmiş token) başlatıldıysa kendini UAC ile
// yükseltilmiş yeniden açar; WinDivert ve firewall kuralları yönetici ister. Standart kullanıcıda
// (yükseltecek yetki yok) veya UAC reddedilirse false döner ve uygulama yetkisiz devam eder.
func relaunchElevated() bool {
	const tokenElevationTypeLimited = 3
	t := windows.GetCurrentProcessToken()
	var typ, n uint32
	if windows.GetTokenInformation(t, windows.TokenElevationType, (*byte)(unsafe.Pointer(&typ)), 4, &n) != nil ||
		typ != tokenElevationTypeLimited {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL) == nil
}

func runKeyHasApp() bool {
	k, err := openRunKey(false)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(appName)
	return err == nil
}

func setStartup(on bool) {
	if k, err := openRunKey(true); err == nil {
		k.DeleteValue(appName) // eski kayıt: yükseltilmiş uygulamayı zaten başlatmıyordu
		k.Close()
	}
	hiddenOutput("schtasks", "/Delete", "/TN", appName, "/F")
	if !on {
		logInfo("Windows başlangıcından kaldırıldı")
		return
	}
	exe, _ := os.Executable()
	// Yükseltilmiş görev yalnızca standart kullanıcının yazamadığı Program Files'taki exe için:
	// aksi halde exe'yi değiştiren herhangi bir süreç açılışta sessizce yönetici olur.
	pf, _ := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, 0)
	if !underDir(exe, pf) {
		if k, err := openRunKey(true); err == nil {
			k.SetStringValue(appName, exe)
			k.Close()
		}
		logInfo("Windows başlangıcına eklendi (Program Files dışı: yetkisiz başlar)")
		return
	}
	// Varsayılan görev ayarları 72 saatte süreci öldürür ve pilde başlatmaz; ikisi de kapatılır.
	ps := fmt.Sprintf(`Register-ScheduledTask -TaskName '%s' -Force -RunLevel Highest -User "$env:USERDOMAIN\$env:USERNAME" `+
		`-Action (New-ScheduledTaskAction -Execute '%s') `+
		`-Trigger (New-ScheduledTaskTrigger -AtLogOn -User "$env:USERDOMAIN\$env:USERNAME") `+
		`-Settings (New-ScheduledTaskSettingsSet -ExecutionTimeLimit 0 -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries) | Out-Null`,
		appName, strings.ReplaceAll(exe, "'", "''"))
	if out, err := hiddenOutput("powershell", "-NoProfile", "-NonInteractive", "-Command", ps); err != nil {
		logWarn("Başlangıç görevi oluşturulamadı, Run anahtarı kullanılıyor: " + strings.TrimSpace(out) + " " + err.Error())
		if k, err := openRunKey(true); err == nil {
			k.SetStringValue(appName, exe)
			k.Close()
		}
	}
	logInfo("Windows başlangıcına eklendi")
}
