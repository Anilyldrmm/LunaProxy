# DPI Öncelik Sistemi + Bundle + UI Yenileme — Implementasyon Planı

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** GoodbyeDPI için 4 katmanlı öncelik sistemi ekle; bundle embed altyapısını kur; Settings'e DPI Kaynağı seçim grubu ekle; durum satırını güncelle.

**Architecture:** Yeni `dpi_priority.go` servis/proses/manuel/bundle öncelik mantığını barındırır. Config'e `dpi_source` eklenir, `ManageGDPI` kaldırılır. `app` struct'ına `dpiSource` alanı eklenerek aktif kaynak saklanır — her 2 saniyede bir OS sorgusu yapılmaz. Settings sekmesine RadioButton grubu ile kaynak seçimi eklenir.

**Tech Stack:** Go 1.21, github.com/lxn/walk (GUI), golang.org/x/sys/windows, go:embed (withbundle build tag ile)

---

## Dosya Haritası

| Dosya | Değişiklik |
|---|---|
| `config.go` | `DPISource string` ekle, `ManageGDPI` kaldır |
| `dpi_priority.go` | **YENİ** — öncelik tespiti + launch |
| `dpi_priority_test.go` | **YENİ** — parse fonksiyonları unit testleri |
| `bundle.go` | **YENİ** — `//go:embed`, `ExtractBundledGDPI()` (build tag: withbundle) |
| `bundle_stub.go` | **YENİ** — boş stub (build tag: !withbundle) |
| `assets/gdpi/README.txt` | **YENİ** — kullanıcı buraya binary koyar |
| `main.go` | `start()` / `stop()` DPI bloğunu güncelle; `app.dpiSource` ekle |
| `status.go` | `StatusPayload.DPISourceLabel` ekle, `buildStatus()` güncelle |
| `ui.go` | Settings sekmesi DPI Kaynağı grubu; `refreshStatus()` GDPI satırı |

---

## Task 1: Config — DPISource ekle, ManageGDPI kaldır

**Files:**
- Modify: `config.go`

- [ ] **Adım 1: Config struct'ı güncelle**

`config.go` dosyasında `GDPIPath` ve `ManageGDPI` bloğunu şu şekilde değiştir:

```go
// ESKİ:
// GoodbyeDPI yönetimi
GDPIPath   string `json:"gdpi_path"`
ManageGDPI bool   `json:"manage_gdpi"`

// YENİ:
// GoodbyeDPI yönetimi
GDPIPath  string `json:"gdpi_path"`
DPISource string `json:"dpi_source"` // "auto" | "service" | "manual" | "disabled"
```

- [ ] **Adım 2: defaultConfig güncelle**

```go
func defaultConfig() Config {
	return Config{
		ProxyPort:      8888,
		PACPort:        8080,
		DPIMode:        "balanced",
		ChunkSize:      40,
		ISP:            "auto",
		DNSMode:        "unchanged",
		DPISource:      "auto",
		SetSystemProxy: false,
	}
}
```

- [ ] **Adım 3: loadConfig validasyonu güncelle**

`loadConfig()` içindeki validation bloğundan `ManageGDPI` referansını kaldır, `DPISource` kontrolü ekle:

```go
func loadConfig() {
	c := defaultConfig()
	if data, err := os.ReadFile(configFilePath()); err == nil {
		json.Unmarshal(data, &c)
	}
	if c.ProxyPort == 0 { c.ProxyPort = 8888 }
	if c.PACPort == 0 { c.PACPort = 8080 }
	if c.DPIMode == "" { c.DPIMode = "balanced" }
	if c.ChunkSize == 0 { c.ChunkSize = 40 }
	if c.ISP == "" { c.ISP = "auto" }
	if c.DPISource == "" { c.DPISource = "auto" }
	cfgMu.Lock()
	current = c
	cfgMu.Unlock()
}
```

- [ ] **Adım 4: Build kontrolü — derlenir mi?**

```
cd C:\Users\anil_\OneDrive\Masaüstü\mobilDPI
go build ./...
```

`ManageGDPI` referansı olan dosyalar hata verecek (main.go, ui.go). Hata listesine bak, Task 4 ve 6'da bunlar düzeltilecek. **Şimdi build başarısız olması bekleniyor — hataları not et.**

- [ ] **Adım 5: Commit**

```
git add config.go
git commit -m "feat: config — DPISource ekle, ManageGDPI kaldır"
```

---

## Task 2: Bundle altyapısı — bundle.go + bundle_stub.go

**Files:**
- Create: `bundle.go` (build tag: withbundle)
- Create: `bundle_stub.go` (build tag: !withbundle)
- Create: `assets/gdpi/README.txt`

- [ ] **Adım 1: assets/gdpi dizini ve README oluştur**

`assets/gdpi/README.txt` dosyası oluştur:

```
GoodbyeDPI Bundle Dosyaları
============================
Bu klasöre şu dosyaları yerleştir:
  goodbyedpi.exe   (~1.3 MB)
  WinDivert.dll    (~70 KB)
  WinDivert64.sys  (~90 KB)

Ardından bundle ile derle:
  go build -tags withbundle -o SpAC3DPI.exe

Bundle olmadan normal derleme:
  go build -o SpAC3DPI.exe
```

- [ ] **Adım 2: bundle_stub.go oluştur (varsayılan — assets olmadan derlenir)**

```go
//go:build !withbundle

package main

func BundledGDPIAvailable() bool { return false }

func ExtractBundledGDPI() (string, error) {
	return "", nil // bundle yok, sessizce dön
}
```

- [ ] **Adım 3: bundle.go oluştur (withbundle tag ile)**

```go
//go:build withbundle

package main

import (
	"embed"
	"io"
	"os"
	"path/filepath"
)

//go:embed assets/gdpi/goodbyedpi.exe assets/gdpi/WinDivert.dll assets/gdpi/WinDivert64.sys
var bundledFS embed.FS

func BundledGDPIAvailable() bool { return true }

// ExtractBundledGDPI — embed edilmiş dosyaları %APPDATA%\SpAC3DPI\bin\ altına çıkartır.
// Zaten varsa üzerine yazmaz. exe yolunu döner.
func ExtractBundledGDPI() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	binDir := filepath.Join(dir, "SpAC3DPI", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return "", err
	}

	files := []string{
		"assets/gdpi/goodbyedpi.exe",
		"assets/gdpi/WinDivert.dll",
		"assets/gdpi/WinDivert64.sys",
	}
	for _, src := range files {
		dst := filepath.Join(binDir, filepath.Base(src))
		if err := extractFile(bundledFS, src, dst); err != nil {
			return "", err
		}
	}
	return filepath.Join(binDir, "goodbyedpi.exe"), nil
}

func extractFile(fs embed.FS, src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil // zaten var
	}
	in, err := fs.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
```

- [ ] **Adım 4: Normal build hâlâ çalışıyor mu?**

```
go build ./...
```

Beklenen: Sadece Task 1'den gelen `ManageGDPI` hataları kalmalı, bundle hataları YOK.

- [ ] **Adım 5: Commit**

```
git add bundle.go bundle_stub.go assets/gdpi/README.txt
git commit -m "feat: bundle altyapısı — embed stub + withbundle tag"
```

---

## Task 3: dpi_priority.go — Öncelik tespiti

**Files:**
- Create: `dpi_priority.go`
- Create: `dpi_priority_test.go`

- [ ] **Adım 1: dpi_priority.go oluştur**

```go
package main

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// DPILaunchResult — ResolveDPI'ın döndürdüğü sonuç.
// ExePath boşsa GDPI zaten dışarıdan çalışıyor (dokunma).
// Source: "service" | "process" | "manual" | "bundle" | "disabled" | "none"
type DPILaunchResult struct {
	Source  string
	ExePath string // boşsa başlatma (dış kaynak)
}

// IsGDPIServiceRunning — "GoodbyeDPI" Windows servisi çalışıyor mu?
func IsGDPIServiceRunning() bool {
	for _, name := range []string{"GoodbyeDPI", "goodbyedpi"} {
		out, err := hiddenOutput("sc", "query", name)
		if err != nil {
			continue
		}
		if parseServiceRunning(out) {
			return true
		}
	}
	return false
}

// parseServiceRunning — sc query çıktısından RUNNING durumunu parse eder.
func parseServiceRunning(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "STATE") && strings.Contains(line, "RUNNING") {
			return true
		}
	}
	return false
}

// FindGDPIProcess — herhangi bir goodbyedpi.exe prosesi çalışıyor mu?
func FindGDPIProcess() bool {
	out, err := hiddenOutput("tasklist", "/FI", "IMAGENAME eq goodbyedpi.exe", "/NH")
	if err != nil {
		return false
	}
	return parseProcessFound(out)
}

// parseProcessFound — tasklist çıktısında proses adı geçiyor mu?
func parseProcessFound(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "goodbyedpi.exe")
}

// ResolveDPI — config'e göre DPI kaynağını belirler.
// "auto" modunda öncelik sırası: servis → proses → manuel → bundle → yok.
// Servis veya mevcut proses bulunursa ExePath="" döner (dokunma).
func ResolveDPI(c Config) (DPILaunchResult, error) {
	switch c.DPISource {
	case "disabled":
		return DPILaunchResult{Source: "disabled"}, nil

	case "service":
		if !IsGDPIServiceRunning() {
			return DPILaunchResult{Source: "none"}, fmt.Errorf("GoodbyeDPI servisi çalışmıyor")
		}
		return DPILaunchResult{Source: "service"}, nil // ExePath="" → dokunma

	case "manual":
		if c.GDPIPath == "" {
			return DPILaunchResult{Source: "none"}, fmt.Errorf("manuel yol belirtilmemiş")
		}
		return DPILaunchResult{Source: "manual", ExePath: c.GDPIPath}, nil

	default: // "auto" veya boş
		// 1. Windows servisi
		if IsGDPIServiceRunning() {
			return DPILaunchResult{Source: "service"}, nil
		}
		// 2. Mevcut proses
		if FindGDPIProcess() {
			return DPILaunchResult{Source: "process"}, nil
		}
		// 3. Manuel yol
		if c.GDPIPath != "" {
			return DPILaunchResult{Source: "manual", ExePath: c.GDPIPath}, nil
		}
		// 4. Bundle
		if BundledGDPIAvailable() {
			exePath, err := ExtractBundledGDPI()
			if err != nil {
				return DPILaunchResult{Source: "none"}, fmt.Errorf("bundle çıkartılamadı: %w", err)
			}
			return DPILaunchResult{Source: "bundle", ExePath: exePath}, nil
		}
		// 5. Hiçbiri yok
		return DPILaunchResult{Source: "none"}, nil
	}
}

// hiddenOutput — komutu gizli pencere ile çalıştırır, stdout döner.
func hiddenOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	return string(out), err
}
```

- [ ] **Adım 2: dpi_priority_test.go oluştur**

```go
package main

import "testing"

func TestParseServiceRunning(t *testing.T) {
	running := `SERVICE_NAME: GoodbyeDPI
        TYPE               : 10  WIN32_OWN_PROCESS
        STATE              : 4  RUNNING
        WIN32_EXIT_CODE    : 0  (0x0)`

	stopped := `SERVICE_NAME: GoodbyeDPI
        TYPE               : 10  WIN32_OWN_PROCESS
        STATE              : 1  STOPPED
        WIN32_EXIT_CODE    : 0  (0x0)`

	notFound := `[SC] EnumQueryServicesStatus:OpenService FAILED 1060`

	if !parseServiceRunning(running) {
		t.Error("RUNNING durumu tespit edilemedi")
	}
	if parseServiceRunning(stopped) {
		t.Error("STOPPED yanlışlıkla RUNNING döndü")
	}
	if parseServiceRunning(notFound) {
		t.Error("hata çıktısı yanlışlıkla RUNNING döndü")
	}
}

func TestParseProcessFound(t *testing.T) {
	found := `goodbyedpi.exe            1234 Console                    1     4,512 K`
	empty := `INFO: No tasks are currently running which match the specified criteria.`

	if !parseProcessFound(found) {
		t.Error("proses tespit edilemedi")
	}
	if parseProcessFound(empty) {
		t.Error("boş çıktı yanlışlıkla proses döndü")
	}
}

func TestResolveDPIDisabled(t *testing.T) {
	c := Config{DPISource: "disabled"}
	r, err := ResolveDPI(c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "disabled" || r.ExePath != "" {
		t.Errorf("disabled: source=%s exePath=%s", r.Source, r.ExePath)
	}
}

func TestResolveDPIManualNoPath(t *testing.T) {
	c := Config{DPISource: "manual", GDPIPath: ""}
	_, err := ResolveDPI(c)
	if err == nil {
		t.Error("yol yokken hata bekleniyor")
	}
}

func TestResolveDPIManualWithPath(t *testing.T) {
	c := Config{DPISource: "manual", GDPIPath: `C:\gdpi\goodbyedpi.exe`}
	r, err := ResolveDPI(c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "manual" || r.ExePath != `C:\gdpi\goodbyedpi.exe` {
		t.Errorf("manual: source=%s exePath=%s", r.Source, r.ExePath)
	}
}
```

- [ ] **Adım 3: Testleri çalıştır**

```
go test ./... -run TestParse -v
go test ./... -run TestResolveDPI -v
```

Beklenen: 5 test PASS.

- [ ] **Adım 4: Commit**

```
git add dpi_priority.go dpi_priority_test.go
git commit -m "feat: dpi_priority — 4 katmanlı öncelik tespiti + testler"
```

---

## Task 4: main.go — DPI öncelik sistemini bağla

**Files:**
- Modify: `main.go`

- [ ] **Adım 1: `app` struct'ına dpiSource ekle**

`main.go` dosyasındaki `app` struct'ına alan ekle:

```go
type app struct {
	mu        sync.Mutex
	running   bool
	localIP   string
	proxySrv  *http.Server
	pacSrv    *http.Server
	pacPort   int
	dpiSource string // aktif DPI kaynağı ("service"|"process"|"manual"|"bundle"|"disabled"|"none"|"")
}
```

- [ ] **Adım 2: start() içindeki ManageGDPI bloğunu değiştir**

`start()` fonksiyonunda şu bloğu bul ve sil:

```go
if c.ManageGDPI && c.GDPIPath != "" {
    go func() {
        StopWindowsService()
        if err := gdpi.Start(c.GDPIPath, activeGDPIFlags()); err != nil {
            logError("GoodbyeDPI başlatılamadı: " + err.Error())
        }
    }()
}
```

Yerine şunu ekle (aynı konuma):

```go
go func() {
    result, err := ResolveDPI(c)
    if err != nil {
        logWarn("DPI kaynağı belirlenemedi: " + err.Error())
        a.mu.Lock()
        a.dpiSource = "none"
        a.mu.Unlock()
        return
    }
    a.mu.Lock()
    a.dpiSource = result.Source
    a.mu.Unlock()
    if result.ExePath != "" {
        if err := gdpi.Start(result.ExePath, activeGDPIFlags()); err != nil {
            logError("GoodbyeDPI başlatılamadı: " + err.Error())
        }
    } else {
        logInfo("GoodbyeDPI kaynağı: " + result.Source + " (harici, dokunulmuyor)")
    }
}()
```

- [ ] **Adım 3: stop() içindeki ManageGDPI bloğunu değiştir**

`stop()` fonksiyonunda şu satırı bul ve sil:

```go
if c.ManageGDPI {
    gdpi.Stop()
}
```

Yerine şunu ekle:

```go
// Sadece bizim başlattığımız proses varsa durdur (servis/harici prosese dokunma)
if gdpi.IsRunning() {
    gdpi.Stop()
}
a.dpiSource = ""
```

- [ ] **Adım 4: logInfo formatını güncelle**

`start()` sonundaki log satırını güncelle:

```go
// ESKİ:
logInfo(fmt.Sprintf("SpAC3DPI başlatıldı | IP:%s Proxy:%d PAC:%d DPI:%s ISP:%s DNS:%s",
    a.localIP, c.ProxyPort, c.PACPort, c.DPIMode, c.ISP, c.DNSMode))

// YENİ:
logInfo(fmt.Sprintf("SpAC3DPI başlatıldı | IP:%s Proxy:%d PAC:%d DPIMode:%s ISP:%s DNS:%s DPISrc:%s",
    a.localIP, c.ProxyPort, c.PACPort, c.DPIMode, c.ISP, c.DNSMode, c.DPISource))
```

- [ ] **Adım 5: Build başarılı mı?**

```
go build ./...
```

Beklenen: Sadece `ui.go` içindeki `ManageGDPI` referansı hata verebilir. Hata sayısı azaldı mı kontrol et.

- [ ] **Adım 6: Commit**

```
git add main.go
git commit -m "feat: main — DPI öncelik sistemi start/stop bağlandı"
```

---

## Task 5: status.go — DPI kaynak bilgisi ekle

**Files:**
- Modify: `status.go`

- [ ] **Adım 1: StatusPayload'a DPISourceLabel ekle**

`StatusPayload` struct'ına alan ekle:

```go
type StatusPayload struct {
	Running        bool
	Uptime         string
	ActiveConns    int64
	TotalConns     int64
	TotalBytes     string
	Errors         int64
	Restarts       int64
	LocalIP        string
	ProxyPort      int
	PACPort        int
	PACUrl         string
	DPIMode        string
	DPIModeName    string
	ChunkSize      int
	ISP            string
	ISPName        string
	GDPIFlags      string
	GDPIRunning    bool
	GDPIManaged    bool
	DPISourceLabel string // "Sistem Servisi" | "Mevcut Proses" | "Manuel" | "Bundle" | "Devre Dışı" | "—"
	DNSMode        string
	DNSName        string
	SetSysProxy    bool
}
```

- [ ] **Adım 2: buildStatus() güncelle**

`buildStatus()` fonksiyonuna `dpiSourceLabel` hesaplaması ekle ve return'e dahil et:

```go
func buildStatus() StatusPayload {
	c := getConfig()
	ip := g.localIP

	modeName := dpiModeNames[c.DPIMode]
	ispName, ok := ispNames[c.ISP]
	if !ok {
		ispName = c.ISP
	}
	dnsName, ok := dnsNames[c.DNSMode]
	if !ok {
		dnsName = c.DNSMode
	}

	g.mu.Lock()
	dpiSrc := g.dpiSource
	g.mu.Unlock()

	var dpiSourceLabel string
	var gdpiRunning bool
	switch dpiSrc {
	case "service":
		dpiSourceLabel = "Sistem Servisi"
		gdpiRunning = true
	case "process":
		dpiSourceLabel = "Mevcut Proses"
		gdpiRunning = true
	case "manual":
		dpiSourceLabel = "Manuel"
		gdpiRunning = gdpi.IsRunning()
	case "bundle":
		dpiSourceLabel = "Bundle (dahili)"
		gdpiRunning = gdpi.IsRunning()
	case "disabled":
		dpiSourceLabel = "Devre Dışı"
		gdpiRunning = false
	default:
		dpiSourceLabel = "—"
		gdpiRunning = gdpi.IsRunning()
	}

	return StatusPayload{
		Running:        g.running,
		Uptime:         stats.uptimeStr(),
		ActiveConns:    atomic.LoadInt64(&stats.activeConns),
		TotalConns:     atomic.LoadInt64(&stats.totalConns),
		TotalBytes:     stats.bytesStr(),
		Errors:         atomic.LoadInt64(&stats.errors),
		Restarts:       watchdog.RestartCount(),
		LocalIP:        ip,
		ProxyPort:      c.ProxyPort,
		PACPort:        c.PACPort,
		PACUrl:         fmt.Sprintf("http://%s:%d/proxy.pac", ip, c.PACPort),
		DPIMode:        c.DPIMode,
		DPIModeName:    modeName,
		ChunkSize:      c.ChunkSize,
		ISP:            c.ISP,
		ISPName:        ispName,
		GDPIFlags:      activeGDPIFlags(),
		GDPIRunning:    gdpiRunning,
		GDPIManaged:    dpiSrc == "manual" || dpiSrc == "bundle",
		DPISourceLabel: dpiSourceLabel,
		DNSMode:        c.DNSMode,
		DNSName:        dnsName,
		SetSysProxy:    c.SetSystemProxy,
	}
}
```

- [ ] **Adım 3: Build**

```
go build ./...
```

Beklenen: `ui.go` ManageGDPI hatası hâlâ var — Task 6'da çözülecek.

- [ ] **Adım 4: Commit**

```
git add status.go
git commit -m "feat: status — DPISourceLabel eklendi"
```

---

## Task 6: ui.go — Settings DPI Kaynağı grubu + refreshStatus güncelle

**Files:**
- Modify: `ui.go`

- [ ] **Adım 1: appUI struct'ına RadioButton alanları ekle**

`appUI` struct'ındaki `// Ayarlar sekmesi` bölümünde `chkManageGDPI` ve `gdpiSection` alanlarını kaldır, yerine RadioButton alanları ekle:

```go
// Ayarlar sekmesi
cbDPI         *walk.ComboBox
leCustomFlags *walk.LineEdit
cbChunk       *walk.ComboBox
cbISP         *walk.ComboBox
cbDNS         *walk.ComboBox
chkSysProxy   *walk.CheckBox
chkAutoStart  *walk.CheckBox
// DPI Kaynağı
rbDPIAuto     *walk.RadioButton
rbDPIService  *walk.RadioButton
rbDPIManual   *walk.RadioButton
rbDPIDisabled *walk.RadioButton
gdpiPathComp  *walk.Composite  // sadece "Manuel" seçiliyken görünür
leGDPIPath    *walk.LineEdit
neProxyPort   *walk.NumberEdit
nePACPort     *walk.NumberEdit
lblSaveStatus *walk.Label
```

- [ ] **Adım 2: settingsPage() içindeki GoodbyeDPI GroupBox'ı değiştir**

`settingsPage()` fonksiyonunda şu bloğu bul ve **sil**:

```go
GroupBox{
    Title:  "GoodbyeDPI",
    Layout: VBox{},
    Children: []Widget{
        CheckBox{
            AssignTo:  &u.chkManageGDPI,
            Text:      "SpAC3DPI tarafından yönetilsin",
            OnClicked: u.onManageGDPIChange,
        },
        Composite{
            AssignTo: &u.gdpiSection,
            Visible:  false,
            Layout:   HBox{MarginsZero: true},
            Children: []Widget{
                LineEdit{AssignTo: &u.leGDPIPath, CueBanner: `C:\GoodbyeDPI\goodbyedpi.exe`},
                PushButton{Text: "Bul", OnClicked: u.onAutoDetectGDPI, MaxSize: Size{Width: 60}},
            },
        },
    },
},
```

Yerine şunu ekle:

```go
GroupBox{
    Title:  "DPI Kaynağı",
    Layout: VBox{Spacing: 4},
    Children: []Widget{
        RadioButton{
            AssignTo: &u.rbDPIAuto,
            Text:     "Otomatik (önerilen) — Servis → Proses → Manuel → Bundle",
            Value:    "auto",
        },
        RadioButton{
            AssignTo: &u.rbDPIService,
            Text:     "Sistem Servisi — Sadece Windows servisi kullanılır",
            Value:    "service",
        },
        RadioButton{
            AssignTo: &u.rbDPIManual,
            Text:     "Manuel Yol — Aşağıdaki goodbyedpi.exe başlatılır",
            Value:    "manual",
            OnClicked: u.onDPISourceChange,
        },
        Composite{
            AssignTo: &u.gdpiPathComp,
            Visible:  false,
            Layout:   HBox{MarginsZero: true, Spacing: 4},
            Children: []Widget{
                LineEdit{AssignTo: &u.leGDPIPath, CueBanner: `C:\GoodbyeDPI\goodbyedpi.exe`},
                PushButton{Text: "Bul", OnClicked: u.onAutoDetectGDPI, MaxSize: Size{Width: 60}},
            },
        },
        RadioButton{
            AssignTo: &u.rbDPIDisabled,
            Text:     "Devre Dışı — Sadece Proxy + PAC çalışır",
            Value:    "disabled",
        },
    },
},
```

- [ ] **Adım 3: onDPISourceChange handler ekle, onManageGDPIChange kaldır**

`onManageGDPIChange()` fonksiyonunu **sil**, yerine şunu ekle:

```go
func (u *appUI) onDPISourceChange() {
    isManual := u.rbDPIManual != nil && u.rbDPIManual.Checked()
    if u.gdpiPathComp != nil {
        u.gdpiPathComp.SetVisible(isManual)
    }
}
```

- [ ] **Adım 4: loadSettingsForm() güncelle**

`loadSettingsForm()` içinde şu satırları bul ve **sil**:

```go
u.chkManageGDPI.SetChecked(c.ManageGDPI)
if u.leGDPIPath != nil {
    u.leGDPIPath.SetText(c.GDPIPath)
}
u.onManageGDPIChange()
```

Yerine şunu ekle:

```go
switch c.DPISource {
case "service":
    if u.rbDPIService != nil { u.rbDPIService.SetChecked(true) }
case "manual":
    if u.rbDPIManual != nil { u.rbDPIManual.SetChecked(true) }
case "disabled":
    if u.rbDPIDisabled != nil { u.rbDPIDisabled.SetChecked(true) }
default: // "auto"
    if u.rbDPIAuto != nil { u.rbDPIAuto.SetChecked(true) }
}
if u.leGDPIPath != nil {
    u.leGDPIPath.SetText(c.GDPIPath)
}
u.onDPISourceChange()
```

- [ ] **Adım 5: onSaveSettings() güncelle**

`onSaveSettings()` içinde `ManageGDPI` ve `GDPIPath` bloğunu bul:

```go
gdpiPath := ""
if u.leGDPIPath != nil {
    gdpiPath = strings.TrimSpace(u.leGDPIPath.Text())
}
```

ve `nc := Config{...}` içindeki `ManageGDPI: ..., GDPIPath: gdpiPath` kısımlarını güncelle:

```go
// DPI Kaynağı oku
dpiSource := "auto"
switch {
case u.rbDPIService != nil && u.rbDPIService.Checked():
    dpiSource = "service"
case u.rbDPIManual != nil && u.rbDPIManual.Checked():
    dpiSource = "manual"
case u.rbDPIDisabled != nil && u.rbDPIDisabled.Checked():
    dpiSource = "disabled"
}

gdpiPath := ""
if u.leGDPIPath != nil {
    gdpiPath = strings.TrimSpace(u.leGDPIPath.Text())
}
```

`nc := Config{...}` içindeki `ManageGDPI` satırını sil, `DPISource` ekle:

```go
nc := Config{
    DPIMode:        dpiMode,
    ChunkSize:      chunk,
    ISP:            isp,
    CustomFlags:    customFlags,
    DNSMode:        dnsMode,
    SetSystemProxy: u.chkSysProxy != nil && u.chkSysProxy.Checked(),
    DPISource:      dpiSource,
    GDPIPath:       gdpiPath,
    ProxyPort:      int(u.neProxyPort.Value()),
    PACPort:        int(u.nePACPort.Value()),
}
```

- [ ] **Adım 6: onSaveSettings() sonundaki ManageGDPI bloğunu kaldır**

Şu bloğu **sil**:

```go
if nc.ManageGDPI {
    go func() {
        StopWindowsService()
        if err := gdpi.Restart(nc.GDPIPath, activeGDPIFlags()); err != nil {
            logError("GoodbyeDPI yeniden başlatılamadı: " + err.Error())
        }
    }()
} else {
    gdpi.Stop()
}
```

Yerine şunu ekle (ayar kaydedilince aktif session'ı güncelle):

```go
// DPI kaynağı değişti — çalışıyorsa restart
if g.running {
    go g.restart()
}
```

- [ ] **Adım 7: refreshStatus() GDPI satırını güncelle**

`refreshStatus()` içinde şu bloğu bul ve **sil**:

```go
if s.GDPIManaged {
    if s.GDPIRunning {
        setLbl(u.lblGDPI, "✔ Yönetiliyor")
    } else {
        setLbl(u.lblGDPI, "✘ Durdu")
    }
} else {
    setLbl(u.lblGDPI, "— Harici servis")
}
```

Yerine şunu ekle:

```go
if s.GDPIRunning {
    setLbl(u.lblGDPI, "✔ "+s.DPISourceLabel)
} else if s.DPISourceLabel == "Devre Dışı" {
    setLbl(u.lblGDPI, "— Devre Dışı")
} else {
    setLbl(u.lblGDPI, "— "+s.DPISourceLabel)
}
```

- [ ] **Adım 8: Build başarılı olmalı**

```
go build ./...
```

Beklenen: **Sıfır hata.** Derleme başarılı.

- [ ] **Adım 9: Uygulamayı çalıştır, Settings sekmesini kontrol et**

```
.\SpAC3DPI.exe
```

Kontrol listesi:
- [ ] Settings sekmesinde "DPI Kaynağı" grubu görünüyor
- [ ] "Otomatik" seçili (varsayılan)
- [ ] "Manuel Yol" seçilince dosya path alanı açılıyor
- [ ] Kaydet & Uygula çalışıyor
- [ ] Durum sekmesinde GoodbyeDPI satırı doğru kaynağı gösteriyor

- [ ] **Adım 10: Commit**

```
git add ui.go
git commit -m "feat: ui — DPI Kaynağı RadioButton grubu + status güncellendi"
```

---

## Özet

| Task | Commit | Durum |
|---|---|---|
| 1: Config DPISource | `feat: config — DPISource ekle, ManageGDPI kaldır` | - |
| 2: Bundle altyapısı | `feat: bundle altyapısı — embed stub + withbundle tag` | - |
| 3: dpi_priority.go | `feat: dpi_priority — 4 katmanlı öncelik tespiti + testler` | - |
| 4: main.go | `feat: main — DPI öncelik sistemi start/stop bağlandı` | - |
| 5: status.go | `feat: status — DPISourceLabel eklendi` | - |
| 6: ui.go | `feat: ui — DPI Kaynağı RadioButton grubu + status güncellendi` | - |

## Bundle Notu

Bundle (Task 2) derleme zamanında `//go:embed` kullandığı için binary dosyaların `assets/gdpi/` klasöründe olması gerekir. Bu dosyalar repo'ya commit edilmemeli (`.gitignore`'a ekle). Normal derleme (`go build`) bundle olmadan çalışır — sadece 4. öncelik (bundle) devre dışı olur.
