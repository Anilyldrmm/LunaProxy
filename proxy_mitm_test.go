package main

import (
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
