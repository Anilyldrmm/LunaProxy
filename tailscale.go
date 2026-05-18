//go:build windows

package main

import (
	"encoding/json"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

type TailscaleStatus struct {
	Running  bool            `json:"running"`
	SelfIP   string          `json:"selfIP"`
	Hostname string          `json:"hostname"`
	DNSName  string          `json:"dnsName"`
	Peers    []TailscalePeer `json:"peers"`
}

type TailscalePeer struct {
	Name   string `json:"name"`
	IP     string `json:"ip"`
	OS     string `json:"os"`
	Online bool   `json:"online"`
}

var (
	tsMu       sync.Mutex
	tsCache    TailscaleStatus
	tsCachedAt time.Time
)

// getTailscaleStatus — sonucu 15 saniye önbellekler; her 2s'de bir çağrılabilir.
func getTailscaleStatus() TailscaleStatus {
	tsMu.Lock()
	defer tsMu.Unlock()
	if time.Since(tsCachedAt) < 15*time.Second {
		return tsCache
	}
	tsCache = fetchTailscaleStatus()
	tsCachedAt = time.Now()
	return tsCache
}

// refreshTailscaleStatus — önbelleği geçersiz kılar, anında yeniler.
func refreshTailscaleStatus() TailscaleStatus {
	tsMu.Lock()
	defer tsMu.Unlock()
	tsCache = fetchTailscaleStatus()
	tsCachedAt = time.Now()
	return tsCache
}

func fetchTailscaleStatus() TailscaleStatus {
	cmd := exec.Command("tailscale", "status", "--json")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
	out, err := cmd.Output()
	if err != nil {
		return TailscaleStatus{}
	}

	var raw struct {
		BackendState string `json:"BackendState"`
		Self         struct {
			HostName     string   `json:"HostName"`
			DNSName      string   `json:"DNSName"`
			TailscaleIPs []string `json:"TailscaleIPs"`
		} `json:"Self"`
		Peer map[string]struct {
			HostName     string   `json:"HostName"`
			DNSName      string   `json:"DNSName"`
			TailscaleIPs []string `json:"TailscaleIPs"`
			OS           string   `json:"OS"`
			Online       bool     `json:"Online"`
		} `json:"Peer"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return TailscaleStatus{}
	}

	selfIP := ""
	if len(raw.Self.TailscaleIPs) > 0 {
		selfIP = raw.Self.TailscaleIPs[0]
	}

	dnsName := strings.TrimSuffix(raw.Self.DNSName, ".")
	hostname := raw.Self.HostName
	if hostname == "" && dnsName != "" {
		hostname = strings.SplitN(dnsName, ".", 2)[0]
	}

	var peers []TailscalePeer
	for _, p := range raw.Peer {
		ip := ""
		if len(p.TailscaleIPs) > 0 {
			ip = p.TailscaleIPs[0]
		}
		name := p.HostName
		if name == "" {
			dns := strings.TrimSuffix(p.DNSName, ".")
			if dns != "" {
				name = strings.SplitN(dns, ".", 2)[0]
			}
		}
		peers = append(peers, TailscalePeer{
			Name:   name,
			IP:     ip,
			OS:     p.OS,
			Online: p.Online,
		})
	}

	return TailscaleStatus{
		Running:  raw.BackendState == "Running" && selfIP != "",
		SelfIP:   selfIP,
		Hostname: hostname,
		DNSName:  dnsName,
		Peers:    peers,
	}
}
