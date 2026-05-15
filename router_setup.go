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
server.pid-file = "/tmp/spac3dpi_lighttpd.pid"
server.errorlog = "/tmp/spac3dpi_lighttpd.log"
server.modules = ("mod_cgi", "mod_accesslog")
accesslog.filename = "/dev/null"
cgi.assign = (".sh" => "/opt/bin/sh", ".pac" => "/opt/bin/sh")
mimetype.assign = (
  ".pac" => "application/x-ns-proxy-autoconfig",
  ".dat" => "application/x-ns-proxy-autoconfig"
)
`

const routerProxyPac = `#!/bin/sh
PATH=/opt/bin:/opt/sbin:/bin:/sbin:/usr/bin:/usr/sbin
printf 'Content-Type: application/x-ns-proxy-autoconfig\r\nCache-Control: no-store,no-cache\r\n\r\n'
NOW=$(date +%s)
HB=/tmp/spac3dpi_hb
PX=/tmp/spac3dpi_proxy
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
date +%s > /tmp/spac3dpi_hb
printf 'ok\n'
`

const routerUpdateSh = `#!/bin/sh
PATH=/opt/bin:/opt/sbin:/bin:/sbin:/usr/bin:/usr/sbin
printf 'Content-Type: text/plain\r\n\r\n'
MODE=$(echo "$QUERY_STRING" | sed 's/.*mode=//;s/&.*//')
IP=$(echo "$QUERY_STRING" | sed 's/.*ip=//;s/&.*//')
PORT=$(echo "$QUERY_STRING" | sed 's/.*port=//;s/&.*//')
if [ "$MODE" = "proxy" ] && [ -n "$IP" ] && [ -n "$PORT" ]; then
  printf '%s:%s' "$IP" "$PORT" > /tmp/spac3dpi_proxy
  date +%s > /tmp/spac3dpi_hb
else
  rm -f /tmp/spac3dpi_proxy
fi
printf 'ok\n'
`

const routerInitScript = `#!/bin/sh /etc/rc.common
START=80
STOP=20
USE_PROCD=0
start() {
  if [ -f /tmp/spac3dpi_lighttpd.pid ]; then
    kill "$(cat /tmp/spac3dpi_lighttpd.pid)" 2>/dev/null
    rm -f /tmp/spac3dpi_lighttpd.pid
  fi
  /opt/bin/lighttpd -f /opt/share/pac/lighttpd.conf
}
stop() {
  if [ -f /tmp/spac3dpi_lighttpd.pid ]; then
    kill "$(cat /tmp/spac3dpi_lighttpd.pid)" 2>/dev/null
    rm -f /tmp/spac3dpi_lighttpd.pid
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

// sshWriteFile — dosyayı stdin→cat ile router'a yazar.
// sudo != "" ise sudo tee kullanır (non-root kullanıcılar için).
func sshWriteFile(client *ssh.Client, path, content, sudo string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	session.Stdin = strings.NewReader(content)
	if sudo == "" {
		return session.Run(fmt.Sprintf("cat > '%s'", path))
	}
	// sudo tee ile yaz; stdout discard et
	return session.Run(fmt.Sprintf("%stee '%s' > /dev/null", sudo, path))
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

// getSudoPrefix — SSH kullanıcısı root değilse "sudo " döner.
// Yönetici kullanıcı root yetkisiyle SSH açmışsa boş döner.
func getSudoPrefix(client *ssh.Client) string {
	out, _ := sshRun(client, "id -u")
	if strings.TrimSpace(out) == "0" {
		return ""
	}
	return "sudo "
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
	useLighttpd := false
	httpdBin := ""    // system httpd veya "busybox httpd"
	pythonBin := ""   // "python3" veya "python"
	pythonMod := ""   // "http.server --cgi" veya "CGIHTTPServer"

	if hasLighttpd(client) {
		useLighttpd = true
		progress(RouterStep{"lighttpd mevcut", "ok"})
	} else if hasEntware(client) {
		progress(RouterStep{"lighttpd kuruluyor (opkg)...", "info"})
		if out, err := sshRun(client, "opkg update && opkg install lighttpd"); err != nil {
			return fmt.Errorf("lighttpd kurulamadı: %s", out)
		}
		useLighttpd = true
		progress(RouterStep{"lighttpd kuruldu", "ok"})
	} else if bin, ok := findHTTPD(client); ok {
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

	// Dizin yapısı: lighttpd→/opt/share/pac, diğerleri→/tmp/pac
	pacDir := "/opt/share/pac"
	if !hasEntware(client) {
		pacDir = "/tmp/pac"
	}
	cgiDir := pacDir + "/cgi-bin"

	progress(RouterStep{"Dosyalar oluşturuluyor...", "info"})
	if _, err := sshRun(client, sudo+"mkdir -p '"+cgiDir+"'"); err != nil {
		return fmt.Errorf("dizin oluşturulamadı: %w", err)
	}

	// CGI scriptleri cgi-bin/ altına yaz (lighttpd ext-match + BusyBox cgi-bin kuralı)
	files := []struct {
		path    string
		content string
		exec    bool
	}{
		{cgiDir + "/proxy.pac", routerProxyPac, true},
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
		}{"/opt/etc/init.d/S80spac3dpi", routerInitScript, true})
	}
	for _, f := range files {
		if err := sshWriteFile(client, f.path, f.content, sudo); err != nil {
			return fmt.Errorf("%s yazılamadı: %w", f.path, err)
		}
		if f.exec {
			if _, err := sshRun(client, sudo+fmt.Sprintf("chmod +x '%s'", f.path)); err != nil {
				return fmt.Errorf("%s için chmod başarısız: %w", f.path, err)
			}
		}
	}
	progress(RouterStep{"Scriptler yazıldı", "ok"})

	// Eski sunucu süreçlerini durdur — sadece 8090 portundakileri
	sshRun(client, sudo+"fuser -k 8090/tcp 2>/dev/null; true")
	time.Sleep(300 * time.Millisecond)

	if useLighttpd {
		lighttpdBin := "/opt/bin/lighttpd"
		if out, _ := sshRun(client, "which lighttpd 2>/dev/null"); out != "" {
			lighttpdBin = out
		}
		sshRun(client, sudo+`sh -c 'if [ -f /tmp/spac3dpi_lighttpd.pid ]; then kill $(cat /tmp/spac3dpi_lighttpd.pid) 2>/dev/null; sleep 1; fi'`)
		if out, err := sshRun(client, sudo+lighttpdBin+" -f '"+pacDir+"/lighttpd.conf'"); err != nil {
			return fmt.Errorf("lighttpd başlatılamadı: %s", out)
		}
		progress(RouterStep{"lighttpd başlatıldı (port 8090)", "ok"})
	} else if httpdBin != "" {
		// system httpd veya busybox httpd — arka planda başlat
		start := fmt.Sprintf("sh -c 'nohup %s -p 8090 -h %s >/dev/null 2>&1 &'", httpdBin, pacDir)
		if _, err := sshRun(client, sudo+start); err != nil {
			// nohup yoksa basit & ile dene
			if out, err2 := sshRun(client, sudo+httpdBin+" -p 8090 -h '"+pacDir+"' &"); err2 != nil {
				return fmt.Errorf("httpd başlatılamadı: %s", out)
			}
		}
		progress(RouterStep{httpdBin + " başlatıldı (port 8090)", "ok"})
	} else {
		// Python CGI HTTP server
		start := fmt.Sprintf("sh -c 'cd %s && nohup %s -m %s 8090 >/dev/null 2>&1 &'",
			pacDir, pythonBin, pythonMod)
		if _, err := sshRun(client, sudo+start); err != nil {
			return fmt.Errorf("Python HTTP sunucu başlatılamadı")
		}
		progress(RouterStep{"Python HTTP sunucu başlatıldı (port 8090)", "ok"})
	}

	time.Sleep(800 * time.Millisecond)
	testURL := fmt.Sprintf("http://%s:8090/cgi-bin/proxy.pac", cfg.Host)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(testURL)
	if err != nil {
		return fmt.Errorf("kurulum doğrulanamadı: %s erişilemiyor", testURL)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("proxy.pac HTTP %d döndü", resp.StatusCode)
	}
	progress(RouterStep{fmt.Sprintf("Doğrulandı — %s erişilebilir", testURL), "ok"})

	return nil
}

// RouterTest — önceden kurulu bir router'ın PAC endpoint'ini test eder.
func RouterTest(host string) error {
	url := fmt.Sprintf("http://%s:8090/cgi-bin/proxy.pac", host)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(url)
	if err != nil {
		return fmt.Errorf("%s erişilemiyor", url)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
