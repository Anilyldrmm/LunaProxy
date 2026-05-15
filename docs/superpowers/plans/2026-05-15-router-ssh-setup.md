# Router SSH Entegrasyonu Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Uygulama içinde "Router" paneli ekle; kullanıcı SSH bilgilerini girer, Go SSH ile OpenWrt/Entware router'a bağlanır ve PAC+heartbeat CGI scriptlerini otomatik kurar.

**Architecture:** Yeni `router_setup.go` dosyası SSH bağlantısı, OpenWrt tespiti ve script kurulumunu üstlenir. `ipc.go`'ya iki yeni mesaj eklenir (`routerSetup`, `routerTest`). `assets/ui.html`'e yeni "Router" panel + sidebar nav eklenir. Kurulum adımları JS'e gerçek zamanlı olarak push edilir (`routerProgress()`).

**Tech Stack:** `golang.org/x/crypto/ssh` (SSH client), mevcut `go-webview2` IPC altyapısı, lighttpd + BusyBox sh (OpenWrt/Entware tarafında)

---

## Dosya Haritası

| Dosya | Değişiklik |
|---|---|
| `router_setup.go` | YENİ — SSH connect, OpenWrt tespiti, script kurulum fonksiyonları |
| `ipc.go` | MODIFY — `routerSetup` ve `routerTest` case'leri eklenir |
| `assets/ui.html` | MODIFY — Router sidebar nav + panel eklenir |
| `go.mod` / `go.sum` | MODIFY — `golang.org/x/crypto` eklenir |

---

## Task 1: golang.org/x/crypto Bağımlılığı

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Paketi ekle**

```powershell
cd "C:\Users\anil_\OneDrive\Masaüstü\mobilDPI"
go get golang.org/x/crypto@latest
```

Beklenen çıktı: `go: added golang.org/x/crypto vX.Y.Z`

- [ ] **Step 2: go.mod'u doğrula**

go.mod dosyasında şu satır olmalı:
```
golang.org/x/crypto vX.Y.Z
```

- [ ] **Step 3: Derlemeyi kontrol et**

```powershell
go build -ldflags "-H windowsgui" -o SpAC3DPI.exe .
```

Beklenen: hata yok.

---

## Task 2: router_setup.go — SSH Kurulum Mantığı

**Files:**
- Create: `router_setup.go`

Bu dosya SSH bağlantısı, OpenWrt tespiti ve tüm scriptlerin kurulumunu yapar.
Router tarafında kurulacaklar:
- `/opt/share/pac/lighttpd.conf` — port 8090, CGI destekli
- `/opt/share/pac/proxy.pac` — heartbeat kontrolü, dinamik PROXY/DIRECT
- `/opt/share/pac/hb.sh` — heartbeat alıcı CGI
- `/opt/share/pac/update.sh` — PC'den mode güncelleme CGI
- `/opt/etc/init.d/S80spac3dpi` — lighttpd otomatik başlatma

- [ ] **Step 1: Dosyayı oluştur**

`router_setup.go` dosyasını `C:\Users\anil_\OneDrive\Masaüstü\mobilDPI\` altına yaz:

```go
//go:build windows

package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// RouterSetupCfg — SSH bağlantı bilgileri.
type RouterSetupCfg struct {
	Host     string
	Port     int
	User     string
	Password string
}

// RouterStep — kurulum adımı; JS'e push edilir.
type RouterStep struct {
	Msg    string `json:"msg"`
	Status string `json:"status"` // "info" | "ok" | "error"
}

// ── Script içerikleri ────────────────────────────────────────────────────────

const routerLighttpdConf = `server.document-root = "/opt/share/pac"
server.port = 8090
server.pid-file = "/tmp/spac3dpi_lighttpd.pid"
server.errorlog = "/tmp/spac3dpi_lighttpd.log"
server.modules = ("mod_cgi", "mod_accesslog")
accesslog.filename = "/dev/null"
cgi.assign = (".sh" => "/opt/bin/sh", ".pac" => "/opt/bin/sh")
mimetype.assign = (
  ".pac" => "application/x-ns-proxy-autoconfig",
  ".dat" => "application/x-ns-proxy-autoconfig"
)
`

const routerProxyPac = `#!/bin/sh
PATH=/opt/bin:/opt/sbin:/bin:/sbin:/usr/bin:/usr/sbin
printf 'Content-Type: application/x-ns-proxy-autoconfig\r\nCache-Control: no-store,no-cache\r\n\r\n'
NOW=$(date +%s)
HB=/tmp/spac3dpi_hb
PX=/tmp/spac3dpi_proxy
if [ -f "$HB" ] && [ -f "$PX" ]; then
  HB_T=$(cat "$HB" 2>/dev/null)
  DIFF=$((NOW - HB_T))
  if [ "$DIFF" -lt 30 ]; then
    ADDR=$(cat "$PX" 2>/dev/null)
    printf 'function FindProxyForURL(url,host){return "PROXY %s; DIRECT";}\n' "$ADDR"
    exit 0
  fi
fi
printf 'function FindProxyForURL(url,host){return "DIRECT";}\n'
`

const routerHbSh = `#!/bin/sh
PATH=/opt/bin:/opt/sbin:/bin:/sbin:/usr/bin:/usr/sbin
printf 'Content-Type: text/plain\r\n\r\n'
date +%s > /tmp/spac3dpi_hb
printf 'ok\n'
`

const routerUpdateSh = `#!/bin/sh
PATH=/opt/bin:/opt/sbin:/bin:/sbin:/usr/bin:/usr/sbin
printf 'Content-Type: text/plain\r\n\r\n'
MODE=$(echo "$QUERY_STRING" | sed 's/.*mode=//;s/&.*//')
IP=$(echo "$QUERY_STRING" | sed 's/.*ip=//;s/&.*//')
PORT=$(echo "$QUERY_STRING" | sed 's/.*port=//;s/&.*//')
if [ "$MODE" = "proxy" ] && [ -n "$IP" ] && [ -n "$PORT" ]; then
  printf '%s:%s' "$IP" "$PORT" > /tmp/spac3dpi_proxy
  date +%s > /tmp/spac3dpi_hb
else
  rm -f /tmp/spac3dpi_proxy
fi
printf 'ok\n'
`

const routerInitScript = `#!/bin/sh /etc/rc.common
START=80
STOP=20
USE_PROCD=0
start() {
  if [ -f /tmp/spac3dpi_lighttpd.pid ]; then
    kill "$(cat /tmp/spac3dpi_lighttpd.pid)" 2>/dev/null
    rm -f /tmp/spac3dpi_lighttpd.pid
  fi
  /opt/bin/lighttpd -f /opt/share/pac/lighttpd.conf
}
stop() {
  if [ -f /tmp/spac3dpi_lighttpd.pid ]; then
    kill "$(cat /tmp/spac3dpi_lighttpd.pid)" 2>/dev/null
    rm -f /tmp/spac3dpi_lighttpd.pid
  fi
}
`

// ── SSH yardımcıları ─────────────────────────────────────────────────────────

func sshConnect(cfg RouterSetupCfg) (*ssh.Client, error) {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	config := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            []ssh.AuthMethod{ssh.Password(cfg.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // LAN içi bağlantı, güvenli
		Timeout:         10 * time.Second,
	}
	return ssh.Dial("tcp", addr, config)
}

func sshRun(client *ssh.Client, cmd string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	out, err := session.CombinedOutput(cmd)
	return strings.TrimSpace(string(out)), err
}

func sshWriteFile(client *ssh.Client, path, content string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	session.Stdin = strings.NewReader(content)
	return session.Run(fmt.Sprintf("cat > %s", path))
}

// ── Tespit ──────────────────────────────────────────────────────────────────

func isOpenWrt(client *ssh.Client) bool {
	out, err := sshRun(client, "cat /etc/openwrt_release 2>/dev/null | head -1")
	return err == nil && strings.Contains(out, "OpenWrt")
}

func hasEntware(client *ssh.Client) bool {
	_, err := sshRun(client, "ls /opt/bin/opkg 2>/dev/null")
	return err == nil
}

func hasLighttpd(client *ssh.Client) bool {
	_, err := sshRun(client, "ls /opt/bin/lighttpd 2>/dev/null")
	return err == nil
}

// ── Ana kurulum fonksiyonu ───────────────────────────────────────────────────

// RouterInstall — SSH üzerinden router'a PAC+heartbeat scriptlerini kurar.
// Her adımda progress callback çağrılır; hata olursa kısa mesajla döner.
func RouterInstall(cfg RouterSetupCfg, progress func(RouterStep)) error {
	progress(RouterStep{"Router'a bağlanılıyor...", "info"})
	client, err := sshConnect(cfg)
	if err != nil {
		return fmt.Errorf("SSH bağlantısı başarısız: %w", err)
	}
	defer client.Close()
	progress(RouterStep{"SSH bağlantısı kuruldu", "ok"})

	if !isOpenWrt(client) {
		return fmt.Errorf("bu cihaz OpenWrt değil — desteklenmiyor")
	}
	progress(RouterStep{"OpenWrt tespit edildi", "ok"})

	if !hasEntware(client) {
		return fmt.Errorf("Entware bulunamadı — önce Entware kurulmalı")
	}
	progress(RouterStep{"Entware mevcut", "ok"})

	if !hasLighttpd(client) {
		progress(RouterStep{"lighttpd kuruluyor...", "info"})
		if out, err := sshRun(client, "opkg update && opkg install lighttpd"); err != nil {
			return fmt.Errorf("lighttpd kurulum hatası: %s", out)
		}
		progress(RouterStep{"lighttpd kuruldu", "ok"})
	} else {
		progress(RouterStep{"lighttpd mevcut", "ok"})
	}

	progress(RouterStep{"Dosyalar oluşturuluyor...", "info"})
	if _, err := sshRun(client, "mkdir -p /opt/share/pac"); err != nil {
		return fmt.Errorf("dizin oluşturulamadı: %w", err)
	}

	files := []struct {
		path    string
		content string
		exec    bool
	}{
		{"/opt/share/pac/lighttpd.conf", routerLighttpdConf, false},
		{"/opt/share/pac/proxy.pac", routerProxyPac, true},
		{"/opt/share/pac/hb.sh", routerHbSh, true},
		{"/opt/share/pac/update.sh", routerUpdateSh, true},
		{"/opt/etc/init.d/S80spac3dpi", routerInitScript, true},
	}
	for _, f := range files {
		if err := sshWriteFile(client, f.path, f.content); err != nil {
			return fmt.Errorf("%s yazılamadı: %w", f.path, err)
		}
		if f.exec {
			sshRun(client, fmt.Sprintf("chmod +x %s", f.path))
		}
	}
	progress(RouterStep{"Scriptler yazıldı", "ok"})

	// Varsa eski lighttpd örneğini öldür, yenisini başlat
	sshRun(client, `if [ -f /tmp/spac3dpi_lighttpd.pid ]; then kill $(cat /tmp/spac3dpi_lighttpd.pid) 2>/dev/null; sleep 1; fi`)
	if out, err := sshRun(client, "/opt/bin/lighttpd -f /opt/share/pac/lighttpd.conf"); err != nil {
		return fmt.Errorf("lighttpd başlatılamadı: %s", out)
	}
	progress(RouterStep{"lighttpd başlatıldı (port 8090)", "ok"})

	// HTTP ile doğrula
	time.Sleep(500 * time.Millisecond)
	testURL := fmt.Sprintf("http://%s:8090/proxy.pac", cfg.Host)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(testURL)
	if err != nil {
		return fmt.Errorf("kurulum doğrulanamadı: %s erişilemiyor", testURL)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("proxy.pac HTTP %d döndü", resp.StatusCode)
	}
	progress(RouterStep{fmt.Sprintf("Doğrulandı — %s erişilebilir", testURL), "ok"})

	return nil
}

// RouterTest — önceden kurulu bir router'ın PAC endpoint'ini test eder.
func RouterTest(host string) error {
	url := fmt.Sprintf("http://%s:8090/proxy.pac", host)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(url)
	if err != nil {
		return fmt.Errorf("%s erişilemiyor", url)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 2: Derle (import hatası yok mu?)**

```powershell
go build -ldflags "-H windowsgui" -o SpAC3DPI.exe .
```

Beklenen: hata yok.

- [ ] **Step 3: Script içerik testi yaz**

`router_setup_test.go` dosyasını oluştur:

```go
package main

import "testing"

func TestRouterScriptContents(t *testing.T) {
	// proxy.pac shebang ve PATH kontrolü
	if !contains(routerProxyPac, "#!/bin/sh") {
		t.Error("proxy.pac: shebang eksik")
	}
	if !contains(routerProxyPac, "/opt/bin:/opt/sbin") {
		t.Error("proxy.pac: PATH eksik")
	}
	if !contains(routerProxyPac, "DIRECT") {
		t.Error("proxy.pac: DIRECT fallback eksik")
	}

	// hb.sh heartbeat yazıyor mu?
	if !contains(routerHbSh, "date +%s > /tmp/spac3dpi_hb") {
		t.Error("hb.sh: heartbeat yazma komutu eksik")
	}

	// update.sh mode=proxy ile dosya yazıyor mu?
	if !contains(routerUpdateSh, "spac3dpi_proxy") {
		t.Error("update.sh: proxy dosyası yazma eksik")
	}

	// lighttpd.conf port 8090 mı?
	if !contains(routerLighttpdConf, "8090") {
		t.Error("lighttpd.conf: port 8090 eksik")
	}
}

func contains(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 && (func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
```

- [ ] **Step 4: Testi çalıştır**

```powershell
go test -run TestRouterScriptContents -v .
```

Beklenen:
```
--- PASS: TestRouterScriptContents (0.00s)
PASS
```

- [ ] **Step 5: Commit**

```powershell
git add router_setup.go router_setup_test.go go.mod go.sum
git commit -m "feat: router SSH kurulum logic — router_setup.go"
```

---

## Task 3: IPC Handler'ları — ipc.go

**Files:**
- Modify: `ipc.go` (mevcut `switch msg.Type` bloğuna 2 case ekle)

`ipc.go`'daki mevcut `switch msg.Type` bloğunun son case'inden önce ekle:

- [ ] **Step 1: `routerSetup` ve `routerTest` case'lerini ekle**

`ipc.go` içinde `case "applyUpdate":` bloğundan sonra, kapanış `}` parantezinden önce şunu ekle:

```go
	case "routerSetup":
		var p RouterSetupCfg
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			logWarn("routerSetup parse: " + err.Error())
			return
		}
		if p.Port == 0 {
			p.Port = 22
		}
		go func() {
			err := RouterInstall(p, func(step RouterStep) {
				data, _ := json.Marshal(step)
				evalJS(fmt.Sprintf(`routerProgress(%s)`, data))
			})
			if err != nil {
				data, _ := json.Marshal(RouterStep{err.Error(), "error"})
				evalJS(fmt.Sprintf(`routerProgress(%s)`, data))
			} else {
				evalJS(`routerDone()`)
			}
		}()

	case "routerTest":
		var p struct {
			Host string `json:"host"`
		}
		json.Unmarshal(msg.Payload, &p)
		go func() {
			if err := RouterTest(p.Host); err != nil {
				evalJS(fmt.Sprintf(`routerTestResult(%s)`, jsonEscape("HATA: "+err.Error())))
			} else {
				evalJS(fmt.Sprintf(`routerTestResult(%s)`, jsonEscape("OK — PAC erişilebilir")))
			}
		}()
```

- [ ] **Step 2: Derle**

```powershell
go build -ldflags "-H windowsgui" -o SpAC3DPI.exe .
```

Beklenen: hata yok.

- [ ] **Step 3: Commit**

```powershell
git add ipc.go
git commit -m "feat: routerSetup ve routerTest IPC handler'ları"
```

---

## Task 4: UI — Router Paneli (assets/ui.html)

**Files:**
- Modify: `assets/ui.html`

**Değişiklik 1 — CSS:** Mevcut `/* ── LOGS panel ── */` bloğundan önce yeni CSS ekle.

**Değişiklik 2 — Sidebar nav:** Logs nav'ından önce Router nav item ekle.

**Değişiklik 3 — Panel HTML:** `panel-logs` div'inden önce `panel-router` div ekle.

**Değişiklik 4 — JS:** Mevcut `function cp(` bloğundan önce yeni JS fonksiyonları ekle.

- [ ] **Step 1: CSS ekle**

`assets/ui.html` içinde `/* ── LOGS panel ── */` satırından hemen önce ekle:

```css
/* ── ROUTER panel ── */
.rt-form { display:flex; flex-direction:column; gap:8px; margin-bottom:14px; }
.rt-row { display:flex; gap:8px; }
.rt-row .s-input { margin-bottom:0; }
.rt-steps {
  background:#07090f; border:1px solid var(--border); border-radius:10px;
  padding:10px; max-height:220px; overflow-y:auto;
  font-family:Consolas,monospace; font-size:11px; line-height:1.85;
  display:none; margin-bottom:10px;
}
.rt-steps.visible { display:block; }
.rt-step { display:flex; gap:8px; align-items:flex-start; padding:1px 0; }
.rt-step-info { color:#7A8CA8; }
.rt-step-ok   { color:var(--green); }
.rt-step-error{ color:var(--red); }
.rt-step-icon { flex-shrink:0; }
.rt-badge {
  display:inline-block; font-size:10px; font-weight:700;
  padding:2px 8px; border-radius:4px; margin-bottom:10px;
  background:rgba(245,158,11,.15); color:#F59E0B;
  border:1px solid rgba(245,158,11,.3);
}
.rt-done {
  background:rgba(34,197,94,.1); border:1px solid rgba(34,197,94,.3);
  border-radius:10px; padding:12px; text-align:center;
  color:var(--green); font-size:13px; font-weight:700; display:none;
}
.rt-done.visible { display:block; }
```

- [ ] **Step 2: Sidebar nav item ekle**

`assets/ui.html` içinde `<div class="nav" data-p="logs"` satırından hemen önce ekle:

```html
      <div class="nav" data-p="router" onclick="go(this)">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8">
          <rect x="2" y="14" width="20" height="8" rx="2"/>
          <path d="M6 14V8a6 6 0 0 1 12 0v6"/>
          <circle cx="12" cy="18" r="1" fill="currentColor" stroke="none"/>
        </svg>
        <div class="tooltip">Router</div>
      </div>
```

- [ ] **Step 3: Panel HTML ekle**

`assets/ui.html` içinde `<!-- LOGS -->` yorumundan (ya da `<div class="panel" id="panel-logs"`) hemen önce ekle:

```html
      <!-- ROUTER -->
      <div class="panel" id="panel-router">
        <div class="ph"><div class="ph-title">Router Entegrasyonu</div></div>
        <div class="rt-badge">GELİŞMİŞ — OpenWrt + Entware gerektirir</div>
        <div class="card" style="margin-bottom:10px">
          <div class="card-title">Ne işe yarar?</div>
          <div style="font-size:12px;color:var(--sub);line-height:1.7">
            PC kapalıyken telefon proxy yerine doğrudan (DIRECT) bağlanır.
            Router kurulumu olmadan PC kapalıyken kısa bir gecikme yaşanır.
          </div>
        </div>

        <div class="s-label">Router Bağlantısı</div>
        <div class="rt-form">
          <input class="s-input" id="rt-host" placeholder="Router IP (örn: 192.168.1.1)" style="margin-bottom:0">
          <div class="rt-row">
            <input class="s-input" id="rt-port" placeholder="SSH Port (22)" style="flex:0 0 110px">
            <input class="s-input" id="rt-user" placeholder="Kullanıcı (root)">
          </div>
          <input class="s-input" id="rt-pass" type="password" placeholder="SSH Şifresi" style="margin-bottom:0">
        </div>

        <div style="display:flex;gap:8px;margin-bottom:12px">
          <button class="save-btn" style="flex:1" onclick="routerSetup()">Kur</button>
          <button class="save-btn" style="flex:0 0 80px;background:var(--surface2);color:var(--sub);box-shadow:none;border:1px solid var(--border)" onclick="routerTest()">Test</button>
        </div>

        <div class="rt-steps" id="rt-steps"></div>
        <div class="rt-done" id="rt-done">
          ✓ Kurulum tamamlandı — Mobil panelden QR kodunu kullanabilirsin
        </div>
      </div>
```

- [ ] **Step 4: JS fonksiyonları ekle**

`assets/ui.html` içinde `function cp(` satırından hemen önce ekle:

```javascript
function routerSetup() {
  var host = document.getElementById('rt-host').value.trim();
  var port = parseInt(document.getElementById('rt-port').value) || 22;
  var user = document.getElementById('rt-user').value.trim() || 'root';
  var pass = document.getElementById('rt-pass').value;
  if (!host) { alert('Router IP giriniz'); return; }
  var steps = document.getElementById('rt-steps');
  var done  = document.getElementById('rt-done');
  steps.innerHTML = '';
  steps.classList.add('visible');
  done.classList.remove('visible');
  window.goMessage(JSON.stringify({
    type: 'routerSetup',
    payload: { Host: host, Port: port, User: user, Password: pass }
  }));
}

function routerTest() {
  var host = document.getElementById('rt-host').value.trim();
  if (!host) { alert('Router IP giriniz'); return; }
  window.goMessage(JSON.stringify({ type: 'routerTest', payload: { host: host } }));
}

function routerProgress(step) {
  var steps = document.getElementById('rt-steps');
  steps.classList.add('visible');
  var cls = step.status === 'ok' ? 'rt-step-ok' : step.status === 'error' ? 'rt-step-error' : 'rt-step-info';
  var icon = step.status === 'ok' ? '✓' : step.status === 'error' ? '✗' : '…';
  var el = document.createElement('div');
  el.className = 'rt-step ' + cls;
  el.innerHTML = '<span class="rt-step-icon">' + icon + '</span><span>' + step.msg + '</span>';
  steps.appendChild(el);
  steps.scrollTop = steps.scrollHeight;
}

function routerDone() {
  document.getElementById('rt-done').classList.add('visible');
}

function routerTestResult(msg) {
  var steps = document.getElementById('rt-steps');
  steps.classList.add('visible');
  var isOk = msg.startsWith('OK');
  var el = document.createElement('div');
  el.className = 'rt-step ' + (isOk ? 'rt-step-ok' : 'rt-step-error');
  el.innerHTML = '<span class="rt-step-icon">' + (isOk ? '✓' : '✗') + '</span><span>' + msg + '</span>';
  steps.appendChild(el);
  steps.scrollTop = steps.scrollHeight;
}
```

- [ ] **Step 5: Router paneli açılırken gateway IP'yi otomatik doldur**

`assets/ui.html` içinde `function go(nav)` fonksiyonunu bul. Mevcut hali:
```javascript
function go(nav) {
  document.querySelectorAll('.nav').forEach(n => n.classList.remove('active'));
  nav.classList.add('active');
  var p = nav.dataset.p;
  document.querySelectorAll('.panel').forEach(el => el.classList.remove('active'));
  document.getElementById('panel-' + p).classList.add('active');
  if (p === 'mobile') window.goMessage(JSON.stringify({type:'requestQR'}));
  if (p === 'settings') window.goMessage(JSON.stringify({type:'requestSettings'}));
}
```

Bunu şununla değiştir:
```javascript
function go(nav) {
  document.querySelectorAll('.nav').forEach(n => n.classList.remove('active'));
  nav.classList.add('active');
  var p = nav.dataset.p;
  document.querySelectorAll('.panel').forEach(el => el.classList.remove('active'));
  document.getElementById('panel-' + p).classList.add('active');
  if (p === 'mobile') window.goMessage(JSON.stringify({type:'requestQR'}));
  if (p === 'settings') window.goMessage(JSON.stringify({type:'requestSettings'}));
  if (p === 'router') window.goMessage(JSON.stringify({type:'requestRouterDefaults'}));
}
```

- [ ] **Step 6: `requestRouterDefaults` IPC handler ekle**

`ipc.go` içinde `case "routerTest":` bloğundan sonra ekle:

```go
	case "requestRouterDefaults":
		c := getConfig()
		gateway := guessGatewayIP(g.localIP)
		data, _ := json.Marshal(map[string]string{
			"host": gateway,
			"user": "root",
			"port": "22",
			"pacPort": fmt.Sprintf("%d", c.PACPort),
		})
		evalJS(fmt.Sprintf(`loadRouterDefaults(%s)`, data))
```

Ve `assets/ui.html`'e `routerTestResult` fonksiyonundan sonra ekle:

```javascript
function loadRouterDefaults(d) {
  var h = document.getElementById('rt-host');
  if (!h.value) h.value = d.host;
  var u = document.getElementById('rt-user');
  if (!u.value) u.value = d.user;
  var p = document.getElementById('rt-port');
  if (!p.value) p.value = d.port;
}
```

- [ ] **Step 7: Derle**

```powershell
go build -ldflags "-H windowsgui" -o SpAC3DPI.exe .
```

Beklenen: hata yok.

- [ ] **Step 8: Commit**

```powershell
git add assets/ui.html ipc.go
git commit -m "feat: Router paneli — SSH kurulum UI + IPC handler"
```

---

## Task 5: Son Derleme ve Smoke Test

**Files:**
- No new files

- [ ] **Step 1: Tüm testleri çalıştır**

```powershell
go test -v ./...
```

Beklenen: tüm testler PASS.

- [ ] **Step 2: Release build al**

```powershell
go build -ldflags "-H windowsgui" -o SpAC3DPI.exe .
```

- [ ] **Step 3: Uygulamayı başlat ve Router panelini aç**

Uygulamayı çalıştır. Sidebar'da yeni router ikonu görünmeli. Tıklayınca:
- "Router Entegrasyonu" başlığı
- "GELİŞMİŞ" sarı badge
- Form alanları (Host otomatik gateway IP ile dolu olmalı)
- "Kur" ve "Test" butonları

- [ ] **Step 4: Test butonu ile bağlantısız router'ı test et**

Host kısmına erişilemeyen bir IP gir (örn. `192.168.99.99`), "Test" buton'una bas. Adımlar kutusunda hata mesajı görünmeli (kırmızı ✗).

- [ ] **Step 5: Commit**

```powershell
git add .
git commit -m "feat: router SSH entegrasyonu tamamlandı"
```

---

## Self-Review

**Spec Coverage:**
- ✅ SSH bağlantı (Task 2: `sshConnect`)
- ✅ OpenWrt tespiti (Task 2: `isOpenWrt`)
- ✅ Entware tespiti (Task 2: `hasEntware`)
- ✅ lighttpd kurulumu yoksa otomatik yükle (Task 2: `installLighttpd`)
- ✅ Tüm scriptler router'a yazılıyor (Task 2: `files` slice)
- ✅ lighttpd başlatma + PID dosyası (Task 2)
- ✅ HTTP doğrulama (Task 2: `RouterTest` çağrısı)
- ✅ Otomatik başlatma init.d scripti (Task 2: `S80spac3dpi`)
- ✅ IPC `routerSetup` handler goroutine ile (Task 3)
- ✅ IPC `routerTest` handler (Task 3)
- ✅ UI panel: form, progress, done state (Task 4)
- ✅ Gateway IP otomatik doldurma (Task 4 Step 5-6)
- ✅ Test butonu (Task 4 Step 3)

**Placeholder Scan:** Temiz — TBD yok, tüm kod blokları tam.

**Type Consistency:**
- `RouterSetupCfg` → Task 2'de tanımlandı, Task 3'te `json.Unmarshal` ile kullanıldı ✅
- `RouterStep` → Task 2'de tanımlandı, Task 3'te `json.Marshal` ile kullanıldı ✅
- `routerProgress(step)` → Task 3'te `evalJS` ile çağrıldı, Task 4'te JS'de tanımlandı ✅
- `routerDone()` → Task 3'te `evalJS` ile çağrıldı, Task 4'te tanımlandı ✅
- `routerTestResult(msg)` → Task 3'te `jsonEscape` ile çağrıldı, Task 4'te tanımlandı ✅
- `loadRouterDefaults(d)` → Task 4 Step 6'da tanımlandı, IPC handler'da `evalJS` ile çağrıldı ✅
