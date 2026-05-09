package main

import (
	"fmt"
	"sync/atomic"
)

type StatusPayload struct {
	Running     bool
	Uptime      string
	ActiveConns int64
	TotalConns  int64
	TotalBytes  string
	Errors      int64
	Restarts    int64
	LocalIP     string
	ProxyPort   int
	PACPort     int
	PACUrl      string
	DPIMode     string
	DPIModeName string
	ChunkSize   int
	ISP         string
	ISPName     string
	GDPIFlags   string
	GDPIRunning bool
	GDPIManaged bool
	DPISourceLabel string // "Sistem Servisi" | "Mevcut Proses" | "Manuel" | "Bundle (dahili)" | "Devre Dışı" | "—"
	DNSMode     string
	DNSName     string
	SetSysProxy bool
}

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
		Running:     g.running,
		Uptime:      stats.uptimeStr(),
		ActiveConns: atomic.LoadInt64(&stats.activeConns),
		TotalConns:  atomic.LoadInt64(&stats.totalConns),
		TotalBytes:  stats.bytesStr(),
		Errors:      atomic.LoadInt64(&stats.errors),
		Restarts:    watchdog.RestartCount(),
		LocalIP:     ip,
		ProxyPort:   c.ProxyPort,
		PACPort:     c.PACPort,
		PACUrl:      fmt.Sprintf("http://%s:%d/proxy.pac", ip, c.PACPort),
		DPIMode:     c.DPIMode,
		DPIModeName: modeName,
		ChunkSize:   c.ChunkSize,
		ISP:         c.ISP,
		ISPName:     ispName,
		GDPIFlags:   activeGDPIFlags(),
		GDPIRunning: gdpiRunning,
		GDPIManaged: dpiSrc == "manual" || dpiSrc == "bundle",
		DPISourceLabel: dpiSourceLabel,
		DNSMode:     c.DNSMode,
		DNSName:     dnsName,
		SetSysProxy: c.SetSystemProxy,
	}
}
