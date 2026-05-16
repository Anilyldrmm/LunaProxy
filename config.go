package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Config — tüm uygulama ayarları (JSON olarak kaydedilir)
type Config struct {
	// Ağ
	ProxyPort int `json:"proxy_port"`
	PACPort   int `json:"pac_port"`

	// DPI bypass
	DPIMode     string `json:"dpi_mode"`     // turbo | balanced | powerful
	ChunkSize   int    `json:"chunk_size"`   // 4 | 8 | 16 | 40 (byte)
	ISP         string `json:"isp"`          // ek ISP bayrağı katmanı (opsiyonel)
	CustomFlags string `json:"custom_flags"` // DPIMode="custom" ise

	// GoodbyeDPI yönetimi
	GDPIPath  string `json:"gdpi_path"`
	DPISource string `json:"dpi_source"` // "auto" | "service" | "manual" | "disabled"

	// DNS
	DNSMode string `json:"dns_mode"` // unchanged | cloudflare | google | adguard | quad9 | opendns

	// Sistem proxy
	SetSystemProxy bool `json:"set_system_proxy"` // Windows sistem proxy'sini otomatik ayarla

	// Başlangıç
	AutoStart      bool `json:"auto_start"`       // Windows startup kaydı
	ProxyAutoStart bool `json:"proxy_auto_start"` // App açılınca proxy'yi otomatik başlat

	// Bypass domain filtresi — BypassEnabled=true iken sadece listedeki
	// domain'ler proxy'ye yönlendirilir; false ise tüm trafik proxy'den geçer.
	BypassEnabled bool     `json:"bypass_enabled"`
	BypassDomains []string `json:"bypass_domains"`

	// UI
	Theme string `json:"theme"` // "neutral" | "purple"
}

// defaultBypassDomains — yeni kurulumda ön tanımlı gelen domain listesi (Discord + Roblox).
var defaultBypassDomains = []string{
	// Discord
	"discord.com",
	"discordapp.com",
	"discord.gg",
	"discordapp.net",
	"gateway.discord.gg",
	"cdn.discordapp.com",
	"media.discordapp.net",
	"dl.discordapp.net",
	// Roblox
	"roblox.com",
	"rbxcdn.com",
	"roproxy.com",
	"rbxtrk.com",
	"apis.roblox.com",
	"assetdelivery.roblox.com",
}

// ── DPI Modları ───────────────────────────────────────────────────────────────

// dpiModeFlags — chunk size yer tutucusu %d içerir
var dpiModeFlags = map[string]string{
	"turbo":    "-p -q -r -s -e %d",
	"balanced": "-1 -p -q -r -s -e %d --new-mode",
	"powerful": "-1 -p -q -r -s -e %d --new-mode --set-ttl 3 --wrong-chksum",
}

var dpiModeNames = map[string]string{
	"turbo":    "Turbo — Hız öncelikli, temel bypass",
	"balanced": "Dengeli — TLS chunking, standart koruma",
	"powerful": "Güçlü — Paket parçalama + TTL desync",
	"custom":   "Özel — Manuel bayraklar",
}

// ISP ek bayrakları (DPI modunun üzerine eklenir)
var ispPresets = map[string]string{
	"superonline": "",                    // standart mod yeterli
	"ttnet":       " --set-ttl 3",        // TTNet için TTL gerekli
	"vodafone":    " --set-ttl 3",        // Vodafone için TTL gerekli
	"turkcell":    "",                    // standart mod yeterli
	"auto":        "",                    // otomatik (ISP ek bayrağı yok)
}

// ispRecommendedMode — ISP'ye göre önerilen DPI bypass modu.
var ispRecommendedMode = map[string]string{
	"ttnet":       "powerful",
	"vodafone":    "powerful",
	"turkcell":    "balanced",
	"superonline": "balanced",
	"auto":        "",
}

var ispNames = map[string]string{
	"auto":        "Otomatik",
	"superonline": "Superonline / UltraNet",
	"ttnet":       "Türk Telekom (TTNet)",
	"vodafone":    "Vodafone TR",
	"turkcell":    "Turkcell",
}

// ── Config erişim ─────────────────────────────────────────────────────────────

var (
	cfgMu   sync.RWMutex
	current Config
)

func defaultConfig() Config {
	return Config{
		ProxyPort:      8888,
		PACPort:        8080,
		DPIMode:        "balanced",
		ChunkSize:      40,
		ISP:            "auto",
		DNSMode:        "unchanged",
		DPISource:      "auto",
		SetSystemProxy: false,
		BypassEnabled:  true,
		BypassDomains:  defaultBypassDomains,
	}
}

func configFilePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "LunaProxy", "config.json")
}

func loadConfig() {
	c := defaultConfig()
	fileExists := false
	if data, err := os.ReadFile(configFilePath()); err == nil {
		fileExists = true
		json.Unmarshal(data, &c)
	}
	if c.ProxyPort == 0 { c.ProxyPort = 8888 }
	if c.PACPort == 0 { c.PACPort = 8080 }
	if c.DPIMode == "" { c.DPIMode = "balanced" }
	if c.ChunkSize == 0 { c.ChunkSize = 40 }
	if c.ISP == "" { c.ISP = "auto" }
	if c.DPISource == "" { c.DPISource = "auto" }
	// Mevcut config'de bypass_domains yoksa varsayılanları yükle.
	// Yeni kurulum ise BypassEnabled zaten defaultConfig'de true geldi.
	// Eski kurulum (dosya var, alan yok) için mevcut davranış korunur: enabled=false.
	if c.BypassDomains == nil {
		c.BypassDomains = defaultBypassDomains
		if !fileExists {
			c.BypassEnabled = true
		}
	}
	cfgMu.Lock()
	current = c
	cfgMu.Unlock()
}

func getConfig() Config {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return current
}

func setConfig(c Config) error {
	cfgMu.Lock()
	current = c
	cfgMu.Unlock()
	return saveConfig()
}

func saveConfig() error {
	c := getConfig()
	path := configFilePath()
	os.MkdirAll(filepath.Dir(path), 0755)
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// activeGDPIFlags — DPI modu + chunk size + ISP ek bayrağından nihai GoodbyeDPI bayraklarını üretir
func activeGDPIFlags() string {
	c := getConfig()

	// Manuel mod
	if c.DPIMode == "custom" {
		return c.CustomFlags
	}

	// DPI modu temel bayrakları
	baseFmt, ok := dpiModeFlags[c.DPIMode]
	if !ok {
		baseFmt = dpiModeFlags["balanced"]
	}

	chunk := c.ChunkSize
	if chunk <= 0 {
		chunk = 40
	}
	flags := fmt.Sprintf(baseFmt, chunk)

	// ISP ek bayrağı (varsa)
	if extra, ok := ispPresets[c.ISP]; ok && extra != "" {
		// TTL zaten powerful modda var, ekleme
		if c.DPIMode != "powerful" {
			flags += extra
		}
	}

	return flags
}
