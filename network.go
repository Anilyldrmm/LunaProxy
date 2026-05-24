package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
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
	ip := conn.LocalAddr().(*net.UDPAddr).IP.String()
	// Tailscale CGNAT aralığı (100.64.0.0/10) → gerçek LAN IP'sini bul
	if strings.HasPrefix(ip, "100.") {
		if lan := findLANIP(); lan != "" {
			return lan
		}
	}
	return ip
}

// findLANIP — Tailscale dışı, loopback dışı ilk IPv4 adresi döner.
func findLANIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		if strings.Contains(strings.ToLower(iface.Name), "tailscale") {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || ip.IsLoopback() {
				continue
			}
			// Tailscale CGNAT: 100.64.0.0 – 100.127.255.255
			if ip[0] == 100 && ip[1] >= 64 && ip[1] < 128 {
				continue
			}
			return ip.String()
		}
	}
	return ""
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

