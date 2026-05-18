# LunaProxy — Claude Hafızası

> Bu dosya Claude Code'un LunaProxy projesi için hafızasıdır.
> Her oturumun başında oku, her değişiklikten sonra güncelle.
> Repo: https://github.com/Anilyldrmm/LunaProxy

---

## Proje Özeti

**LunaProxy** — Windows için GoodbyeDPI tabanlı DPI bypass proxy.
- PC'de çalışır, aynı Wi-Fi'daki tüm cihazları (telefon, tablet, TV) korur
- PAC URL ile cihazlar otomatik yapılandırılır
- Router SSH kurulumu ile tüm ağ yönlendirilebilir
- Go + WebView2 (Windows only)

**Repo:** `https://github.com/Anilyldrmm/LunaProxy`
**Geliştirme branch'i:** `claude/add-mobile-data-support-f10DV`
**Go versiyonu:** 1.25.0

---

## Mimari

```
main.go          — uygulama başlangıcı, tray, watchdog
proxy.go         — HTTP proxy sunucusu (port 8888)
pac.go           — PAC sunucusu (port 8080), /setup sayfası, QR endpoint
network.go       — yerel IP, firewall, fetchPublicIP()
ipc.go           — JS↔Go mesajlaşması (goMessage / evalJS)
config.go        — JSON config (UserConfigDir/LunaProxy/config.json)
status.go        — StatusPayload, buildStatus()
goodbyedpi.go    — GoodbyeDPI proses yönetimi
dns.go           — DNS modu değiştirme
router.go        — SSH üzerinden router PAC kurulumu
mdns.go          — mDNS hostname çözümleme
updater.go       — GitHub Releases otomatik güncelleme
assets/ui.html   — tek dosya WebView2 arayüzü (tüm CSS+JS içinde)
```

### UI Panel Yapısı (sidebar)
| Panel ID | Sekme |
|---|---|
| panel-status | Durum (ana ekran) |
| panel-devices | Bağlı Cihazlar |
| panel-settings | Ayarlar |
| panel-mobile | Mobil (Wi-Fi Ağı / Mobil Veri sekmeleri) |
| panel-router | Router Entegrasyonu |
| panel-domains | Domain Filtresi |
| panel-logs | Loglar |

### IPC Mesajları (JS → Go)
| Mesaj | Açıklama |
|---|---|
| toggle | Proxy başlat/durdur |
| saveSettings | Config kaydet |
| requestQR | QR ve PAC URL'lerini al |
| requestMobileData | Genel IP + mobil proxy URL + QR |
| requestSettings | Config'i UI'a yükle |
| requestRouterDefaults | Router varsayılan değerlerini al |
| routerSetup | SSH ile router kurulumu |
| routerTest | Router PAC erişim testi |
| copyToClipboard | Panoya kopyala |
| clearLogs | Log temizle |
| setTheme | Tema değiştir (neutral/purple) |
| ispChanged | ISP öneri güncelle |
| applyUpdate | Güncellemeyi indir ve uygula |

### IPC Mesajları (Go → JS / evalJS)
| Fonksiyon | Açıklama |
|---|---|
| updateStatus(s) | 2 saniyede bir durum güncelle |
| updateQR(data) | QR PNG base64 + URL'ler |
| updateMobileData(d) | Genel IP + mobil proxy URL + QR |
| loadSettings(cfg) | Ayarları forma yükle |
| appendLogs(entries) | Yeni log satırları ekle |
| routerProgress(step) | SSH kurulum adımı |
| routerDone() | SSH kurulum tamamlandı |
| routerTestResult(msg) | Router test sonucu |
| showISPSuggestion(isp, mode) | ISP'ye göre mod önerisi |
| loadRouterDefaults(d) | Router form varsayılanları |

---

## Yapılan İşler (Kronolojik)

### v1.0.0 — Temel Sürüm
- GoodbyeDPI entegrasyonu, HTTP proxy, PAC sunucu
- WebView2 arayüzü, system tray, otomatik güncelleme
- Router SSH kurulumu (OpenWrt/Entware)
- ISP algılama (TTNet, Vodafone, Turkcell, Superonline)

### v1.0.19
- Proxy, PAC, mDNS, router, auto-update tam entegrasyon
- Modern UI (karanlık tema, mor tema)

### v1.0.20
- PowerShell güncelleme penceresi gizlendi
- `.invalid` domain CONNECT hatası logdan filtrelendi
- CI/CD: GitHub Actions release workflow, Inno Setup installer

### Mobil Veri Desteği (branch: claude/add-mobile-data-support-f10DV)
**Commit:** `f5a0446`

**Ne eklendi:**
- `network.go`: `fetchPublicIP()` — ipify / ifconfig.me / checkip.amazonaws.com ile genel IP alır
- `ipc.go`: `requestMobileData` handler — genel IP, proxy URL, QR kod döner
- `ui.html`: Mobil panele "Wi-Fi Ağı" / "Mobil Veri" sekme geçişi
  - Genel IP gösterimi + Yenile butonu
  - Proxy bağlantı adresi (GENEL_IP:8888) + kopyala
  - QR kod
  - Port yönlendirme adım adım rehber
  - Platform kurulum kılavuzları (Android/Every Proxy, iOS/Shadowrocket, Windows)

---

## Açık Sorunlar / Yapılacaklar

### Yüksek Öncelikli
- [ ] **Mobil Veri — Dinamik IP Sorunu:** Kullanıcının evdeki IP'si statik değil.
  - Çözüm: DDNS entegrasyonu (DuckDNS API ile otomatik güncelleme)
  - Hedef: LunaProxy arka planda DuckDNS'e kendi IP'sini kaydetsin
  - Ayarlar paneline DDNS bölümü eklenecek (subdomain + token alanları)
  - `network.go`'ya `updateDDNS(subdomain, token, ip string)` fonksiyonu

### Orta Öncelikli
- [ ] **Proxy Kimlik Doğrulama:** Port açıldığında proxy internete açık kalıyor.
  - Proxy'ye kullanıcı adı/şifre eklenmesi gerekiyor (Basic Auth)
  - PAC dosyası da auth bilgisini içermeli
- [ ] **UPnP Desteği:** Port yönlendirmeyi otomatik yapabilmek için
  - Router'ın UPnP özelliği varsa otomatik port açabilir

### Düşük Öncelikli
- [ ] **Bağlantı Testi:** Mobil Veri sekmesinde "Bağlantıyı Test Et" butonu
  - Genel IP:8888'e dışarıdan erişim olup olmadığını kontrol et
- [ ] **DDNS Durum Göstergesi:** Durum panelinde DDNS'in güncel olup olmadığı

---

## Teknik Notlar

### Config Dosyası Konumu
`%APPDATA%\LunaProxy\config.json`

### Port Bilgileri
- Proxy: 8888 (varsayılan, değiştirilebilir)
- PAC: 8080 (varsayılan, değiştirilebilir)
- Router PAC: 8090 (router üzerinde sabit)

### DPI Modları
- `turbo`: `-p -q -r -s -e %d`
- `balanced`: `-1 -p -q -r -s -e %d --new-mode`
- `powerful`: `-1 -p -q -r -s -e %d --new-mode --set-ttl 3 --wrong-chksum`

### ISP → Önerilen Mod
- TTNet / Vodafone → `powerful`
- Turkcell / Superonline → `balanced`

### Firewall Kuralları
LunaProxy başlarken `netsh advfirewall` ile LunaProxy_Proxy ve LunaProxy_PAC kurallarını otomatik ekler.

---

## Oturum Geçmişi

### Oturum 1 (2026-05-18)
**Konu:** Mobil veri desteği
**Yapılan:** fetchPublicIP, requestMobileData IPC, Mobil Veri sekmesi UI
**Kalan Sorun:** Dinamik IP — DuckDNS entegrasyonu yapılacak
**Branch:** `claude/add-mobile-data-support-f10DV` (push edildi)
