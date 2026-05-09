package main

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

// ── Dinamik PAC içeriği ───────────────────────────────────────────────────────
// PAC sunucusu her zaman çalışır; proxy durumu değişince içerik güncellenir.
// Telefon PAC'ı çekemese bile DIRECT fallback devreye girer.

var (
	pacMu   sync.RWMutex
	pacBody = `function FindProxyForURL(url,host){return "DIRECT";}`
)

// setPACRunning — proxy açıkken PAC'ı proxy+DIRECT moduna alır.
func setPACRunning(localIP string, proxyPort int) {
	pacMu.Lock()
	pacBody = fmt.Sprintf(
		`function FindProxyForURL(url,host){return "PROXY %s:%d; DIRECT";}`,
		localIP, proxyPort,
	)
	pacMu.Unlock()
}

// setPACDirect — proxy kapalıyken PAC'ı DIRECT moduna alır.
// Böylece telefon PAC sunucusuna erişebildiği sürece interneti kesmez.
func setPACDirect() {
	pacMu.Lock()
	pacBody = `function FindProxyForURL(url,host){return "DIRECT";}`
	pacMu.Unlock()
}

// ── PAC sunucusu ──────────────────────────────────────────────────────────────

func startPAC(localIP string, port int) (*http.Server, error) {
	mux := buildPACMux(localIP, port)
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	logInfo(fmt.Sprintf("PAC sunucu başlatıldı → http://%s:%d/proxy.pac", localIP, port))
	return srv, nil
}

// pushRouterPAC — PC→Router HTTP CGI ile PAC durumunu günceller.
// Proxy modunda IP ve port da gönderilir; router PAC'ı dinamik yazar.
// Router erişilemezse sessizce geçer.
func pushRouterPAC(localIP, mode string, proxyPort int) {
	gateway := guessGatewayIP(localIP)
	var url string
	if mode == "proxy" {
		url = fmt.Sprintf("http://%s:8090/update.sh?mode=proxy&ip=%s&port=%d", gateway, localIP, proxyPort)
	} else {
		url = fmt.Sprintf("http://%s:8090/update.sh?mode=direct", gateway)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < 3; i++ {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			return
		}
		if i < 2 {
			time.Sleep(2 * time.Second)
		}
	}
}

func buildPACMux(localIP string, port int) *http.ServeMux {
	pacURL := fmt.Sprintf("http://%s:%d/proxy.pac", localIP, port)

	servePAC := func(w http.ResponseWriter, r *http.Request) {
		pacMu.RLock()
		body := pacBody
		pacMu.RUnlock()
		w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
		// no-cache: iOS PAC'ı önbelleğe almasın; proxy kapanınca DIRECT'i hemen görsün.
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		fmt.Fprint(w, body)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/proxy.pac", servePAC)
	mux.HandleFunc("/wpad.dat", servePAC)

	mux.HandleFunc("/qr.png", func(w http.ResponseWriter, r *http.Request) {
		png, err := qrcode.Encode(pacURL, qrcode.High, 300)
		if err != nil {
			http.Error(w, "QR oluşturulamadı", 500)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(png)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	return mux
}
