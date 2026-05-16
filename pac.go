package main

import (
	"fmt"
	"net"
	"net/http"
	"strings"
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
// BypassEnabled=true ve BypassDomains doluysa sadece o domain'ler proxy'den geçer;
// aksi takdirde tüm trafik proxy'den geçer.
func setPACRunning(localIP string, proxyPort int) {
	c := getConfig()
	proxy := fmt.Sprintf(`"PROXY %s:%d; DIRECT"`, localIP, proxyPort)
	var body string
	if !c.BypassEnabled || len(c.BypassDomains) == 0 {
		body = fmt.Sprintf(`function FindProxyForURL(url,host){return %s;}`, proxy)
	} else {
		var sb strings.Builder
		sb.WriteString(`function FindProxyForURL(url,host){`)
		for _, d := range c.BypassDomains {
			d = strings.TrimSpace(d)
			if d == "" {
				continue
			}
			sb.WriteString(fmt.Sprintf(`if(dnsDomainIs(host,%q)||host===%q)return %s;`, d, d, proxy))
		}
		sb.WriteString(`return "DIRECT";}`)
		body = sb.String()
	}
	pacMu.Lock()
	pacBody = body
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
// Yeni kurulum /cgi-bin/update.sh, eski kurulum /update.sh — her ikisini dener.
func pushRouterPAC(localIP, mode string, proxyPort int) {
	gateway := guessGatewayIP(localIP)
	var query string
	if mode == "proxy" {
		query = fmt.Sprintf("mode=proxy&ip=%s&port=%d", localIP, proxyPort)
	} else {
		query = "mode=direct"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, path := range []string{"/cgi-bin/update.sh", "/update.sh"} {
		url := fmt.Sprintf("http://%s:8090%s?%s", gateway, path, query)
		for i := 0; i < 3; i++ {
			resp, err := client.Get(url)
			if err == nil {
				ok := resp.StatusCode == 200
				resp.Body.Close()
				if ok {
					return
				}
				break // 404 gibi hata — bu path'ı deneme, sonrakine geç
			}
			if i < 2 {
				time.Sleep(2 * time.Second)
			}
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

	routerPACURL := fmt.Sprintf("http://%s:8090/cgi-bin/proxy.pac", guessGatewayIP(localIP))

	mux.HandleFunc("/setup", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, setupPageHTML, routerPACURL, pacURL)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	return mux
}

const setupPageHTML = `<!DOCTYPE html>
<html lang="tr"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>LunaProxy Kurulum</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:system-ui,sans-serif;background:#0D0B14;color:#F1F0F5;min-height:100vh;
  display:flex;align-items:center;justify-content:center;padding:20px}
.card{background:#13101E;border:1px solid #2A2240;border-radius:16px;padding:28px;max-width:440px;width:100%%}
h1{font-size:20px;font-weight:700;margin-bottom:4px}
.sub{font-size:13px;color:#6B6490;margin-bottom:20px}
.label{font-size:11px;text-transform:uppercase;letter-spacing:1px;color:#6B6490;margin-bottom:6px;font-weight:600}
.badge{display:inline-block;font-size:10px;font-weight:700;padding:2px 7px;border-radius:4px;
  background:rgba(34,197,94,.15);color:#22c55e;border:1px solid rgba(34,197,94,.3);margin-bottom:6px}
.badge.fallback{background:rgba(107,100,144,.15);color:#6B6490;border-color:rgba(107,100,144,.3)}
.url-box{background:#1A1628;border:1px solid #2A2240;border-radius:8px;padding:10px 12px;
  font-family:monospace;font-size:12px;word-break:break-all;color:#A855F7;margin-bottom:8px}
.copy-btn{width:100%%;padding:11px;border-radius:8px;border:none;
  background:linear-gradient(135deg,#8B3FBF,#6B21A8);color:#fff;
  font-size:13px;font-weight:700;cursor:pointer;margin-bottom:20px}
.section{margin-bottom:18px}
.section h2{font-size:13px;font-weight:700;margin-bottom:8px;color:#A855F7}
.step{font-size:13px;color:#6B6490;padding:3px 0;padding-left:16px;position:relative}
.step::before{content:"→";position:absolute;left:0;color:#8B3FBF}
.copied{background:linear-gradient(135deg,#16a34a,#15803d)!important}
.fallback-btn{background:#1A1628;border:1px solid #2A2240;color:#6B6490}
hr{border:none;border-top:1px solid #2A2240;margin:16px 0}
</style></head>
<body><div class="card">
<h1>LunaProxy Kurulum</h1>
<p class="sub">Proxy ayari icin asagidaki URL'yi kopyalayin.</p>
<div class="badge">ONERILEN — Router (her zaman erisebilir)</div>
<div class="url-box" id="rurl">%s</div>
<button class="copy-btn" id="rbtn" onclick="cp('rurl','rbtn')">Kopyala</button>
<div class="badge fallback">YEDEK — Sadece PC acikken</div>
<div class="url-box" id="purl">%s</div>
<button class="copy-btn fallback-btn" id="pbtn" onclick="cp('purl','pbtn')">Kopyala</button>
<hr>
<div class="section"><h2>Android</h2>
<div class="step">Wi-Fi ayarlari → Bağli ağa uzun bas → Aği degistir</div>
<div class="step">Gelismis secenekler → Proxy: Otomatik</div>
<div class="step">PAC URL → Kaydet</div></div>
<div class="section"><h2>iOS</h2>
<div class="step">Ayarlar → Wi-Fi → Bağli ağin yanindaki (i)</div>
<div class="step">HTTP Proxy → Otomatik</div>
<div class="step">URL alani → yapistir → Kaydet</div></div>
<div class="section"><h2>Windows</h2>
<div class="step">Ayarlar → Ağ ve Internet → Proxy</div>
<div class="step">Kurulum betigi kullan → URL alanina yapistir → Kaydet</div></div>
</div>
<script>
function cp(id,btn){
  var url=document.getElementById(id).textContent.trim();
  navigator.clipboard.writeText(url).then(function(){
    var b=document.getElementById(btn);
    var orig=b.textContent;
    b.textContent='Kopyalandi';b.classList.add('copied');
    setTimeout(function(){b.textContent=orig;b.classList.remove('copied');},2000);
  });
}
</script></body></html>`
