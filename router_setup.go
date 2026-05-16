//go:build windows

package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// RouterSetupCfg — SSH bağlantı bilgileri.
type RouterSetupCfg struct {
	Host     string
	Port     int
	User     string
	Password string
}

// RouterStep — kurulum adımı; JS'e push edilir.
type RouterStep struct {
	Msg    string `json:"msg"`
	Status string `json:"status"` // "info" | "ok" | "error"
}

// ── Script içerikleri ────────────────────────────────────────────────────────

const routerLighttpdConf = `server.document-root = "/opt/share/pac"
server.port = 8090
server.pid-file = "/tmp/lunaproxy_lighttpd.pid"
server.errorlog = "/tmp/lunaproxy_lighttpd.log"
server.modules = ("mod_cgi", "mod_accesslog")
accesslog.filename = "/dev/null"
cgi.assign = (".sh" => "/opt/bin/sh", ".pac" => "/opt/bin/sh")
$HTTP["url"] == "/pac" {
  cgi.assign = ("" => "/opt/bin/sh")
}
mimetype.assign = (
  ".pac" => "application/x-ns-proxy-autoconfig",
  ".dat" => "application/x-ns-proxy-autoconfig"
)
`

const routerProxyPac = `#!/bin/sh
PATH=/opt/bin:/opt/sbin:/bin:/sbin:/usr/bin:/usr/sbin
printf 'Content-Type: application/x-ns-proxy-autoconfig\r\nCache-Control: no-store,no-cache\r\n\r\n'
NOW=$(date +%s)
HB=/tmp/lunaproxy_hb
PX=/tmp/lunaproxy_proxy
if [ -f "$HB" ] && [ -f "$PX" ]; then
  HB_T=$(cat "$HB" 2>/dev/null)
  DIFF=$((NOW - HB_T))
  if [ "$DIFF" -lt 30 ]; then
    ADDR=$(cat "$PX" 2>/dev/null)
    printf 'function FindProxyForURL(url,host){return "PROXY %s; DIRECT";}\n' "$ADDR"
    exit 0
  fi
fi
printf 'function FindProxyForURL(url,host){return "DIRECT";}\n'
`

const routerHbSh = `#!/bin/sh
PATH=/opt/bin:/opt/sbin:/bin:/sbin:/usr/bin:/usr/sbin
printf 'Content-Type: text/plain\r\n\r\n'
date +%s > /tmp/lunaproxy_hb
printf 'ok\n'
`

const routerUpdateSh = `#!/bin/sh
PATH=/opt/bin:/opt/sbin:/bin:/sbin:/usr/bin:/usr/sbin
printf 'Content-Type: text/plain\r\n\r\n'
MODE=$(echo "$QUERY_STRING" | sed 's/.*mode=//;s/&.*//')
IP=$(echo "$QUERY_STRING" | sed 's/.*ip=//;s/&.*//')
PORT=$(echo "$QUERY_STRING" | sed 's/.*port=//;s/&.*//')
if [ "$MODE" = "proxy" ] && [ -n "$IP" ] && [ -n "$PORT" ]; then
  printf '%s:%s' "$IP" "$PORT" > /tmp/lunaproxy_proxy
  date +%s > /tmp/lunaproxy_hb
else
  rm -f /tmp/lunaproxy_proxy
fi
printf 'ok\n'
`

const routerInitScript = `#!/bin/sh /etc/rc.common
START=80
STOP=20
USE_PROCD=0
start() {
  if [ -f /tmp/lunaproxy_lighttpd.pid ]; then
    kill "$(cat /tmp/lunaproxy_lighttpd.pid)" 2>/dev/null
    rm -f /tmp/lunaproxy_lighttpd.pid
  fi
  /opt/bin/lighttpd -f /opt/share/pac/lighttpd.conf
}
stop() {
  if [ -f /tmp/lunaproxy_lighttpd.pid ]; then
    kill "$(cat /tmp/lunaproxy_lighttpd.pid)" 2>/dev/null
    rm -f /tmp/lunaproxy_lighttpd.pid
  fi
}
`

// ── SSH yardımcıları ─────────────────────────────────────────────────────────

func sshConnect(cfg RouterSetupCfg) (*ssh.Client, error) {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	config := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            []ssh.AuthMethod{ssh.Password(cfg.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
		Timeout:         10 * time.Second,
	}
	return ssh.Dial("tcp", addr, config)
}

func sshRun(client *ssh.Client, cmd string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	out, err := session.CombinedOutput(cmd)
	return strings.TrimSpace(string(out)), err
}

// shQuote — sh single-quote escape: 'foo bar' → içindeki ' → '\''
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sshPath — router SSH exec oturumlarında minimal PATH sorununu önler.
// Bazı firmware'lerde exec komutu login shell PATH'ini taşımaz; explicit set gerekir.
const sshPath = "PATH=/opt/bin:/opt/sbin:/bin:/sbin:/usr/bin:/usr/sbin"

// sshExec — her zaman sh -c + explicit PATH ile çalıştırır.
// sudo gerekiyorsa 'sudo sh -c ...' wrapper'ı kullanır (secure_path aşılır).
func sshExec(client *ssh.Client, cmd, sudo string) (string, error) {
	full := sshPath + "; " + cmd
	if sudo == "" {
		return sshRun(client, "sh -c "+shQuote(full))
	}
	return sshRun(client, "sudo sh -c "+shQuote(full))
}

// sshWriteFile — dosyayı stdin→cat/tee ile router'a yazar.
func sshWriteFile(client *ssh.Client, path, content, sudo string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	session.Stdin = strings.NewReader(content)
	if sudo == "" {
		return session.Run("sh -c " + shQuote(sshPath+"; cat > "+shQuote(path)))
	}
	return session.Run("sudo sh -c " + shQuote(sshPath+"; tee "+shQuote(path)+" > /dev/null"))
}

// ── Keenetic NDM yardımcıları ────────────────────────────────────────────────

// isKeenetic — SSH exec'te "show version" ile Keenetic firmware tespiti yapar.
func isKeenetic(client *ssh.Client) bool {
	out, _ := sshRun(client, "show version")
	clean := strings.ReplaceAll(out, "\x1b[K", "")
	return strings.Contains(clean, "Keenetic")
}

// sshExecNDM — Keenetic NDM CLI için 'exec <cmd>' wrapper.
// Keenetic SSH exec'inde sh -c çalışmaz; NDM'nin kendi exec komutu kullanılır.
func sshExecNDM(client *ssh.Client, cmd string) (string, error) {
	return sshRun(client, "exec "+cmd)
}

// sshWriteNDM — Keenetic'te dosya yazmak için 'exec tee <path>' + stdin kullanır.
// Shell redirection yok; NDM exec tee komutunu destekler.
func sshWriteNDM(client *ssh.Client, path, content string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	session.Stdin = strings.NewReader(content)
	return session.Run("exec tee " + path)
}

// ── Tespit fonksiyonları ─────────────────────────────────────────────────────

func isOpenWrt(client *ssh.Client) bool {
	out, err := sshRun(client, "head -1 /etc/openwrt_release 2>/dev/null")
	return err == nil && strings.Contains(out, "OpenWrt")
}

func hasEntware(client *ssh.Client) bool {
	_, err := sshRun(client, "ls /opt/bin/opkg 2>/dev/null")
	return err == nil
}

func hasLighttpd(client *ssh.Client) bool {
	_, err := sshRun(client, "ls /opt/bin/lighttpd 2>/dev/null")
	return err == nil
}

// findHTTPD — sistem genelinde httpd binary'sini arar.
// command -v (POSIX builtin) önce, ardından which ve bilinen yollar denenir.
func findHTTPD(client *ssh.Client) (string, bool) {
	out, _ := sshRun(client,
		"command -v httpd 2>/dev/null || "+
			"which httpd 2>/dev/null || "+
			"ls /usr/sbin/httpd /usr/bin/httpd /bin/httpd /sbin/httpd 2>/dev/null | head -1")
	out = strings.TrimSpace(out)
	if out != "" && strings.Contains(out, "httpd") {
		return out, true
	}
	return "", false
}

// hasBusyboxHTTPD — busybox binary'si httpd applet içeriyor mu?
func hasBusyboxHTTPD(client *ssh.Client) bool {
	out, _ := sshRun(client,
		"command -v busybox 2>/dev/null || which busybox 2>/dev/null || "+
			"ls /bin/busybox /usr/bin/busybox 2>/dev/null | head -1")
	if !strings.Contains(out, "busybox") {
		return false
	}
	// --list ile applet listesi, yoksa doğrudan test et
	applets, _ := sshRun(client, "busybox --list 2>/dev/null")
	if strings.Contains(applets, "httpd") {
		return true
	}
	help, _ := sshRun(client, "busybox httpd --help 2>&1")
	return strings.Contains(help, "-p") || strings.Contains(help, "port")
}

// findPython — python3 veya python2 binary'sini arar.
// (bin, cgiModule, ok) döner; cgiModule http.server --cgi veya CGIHTTPServer.
func findPython(client *ssh.Client) (bin, cgiModule string, ok bool) {
	out, _ := sshRun(client, "command -v python3 2>/dev/null || which python3 2>/dev/null")
	if strings.TrimSpace(out) != "" {
		return "python3", "http.server --cgi", true
	}
	out, _ = sshRun(client, "command -v python 2>/dev/null || which python 2>/dev/null")
	if strings.TrimSpace(out) != "" {
		return "python", "CGIHTTPServer", true
	}
	return "", "", false
}

// getSudoPrefix — SSH kullanıcısı root değilse ve sudo mevcutsa "sudo " döner.
// sudo yoksa (çoğu consumer router) boş döner — admin kullanıcı /tmp yazabilir.
func getSudoPrefix(client *ssh.Client) string {
	out, _ := sshRun(client, "id -u")
	if strings.TrimSpace(out) == "0" {
		return "" // zaten root
	}
	// sudo mevcut mu?
	sudoPath, _ := sshRun(client, "command -v sudo 2>/dev/null || which sudo 2>/dev/null")
	if strings.TrimSpace(sudoPath) == "" {
		return "" // sudo yok; admin kullanıcı /tmp'ye doğrudan yazabilir
	}
	return "sudo "
}

// ── Keenetic kurulum fonksiyonu ─────────────────────────────────────────────

// routerInstallKeenetic — Keenetic NDM CLI üzerinden kurulum yapar.
// 'exec <cmd>' ve 'exec tee <path>' ile shell syntax kullanmadan dosya yazar.
// Entware + lighttpd zaten kurulu varsayılır; scriptleri günceller ve lighttpd başlatır.
func routerInstallKeenetic(client *ssh.Client, cfg RouterSetupCfg, progress func(RouterStep)) error {
	progress(RouterStep{"Keenetic firmware tespit edildi", "ok"})

	pacDir := "/opt/share/pac"

	// Entware kurulu mu?
	out, _ := sshExecNDM(client, "ls /opt/bin/sh 2>/dev/null")
	if !strings.Contains(out, "sh") {
		return fmt.Errorf("Entware kurulu değil — /opt/bin/sh bulunamadı")
	}
	progress(RouterStep{"Entware mevcut", "ok"})

	// Dizin oluştur
	if _, err := sshExecNDM(client, "mkdir -p "+pacDir); err != nil {
		return fmt.Errorf("dizin oluşturulamadı: %w", err)
	}

	// Scriptleri yaz
	progress(RouterStep{"Scriptler yazılıyor...", "info"})
	scripts := []struct {
		path    string
		content string
	}{
		{pacDir + "/pac", routerProxyPac},       // /pac URL için (uzantısız)
		{pacDir + "/proxy.pac", routerProxyPac}, // /proxy.pac URL için (geriye dönük)
		{pacDir + "/update.sh", routerUpdateSh},
		{pacDir + "/hb.sh", routerHbSh},
		{pacDir + "/lighttpd.conf", routerLighttpdConf},
	}
	for _, s := range scripts {
		if err := sshWriteNDM(client, s.path, s.content); err != nil {
			return fmt.Errorf("%s yazılamadı: %w", s.path, err)
		}
		sshExecNDM(client, "chmod +x "+s.path) //nolint:errcheck
	}
	progress(RouterStep{"Scriptler yazıldı", "ok"})

	// lighttpd'yi başlat ya da yeniden başlat
	sshExecNDM(client, "kill $(cat /tmp/lunaproxy_lighttpd.pid 2>/dev/null) 2>/dev/null") //nolint:errcheck
	time.Sleep(300 * time.Millisecond)

	lighttpdBin := "/opt/bin/lighttpd"
	if _, err := sshExecNDM(client, lighttpdBin+" -f "+pacDir+"/lighttpd.conf"); err != nil {
		// init.d üzerinden dene
		sshExecNDM(client, "/opt/etc/init.d/S80lighttpd start") //nolint:errcheck
	}
	progress(RouterStep{"lighttpd başlatıldı (port 8090)", "ok"})

	time.Sleep(800 * time.Millisecond)
	testURL := fmt.Sprintf("http://%s:8090/pac", cfg.Host)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(testURL)
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		// /proxy.pac'ı da dene
		testURL = fmt.Sprintf("http://%s:8090/proxy.pac", cfg.Host)
		resp, err = (&http.Client{Timeout: 5 * time.Second}).Get(testURL)
	}
	if err != nil {
		return fmt.Errorf("kurulum doğrulanamadı: %s erişilemiyor", testURL)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("PAC HTTP %d döndü", resp.StatusCode)
	}
	progress(RouterStep{fmt.Sprintf("Doğrulandı — %s erişilebilir", testURL), "ok"})
	return nil
}

// ── Ana kurulum fonksiyonu ───────────────────────────────────────────────────

// RouterInstall — SSH üzerinden router'a PAC+heartbeat scriptlerini kurar.
// Sunucu önceliği: lighttpd (Entware) → sistem httpd → BusyBox httpd → hata.
// Tüm CGI scriptler cgi-bin/ altına yazılır; URL: /cgi-bin/proxy.pac vb.
func RouterInstall(cfg RouterSetupCfg, progress func(RouterStep)) error {
	progress(RouterStep{"Router'a bağlanılıyor...", "info"})
	client, err := sshConnect(cfg)
	if err != nil {
		return fmt.Errorf("SSH bağlantısı başarısız: %w", err)
	}
	defer client.Close()
	progress(RouterStep{"SSH bağlantısı kuruldu", "ok"})

	// Keenetic firmware tespiti — NDM CLI farklı kurulum yolu gerektirir
	if isKeenetic(client) {
		return routerInstallKeenetic(client, cfg, progress)
	}

	// Yetki kontrolü — root değilse sudo prefix kullan
	sudo := getSudoPrefix(client)
	if sudo != "" {
		progress(RouterStep{"sudo ile admin yetkileriyle devam ediliyor", "info"})
	}

	if isOpenWrt(client) {
		progress(RouterStep{"OpenWrt tespit edildi", "ok"})
	} else {
		progress(RouterStep{"Linux tabanlı firmware — devam ediliyor", "info"})
	}

	// HTTP sunucu seçimi — öncelik: lighttpd > httpd > busybox httpd > python
	entware := hasEntware(client)
	useLighttpd := false
	httpdBin := ""  // system httpd veya "busybox httpd"
	pythonBin := "" // "python3" veya "python"
	pythonMod := "" // "http.server --cgi" veya "CGIHTTPServer"

	if hasLighttpd(client) {
		useLighttpd = true
		progress(RouterStep{"lighttpd mevcut", "ok"})
	} else if entware {
		progress(RouterStep{"lighttpd kuruluyor (opkg)...", "info"})
		if out, err := sshRun(client, "opkg update && opkg install lighttpd"); err != nil {
			// ISP opkg repolarını engelliyor olabilir — alternatif HTTP sunucu ara
			progress(RouterStep{"opkg erişilemiyor, alternatif HTTP sunucu aranıyor...", "info"})
			logWarn("opkg hata: " + strings.TrimSpace(out))
		} else {
			useLighttpd = true
			progress(RouterStep{"lighttpd kuruldu", "ok"})
		}
	}

	if !useLighttpd {
		if bin, ok := findHTTPD(client); ok {
			httpdBin = bin
			progress(RouterStep{"httpd tespit edildi: " + bin, "info"})
		} else if hasBusyboxHTTPD(client) {
			httpdBin = "busybox httpd"
			progress(RouterStep{"BusyBox httpd kullanılacak", "info"})
		} else if pb, pm, ok := findPython(client); ok {
			pythonBin = pb
			pythonMod = pm
			progress(RouterStep{"Python CGI sunucu kullanılacak (" + pb + ")", "info"})
		} else {
			return fmt.Errorf("HTTP sunucu bulunamadı — lighttpd, httpd, BusyBox veya Python gerekli")
		}
	}

	// Dizin yapısı: lighttpd→/opt/share/pac, diğerleri→/tmp/pac
	pacDir := "/opt/share/pac"
	if !entware {
		pacDir = "/tmp/pac"
	}
	cgiDir := pacDir + "/cgi-bin"

	progress(RouterStep{"Dosyalar oluşturuluyor...", "info"})
	if _, err := sshExec(client, "mkdir -p "+shQuote(cgiDir), sudo); err != nil {
		return fmt.Errorf("dizin oluşturulamadı: %w", err)
	}

	// CGI scriptleri cgi-bin/ altına yaz (lighttpd ext-match + BusyBox cgi-bin kuralı)
	// /pac dosyası doc-root'a yazılır; lighttpd koşullu cgi.assign ile /pac URL'sini çalıştırır.
	files := []struct {
		path    string
		content string
		exec    bool
	}{
		{pacDir + "/pac", routerProxyPac, true},
		{cgiDir + "/proxy.pac", routerProxyPac, true}, // eski kurulumlar için
		{cgiDir + "/hb.sh", routerHbSh, true},
		{cgiDir + "/update.sh", routerUpdateSh, true},
	}
	if useLighttpd {
		files = append(files, struct {
			path    string
			content string
			exec    bool
		}{pacDir + "/lighttpd.conf", routerLighttpdConf, false})
	}
	if hasEntware(client) {
		files = append(files, struct {
			path    string
			content string
			exec    bool
		}{"/opt/etc/init.d/S80lunaproxy", routerInitScript, true})
	}
	for _, f := range files {
		if err := sshWriteFile(client, f.path, f.content, sudo); err != nil {
			return fmt.Errorf("%s yazılamadı: %w", f.path, err)
		}
		if f.exec {
			if _, err := sshExec(client, "chmod +x "+shQuote(f.path), sudo); err != nil {
				return fmt.Errorf("%s için chmod başarısız: %w", f.path, err)
			}
		}
	}
	progress(RouterStep{"Scriptler yazıldı", "ok"})

	// Eski sunucu süreçlerini durdur — sadece 8090 portundakileri
	sshExec(client, "fuser -k 8090/tcp 2>/dev/null; true", sudo) //nolint:errcheck
	time.Sleep(300 * time.Millisecond)

	if useLighttpd {
		lighttpdBin := "/opt/bin/lighttpd"
		if out, _ := sshRun(client, "which lighttpd 2>/dev/null"); out != "" {
			lighttpdBin = out
		}
		sshExec(client, `if [ -f /tmp/lunaproxy_lighttpd.pid ]; then kill $(cat /tmp/lunaproxy_lighttpd.pid) 2>/dev/null; sleep 1; fi`, sudo) //nolint:errcheck
		if out, err := sshExec(client, lighttpdBin+" -f "+shQuote(pacDir+"/lighttpd.conf"), sudo); err != nil {
			return fmt.Errorf("lighttpd başlatılamadı: %s", out)
		}
		progress(RouterStep{"lighttpd başlatıldı (port 8090)", "ok"})
	} else if httpdBin != "" {
		// system httpd veya busybox httpd — nohup ile arka planda başlat
		startCmd := fmt.Sprintf("nohup %s -p 8090 -h %s >/dev/null 2>&1 &", httpdBin, shQuote(pacDir))
		if _, err := sshExec(client, startCmd, sudo); err != nil {
			// nohup yoksa basit & ile dene
			if out, err2 := sshExec(client, fmt.Sprintf("%s -p 8090 -h %s &", httpdBin, shQuote(pacDir)), sudo); err2 != nil {
				return fmt.Errorf("httpd başlatılamadı: %s", out)
			}
		}
		progress(RouterStep{httpdBin + " başlatıldı (port 8090)", "ok"})
	} else {
		// Python CGI HTTP server
		startCmd := fmt.Sprintf("cd %s && nohup %s -m %s 8090 >/dev/null 2>&1 &",
			shQuote(pacDir), pythonBin, pythonMod)
		if _, err := sshExec(client, startCmd, sudo); err != nil {
			return fmt.Errorf("Python HTTP sunucu başlatılamadı")
		}
		progress(RouterStep{"Python HTTP sunucu başlatıldı (port 8090)", "ok"})
	}

	time.Sleep(800 * time.Millisecond)
	testURL := fmt.Sprintf("http://%s:8090/pac", cfg.Host)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(testURL)
	if err != nil {
		return fmt.Errorf("kurulum doğrulanamadı: %s erişilemiyor", testURL)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("/pac HTTP %d döndü", resp.StatusCode)
	}
	progress(RouterStep{fmt.Sprintf("Doğrulandı — %s erişilebilir", testURL), "ok"})

	return nil
}

// RouterTest — önceden kurulu bir router'ın PAC endpoint'ini test eder.
// /pac ve /proxy.pac sırasıyla denenir (Keenetic uyumluluğu için).
func RouterTest(host string) error {
	c := &http.Client{Timeout: 5 * time.Second}
	for _, path := range []string{"/pac", "/proxy.pac"} {
		resp, err := c.Get(fmt.Sprintf("http://%s:8090%s", host, path))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
	}
	return fmt.Errorf("http://%s:8090 PAC endpoint erişilemiyor", host)
}
