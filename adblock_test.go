package main

import "testing"

func TestAdblockEngine_BlocksKnownAdDomain(t *testing.T) {
	e, err := newAdblockEngine("||ads.example.com^")
	if err != nil {
		t.Fatalf("motor kurulamadı: %v", err)
	}
	if !e.ShouldBlock("https://ads.example.com/banner.js", "news.example.org") {
		t.Error("ads.example.com bloklanmalıydı")
	}
}

func TestAdblockEngine_AllowsUnlistedDomain(t *testing.T) {
	e, err := newAdblockEngine("||ads.example.com^")
	if err != nil {
		t.Fatalf("motor kurulamadı: %v", err)
	}
	if e.ShouldBlock("https://cdn.example.org/app.js", "news.example.org") {
		t.Error("cdn.example.org bloklanmamalıydı")
	}
}

func TestAdblockEngine_CosmeticRules(t *testing.T) {
	e, err := newAdblockEngine("news.example.org##.ad-banner")
	if err != nil {
		t.Fatalf("motor kurulamadı: %v", err)
	}
	found := false
	for _, sel := range e.CosmeticRules("news.example.org") {
		if sel == ".ad-banner" {
			found = true
		}
	}
	if !found {
		t.Errorf("cosmetic selector .ad-banner bulunamadı, got %v", e.CosmeticRules("news.example.org"))
	}
}

func TestLoadBundledAdblockEngine(t *testing.T) {
	e, err := loadBundledAdblockEngine()
	if err != nil {
		t.Fatalf("gömülü filtre listeleri yüklenemedi: %v", err)
	}
	if e == nil {
		t.Fatal("motor nil döndü")
	}
}
