# SpAC3DPI UI Redesign — WebView2 Professional UI

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the existing `github.com/lxn/walk` GUI with a fully frameless WebView2-based UI that matches Windscribe's professional look and feel, using a purple accent theme aligned with the SpAC3DPI logo.

**Architecture:** Go business logic is unchanged. The walk dependency is removed entirely. A Win32 frameless popup window hosts a Microsoft WebView2 (Chromium) renderer. System tray is managed via `github.com/getlantern/systray`. HTML/CSS/JS assets are embedded in the binary via `//go:embed`.

**Tech Stack:** Go 1.21, `github.com/jchv/go-webview2`, `github.com/getlantern/systray`, Win32 syscall, HTML5 + CSS3 + Vanilla JS, `//go:embed`

---

## 1. Window

- **Size:** 380×600 px, fixed (not resizable)
- **Style:** `WS_POPUP | WS_VISIBLE` — no title bar, no border
- **Border radius:** 12 px (CSS `border-radius` on `<body>`)
- **Drop shadow:** Win32 `CS_DROPSHADOW` class style
- **Dragging:** CSS `app-region: drag` on the header bar; close/minimize buttons excluded with `app-region: no-drag`
- **Position:** Centered on primary monitor at startup; last position not persisted
- **Window controls:** Custom HTML buttons — minimize (`_`) and close (`✕`) in top-right corner
  - Minimize: calls `window.chrome.webview.postMessage({type:"windowMinimize"})`
  - Close: hides window to tray (does not exit), calls `{type:"windowHide"}`
- **Always on top:** No

---

## 2. Layout

```
┌──────────────────────────────────────────┐
│ [S]  SpAC3DPI    DPI Bypass   [_]  [✕]  │  ← header, 48px, draggable
├──────┬───────────────────────────────────┤
│  🏠  │                                   │
│  ⚙   │         CONTENT PANEL            │
│  📱  │         (switches per nav)        │
│  📋  │                                   │
│      │                                   │
└──────┴───────────────────────────────────┘
 60px        320px
```

### Sidebar
- Width: 60 px
- Background: `#0a0a14`
- 4 nav icons (SVG): Home/Status, Settings, Mobile, Logs
- Active state: purple circle highlight `#7c3aed` at 30% opacity behind icon, icon color `#e0d0ff`
- Inactive state: icon color `#6a5a8a`
- Hover state: icon color `#9d5bff`
- No text labels — tooltips on hover

### Content Panel
- Background: `#0f0f1a`
- Padding: 20 px

---

## 3. Colour Palette

| CSS Variable | Hex | Usage |
|---|---|---|
| `--bg` | `#0f0f1a` | Main background |
| `--sidebar` | `#0a0a14` | Sidebar |
| `--card` | `#1a1a2e` | Hero / card backgrounds |
| `--accent` | `#7c3aed` | Purple accent |
| `--accent-hover` | `#9d5bff` | Hover state |
| `--btn-stop` | `#dc4b4b` | Stop button (red) |
| `--btn-stop-hover` | `#e06060` | Stop button hover |
| `--green` | `#48c774` | Connected state |
| `--red` | `#dc4b4b` | Disconnected state |
| `--text` | `#e0d0ff` | Primary text |
| `--sub` | `#6a5a8a` | Secondary text |
| `--border` | `#2a2a40` | Subtle dividers |

Font: `Segoe UI`, fallback `system-ui`. No external fonts fetched.

---

## 4. Status Panel (Default View)

```
         [logo — 80×80 px bitmap]
              SpAC3DPI
           DPI Bypass Proxy

        ●  BAĞLI DEĞİL
         192.168.1.41:8888

  ┌──────────────────────────────────┐
  │         ▶   BAŞLAT              │   ← pill button, 52px height
  └──────────────────────────────────┘

    SÜRE          BAĞ          VERİ
    0:00           0            0 B

  ──────────────────────────────────
  Proxy ✔   PAC ✔   DPI: Bundle   QR ✔
```

- Logo: Go base64-encodes `rawLogoBytes` at startup and injects `window.__logoB64 = "<base64>"` via `webview.Init()`. HTML renders `<img id="logo" src="data:image/png;base64,...">`. Stopped state: CSS `filter: grayscale(100%)` applied via JS on status update. Running state: filter removed.
- Status dot + text: `● BAĞLI` green / `● BAĞLI DEĞİL` red, 22 px bold
- IP:Port line: 13 px, `--sub` color, hidden when stopped
- Toggle button: full-width pill (`border-radius: 50px`), `--accent` when stopped, `--btn-stop` when running
- Stats row: 3 columns, value bold 16px `--text`, label 11px `--sub` uppercase
- Status bar: 12px, `--sub`, divider line above

---

## 5. Settings Panel

Sections rendered as dark cards (`--card` background, `border-radius: 8px`, padding 16px).

### DPI Bypass Modu
- Radio buttons (custom styled): Turbo / Dengeli / Güçlü / Özel
- Custom flags: `<input type="text">` shown only when Özel selected
- Chunk size: styled `<select>` — 4 / 8 / 16 / 40 byte
- ISP: styled `<select>` — Auto / Superonline / TTNet / Vodafone / Turkcell

### DNS Şifreleme
- Styled `<select>`: Değiştirilmesin / Cloudflare / Google / AdGuard / Quad9 / OpenDNS

### DPI Kaynağı
- Radio buttons: Otomatik / Sistem Servisi / Manuel Yol / Devre Dışı
- Manuel yol: text input + "Bul" button (shown only when Manuel selected)

### Sistem
- Toggle switches (CSS): "Windows sistem proxy'sini otomatik ayarla", "Windows ile otomatik başlat"

### Portlar
- Number inputs: Proxy port, PAC port

### Save Button
- Full-width purple button: `💾 Kaydet ve Uygula`
- Success feedback: green checkmark text for 3 seconds

---

## 6. Mobile Panel

- Router PAC URL: read-only input + Kopyala button
- PC PAC URL: read-only input + Kopyala button
- QR code: `<img>` element, 200×200, generated server-side as base64 PNG via `go-qrcode`, sent over IPC on panel open
- Setup guides: 3 collapsible cards — Android, iOS, Windows

---

## 7. Logs Panel

- Header row: Temizle + Kopyala buttons, auto-scroll checkbox, record count
- Log list: `<div>` with overflow-y scroll, monospace font (Consolas), background `#080810`
- Each log entry: `<span class="time">` `<span class="level INFO|WARN|ERROR">` `<span class="msg">`
- Level colours: INFO `#7a9fff`, WARN `#f0b429`, ERROR `#dc4b4b`
- Auto-scroll: `scrollTop = scrollHeight` on new entries when checkbox checked
- Max displayed entries: 500 (older entries dropped from DOM to avoid memory growth)

---

## 8. IPC Contract

### JS → Go (`window.chrome.webview.postMessage(JSON)`)

| type | payload | action |
|---|---|---|
| `toggle` | — | start or stop proxy |
| `saveSettings` | `{ dpiMode, chunkSize, isp, customFlags, dnsMode, setSystemProxy, autoStart, dpiSource, gdpiPath, proxyPort, pacPort }` | save config + restart if running |
| `clearLogs` | — | clear log buffer |
| `copyToClipboard` | `{ text }` | Go writes to Windows clipboard |
| `windowMinimize` | — | minimize Win32 window |
| `windowHide` | — | hide window (minimize to tray) |
| `windowExit` | — | clean shutdown (same as tray Çıkış) |
| `openBrowser` | `{ url }` | Go calls rundll32 to open URL |
| `requestQR` | — | Go generates QR, sends `updateQR` back |
| `requestStatus` | — | Go sends current status immediately |

### Go → JS (`webview.Eval(js)`)

| Function | Payload | Trigger |
|---|---|---|
| `updateStatus(s)` | `{ running, localIP, proxyPort, pacPort, uptime, activeConns, totalBytes, totalConns, errors, restarts, gdpiRunning, dpiSourceLabel, dnsMode, setSystemProxy, pacUrl, dpiModeName, chunkSize, isp, gdpiFlags }` | every 2 s + on state change |
| `updateLogs(entries)` | `[{ time, level, msg }]` | every 2 s |
| `updateQR(data)` | `{ routerURL, pcURL, qrBase64 }` | on `requestQR` |
| `loadSettings(cfg)` | full Config struct as JSON | on settings panel open |

---

## 9. System Tray

- Library: `github.com/getlantern/systray`
- Icon: generated from `icon.go` — `getTrayIcon(active bool)` with green/red status dot (already implemented)
- Menu items: `Arayüzü Aç`, separator, `Başlat / Durdur`, separator, `Çıkış`
- Left-click: show/restore window
- `Çıkış`: same shutdown sequence as current (PAC→DIRECT, 2s router push, os.Exit)

---

## 10. File Structure

```
main.go            — entry: Win32 window creation, WebView2 init, main loop
webview_win.go     — Win32 frameless window helpers (create, show, hide, minimize)
ipc.go             — WebMessage handler: routes JS→Go messages, Eval helpers
tray.go            — systray setup and menu (replaces walk tray code)
icon.go            — ICO/PNG generation (unchanged, reused by tray.go)
assets/
  index.html       — single-page app shell
  style.css        — all styles (CSS variables, layout, components)
  app.js           — UI state machine, IPC send/receive, DOM updates
```

Files **removed**: `ui.go` (walk UI — replaced entirely)  
Files **unchanged**: `proxy.go`, `pac.go`, `config.go`, `dns.go`, `gdpi.go`, `network.go`, `dpi_priority.go`, `log.go`, `watchdog.go`, `sentinel.go`  
Files **modified**: `main.go` (remove walk bootstrap, add WebView2 init), `go.mod` (add go-webview2 + systray, remove walk)

---

## 11. Dependencies

```
# Add
github.com/jchv/go-webview2    v0.0.0-latest
github.com/getlantern/systray  v1.2.2

# Remove
github.com/lxn/walk
github.com/lxn/win
```

WebView2 Runtime is bundled with Windows 11 and Edge-updated Windows 10 systems. No separate installer required for target users. If WebView2 is not available, `go-webview2` returns an error at init; show a Win32 `MessageBox` ("Microsoft Edge WebView2 Runtime kurulu değil. Lütfen yükleyin.") and exit.

---

## 12. Single-Instance Guard

Unchanged: Windows named mutex `"Local\\SpAC3DPI_v3_SingleInstance"` in `sentinel.go`.

---

## 13. Out of Scope

- Animations / transitions (CSS transitions on toggle button only — colour change)
- Dark/light theme toggle (always dark)
- Window resizing
- Localization beyond Turkish
- WebView2 Runtime installer/bundler
