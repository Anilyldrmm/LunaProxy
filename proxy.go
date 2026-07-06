package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
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
	// Tailscale IP'si (100.x.x.x) — peer listesinden doğrudan al
	if strings.HasPrefix(ip, "100.") {
		ts := getTailscaleStatus()
		for _, p := range ts.Peers {
			if p.IP == ip && p.Name != "" {
				name = p.Name
				break
			}
		}
	}
	// DNS PTR (Windows cihazları, bazı Android)
	if name == "" {
		if ns, err := net.LookupAddr(ip); err == nil && len(ns) > 0 {
			candidate := strings.TrimSuffix(ns[0], ".")
			// "localhost" ve anlamsız genel isimler geçersiz say
			if candidate != "localhost" && candidate != "localhost.localdomain" && candidate != ip {
				name = candidate
			}
		}
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
	e := v.(*deviceEntry)
	atomic.AddInt64(&e.ActiveConns, 1)
	// İlk bağlantıda ya da daha önce hostname çözülemediyse tekrar dene
	if !loaded || e.hostname.Load() == nil {
		go resolveHostname(ip)
	}
}

func decDeviceConn(ip string) {
	if v, ok := devices.Load(ip); ok {
		atomic.AddInt64(&v.(*deviceEntry).ActiveConns, -1)
	}
}

func GetDevices() []DeviceInfo {
	seen := map[string]bool{}
	var list []DeviceInfo

	devices.Range(func(k, v any) bool {
		ip := k.(string)
		seen[ip] = true
		e := v.(*deviceEntry)
		hn := ""
		if h := e.hostname.Load(); h != nil {
			hn = h.(string)
		}
		list = append(list, DeviceInfo{
			IP:          ip,
			Bytes:       atomic.LoadInt64(&e.Bytes),
			ActiveConns: atomic.LoadInt64(&e.ActiveConns),
			Hostname:    hn,
		})
		return true
	})

	// Tailscale peer'larını ekle — proxy'den geçmeyenler (exit node kullanıcıları)
	ts := getTailscaleStatus()
	for _, p := range ts.Peers {
		if p.IP == "" || seen[p.IP] {
			continue
		}
		// Hiç veri yok ve çevrimdışı → gösterme
		if !p.Online && p.RxBytes == 0 && p.TxBytes == 0 {
			continue
		}
		list = append(list, DeviceInfo{
			IP:          p.IP,
			Bytes:       p.RxBytes + p.TxBytes,
			ActiveConns: 0,
			Hostname:    p.Name,
		})
	}

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

	host := hostOnly(r.Host)
	c := getConfig()
	if c.AdBlockEnabled && !isMITMExempt(host) {
		mitmConnect(w, r, ip, host)
		return
	}
	passthroughConnect(w, r, ip)
}

// passthroughConnect — ham TCP tünel (MITM-exempt host'lar ve AdBlock
// kapalıyken kullanılan, önceki handleConnect'in birebir aynısı).
func passthroughConnect(w http.ResponseWriter, r *http.Request, ip string) {
	dst, err := net.DialTimeout("tcp", r.Host, 15*time.Second)
	if err != nil {
		http.Error(w, "bağlantı kurulamadı", http.StatusBadGateway)
		stats.incError()
		host := r.Host
		if h, _, e := net.SplitHostPort(host); e == nil {
			host = h
		}
		if !strings.HasSuffix(host, ".invalid") {
			logWarn(fmt.Sprintf("CONNECT %s → hata: %v", r.Host, err))
		}
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
	rawTunnel(&bufferedConn{r: rw.Reader, Conn: src}, dst, ip)
}

// bufferedConn — Hijack()'ten gelen bufio.Reader'daki (varsa) önceden
// okunmuş byte'ları kaybetmeden ham tünele geçmek için kullanılır.
type bufferedConn struct {
	r *bufio.Reader
	net.Conn
}

func (b *bufferedConn) Read(p []byte) (int, error) {
	return b.r.Read(p)
}

// countingWriter — Write edilen gercek byte sayisini tutar (Content-Length
// veya chunked framing'den bagimsiz, stats/device takibi icin).
type countingWriter struct {
	io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.Writer.Write(p)
	c.n += int64(n)
	return n, err
}

// rawTunnel — iki bağlantı arasında ham byte kopyası yapar (bufPool'lu),
// her iki yön kapanana kadar bloklar. Her iki taraf da fonksiyon dönmeden
// önce kapatılır.
func rawTunnel(a, b io.ReadWriteCloser, ip string) {
	defer a.Close()
	defer b.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	cp := func(dst io.Writer, src io.Reader) {
		defer wg.Done()
		buf := bufPool.Get().(*[]byte)
		n, _ := io.CopyBuffer(dst, src, *buf)
		bufPool.Put(buf)
		stats.addBytes(n)
		trackDevice(ip, n)
	}
	go cp(a, b)
	cp(b, a)
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

// mitmConnect — AdBlockEnabled=true ve host MITM-exempt değilken çalışır.
// Hedefe TLS handshake başarısız olursa (örn. cert pinning) ham tünele düşer.
func mitmConnect(w http.ResponseWriter, r *http.Request, ip, host string) {
	dst, err := tls.DialWithDialer(&net.Dialer{Timeout: 15 * time.Second}, "tcp", r.Host, &tls.Config{ServerName: host, NextProtos: []string{"http/1.1"}})
	if err != nil {
		logWarn(fmt.Sprintf("MITM %s → hedef TLS reddetti, ham tünele düşülüyor: %v", host, err))
		passthroughConnect(w, r, ip)
		return
	}

	// Leaf sertifikayı istemciyi hijack etmeden ÖNCE üret — istemciye
	// "200 Connection Established" söylendikten sonra artık ham pass-through'a
	// dönülemez (istemci fake sertifika bekliyor olur), o yüzden hata burada
	// yakalanmalı.
	if _, err := leafCertFor(host); err != nil {
		dst.Close()
		logWarn(fmt.Sprintf("MITM %s → leaf sertifika üretilemedi, ham tünele düşülüyor: %v", host, err))
		passthroughConnect(w, r, ip)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		dst.Close()
		http.Error(w, "hijack desteklenmiyor", http.StatusInternalServerError)
		return
	}
	clientRaw, _, err := hj.Hijack()
	if err != nil {
		dst.Close()
		return
	}
	if _, err := clientRaw.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		clientRaw.Close()
		dst.Close()
		return
	}

	clientTLS := tls.Server(clientRaw, &tls.Config{
		NextProtos: []string{"http/1.1"},
		// Bilinen sınır: istemci ClientHello'da CONNECT hedefinden (host) farklı bir
		// SNI sunarsa (nadir — connection coalescing veya standart dışı istemci) ve o
		// isim için leafCertFor hata verirse, handshake sert hata ile kapanır — bu
		// noktada istemciye zaten "200 Connection Established" gönderilmiş ve
		// istemci bizim sahte kimliğimizle TLS handshake'ine başlamış olduğundan ham
		// tünele düşmek artık mümkün değil (gerçek sunucunun handshake byte'larını
		// bu aşamada istemciye iletemeyiz). Yukarıdaki eager leafCertFor(host)
		// ön-kontrolü sadece CONNECT hedefi için hata olasılığını elemine eder.
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			name := hello.ServerName
			if name == "" {
				name = host
			}
			return leafCertFor(name)
		},
	})
	clientRaw.SetDeadline(time.Now().Add(15 * time.Second))
	if err := clientTLS.Handshake(); err != nil {
		clientTLS.Close()
		dst.Close()
		logWarn(fmt.Sprintf("MITM %s → istemci TLS handshake hatası: %v", host, err))
		return
	}
	clientRaw.SetDeadline(time.Time{})
	defer clientTLS.Close()
	defer dst.Close()

	clientBR := bufio.NewReader(clientTLS)
	serverBR := bufio.NewReader(dst)

	for {
		req, err := http.ReadRequest(clientBR)
		if err != nil {
			return
		}
		req.URL.Scheme = "https"
		req.URL.Host = host
		reqURL := req.URL.String()

		if adblock != nil && adblock.ShouldBlock(reqURL, host) {
			io.Copy(io.Discard, req.Body)
			clientTLS.Write([]byte("HTTP/1.1 204 No Content\r\nContent-Length: 0\r\nConnection: keep-alive\r\n\r\n"))
			continue
		}

		req.Header.Set("Accept-Encoding", "identity")
		req.RequestURI = ""
		reqCW := &countingWriter{Writer: dst}
		if err := req.Write(reqCW); err != nil {
			return
		}

		resp, err := http.ReadResponse(serverBR, req)
		if err != nil {
			return
		}

		if isHTMLResponse(resp) {
			injectCosmeticCSS(resp, host)
		}

		respCW := &countingWriter{Writer: clientTLS}
		if err := resp.Write(respCW); err != nil {
			resp.Body.Close()
			return
		}
		resp.Body.Close()
		if total := reqCW.n + respCW.n; total > 0 {
			stats.addBytes(total)
			trackDevice(ip, total)
		}

		if resp.StatusCode == http.StatusSwitchingProtocols {
			rawTunnel(&bufferedConn{r: clientBR, Conn: clientTLS}, &bufferedConn{r: serverBR, Conn: dst}, ip)
			return
		}
		if req.Close || resp.Close {
			return
		}
	}
}

// isHTMLResponse — yanıtın Content-Type'ının text/html olup olmadığını döner.
func isHTMLResponse(resp *http.Response) bool {
	return strings.Contains(resp.Header.Get("Content-Type"), "text/html")
}

// injectCosmeticCSS — HTML gövdesini (en fazla 2MB) buffera alıp </head>
// öncesine cosmetic CSS enjekte eder, Content-Length'i günceller.
// Selector yoksa veya gövde okunamazsa dokunmadan bırakır.
func injectCosmeticCSS(resp *http.Response, host string) {
	if adblock == nil {
		return
	}
	selectors := adblock.CosmeticRules(host)
	if len(selectors) == 0 {
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), resp.Body))
		return
	}

	idx := bytes.LastIndex(body, []byte("</head>"))
	if idx < 0 {
		// </head> bulunamadi (govde limit disinda kalmis olabilir) — govdeye dokunma, oldugu gibi ilet
		resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), resp.Body))
		return
	}

	style := []byte("<style>" + strings.Join(selectors, ",") + "{display:none!important}</style></head>")
	newBody := make([]byte, 0, len(body)+len(style))
	newBody = append(newBody, body[:idx]...)
	newBody = append(newBody, style...)
	newBody = append(newBody, body[idx+len("</head>"):]...)

	// newBody sadece ilk 2MB'lık ön ekten oluşuyor — gerçek gövde daha
	// büyükse resp.Body'de hâlâ okunmamış kuyruk vardır. Onu MultiReader ile
	// zincirleyerek hem veri kaybını önlüyoruz hem de alttaki bağlantıyı
	// tam olarak boşaltıp bir sonraki keep-alive isteğinin doğru
	// ayrıştırılmasını garanti ediyoruz. Nihai uzunluk önceden bilinemediği
	// için ContentLength=-1 ve Transfer-Encoding: chunked kullanılıyor —
	// (*http.Response).Write bunu otomatik olarak chunked olarak yazar.
	resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(newBody), resp.Body))
	resp.ContentLength = -1
	resp.TransferEncoding = []string{"chunked"}
	resp.Header.Del("Content-Length")
	resp.Header.Del("Transfer-Encoding")
}
