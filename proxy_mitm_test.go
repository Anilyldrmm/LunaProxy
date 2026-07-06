package main

import (
	"bufio"
	"bytes"
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
	if resp.Header.Get("Content-Length") != strconv.Itoa(len(out)) {
		t.Errorf("Content-Length güncellenmedi: got %s, beklenen %d", resp.Header.Get("Content-Length"), len(out))
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
