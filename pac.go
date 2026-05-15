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

	setupURL := fmt.Sprintf("http://%s:%d/setup", localIP, port)
	_ = setupURL

	mux.HandleFunc("/setup", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, setupPageHTML, pacURL, pacURL)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	return mux
}

const setupPageHTML = `<!DOCTYPE html>
<html lang="tr"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>SpAC3DPI Kurulum</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:system-ui,sans-serif;background:#0D0B14;color:#F1F0F5;min-height:100vh;
  display:flex;align-items:center;justify-content:center;padding:20px}
.card{background:#13101E;border:1px solid #2A2240;border-radius:16px;padding:28px;max-width:420px;width:100%%}
h1{font-size:20px;font-weight:700;margin-bottom:4px}
.sub{font-size:13px;color:#6B6490;margin-bottom:24px}
.label{font-size:11px;text-transform:uppercase;letter-spacing:1px;color:#6B6490;margin-bottom:6px;font-weight:600}
.url-box{background:#1A1628;border:1px solid #2A2240;border-radius:8px;padding:10px 12px;
  font-family:monospace;font-size:12px;word-break:break-all;color:#A855F7;margin-bottom:8px}
.copy-btn{width:100%%;padding:12px;border-radius:8px;border:none;
  background:linear-gradient(135deg,#8B3FBF,#6B21A8);color:#fff;
  font-size:14px;font-weight:700;cursor:pointer;margin-bottom:24px}
.section{margin-bottom:20px}
.section h2{font-size:13px;font-weight:700;margin-bottom:10px;color:#A855F7}
.step{font-size:13px;color:#6B6490;padding:4px 0;padding-left:16px;position:relative}
.step::before{content:"→";position:absolute;left:0;color:#8B3FBF}
.copied{background:linear-gradient(135deg,#16a34a,#15803d)!important}
</style></head>
<body><div class="card">
<h1>🟣 SpAC3DPI Kurulum</h1>
<p class="sub">Proxy ayarı için PAC URL'ini kopyalayın ve talimatları izleyin.</p>
<div class="label">PAC URL</div>
<div class="url-box" id="url">%s</div>
<button class="copy-btn" onclick="cp()">Kopyala</button>
<div class="section"><h2>📱 Android</h2>
<div class="step">Wi-Fi ayarlarına girin</div>
<div class="step">Bağlı ağa uzun basın → Ağı değiştir</div>
<div class="step">Gelişmiş seçenekler → Proxy: Otomatik</div>
<div class="step">PAC URL'ini yapıştırın → Kaydedin</div></div>
<div class="section"><h2>🍎 iOS</h2>
<div class="step">Ayarlar → Wi-Fi açın</div>
<div class="step">Bağlı ağın yanındaki ⓘ simgesine basın</div>
<div class="step">HTTP Proxy → Otomatik seçin</div>
<div class="step">PAC URL'ini yapıştırın → Kaydedin</div></div>
<div class="section"><h2>💻 Windows</h2>
<div class="step">Ayarlar → Ağ ve İnternet → Proxy</div>
<div class="step">Kurulum betiği kullan → Açık</div>
<div class="step">Betik adresi: PAC URL'ini yapıştırın</div>
<div class="step">Kaydet</div></div>
</div>
<script>
function cp(){
  navigator.clipboard.writeText('%s').then(()=>{
    var b=document.querySelector('.copy-btn');
    b.textContent='✔ Kopyalandı';b.classList.add('copied');
    setTimeout(()=>{b.textContent='Kopyala';b.classList.remove('copied')},2000);
  });
}
</script></body></html>`
