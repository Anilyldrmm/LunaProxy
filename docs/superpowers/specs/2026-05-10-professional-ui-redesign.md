# SpAC3DPI — Profesyonel UI Yeniden Tasarımı
Tarih: 2026-05-10

## Sorunlar

| # | Sorun | Kök Neden |
|---|-------|-----------|
| 1 | Native Windows başlık çubuğu (amatör görünüm) | walk MainWindow varsayılan ayarı |
| 2 | İkon oran bozulması (4:3 veya 9:16 görünüyor) | `ImageViewModeStretch` — ImageView kare olmadığında kaynağı eziyor |
| 3 | Metin okunaksız | Label'larda `TextColor` eksik → Windows default renk kullanılıyor |
| 4 | Windscribe VPN kalitesinde tasarım yok | Tab-based, plain layout |

## Tasarım Kararları

### 1. Frameless Pencere
- `WS_POPUP` stili: native chrome (titlebar, border, resize) tamamen kaldırılır
- `WS_EX_APPWINDOW`: görev çubuğunda görünür
- `SWP_FRAMECHANGED`: style değişikliği uygulanır
- Sabit boyut: **440 × 660 px** (resize yok)
- Ekran ortasında açılır

### 2. Custom Titlebar (48 px)
```
[ İkon 24×24 ] [ SpAC3DPI | DPI Bypass ]  ··  [ ─ ][ ✕ ]
```
- Arka plan: #0a0a14 (sidebar ile aynı)
- Sürükleme: `ReleaseCapture` + `SendMessage(WM_NCLBUTTONDOWN, HTCAPTION)`
  — Walk'un WndProc'una müdahale etmez, standart Win32 trick
- Minimize: `ShowWindow(SW_MINIMIZE)` — WS_POPUP ile çalışır
- Kapat: `onQuit()` — tray "Çıkış" ile aynı logic

### 3. İkon Düzeltmesi
- **`ImageViewModeStretch` → `ImageViewModeZoom`** tüm ImageView'larda
  — Walk, zoom modunda boyut oranını korur (pillarbox/letterbox, distortion yok)
- `loadLogoBitmap`: bitmap boyutu 52 → 72 px (gereksiz upscaling kalkıyor)

### 4. Metin Okunabilirliği
- Tüm `Label` widget'larında explicit `TextColor` (clrText veya clrSub)
- Status panel detay grupları: GroupBox → **custom card Composite**
  (walk'un native GroupBox'ı dark modda border/title rengini tam kontrol etmiyor)
- Settings panel: GroupBox korunur (interactive widget'lar içeriyor)

### 5. Renk Paleti
| Değişken | Hex | Kullanım |
|----------|-----|----------|
| clrBg | #0f0f1a | Ana pencere arka planı |
| clrSidebar | #0a0a14 | Sidebar + titlebar |
| clrCard | #16162a | Card composite arka planı |
| clrBtnOff | #7c3aed | BAŞLAT butonu |
| clrBtnOn | #dc4b4b | DURDUR butonu |
| clrText | #e0d0ff | Birincil metin |
| clrSub | #6a5a8a | İkincil metin / başlıklar |
| clrGreen | #48c774 | Bağlı durum |
| clrRed | #dc4b4b | Bağlı değil |

## Etkilenen Dosyalar

### icon.go
- `loadLogoBitmap`: bitmap 52 → 72 px

### ui.go
Yapı değişimi:
```
Before: MainWindow → [heroPanel] + TabWidget(4 tab)
After:  MainWindow → VBox
          ├── titleBar (48px, custom, draggable)
          └── HBox
              ├── sidebar (56px, 4 nav icons)
              └── content (4 show/hide panels)
```

Yeni fonksiyonlar:
- `makeFrameless()` — WS_POPUP + WS_EX_APPWINDOW + SWP_FRAMECHANGED
- `buildTitleBar()` — custom titlebar widget builder
- `onQuit()` — tray + titlebar close ortak logic
- `beginDrag()` — ReleaseCapture + SendMessage trick

Kaldırılan:
- `heroSection()`, `heroPanel`, `applyDarkHero()`
- `statusPage()`, `settingsPage()`, `mobilePage()`, `logsPage()` → build*Panel
- `TabWidget` ve tüm tab referansları

## Kısıtlamalar (Walk Framework)

| CSS Özelliği | Walk Karşılığı |
|---|---|
| border-radius | Yok (özel WM_PAINT gerekir, yapılmıyor) |
| hover transition | Yok (MouseEnter/Leave Composite'de yok) |
| box-shadow | Yok |
| pill radio button | Standart RadioButton |
| CSS toggle switch | Standart CheckBox |
