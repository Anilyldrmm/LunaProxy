//go:build windows

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

// pendingUpdateTag — güncelleme varsa set edilir; pushStatus payload'una eklenir.
var pendingUpdateTag atomic.Value

// lastLogSent — pushLogs'un son gönderdiği log index'i (dedup için).
var lastLogSent atomic.Int64

// handleIPCMessage — JS'den gelen goMessage çağrılarını işler.
func handleIPCMessage(data string) {
	var msg struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		logWarn("IPC parse hatası: " + err.Error())
		return
	}
	switch msg.Type {
	case "toggle":
		if g.running {
			g.stop()
		} else {
			if err := g.start(); err != nil {
				logError("Başlatma hatası: " + err.Error())
				evalJS(fmt.Sprintf(`showError(%s)`, jsonEscape(err.Error())))
			}
		}
		pushStatus()

	case "saveSettings":
		var cfg Config
		if err := json.Unmarshal(msg.Payload, &cfg); err != nil {
			logWarn("saveSettings parse: " + err.Error())
			return
		}
		setConfig(cfg)
		setStartup(cfg.AutoStart)
		evalJS(`showSaveSuccess()`)
		if g.running {
			g.restart()
		}

	case "clearLogs":
		appLog.Clear()
		lastLogSent.Store(0)

	case "copyToClipboard":
		var p struct {
			Text string `json:"text"`
		}
		json.Unmarshal(msg.Payload, &p)
		setClipboard(p.Text)

	case "windowMinimize":
		hwnd := uintptr(wv.Window())
		wvProcShowWindow.Call(hwnd, uintptr(2)) // SW_MINIMIZE
		_ = hwnd

	case "windowHide":
		hideWindow()

	case "windowExit":
		appExiting = true
		g.shutdown()
		os.Exit(0)

	case "startDrag":
		hwnd := uintptr(wv.Window())
		wvProcReleaseCapture.Call()
		wvProcSendMessage.Call(hwnd, uintptr(0x00A1), uintptr(2), uintptr(0)) // WM_NCLBUTTONDOWN, HTCAPTION
		_ = hwnd

	case "requestQR":
		pushQR()

	case "requestSettings":
		pushSettings()

	case "openFileDialog":
		evalJS(`fileSelected("gdpiPath", "")`)

	case "applyUpdate":
		var p struct {
			URL string `json:"url"`
		}
		json.Unmarshal(msg.Payload, &p)
		if err := DownloadAndReplace(p.URL); err != nil {
			logError("Güncelleme başarısız: " + err.Error())
		}

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
				data, _ := json.Marshal(RouterStep{Msg: err.Error(), Status: "error"})
				evalJS(fmt.Sprintf(`routerProgress(%s)`, data))
			} else {
				evalJS(`routerDone()`)
			}
		}()

	case "routerTest":
		var p struct {
			Host string `json:"host"`
		}
		json.Unmarshal(msg.Payload, &p) //nolint:errcheck
		go func() {
			if err := RouterTest(p.Host); err != nil {
				evalJS(fmt.Sprintf(`routerTestResult(%s)`, jsonEscape("HATA: "+err.Error())))
			} else {
				evalJS(fmt.Sprintf(`routerTestResult(%s)`, jsonEscape("OK — PAC erişilebilir")))
			}
		}()

	case "requestRouterDefaults":
		c := getConfig()
		gateway := guessGatewayIP(g.localIP)
		data, _ := json.Marshal(map[string]string{
			"host":    gateway,
			"user":    "root",
			"port":    "22",
			"pacPort": fmt.Sprintf("%d", c.PACPort),
		})
		evalJS(fmt.Sprintf(`loadRouterDefaults(%s)`, data))
	}
}

// startIPCTicker — her 2s'de status ve log push eder.
func startIPCTicker() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		pushStatus()
		pushLogs()
	}
}

// pushStatus — StatusPayload'u JSON olarak UI'a gönderir.
func pushStatus() {
	s := buildStatus()
	if tag, ok := pendingUpdateTag.Load().(string); ok && tag != "" {
		s.UpdateTag = tag
	}
	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	evalJS(fmt.Sprintf(`updateStatus(%s)`, data))
}

// pushLogs — sadece yeni log girişlerini UI'a gönderir (dedup için pozisyon takibi).
func pushLogs() {
	all := appLog.All()
	last := int(lastLogSent.Load())
	if len(all) <= last {
		return
	}
	newEntries := all[last:]
	if len(newEntries) == 0 {
		return
	}
	data, err := json.Marshal(newEntries)
	if err != nil {
		return
	}
	lastLogSent.Store(int64(len(all)))
	evalJS(fmt.Sprintf(`appendLogs(%s)`, data))
}

// pushQR — QR PNG'yi base64 olarak UI'a gönderir.
func pushQR() {
	c := getConfig()
	setupURL := fmt.Sprintf("http://%s:%d/setup", g.localIP, c.PACPort)
	pcPACURL := fmt.Sprintf("http://%s:%d/proxy.pac", g.localIP, c.PACPort)
	routerPACURL := fmt.Sprintf("http://%s:8090/cgi-bin/proxy.pac", guessGatewayIP(g.localIP))

	// QR → setup sayfası (telefon tarayıcısında açılır, PAC URL'ini gösterir)
	png, err := qrcode.Encode(setupURL, qrcode.High, 200)
	if err != nil {
		return
	}
	b64 := base64.StdEncoding.EncodeToString(png)
	data, _ := json.Marshal(map[string]string{
		"setupUrl":     setupURL,
		"pcUrl":        pcPACURL,
		"routerUrl":    routerPACURL,
		"qrBase64":     b64,
	})
	evalJS(fmt.Sprintf(`updateQR(%s)`, data))
}

// pushSettings — mevcut config'i UI'a gönderir.
func pushSettings() {
	c := getConfig()
	data, _ := json.Marshal(c)
	evalJS(fmt.Sprintf(`loadSettings(%s)`, data))
}

// setClipboard — metni Windows clipboard'a yazar.
func setClipboard(text string) {
	hiddenRun("powershell", "-Command",
		fmt.Sprintf(`Set-Clipboard -Value %s`, jsonEscape(text)))
}
