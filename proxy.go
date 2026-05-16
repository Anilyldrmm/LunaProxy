package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

)

// ── Per-IP cihaz takibi ──────────────────────────────────────────────────────

type deviceEntry struct {
	Bytes       int64
	ActiveConns int64
	hostname    atomic.Value // string — reverse DNS, async set edilir, bir kez yazılır
}

var devices sync.Map // key: string IP, value: *deviceEntry

// DeviceInfo — UI'a gönderilen cihaz bilgisi.
type DeviceInfo struct {
	IP          string `json:"ip"`
	Bytes       int64  `json:"bytes"`
	ActiveConns int64  `json:"activeConns"`
	Hostname    string `json:"hostname"`
}

func trackDevice(ip string, bytes int64) {
	v, _ := devices.LoadOrStore(ip, &deviceEntry{})
	e := v.(*deviceEntry)
	atomic.AddInt64(&e.Bytes, bytes)
}

func resolveHostname(ip string) {
	name := ""
	// DNS PTR (Windows cihazları, bazı Android)
	if ns, err := net.LookupAddr(ip); err == nil && len(ns) > 0 {
		name = strings.TrimSuffix(ns[0], ".")
	}
	// mDNS/Bonjour unicast (iPhone, macOS, modern Android)
	if name == "" {
		name = mdnsLookup(ip)
	}
	if name == "" {
		return
	}
	if v, ok := devices.Load(ip); ok {
		v.(*deviceEntry).hostname.Store(name)
	}
}

func incDeviceConn(ip string) {
	v, loaded := devices.LoadOrStore(ip, &deviceEntry{})
	atomic.AddInt64(&v.(*deviceEntry).ActiveConns, 1)
	if !loaded {
		go resolveHostname(ip) // ilk bağlantıda async hostname çözümle
	}
}

func decDeviceConn(ip string) {
	if v, ok := devices.Load(ip); ok {
		atomic.AddInt64(&v.(*deviceEntry).ActiveConns, -1)
	}
}

func GetDevices() []DeviceInfo {
	var list []DeviceInfo
	devices.Range(func(k, v any) bool {
		e := v.(*deviceEntry)
		hn := ""
		if h := e.hostname.Load(); h != nil {
			hn = h.(string)
		}
		list = append(list, DeviceInfo{
			IP:          k.(string),
			Bytes:       atomic.LoadInt64(&e.Bytes),
			ActiveConns: atomic.LoadInt64(&e.ActiveConns),
			Hostname:    hn,
		})
		return true
	})
	return list
}

func remoteIP(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[:i]
	}
	return addr
}

var hopByHop = []string{
	"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
	"Te", "Trailers", "Transfer-Encoding", "Upgrade", "Proxy-Connection",
}

// sharedTransport — connection pool; multiple devices reuse idle connections
// instead of opening a fresh TCP socket per request.
var sharedTransport = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	MaxIdleConns:          400,
	MaxIdleConnsPerHost:   40,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	ResponseHeaderTimeout: 30 * time.Second,
}

var proxyClient = &http.Client{
	Transport: sharedTransport,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// bufPool — reusable 32 KB copy buffers; reduces GC pressure under many
// concurrent device connections.
var bufPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 32*1024)
		return &b
	},
}

func startProxy(port int) (*http.Server, error) {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           http.HandlerFunc(proxyHandler),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return nil, err
	}
	go srv.Serve(ln)
	logInfo(fmt.Sprintf("HTTP Proxy başlatıldı → 0.0.0.0:%d", port))
	return srv, nil
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		handleConnect(w, r)
	} else {
		handleHTTP(w, r)
	}
}

func handleConnect(w http.ResponseWriter, r *http.Request) {
	stats.incConn()
	ip := remoteIP(r.RemoteAddr)
	incDeviceConn(ip)
	defer stats.decConn()
	defer decDeviceConn(ip)

	dst, err := net.DialTimeout("tcp", r.Host, 15*time.Second)
	if err != nil {
		http.Error(w, "bağlantı kurulamadı", http.StatusBadGateway)
		stats.incError()
		logWarn(fmt.Sprintf("CONNECT %s → hata: %v", r.Host, err))
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		dst.Close()
		http.Error(w, "hijack desteklenmiyor", http.StatusInternalServerError)
		return
	}
	src, rw, err := hj.Hijack()
	if err != nil {
		dst.Close()
		return
	}

	src.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		buf := bufPool.Get().(*[]byte)
		n, _ := io.CopyBuffer(dst, rw, *buf)
		bufPool.Put(buf)
		stats.addBytes(n)
		trackDevice(ip, n)
		dst.Close()
	}()
	go func() {
		defer wg.Done()
		buf := bufPool.Get().(*[]byte)
		n, _ := io.CopyBuffer(src, dst, *buf)
		bufPool.Put(buf)
		stats.addBytes(n)
		trackDevice(ip, n)
		src.Close()
	}()
	wg.Wait()
}

func handleHTTP(w http.ResponseWriter, r *http.Request) {
	if !r.URL.IsAbs() {
		http.Error(w, "tam URL gerekli", http.StatusBadRequest)
		return
	}

	stats.incConn()
	ip := remoteIP(r.RemoteAddr)
	incDeviceConn(ip)
	defer stats.decConn()
	defer decDeviceConn(ip)

	out := r.Clone(r.Context())
	out.RequestURI = ""
	for _, h := range hopByHop {
		out.Header.Del(h)
	}

	resp, err := proxyClient.Do(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		stats.incError()
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	for _, h := range hopByHop {
		w.Header().Del(h)
	}

	w.WriteHeader(resp.StatusCode)
	buf := bufPool.Get().(*[]byte)
	n, _ := io.CopyBuffer(w, resp.Body, *buf)
	bufPool.Put(buf)
	stats.addBytes(n)
	trackDevice(ip, n)
}
