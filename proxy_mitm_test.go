package main

import (
	"bufio"
	"bytes"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestInjectCosmeticCSS_InsertsBeforeHeadClose(t *testing.T) {
	adblock, _ = newAdblockEngine("news.example.org##.ad-banner")
	body := `<html><head><title>t</title></head><body>hi</body></html>`
	resp := &http.Response{
		Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}},
		Body:   io.NopCloser(strings.NewReader(body)),
	}

	injectCosmeticCSS(resp, "news.example.org")

	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("gövde okunamadı: %v", err)
	}
	if !bytes.Contains(out, []byte(".ad-banner{display:none!important}")) {
		t.Errorf("cosmetic CSS enjekte edilmedi, got: %s", out)
	}
	if !bytes.Contains(out, []byte("</head>")) {
		t.Error("</head> etiketi kayboldu")
	}
	// Enjeksiyon sonrası nihai uzunluk artık önceden bilinmiyor (kalan gövde
	// akıştan geliyor olabilir) — Content-Length header'ı kaldırılıp yerine
	// chunked framing kullanılmalı.
	if resp.Header.Get("Content-Length") != "" {
		t.Errorf("Content-Length header'ı silinmeliydi, got %q", resp.Header.Get("Content-Length"))
	}
	if resp.ContentLength != -1 {
		t.Errorf("resp.ContentLength=-1 olmalıydı, got %d", resp.ContentLength)
	}
	if len(resp.TransferEncoding) != 1 || resp.TransferEncoding[0] != "chunked" {
		t.Errorf("resp.TransferEncoding=[\"chunked\"] olmalıydı, got %v", resp.TransferEncoding)
	}
}

// TestInjectCosmeticCSS_LargeBody_StreamsRemainderAfterHeadClose — </head>
// ilk 2MB'lık ön ek içinde bulunuyor ama gerçek gövde 2MB sınırının çok
// ötesine geçiyor. Enjeksiyondan sonra resp.Body hâlâ okunmamış orijinal
// kuyruğu (LimitReader'ın arkasında kalan kısmı) içermeli; hiçbir byte
// kaybolmamalı ve alttaki stream tamamen boşaltılabilmeli.
func TestInjectCosmeticCSS_LargeBody_StreamsRemainderAfterHeadClose(t *testing.T) {
	adblock, _ = newAdblockEngine("big.example.org##.ad-banner")

	head := "<html><head><title>t</title></head>"
	const padLen = 3 * 1024 * 1024 // 3MB — 2MB sınırının kesinlikle ötesinde
	pad := strings.Repeat("A", padLen)
	body := head + "<body>" + pad + "</body></html>"

	resp := &http.Response{
		Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}},
		Body:   io.NopCloser(strings.NewReader(body)),
	}

	injectCosmeticCSS(resp, "big.example.org")

	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("gövde okunamadı: %v", err)
	}

	styleSnippet := "<style>" + ".ad-banner{display:none!important}" + "</style>"
	if n := bytes.Count(out, []byte(styleSnippet)); n != 1 {
		t.Errorf("style bloğu tam olarak 1 kez görünmeliydi, got %d kez", n)
	}
	if idx := bytes.Index(out, []byte(styleSnippet)); idx < 0 || idx > bytes.Index(out, []byte("</head>")) {
		t.Error("style bloğu </head>'ten önce olmalıydı")
	}

	wantLen := len(body) + len(styleSnippet)
	if len(out) != wantLen {
		t.Errorf("toplam uzunluk yanlış: got %d, beklenen %d (orijinal %d + style %d)", len(out), wantLen, len(body), len(styleSnippet))
	}

	// Padding'in tamamı, bozulmadan </head> sonrasında bulunmalı.
	if !bytes.Contains(out, []byte(pad)) {
		t.Error("3MB'lık padding gövdede eksiksiz bulunamadı — veri kaybı var")
	}
}

func TestInjectCosmeticCSS_NoSelectors_LeavesBodyUnchanged(t *testing.T) {
	adblock, _ = newAdblockEngine("")
	body := `<html><head></head><body>hi</body></html>`
	resp := &http.Response{
		Header: http.Header{"Content-Type": {"text/html"}},
		Body:   io.NopCloser(strings.NewReader(body)),
	}

	injectCosmeticCSS(resp, "no-rules.example.org")

	out, _ := io.ReadAll(resp.Body)
	if string(out) != body {
		t.Errorf("selector yokken gövde değişmemeliydi, got: %s", out)
	}
}

func TestInjectCosmeticCSS_NoHeadTag_PassesThroughByteForByte(t *testing.T) {
	adblock, _ = newAdblockEngine("news.example.org##.ad-banner")
	body := `{"json":"fragment","no":"head tag here"}`
	resp := &http.Response{
		Header: http.Header{
			"Content-Type":   {"text/html; charset=utf-8"},
			"Content-Length": {strconv.Itoa(len(body))},
		},
		ContentLength: int64(len(body)),
		Body:          io.NopCloser(strings.NewReader(body)),
	}

	injectCosmeticCSS(resp, "news.example.org")

	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("gövde okunamadı: %v", err)
	}
	if string(out) != body {
		t.Errorf("</head> yokken gövde byte-for-byte aynı kalmalıydı, got: %q, beklenen: %q", out, body)
	}
	if resp.Header.Get("Content-Length") != strconv.Itoa(len(body)) {
		t.Errorf("Content-Length header'ına dokunulmamalıydı, got %s", resp.Header.Get("Content-Length"))
	}
	if resp.Header.Get("Transfer-Encoding") != "" {
		t.Errorf("Transfer-Encoding header'ı eklenmemeliydi, got %q", resp.Header.Get("Transfer-Encoding"))
	}
	if resp.ContentLength != int64(len(body)) {
		t.Errorf("resp.ContentLength'a dokunulmamalıydı, got %d, beklenen %d", resp.ContentLength, len(body))
	}
}

func TestInjectCosmeticCSS_TruncatedBeforeHeadClose_PassesThroughByteForByte(t *testing.T) {
	adblock, _ = newAdblockEngine("news.example.org##.ad-banner")
	// </head> yok — sadece <head> açık kalmış, HTML gövdesi kesilmiş gibi.
	body := `<html><head><title>uzun bir sayfa basligi</title>`
	resp := &http.Response{
		Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}},
		Body:   io.NopCloser(strings.NewReader(body)),
	}

	injectCosmeticCSS(resp, "news.example.org")

	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("gövde okunamadı: %v", err)
	}
	if string(out) != body {
		t.Errorf("</head> bulunamadığında gövde değişmemeliydi, got: %q", out)
	}
}

func TestBufferedConn_PreservesPrebufferedBytesThroughRawTunnel(t *testing.T) {
	a1, a2 := net.Pipe()
	b1, b2 := net.Pipe()
	devices.Range(func(k, v any) bool { devices.Delete(k); return true })

	go func() {
		a1.Write([]byte("hello world"))
		a1.Close()
	}()

	br := bufio.NewReader(a2)
	// bufio.Reader'ın Hijack()'in yaptığı gibi en az bir byte'ı önceden
	// okuyup kendi iç buffer'ına almasını garanti et.
	if _, err := br.Peek(1); err != nil {
		t.Fatalf("peek hata: %v", err)
	}

	bc := &bufferedConn{r: br, Conn: a2}

	done := make(chan struct{})
	go func() {
		rawTunnel(bc, b2, "127.0.0.1")
		close(done)
	}()

	want := "hello world"
	buf := make([]byte, len(want))
	n, err := io.ReadFull(b1, buf)
	if err != nil {
		t.Fatalf("b1'den okunamadı: %v", err)
	}
	if string(buf[:n]) != want {
		t.Errorf("bufferedConn üzerinden pre-buffered byte'lar kayboldu, got %q", buf[:n])
	}
	b1.Close()
	<-done
}

func TestCountingWriter_CountsBytesWrittenRegardlessOfFraming(t *testing.T) {
	var buf bytes.Buffer
	cw := &countingWriter{Writer: &buf}

	n1, err := cw.Write([]byte("abc"))
	if err != nil || n1 != 3 {
		t.Fatalf("ilk Write hata: n=%d err=%v", n1, err)
	}
	n2, err := cw.Write([]byte("defgh"))
	if err != nil || n2 != 5 {
		t.Fatalf("ikinci Write hata: n=%d err=%v", n2, err)
	}
	if cw.n != 8 {
		t.Errorf("countingWriter.n = %d, beklenen 8", cw.n)
	}
	if buf.String() != "abcdefgh" {
		t.Errorf("underlying writer'a doğru yazılmadı, got %q", buf.String())
	}
}

// TestShouldMITM_FallsBackToPassthrough_WhenPrerequisitesNotReady —
// handleConnect'in MITM dispatch koşulunu doğrudan test eder: AdBlockEnabled
// açık olsa ve host exempt olmasa bile, CA başlangıçta yüklenemediyse
// (mitmCACert==nil) ya da adblock motoru parse edilemediyse (adblock==nil),
// shouldMITM false dönmeli — böylece handleConnect ham tünele
// (passthroughConnect) düşer, mitmConnect→leafCertFor→x509.CreateCertificate
// nil CA ile panic'e yol açmaz.
func TestShouldMITM_FallsBackToPassthrough_WhenPrerequisitesNotReady(t *testing.T) {
	origCA, origKey := mitmCACert, mitmCAKey
	origAdblock := adblock
	defer func() { mitmCACert, mitmCAKey = origCA, origKey; adblock = origAdblock }()

	dummyCert := &x509.Certificate{}
	dummyAdblock, err := newAdblockEngine("")
	if err != nil {
		t.Fatalf("test adblock motoru oluşturulamadı: %v", err)
	}

	// isMITMExempt paket-seviyesindeki global `current` konfigürasyonunu okur —
	// host'un kesinlikle exempt sayılmamasını garanti etmek için boş bir
	// MITMExemptDomains ile sabitliyoruz (config_test.go'daki save/restore deseni).
	cfgMu.Lock()
	saved := current
	current = Config{MITMExemptDomains: nil}
	cfgMu.Unlock()
	t.Cleanup(func() {
		cfgMu.Lock()
		current = saved
		cfgMu.Unlock()
	})

	cfg := Config{AdBlockEnabled: true}
	host := "not-exempt.example.org"

	cases := []struct {
		name       string
		ca         *x509.Certificate
		ab         *adblockEngine
		wantMITM   bool
		wantReason string
	}{
		{"CA hazır + adblock hazır → MITM", dummyCert, dummyAdblock, true, "her iki ön koşul da hazır"},
		{"CA nil → ham tünel", nil, dummyAdblock, false, "mitmCACert nil"},
		{"adblock nil → ham tünel", dummyCert, nil, false, "adblock nil"},
		{"ikisi de nil → ham tünel", nil, nil, false, "ikisi de nil"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mitmCACert = tc.ca
			adblock = tc.ab
			got := shouldMITM(cfg, host)
			if got != tc.wantMITM {
				t.Errorf("shouldMITM() = %v, beklenen %v (%s)", got, tc.wantMITM, tc.wantReason)
			}
		})
	}
}

// TestShouldMITM_RespectsAdBlockDisabledAndExemptHost — mevcut davranışın
// (AdBlockEnabled=false veya exempt host) bu değişiklikle bozulmadığını
// doğrular.
func TestShouldMITM_RespectsAdBlockDisabledAndExemptHost(t *testing.T) {
	origCA, origKey := mitmCACert, mitmCAKey
	origAdblock := adblock
	defer func() { mitmCACert, mitmCAKey = origCA, origKey; adblock = origAdblock }()

	mitmCACert = &x509.Certificate{}
	var errAb error
	adblock, errAb = newAdblockEngine("")
	if errAb != nil {
		t.Fatalf("test adblock motoru oluşturulamadı: %v", errAb)
	}

	// isMITMExempt paket-seviyesindeki global `current` konfigürasyonunu
	// okur (shouldMITM'e geçilen cfg parametresini değil) — config_test.go'daki
	// TestIsMITMExempt ile aynı save/restore deseni kullanılıyor.
	cfgMu.Lock()
	saved := current
	current = Config{MITMExemptDomains: []string{"apple.com"}}
	cfgMu.Unlock()
	t.Cleanup(func() {
		cfgMu.Lock()
		current = saved
		cfgMu.Unlock()
	})

	if shouldMITM(Config{AdBlockEnabled: false}, "not-exempt.example.org") {
		t.Error("AdBlockEnabled=false iken shouldMITM true dönmemeliydi")
	}
	if shouldMITM(Config{AdBlockEnabled: true}, "apple.com") {
		t.Error("MITM-exempt host için shouldMITM true dönmemeliydi")
	}
	if !shouldMITM(Config{AdBlockEnabled: true}, "not-exempt.example.org") {
		t.Error("exempt olmayan host + hazır ön koşullarla shouldMITM true dönmeliydi")
	}
}

func TestIsHTMLResponse(t *testing.T) {
	html := &http.Response{Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}}
	json := &http.Response{Header: http.Header{"Content-Type": {"application/json"}}}
	if !isHTMLResponse(html) {
		t.Error("text/html true dönmeliydi")
	}
	if isHTMLResponse(json) {
		t.Error("application/json false dönmeliydi")
	}
}

func TestRawTunnel_CopiesBothDirections(t *testing.T) {
	a1, a2 := net.Pipe()
	b1, b2 := net.Pipe()
	devices.Range(func(k, v any) bool { devices.Delete(k); return true })

	done := make(chan struct{})
	go func() {
		rawTunnel(a2, b2, "127.0.0.1")
		close(done)
	}()

	go func() { a1.Write([]byte("merhaba")); a1.Close() }()
	buf := make([]byte, 7)
	n, _ := io.ReadFull(b1, buf)
	if string(buf[:n]) != "merhaba" {
		t.Errorf("a→b kopyalanmadı, got %q", buf[:n])
	}
	b1.Close()
	<-done
}
