//go:build windows

package main

import (
	"fmt"
	"strings"
	"unsafe"

	webview "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

var (
	wvModUser32            = windows.NewLazySystemDLL("user32.dll")
	wvProcGetWindowLong    = wvModUser32.NewProc("GetWindowLongW")
	wvProcSetWindowLong    = wvModUser32.NewProc("SetWindowLongW")
	wvProcSetWindowPos     = wvModUser32.NewProc("SetWindowPos")
	wvProcGetSystemMetrics = wvModUser32.NewProc("GetSystemMetrics")
	wvProcShowWindow       = wvModUser32.NewProc("ShowWindow")

	wvModDwmapi            = windows.NewLazySystemDLL("dwmapi.dll")
	wvProcDwmSetWindowAttr = wvModDwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	wvWsPopup      = uint32(0x80000000)
	wvWsCaption    = uint32(0x00C00000)
	wvWsSysMenu    = uint32(0x00080000)
	wvWsThickFrame = uint32(0x00040000)
	wvWsMinBox     = uint32(0x00020000)
	wvWsMaxBox     = uint32(0x00010000)
	wvSwpFrameChg  = uint32(0x0020)
	wvSwpNoMove    = uint32(0x0002)
	wvSwpNoSize    = uint32(0x0001)
	wvSwpNoZOrder  = uint32(0x0004)
	wvSwShow       = uintptr(5)
	wvSwHide       = uintptr(0)
	wvSmCxScreen   = uintptr(0)
	wvSmCyScreen   = uintptr(1)
	// DWM
	wvDwmwaBorderColor = uintptr(34)
	wvDwmwaColorNone   = uint32(0xFFFFFFFE)
)

// wv — global WebView referansı; ipc.go'dan erişilir.
var wv webview.WebView

// initWindow — WebView2 penceresini oluşturur, frameless yapar ve çalıştırır.
// Bu fonksiyon main goroutine'de çağrılmalı; bloklar (Run() içerir).
func initWindow() {
	wv = webview.New(false)
	if wv == nil {
		panic("WebView2 oluşturulamadı — Edge WebView2 Runtime kurulu mu?")
	}
	defer wv.Destroy()

	wv.SetTitle("SpAC3DPI")
	wv.SetSize(400, 640, webview.HintFixed)

	hwnd := uintptr(wv.Window())
	wvMakeFrameless(hwnd)
	wvCenterWindow(hwnd, 400, 640)
	wvApplyDWMShadow(hwnd)

	// Logo inject — wv.SetHtml'den önce
	logoB64 := logoBase64()
	wv.Init(fmt.Sprintf(`window.__logoB64 = "%s";`, logoB64))

	// UI HTML yükle
	html := string(uiHTMLBytes)
	wv.SetHtml(html)

	// IPC mesaj handler'ı bağla
	wv.Bind("goMessage", handleIPCMessage) //nolint:errcheck

	// Status/log push ticker başlat
	go startIPCTicker()

	wv.Run()
}

func wvMakeFrameless(hwnd uintptr) {
	const nIndex = uintptr(0xFFFFFFF0) // GWL_STYLE = -16 as uintptr
	style, _, _ := wvProcGetWindowLong.Call(hwnd, nIndex)
	newStyle := (uint32(style) &^ (wvWsCaption | wvWsSysMenu | wvWsThickFrame | wvWsMinBox | wvWsMaxBox)) | wvWsPopup
	wvProcSetWindowLong.Call(hwnd, nIndex, uintptr(newStyle))
	wvProcSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0,
		uintptr(wvSwpNoMove|wvSwpNoSize|wvSwpNoZOrder|wvSwpFrameChg))
}

func wvCenterWindow(hwnd uintptr, w, h int) {
	sw, _, _ := wvProcGetSystemMetrics.Call(wvSmCxScreen)
	sh, _, _ := wvProcGetSystemMetrics.Call(wvSmCyScreen)
	x := (int(sw) - w) / 2
	y := (int(sh) - h) / 2
	wvProcSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		uintptr(wvSwpNoZOrder|wvSwpFrameChg))
}

func wvApplyDWMShadow(hwnd uintptr) {
	color := wvDwmwaColorNone
	wvProcDwmSetWindowAttr.Call(hwnd, wvDwmwaBorderColor,
		uintptr(unsafe.Pointer(&color)), 4)
}

// showWindow — pencereyi göster (tray'den çağrılır).
func showWindow() {
	if wv == nil {
		return
	}
	hwnd := uintptr(wv.Window())
	wvProcShowWindow.Call(hwnd, wvSwShow)
}

// hideWindow — pencereyi gizle (tray'e minimize).
func hideWindow() {
	if wv == nil {
		return
	}
	hwnd := uintptr(wv.Window())
	wvProcShowWindow.Call(hwnd, wvSwHide)
}

// evalJS — WebView thread'inde güvenli JS eval.
func evalJS(js string) {
	if wv == nil {
		return
	}
	wv.Dispatch(func() {
		wv.Eval(js)
	})
}

// jsonEscape — basit JSON string escape (tırnak ve ters eğik çizgi için).
func jsonEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", ``)
	return fmt.Sprintf(`"%s"`, s)
}
