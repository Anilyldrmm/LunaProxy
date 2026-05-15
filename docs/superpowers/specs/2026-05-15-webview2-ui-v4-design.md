# SpAC3DPI v4 — WebView2 Profesyonel UI Tasarım Spec'i
Tarih: 2026-05-15
Durum: **ONAYLANDI**

---

## Genel Bakış

Walk (`lxn/walk`) kütüphanesi tamamen kaldırılıyor. Yerine:
- **Win32 `WS_POPUP` frameless pencere** — başlık çubuğu/border yok, köşe radius CSS ile
- **WebView2 (go-webview2)** — Chromium embed, tüm UI HTML/CSS/JS
- **github.com/getlantern/systray** — system tray yönetimi

Hedef: Windscribe VPN kalitesinde profesyonel, animasyonlu, interaktif dark-theme UI.

---

## Renk Paleti (Logo'dan Türetildi)

Logo: Mor/violet "S" harfi, glitch pixel efekti, koyu arka plan.

| CSS Değişkeni    | Hex       | Kullanım                          |
|-----------------|-----------|-----------------------------------|
| `--bg`          | `#0D0B14` | Ana pencere arka planı            |
| `--surface`     | `#13101E` | Sidebar, titlebar, kart arka planı|
| `--surface2`    | `#1A1628` | Elevated kartlar, inputlar        |
| `--surface3`    | `#221C38` | Tooltip, dropdown arka planı      |
| `--accent`      | `#8B3FBF` | Logo moru — birincil aksan        |
| `--accent-b`    | `#A855F7` | Hover, aktif, vurgular            |
| `--accent-glow` | `#C084FC` | Neon glow efektleri               |
| `--green`       | `#22C55E` | Bağlı durum                       |
| `--red`         | `#EF4444` | Hata, bağlı değil, DURDUR         |
| `--text`        | `#F1F0F5` | Birincil metin                    |
| `--sub`         | `#6B6490` | İkincil metin, etiketler          |
| `--border`      | `#2A2240` | İnce ayraçlar, kartlar            |

Font: `'Segoe UI', system-ui, sans-serif` — dış font yok.
Monospace: `Consolas, monospace` — IP, port, log, veri değerleri.

---

## Pencere

| Parametre   | Değer                                             |
|------------|---------------------------------------------------|
| Boyut      | 400 × 640 px, sabit (resize yok)                  |
| Stil       | `WS_POPUP \| WS_VISIBLE`, sıfır border            |
| Drop shadow| `CS_DROPSHADOW` + CSS `box-shadow` iç glow        |
| Radius     | 14px (`body { border-radius: 14px; overflow: hidden }`) |
| Konum      | Başlangıçta ekran ortası; konum kaydedilmez       |
| Sürükleme  | Titlebar üzerinde `WM_NCHITTEST → HTCAPTION`      |
| Minimize   | `ShowWindow SW_MINIMIZE`                           |
| Kapat      | Pencereyi gizle → tray'e geç (`appExiting=false`) |

---

## Genel Layout

```
┌──────────────────────────────────────────┐  400px
│ [S] SpAC3DPI  DPI Bypass         [—][✕] │  titlebar 44px
├──────┬───────────────────────────────────┤
│  ◉   │                                   │
│  ◈   │   CONTENT (344px)                 │  596px
│  ⚙   │   overflow-y: auto                │
│  📱  │                                   │
│  ☰   │                                   │
└──────┴───────────────────────────────────┘
  56px
```

### Titlebar (44px)
- Arka plan: `--surface`, alt: `1px solid --border`
- Sol: Logo kutusu (28×28, border-radius:8, mor gradient, `S` italic) + "SpAC3DPI" bold + "DPI Bypass Proxy" sub
- Sağ: `[—]` minimize, `[✕]` close butonu (hover: close → `--red`)
- Tüm titlebar `app-region: drag` / butonlar `no-drag`

### Sidebar (56px)
- Arka plan: `--surface`, sağ: `1px solid --border`
- 5 nav butonu, 40×40px, border-radius:10px
- Aktif: `background: rgba(139,63,191,.2)`, `box-shadow: inset 3px 0 0 --accent-b`
- Hover: `rgba(168,85,247,.1)`, color `--accent-glow`
- Tooltip: sağa çıkar, `--surface3` arka plan, 150ms fade

### İçerik Alanı (344px)
- `overflow-y: auto`, 4px scrollbar (`--border` rengi)
- Panel geçişi: `opacity + translateX(8px)` → `translateX(0)`, 200ms ease
- Sadece aktif panel görünür

---

## Panel 1 — Durum (Status)

### Hero Bölümü
- Logo: 84×84px, border-radius:20, mor gradient, `S` italic 44px
  - Glitch pixel detaylar: 4-5 küçük `div.px` farklı boyut/opaklıkta logo kenarlarında
  - Durdurulmuş: `filter: grayscale(0.6) brightness(0.7)`
  - Çalışıyor: `box-shadow: 0 0 0 2px rgba(168,85,247,.35), 0 10px 40px rgba(139,63,191,.6)`
- Ring: Logo çevresinde 2px border, çalışırken `ringPulse` animasyonu (2.5s ease-in-out ∞)
- Durum dot: 8px yeşil/kırmızı, `dotPulse` animasyonu (2s ∞)
- IP: `192.168.1.41 : 8888`, Consolas, gizli iken `opacity:0`

### Toggle Butonu
- Tam genişlik, 50px yükseklik, border-radius:50px
- BAŞLAT: mor gradient + mor glow shadow
- DURDUR: kırmızı gradient + kırmızı glow shadow
- Hover: `translateY(-2px)` + güçlendirilmiş shadow
- İkon: play/pause SVG

### İstatistik Satırı
```
SÜRE    BAĞLANTI    VERİ    HATA
1:23      4         12MB     0
```
- Değerler: 17px bold Consolas; etiketler: 9px uppercase `--sub`

### Servis Çubuğu
```
● Proxy  ● PAC  ● DPI: Bundle  ● QR
```
- 6px dot: yeşil=ok, kırmızı=hata
- `border-top/bottom: 1px solid --border`

### Detay Kartları
- **Servis Durumu**: HTTP Proxy, PAC Sunucu, GoodbyeDPI, DNS
- **Ağ Bilgisi**: Yerel IP, DPI Modu, DPI Kaynağı, Chunk, ISP

---

## Panel 2 — Cihazlar

Proxy üzerinden geçen benzersiz IP'ler listelenir.

### Cihaz Kartı
```
[📱 ikon]  192.168.1.105          12.4 MB  ●
           Aktif bağlantı         3 bağlantı
```
- Yeni cihaz gelince: `slideIn` animasyonu (translateY(-6px) → 0, 300ms)
- Hover: `border-color: rgba(168,85,247,.35)`
- Aktif dot: yeşil pulse

### Toplam İstatistik Kartı
- Cihaz sayısı, toplam veri, aktif bağlantı

### Backend Gereksinimi
`proxy.go`'da per-IP tracking:
```go
type deviceEntry struct {
    FirstSeen time.Time
    LastSeen  time.Time
    Bytes     int64
    ActiveConns int64
}
var devices sync.Map  // key: string (IP), value: *deviceEntry
```
`proxyHandler`'da istek gelince `r.RemoteAddr`'dan IP alınıp güncellenir.
`updateStatus` payload'una `devices []DeviceInfo` eklenir.
UI 2s'de bir `updateDevices(data)` çağrısıyla güncellenir.

---

## Panel 3 — Ayarlar

Her bölüm `<div class="card">` içinde.

### DPI Bypass Modu
Custom radio butonlar (4 seçenek):
- Turbo, Dengeli, Güçlü, Özel
- "Özel" seçilince text input görünür (custom flags)

### ISP & DNS
- `<select>` styled (dark bg, custom arrow, `--accent` focus border)
- ISP: Otomatik, Superonline, TTNet, Vodafone, Turkcell
- DNS: Değiştirilmedi, Cloudflare, Google, AdGuard, Quad9, OpenDNS

### DPI Kaynağı
Radio: Otomatik, Servis, Manuel, Devre Dışı
- "Manuel" seçilince path input + "Bul" butonu (file dialog IPC)

### Sistem
CSS toggle switch (checked → `--accent`):
- Sistem proxy'sini otomatik ayarla
- **Windows ile başlat** (registry `Run` key)
- **Arka planda çalış** (kapat → tray'e küçül)

### Ağ Portları
- Proxy Portu (varsayılan: 8888)
- PAC Portu (varsayılan: 8080)

### Kaydet Butonu
- Tam genişlik, mor gradient
- 2s başarı: yeşil `✔ Kaydedildi` animasyonu

---

## Panel 4 — Mobil

### PAC URL'leri
```
Router PAC URL (Önerilen)
[http://192.168.1.1:8080/proxy.pac] [Kopyala]

PC PAC URL (Alternatif)
[http://192.168.1.41:8080/proxy.pac] [Kopyala]
```
- `readonly` inputlar, Kopyala → clipboard IPC
- "Kopyala" → 1.8s sonra geri döner

### QR Kodu
- **QR, PAC URL'i DEĞİL `/setup` URL'ini kodlar:**
  `http://[localIP]:[pacPort]/setup`
- Mobil cihaz QR'ı okuyunca PAC dosyasını indirmeye çalışmaz
- `/setup` sayfası: mobil-uyumlu HTML, PAC URL gösterir + kopyala butonu + talimatlar
- QR: `go-qrcode` ile Go tarafında üretilir, base64 PNG olarak JS'e enjekte edilir

### `/setup` Sayfası (pac.go'da yeni endpoint)
PAC sunucusu `/setup` route'u ekler:
```
┌─────────────────────────────────┐
│  🟣 SpAC3DPI Proxy Kurulumu     │
│                                 │
│  PAC URL:                       │
│  http://192.168.1.41:8080/...   │
│  [ Kopyala ]                    │
│                                 │
│  📱 Android nasıl kurulur?      │
│  ├ Wi-Fi → Ağa uzun bas         │
│  ├ Gelişmiş seçenekler          │
│  ├ Proxy: Otomatik              │
│  └ URL'yi yapıştır              │
│                                 │
│  🍎 iOS nasıl kurulur?          │
│  ├ Ayarlar → Wi-Fi              │
│  ├ Ağ adına tıkla               │
│  ├ HTTP Proxy: Otomatik         │
│  └ URL'yi yapıştır              │
└─────────────────────────────────┘
```
Mobil tarayıcıda responsive, single-file HTML (inline CSS), `go:embed` ile gömülmez — runtime'da template ile üretilir.

### Kurulum Talimatları (Collapsible)
`<details><summary>` ile:
- Android kurulum
- iOS kurulum
- Windows kurulum

---

## Panel 5 — Loglar

### Araç Çubuğu
```
[Temizle] [Kopyala]  ☑ Otomatik kaydır    42 kayıt
```

### Log Alanı
- Koyu `#07090f` arka plan, Consolas 11px, line-height 1.85
- Renk kodlaması: INFO `#5b8fff`, WARN `#F59E0B`, ERR `--red`
- Zaman: `#2E3A50` (soluk)
- Otomatik kaydır: yeni satır → `scrollTop = scrollHeight`
- Max 500 kayıt DOM'da (eski satırlar silinir)
- Temizle: `postMessage({type:"clearLogs"})`

---

## IPC Sözleşmesi

### JS → Go (`window.chrome.webview.postMessage(JSON)`)

| type               | payload               | Eylem                                  |
|--------------------|-----------------------|----------------------------------------|
| `toggle`           | —                     | Proxy başlat/durdur                    |
| `saveSettings`     | Config alanları       | Config kaydet + yeniden başlat         |
| `clearLogs`        | —                     | Log buffer temizle                     |
| `copyToClipboard`  | `{text}`              | Windows clipboard'a yaz                |
| `windowMinimize`   | —                     | `ShowWindow SW_MINIMIZE`               |
| `windowHide`       | —                     | Gizle → tray                          |
| `windowExit`       | —                     | Temiz kapanış                          |
| `requestQR`        | —                     | QR base64 üret, `updateQR` gönder     |
| `openFileDialog`   | `{id}`                | Dosya seçici, `fileSelected` ile döner |
| `requestSettings`  | —                     | Config JSON → `loadSettings` gönder    |

### Go → JS (`webview.Eval(js)`)

| Fonksiyon        | Payload                        | Tetikleyici                    |
|-----------------|--------------------------------|--------------------------------|
| `updateStatus`  | `StatusPayload + DeviceList`   | Her 2s + değişiklikte          |
| `appendLogs`    | `[{time,level,msg}]`           | Her 2s                         |
| `updateQR`      | `{setupURL, pcURL, qrBase64}`  | `requestQR` alındığında        |
| `loadSettings`  | Config JSON                    | `requestSettings` alındığında  |
| `fileSelected`  | `{id, path}`                   | Dosya seçici kapandığında      |

### StatusPayload (genişletilmiş)
```json
{
  "running": true,
  "uptime": "1:23:45",
  "activeConns": 4,
  "totalConns": 142,
  "totalBytes": "12.4 MB",
  "errors": 0,
  "restarts": 0,
  "localIP": "192.168.1.41",
  "proxyPort": 8888,
  "pacPort": 8080,
  "pacUrl": "http://192.168.1.41:8080/proxy.pac",
  "setupUrl": "http://192.168.1.41:8080/setup",
  "dpiModeName": "Dengeli",
  "chunkSize": 40,
  "ispName": "Otomatik",
  "gdpiFlags": "-1 -p -q ...",
  "gdpiRunning": true,
  "dpiSourceLabel": "Bundle (dahili)",
  "dnsName": "Değiştirilmedi",
  "setSysProxy": false,
  "devices": [
    {"ip": "192.168.1.105", "bytes": 12991488, "activeConns": 3},
    {"ip": "192.168.1.112", "bytes": 3250176, "activeConns": 1}
  ]
}
```

---

## Animasyonlar & Efektler

| Eleman              | Animasyon                                             | Süre       |
|--------------------|-------------------------------------------------------|------------|
| Logo ring (çalışır)| `ringPulse`: border-color + box-shadow salınım        | 2.5s ∞     |
| Status dot (bağlı) | `dotPulse`: glow büyüme/küçülme                       | 2s ∞       |
| Panel geçişi       | `opacity:0 + translateX(8px)` → normal                | 200ms ease |
| Yeni cihaz         | `slideIn`: `translateY(-6px) opacity:0` → normal      | 300ms ease |
| Toggle btn hover   | `translateY(-2px)` + güçlü shadow                     | 200ms      |
| Save btn success   | Yeşil flash, `✔ Kaydedildi`                           | 2s         |
| Copy btn           | `✔ Kopyalandı` + yeşil renk                           | 1.8s       |
| Nav hover          | Background + color geçişi                             | 150ms      |

---

## System Tray

Kütüphane: `github.com/getlantern/systray`

- İkon: `getTrayIcon(active bool)` — mevcut `icon.go` (değişmiyor)
- Menü: **Arayüzü Aç** | — | **Başlat / Durdur** | — | **Çıkış**
- Sol tık: pencereyi göster/restore
- `Çıkış`: PAC→DIRECT + 2s router push + `os.Exit(0)`

---

## Dosya Mimarisi

```
main.go           ← bootstrap (walk import yok)
webview_win.go    ← Win32 WS_POPUP + WebView2 controller + sürükleme
ipc.go            ← JS→Go mesaj router + Go→JS Eval helper'lar
tray.go           ← systray yönetimi (ayrı goroutine)
icon.go           ← değişmiyor (ICO/PNG üretimi)
pac.go            ← /proxy.pac + /wpad.dat + /setup (YENİ endpoint)
proxy.go          ← per-IP device tracking eklendi
status.go         ← DeviceInfo + devices listesi StatusPayload'a eklendi
assets/
  index.html      ← SPA kabuğu (go:embed)
  style.css       ← tüm stiller (go:embed)
  app.js          ← UI durum makinesi, IPC, DOM (go:embed)
```

**Kaldırılan:** `ui.go` (Walk kütüphanesi tamamen çıkarılıyor)
**Değişmeyen:** `config.go`, `dns.go`, `goodbyedpi.go`, `network.go`,
`dpi_priority.go`, `logs.go`, `watchdog.go`, `sentinel.go`, `bundle.go`

---

## Performans Gereksinimleri

- Go backend minimal CPU: proxy sadece I/O, goroutine başına thread yok
- `updateStatus` tick: 2s — UI polling yok, Go push ediyor
- WebView2 render thread: oyunun DirectX/GPU'sundan tamamen izole (ayrı process)
- `sharedTransport` connection pool: zaten mevcut, 400 idle conn
- Buffer pool (`sync.Pool`): zaten mevcut, GC baskısı düşük
- Device tracking: `sync.Map` + atomic counter, mutex yok kritik path'te
- Log buffer: 500 entry ring buffer (zaten `logs.go`'da mevcut)
- Watchdog: 5s tick, port kontrolü haricinde CPU kullanımı sıfır

---

## Kapsam Dışı

- Açık/koyu tema geçişi (her zaman koyu)
- Pencere boyutlandırma
- Türkçe dışı dil desteği
- WebView2 Runtime installer/bundler (Windows 10/11'de zaten mevcut)
- Cihaz engelleme (IP bazlı blok) — gelecek sürüm
- Cihaz hostname çözümleme — gelecek sürüm

---

## Riskler

| Risk | Azaltma |
|------|---------|
| WebView2 Runtime yok | Başlangıçta kontrol, `MessageBox` uyarısı |
| go-webview2 CGO gerektirir | mingw-w64 toolchain gerekli |
| IPC JSON serialize hatası | `ipc.go`'da log + JS'e hata mesajı |
| Tray/WebView thread conflict | Channel üzerinden UI thread'e gönderim |
