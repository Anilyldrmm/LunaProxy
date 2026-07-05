package main

import (
	"crypto/x509"
	"path/filepath"
	"sync"
	"testing"
)

func TestGenerateCA_SaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	cert, key, err := generateCA()
	if err != nil {
		t.Fatalf("CA üretilemedi: %v", err)
	}
	certPath := filepath.Join(dir, "ca.crt")
	keyPath := filepath.Join(dir, "ca.key")
	if err := saveCA(certPath, keyPath, cert, key); err != nil {
		t.Fatalf("CA kaydedilemedi: %v", err)
	}
	loaded, _, err := loadCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("CA yüklenemedi: %v", err)
	}
	if loaded.SerialNumber.Cmp(cert.SerialNumber) != 0 {
		t.Error("yüklenen CA serial numarası orijinaliyle eşleşmiyor")
	}
	if !loaded.IsCA {
		t.Error("yüklenen sertifika IsCA=true olmalı")
	}
}

func TestLeafCertFor_SignedByCA(t *testing.T) {
	cert, key, err := generateCA()
	if err != nil {
		t.Fatalf("CA üretilemedi: %v", err)
	}
	mitmCACert, mitmCAKey = cert, key
	leafCache = sync.Map{}

	leaf, err := leafCertFor("example.com")
	if err != nil {
		t.Fatalf("leaf sertifika üretilemedi: %v", err)
	}
	leafX509, err := x509.ParseCertificate(leaf.Certificate[0])
	if err != nil {
		t.Fatalf("leaf parse edilemedi: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	if _, err := leafX509.Verify(x509.VerifyOptions{DNSName: "example.com", Roots: roots}); err != nil {
		t.Errorf("leaf sertifika CA tarafından doğrulanamadı: %v", err)
	}
}

func TestLeafCertFor_UsesCache(t *testing.T) {
	cert, key, err := generateCA()
	if err != nil {
		t.Fatalf("CA üretilemedi: %v", err)
	}
	mitmCACert, mitmCAKey = cert, key
	leafCache = sync.Map{}

	first, err := leafCertFor("cache.example.com")
	if err != nil {
		t.Fatalf("ilk leaf üretilemedi: %v", err)
	}
	second, err := leafCertFor("cache.example.com")
	if err != nil {
		t.Fatalf("ikinci leaf üretilemedi: %v", err)
	}
	if first != second {
		t.Error("aynı host için farklı sertifika döndü, cache çalışmıyor")
	}
}

func TestHostOnly(t *testing.T) {
	if got := hostOnly("example.com:443"); got != "example.com" {
		t.Errorf("hostOnly(\"example.com:443\") = %q, beklenen \"example.com\"", got)
	}
	if got := hostOnly("example.com"); got != "example.com" {
		t.Errorf("hostOnly(\"example.com\") = %q, beklenen \"example.com\"", got)
	}
}
