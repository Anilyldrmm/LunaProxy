package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

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
	defer stats.decConn()

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
		dst.Close()
	}()
	go func() {
		defer wg.Done()
		buf := bufPool.Get().(*[]byte)
		n, _ := io.CopyBuffer(src, dst, *buf)
		bufPool.Put(buf)
		stats.addBytes(n)
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
	defer stats.decConn()

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
}
