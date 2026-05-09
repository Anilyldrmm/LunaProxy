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
		GDPIRunning: gdpi.IsRunning(),
		GDPIManaged: c.ManageGDPI,
		DNSMode:     c.DNSMode,
		DNSName:     dnsName,
		SetSysProxy: c.SetSystemProxy,
	}
}
