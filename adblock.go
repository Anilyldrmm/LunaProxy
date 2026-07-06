package main

import (
	"embed"

	"github.com/AdguardTeam/urlfilter"
	"github.com/AdguardTeam/urlfilter/filterlist"
	"github.com/AdguardTeam/urlfilter/rules"
)

//go:embed assets/filters/adguard_base.txt assets/filters/adguard_turkish.txt
var filterFS embed.FS

// adblockEngine — network (blok) ve cosmetic (element gizleme) kurallarını
// tutan urlfilter motoru sarmalayıcısı.
type adblockEngine struct {
	engine *urlfilter.Engine
}

// newAdblockEngine — verilen ham filtre metinlerinden bir motor kurar.
// Testlerde küçük, deterministik kural setleriyle çağrılır.
func newAdblockEngine(ruleTexts ...string) (*adblockEngine, error) {
	var lists []filterlist.Interface
	for i, text := range ruleTexts {
		lists = append(lists, filterlist.NewString(&filterlist.StringConfig{
			RulesText: text,
			ID:        rules.ListID(i + 1),
		}))
	}
	storage, err := filterlist.NewRuleStorage(lists)
	if err != nil {
		return nil, err
	}
	return &adblockEngine{engine: urlfilter.NewEngine(storage)}, nil
}

// loadBundledAdblockEngine — go:embed edilen AdGuard Base + Turkish
// filtre listelerinden motor kurar.
func loadBundledAdblockEngine() (*adblockEngine, error) {
	base, err := filterFS.ReadFile("assets/filters/adguard_base.txt")
	if err != nil {
		return nil, err
	}
	tr, err := filterFS.ReadFile("assets/filters/adguard_turkish.txt")
	if err != nil {
		return nil, err
	}
	return newAdblockEngine(string(base), string(tr))
}

// ShouldBlock — istek URL'sinin bir network kuralına göre bloklanması
// gerekip gerekmediğini döner (whitelist kuralı varsa false).
func (e *adblockEngine) ShouldBlock(reqURL, sourceHost string) bool {
	req := rules.NewRequest(reqURL, "https://"+sourceHost+"/", rules.TypeOther)
	res := e.engine.MatchRequest(req)
	basic := res.GetBasicResult()
	return basic != nil && !basic.Whitelist
}

// CosmeticRules — host için element-gizleme CSS selector'larını
// (generic + o host'a özel) döner.
func (e *adblockEngine) CosmeticRules(host string) []string {
	res := e.engine.GetCosmeticResult(host, rules.CosmeticOptionCSS|rules.CosmeticOptionGenericCSS)
	var out []string
	out = append(out, res.ElementHiding.Generic...)
	out = append(out, res.ElementHiding.Specific...)
	return out
}

// adblock — uygulama genelinde kullanılan tekil motor örneği.
// Yüklenemezse nil kalır; çağıranlar nil kontrolü yapmalı.
var adblock *adblockEngine

// initAdblock — gömülü filtre listelerinden motoru kurar. Başarısız olursa
// reklam engelleme sessizce devre dışı kalır, proxy filtresiz çalışmaya devam eder.
func initAdblock() {
	e, err := loadBundledAdblockEngine()
	if err != nil {
		logWarn("adblock motoru yüklenemedi, reklam engelleme devre dışı: " + err.Error())
		return
	}
	mitmMu.Lock()
	adblock = e
	mitmMu.Unlock()
	logInfo("adblock motoru yüklendi")
}
