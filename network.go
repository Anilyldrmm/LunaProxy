package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

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
