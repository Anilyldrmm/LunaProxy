package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// probeRouterPACPath — router'da gerçekte çalışan PAC path'ini bulur.
// /pac (yeni kurulum) veya /proxy.pac (eski/Keenetic kurulum) sırasıyla dener.
func probeRouterPACPath(gateway string) string {
	c := &http.Client{Timeout: 3 * time.Second}
	for _, path := range []string{"/pac", "/proxy.pac"} {
		resp, err := c.Get(fmt.Sprintf("http://%s:8090%s", gateway, path))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return path
			}
		}
	}
	return "/pac"
}

// guessGatewayIP — local IP'den varsayılan gateway'i tahmin eder.
// 192.168.1.41 → 192.168.1.1
func guessGatewayIP(localIP string) string {
	parts := strings.Split(localIP, ".")
	if len(parts) == 4 {
		return parts[0] + "." + parts[1] + "." + parts[2] + ".1"
	}
	return "192.168.1.1"
}

func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

// ── Firewall kuralları ────────────────────────────────────────────────────────

func addFirewallRules(proxyPort, pacPort int) {
	for _, r := range []struct{ name, port string }{
		{"LunaProxy_Proxy", fmt.Sprintf("%d", proxyPort)},
		{"LunaProxy_PAC", fmt.Sprintf("%d", pacPort)},
	} {
		hiddenRun("netsh", "advfirewall", "firewall", "delete", "rule",
			"name="+r.name)
		hiddenRun("netsh", "advfirewall", "firewall", "add", "rule",
			"name="+r.name,
			"dir=in", "action=allow", "protocol=TCP",
			"localport="+r.port,
			"profile=any",
			"enable=yes",
		)
	}
}

// hiddenRun — pencere açmadan harici komut çalıştırır (CMD flash yok).
func hiddenRun(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	cmd.Run()
}

// fetchPublicIP — harici servislerden genel IP adresini alır.
func fetchPublicIP() string {
	services := []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://checkip.amazonaws.com",
	}
	c := &http.Client{Timeout: 5 * time.Second}
	for _, svc := range services {
		resp, err := c.Get(svc)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			continue
		}
		ip := strings.TrimSpace(string(body))
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	return ""
}

// openBrowser — PAC QR fallback.
func openBrowser(url string) {
	hiddenRun("rundll32", "url.dll,FileProtocolHandler", url)
}

// writeTempPNG — QR PNG için yardımcı
func writeTempPNG(data []byte) (path string, cleanup func()) {
	f, err := os.CreateTemp("", "lunaproxy_*.png")
	if err != nil {
		return "", func() {}
	}
	f.Write(data)
	f.Close()
	return f.Name(), func() { os.Remove(f.Name()) }
}

// ── DuckDNS entegrasyonu ──────────────────────────────────────────────────────

var (
	ddnsMu        sync.Mutex
	ddnsLastIP    string
	ddnsLastAt    time.Time
	ddnsLastError string
)

// DDNSStatus — DDNS durumunu dışarıya açar.
type DDNSStatus struct {
	Enabled  bool   `json:"enabled"`
	Hostname string `json:"hostname"`
	LastIP   string `json:"lastIP"`
	LastAt   string `json:"lastAt"`
	Error    string `json:"error"`
}

func getDDNSStatus() DDNSStatus {
	c := getConfig()
	ddnsMu.Lock()
	defer ddnsMu.Unlock()
	hostname := ""
	if c.DDNSSubdomain != "" {
		hostname = c.DDNSSubdomain + ".duckdns.org"
	}
	lastAt := ""
	if !ddnsLastAt.IsZero() {
		lastAt = ddnsLastAt.Format("15:04:05")
	}
	return DDNSStatus{
		Enabled:  c.DDNSEnabled,
		Hostname: hostname,
		LastIP:   ddnsLastIP,
		LastAt:   lastAt,
		Error:    ddnsLastError,
	}
}

// updateDDNS — DuckDNS API'ye IP güncellemesi gönderir.
func updateDDNS(subdomain, token, ip string) error {
	url := fmt.Sprintf("https://www.duckdns.org/update?domains=%s&token=%s&ip=%s&verbose=false",
		subdomain, token, ip)
	c := &http.Client{Timeout: 10 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	result := strings.TrimSpace(string(body))
	if result != "OK" {
		return fmt.Errorf("duckdns yanıtı: %s", result)
	}
	return nil
}

// ddnsTick — IP değiştiyse DuckDNS'i günceller. Değişmadıysa atlar.
func ddnsTick() {
	c := getConfig()
	if !c.DDNSEnabled || c.DDNSSubdomain == "" || c.DDNSToken == "" {
		return
	}
	ip := fetchPublicIP()
	if ip == "" {
		ddnsMu.Lock()
		ddnsLastError = "genel IP alınamadı"
		ddnsMu.Unlock()
		return
	}
	ddnsMu.Lock()
	lastIP := ddnsLastIP
	ddnsMu.Unlock()
	if ip == lastIP {
		return
	}
	if err := updateDDNS(c.DDNSSubdomain, c.DDNSToken, ip); err != nil {
		ddnsMu.Lock()
		ddnsLastError = err.Error()
		ddnsMu.Unlock()
		logWarn("DDNS güncelleme hatası: " + err.Error())
		return
	}
	ddnsMu.Lock()
	ddnsLastIP = ip
	ddnsLastAt = time.Now()
	ddnsLastError = ""
	ddnsMu.Unlock()
	logInfo(fmt.Sprintf("DDNS güncellendi: %s.duckdns.org → %s", c.DDNSSubdomain, ip))
}

// ddnsTickForce — IP değişmemiş olsa bile DuckDNS'i günceller (test için).
func ddnsTickForce() {
	c := getConfig()
	if c.DDNSSubdomain == "" || c.DDNSToken == "" {
		ddnsMu.Lock()
		ddnsLastError = "subdomain veya token eksik"
		ddnsMu.Unlock()
		return
	}
	ip := fetchPublicIP()
	if ip == "" {
		ddnsMu.Lock()
		ddnsLastError = "genel IP alınamadı"
		ddnsMu.Unlock()
		return
	}
	if err := updateDDNS(c.DDNSSubdomain, c.DDNSToken, ip); err != nil {
		ddnsMu.Lock()
		ddnsLastError = err.Error()
		ddnsMu.Unlock()
		return
	}
	ddnsMu.Lock()
	ddnsLastIP = ip
	ddnsLastAt = time.Now()
	ddnsLastError = ""
	ddnsMu.Unlock()
	logInfo(fmt.Sprintf("DDNS test güncellendi: %s.duckdns.org → %s", c.DDNSSubdomain, ip))
}

// startDDNSUpdater — arka planda 5 dakikada bir IP değişimini kontrol eder.
func startDDNSUpdater() {
	go func() {
		ddnsTick()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			ddnsTick()
		}
	}()
}
