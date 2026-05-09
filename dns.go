package main

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

// DNS sağlayıcı tanımları
var dnsProviders = map[string][2]string{
	"cloudflare": {"1.1.1.1", "1.0.0.1"},
	"google":     {"8.8.8.8", "8.8.4.4"},
	"adguard":    {"94.140.14.14", "94.140.15.15"},
	"quad9":      {"9.9.9.9", "149.112.112.112"},
	"opendns":    {"208.67.222.222", "208.67.220.220"},
}

var dnsNames = map[string]string{
	"unchanged":  "Değiştirilmesin",
	"cloudflare": "Cloudflare (1.1.1.1 / 1.0.0.1)",
	"google":     "Google (8.8.8.8 / 8.8.4.4)",
	"adguard":    "AdGuard (94.140.14.14 / 94.140.15.15)",
	"quad9":      "Quad9 (9.9.9.9 / 149.112.112.112)",
	"opendns":    "OpenDNS (208.67.222.222 / 208.67.220.220)",
}

var (
	origDNSServers []string
	origDNSAdapter string
	dnsMu          sync.Mutex
)

// getActiveAdapterName — internet çıkışı için kullanılan ağ adaptörünü bulur.
func getActiveAdapterName() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	localIP := conn.LocalAddr().(*net.UDPAddr).IP.String()

	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err == nil && ip.String() == localIP {
				return iface.Name
			}
		}
	}
	return ""
}

// ApplyDNS — seçili DNS sağlayıcısını etkin adaptörde uygular.
// "unchanged" ise hiçbir şey yapmaz.
func ApplyDNS(mode string) error {
	if mode == "unchanged" || mode == "" {
		return nil
	}

	providers, ok := dnsProviders[mode]
	if !ok {
		return fmt.Errorf("bilinmeyen DNS modu: %s", mode)
	}

	adapter := getActiveAdapterName()
	if adapter == "" {
		return fmt.Errorf("aktif ağ adaptörü bulunamadı")
	}

	// Mevcut DNS'i yedekle
	BackupDNS(adapter)

	// Yeni DNS'i uygula
	ps := fmt.Sprintf(
		"Set-DnsClientServerAddress -InterfaceAlias '%s' -ServerAddresses '%s','%s'",
		adapter, providers[0], providers[1],
	)
	if err := runPS(ps); err != nil {
		return fmt.Errorf("DNS ayarlanamadı: %w", err)
	}

	// Windows 11+ için DoH (başarısız olsa da sorun değil)
	applyDoH(adapter, mode, providers[0])

	logInfo(fmt.Sprintf("DNS uygulandı: %s → %s / %s", adapter, providers[0], providers[1]))
	return nil
}

// RestoreDNS — yedeklenmiş DNS ayarlarını geri yükler.
func RestoreDNS() {
	dnsMu.Lock()
	adapter := origDNSAdapter
	servers := append([]string(nil), origDNSServers...)
	dnsMu.Unlock()

	if adapter == "" {
		return
	}

	var ps string
	if len(servers) > 0 && servers[0] != "" {
		quoted := make([]string, len(servers))
		for i, s := range servers {
			quoted[i] = "'" + strings.TrimSpace(s) + "'"
		}
		ps = fmt.Sprintf("Set-DnsClientServerAddress -InterfaceAlias '%s' -ServerAddresses %s",
			adapter, strings.Join(quoted, ","))
	} else {
		ps = fmt.Sprintf("Set-DnsClientServerAddress -InterfaceAlias '%s' -ResetServerAddresses", adapter)
	}
	runPS(ps)
	logInfo("DNS eski değerine döndürüldü: " + adapter)
}

// BackupDNS — mevcut DNS sunucularını kaydeder (daha önce kaydedilmemişse).
func BackupDNS(adapter string) {
	dnsMu.Lock()
	defer dnsMu.Unlock()
	if origDNSAdapter != "" {
		return // zaten yedeklenmiş
	}

	psCmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command",
		fmt.Sprintf("(Get-DnsClientServerAddress -InterfaceAlias '%s' -AddressFamily IPv4).ServerAddresses -join ','", adapter))
	psCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := psCmd.Output()
	if err == nil {
		s := strings.TrimSpace(string(out))
		if s != "" {
			origDNSServers = strings.Split(s, ",")
		}
	}
	origDNSAdapter = adapter
}

// applyDoH — Windows 11 (21H2+) üzerinde DoH şablonunu etkinleştirir.
// Desteklenmiyorsa sessizce devam eder.
func applyDoH(adapter, mode, primary string) {
	templates := map[string]string{
		"cloudflare": "https://cloudflare-dns.com/dns-query",
		"google":     "https://dns.google/dns-query",
		"adguard":    "https://dns.adguard-dns.com/dns-query",
		"quad9":      "https://dns.quad9.net/dns-query",
	}
	tpl, ok := templates[mode]
	if !ok {
		return
	}
	ps := fmt.Sprintf(
		"if (Get-Command Add-DnsClientDohServerAddress -EA SilentlyContinue) {"+
			" Add-DnsClientDohServerAddress -ServerAddress '%s' -DohTemplate '%s' -AllowFallbackToUdp $False -AutoUpgrade $True -EA SilentlyContinue }",
		primary, tpl,
	)
	runPS(ps)
}

func runPS(cmd string) error {
	c := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", cmd)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return c.Run()
}
