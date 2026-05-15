package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSetupEndpoint(t *testing.T) {
	mux := buildPACMux("192.168.1.41", 8080)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/setup", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, beklenen 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "proxy.pac") {
		t.Error("setup sayfası PAC URL içermiyor")
	}
	if !strings.Contains(body, "Android") {
		t.Error("setup sayfası Android talimatı içermiyor")
	}
	if !strings.Contains(body, "iOS") {
		t.Error("setup sayfası iOS talimatı içermiyor")
	}
}
