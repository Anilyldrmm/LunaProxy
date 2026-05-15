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

func sshWriteFile(client *ssh.Client, path, content string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	session.Stdin = strings.NewReader(content)
	return session.Run(fmt.Sprintf("cat > %s", path))
}

// ── Tespit ──────────────────────────────────────────────────────────────────

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

// ── Ana kurulum fonksiyonu ───────────────────────────────────────────────────

// RouterInstall — SSH üzerinden router'a PAC+heartbeat scriptlerini kurar.
func RouterInstall(cfg RouterSetupCfg, progress func(RouterStep)) error {
	progress(RouterStep{"Router'a bağlanılıyor...", "info"})
	client, err := sshConnect(cfg)
	if err != nil {
		return fmt.Errorf("SSH bağlantısı başarısız: %w", err)
	}
	defer client.Close()
	progress(RouterStep{"SSH bağlantısı kuruldu", "ok"})

	if !isOpenWrt(client) {
		return fmt.Errorf("bu cihaz OpenWrt değil — desteklenmiyor")
	}
	progress(RouterStep{"OpenWrt tespit edildi", "ok"})

	if !hasEntware(client) {
		return fmt.Errorf("Entware bulunamadı — önce Entware kurulmalı")
	}
	progress(RouterStep{"Entware mevcut", "ok"})

	if !hasLighttpd(client) {
		progress(RouterStep{"lighttpd kuruluyor...", "info"})
		if out, err := sshRun(client, "opkg update && opkg install lighttpd"); err != nil {
			return fmt.Errorf("lighttpd kurulum hatası: %s", out)
		}
		progress(RouterStep{"lighttpd kuruldu", "ok"})
	} else {
		progress(RouterStep{"lighttpd mevcut", "ok"})
	}

	progress(RouterStep{"Dosyalar oluşturuluyor...", "info"})
	if _, err := sshRun(client, "mkdir -p /opt/share/pac"); err != nil {
		return fmt.Errorf("dizin oluşturulamadı: %w", err)
	}

	files := []struct {
		path    string
		content string
		exec    bool
	}{
		{"/opt/share/pac/lighttpd.conf", routerLighttpdConf, false},
		{"/opt/share/pac/proxy.pac", routerProxyPac, true},
		{"/opt/share/pac/hb.sh", routerHbSh, true},
		{"/opt/share/pac/update.sh", routerUpdateSh, true},
		{"/opt/etc/init.d/S80spac3dpi", routerInitScript, true},
	}
	for _, f := range files {
		if err := sshWriteFile(client, f.path, f.content); err != nil {
			return fmt.Errorf("%s yazılamadı: %w", f.path, err)
		}
		if f.exec {
			if _, err := sshRun(client, fmt.Sprintf("chmod +x '%s'", f.path)); err != nil {
				return fmt.Errorf("%s için chmod başarısız: %w", f.path, err)
			}
		}
	}
	progress(RouterStep{"Scriptler yazıldı", "ok"})

	sshRun(client, `if [ -f /tmp/spac3dpi_lighttpd.pid ]; then kill $(cat /tmp/spac3dpi_lighttpd.pid) 2>/dev/null; sleep 1; fi`)
	if out, err := sshRun(client, "/opt/bin/lighttpd -f /opt/share/pac/lighttpd.conf"); err != nil {
		return fmt.Errorf("lighttpd başlatılamadı: %s", out)
	}
	progress(RouterStep{"lighttpd başlatıldı (port 8090)", "ok"})

	time.Sleep(500 * time.Millisecond)
	testURL := fmt.Sprintf("http://%s:8090/proxy.pac", cfg.Host)
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
	url := fmt.Sprintf("http://%s:8090/proxy.pac", host)
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
