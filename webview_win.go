//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
	wvProcLoadImage        = wvModUser32.NewProc("LoadImageW")

	wvProcReleaseCapture      = wvModUser32.NewProc("ReleaseCapture")
	wvProcSendMessage         = wvModUser32.NewProc("SendMessageW")
	wvProcSetForegroundWindow = wvModUser32.NewProc("SetForegroundWindow")
	wvProcFindWindowW         = wvModUser32.NewProc("FindWindowW")
	wvProcBringWindowToTop    = wvModUser32.NewProc("BringWindowToTop")
	wvProcIsIconic            = wvModUser32.NewProc("IsIconic")
	wvProcSwitchToThisWindow  = wvModUser32.NewProc("SwitchToThisWindow")

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
	wvSwRestore    = uintptr(9)
	wvSwHide       = uintptr(0)
	wvSmCxScreen   = uintptr(0)
	wvSmCyScreen   = uintptr(1)
	wvSmCxIcon     = uintptr(11) // SM_CXICON — büyük ikon (32px @100%)
	wvSmCxSmIcon   = uintptr(49) // SM_CXSMICON — küçük ikon (16px @100%)
	// DWM
	wvDwmwaBorderColor  = uintptr(34)
	wvDwmwaColorNone    = uint32(0xFFFFFFFE)
	wvDwmwaCornerPref   = uintptr(33)   // DWMWA_WINDOW_CORNER_PREFERENCE (Win11+)
	wvDwmwcpRound       = uint32(2)     // DWMWCP_ROUND
)

// wv — global WebView referansı; ipc.go'dan erişilir.
var wv webview.WebView

// initWindow — WebView2 penceresini oluşturur, frameless yapar ve çalıştırır.
// Bu fonksiyon main goroutine'de çağrılmalı; bloklar (Run() içerir).
func initWindow() {
	dataPath := filepath.Join(os.Getenv("LOCALAPPDATA"), "LunaProxy", "WebView2")
	wv = webview.NewWithOptions(webview.WebViewOptions{
		Debug:    false,
		DataPath: dataPath,
	})
	if wv == nil {
		panic("WebView2 oluşturulamadı — Edge WebView2 Runtime kurulu mu?")
	}
	defer wv.Destroy()

	wv.SetTitle("LunaProxy")
	wv.SetSize(400, 640, webview.HintFixed)

	hwnd := uintptr(wv.Window())
	wvMakeFrameless(hwnd)
	wvCenterWindow(hwnd, 400, 640)
	wvApplyDWMShadow(hwnd)
	wvSetTaskbarIcon(hwnd)

	// Logo + versiyon inject — sayfa yüklenmeden önce hazır olsun
	logoB64 := logoBase64()
	wv.Init(fmt.Sprintf(`window.__logoB64 = "%s"; window.__version = "%s";`, logoB64, Version))

	// IPC mesaj handler'ı ÖNCE bağla — SetHtml'den önce bağlanmalı
	wv.Bind("goMessage", handleIPCMessage) //nolint:errcheck

	// UI HTML yükle
	html := string(uiHTMLBytes)
	wv.SetHtml(html)

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
	// Yuvarlak köşeler — Win11+ DWMWCP_ROUND; Win10'da görmezden geliniyor
	round := wvDwmwcpRound
	wvProcDwmSetWindowAttr.Call(hwnd, wvDwmwaCornerPref,
		uintptr(unsafe.Pointer(&round)), 4)
}

// wvSetTaskbarIcon — LoadImageW ile ICO'yu HICON'a yükler, WM_SETICON gönderir.
// DPI-aware: GetSystemMetrics(SM_CXSMICON/SM_CXICON) ile gerçek boyutu sorgular.
// LoadImageW PNG-in-ICO formatını Vista+ üzerinde doğru destekler.
func wvSetTaskbarIcon(hwnd uintptr) {
	icoBytes := rawICOBytes
	if len(icoBytes) == 0 {
		icoBytes = makeICOBytes(true)
	}
	if len(icoBytes) == 0 {
		return
	}

	f, err := os.CreateTemp("", "lunaproxy_*.ico")
	if err != nil {
		return
	}
	defer os.Remove(f.Name())
	f.Write(icoBytes) //nolint:errcheck
	f.Close()

	path16, err := syscall.UTF16PtrFromString(f.Name())
	if err != nil {
		return
	}

	const imageIcon    = uintptr(1)
	const lrLoadFile   = uintptr(0x10)
	const wmSetIcon    = uintptr(0x0080)

	// DPI-aware boyutlar: 100%→16/32, 125%→20/40, 150%→24/48, 200%→32/64
	smW, _, _ := wvProcGetSystemMetrics.Call(wvSmCxSmIcon)
	bgW, _, _ := wvProcGetSystemMetrics.Call(wvSmCxIcon)

	hSmall, _, _ := wvProcLoadImage.Call(0, uintptr(unsafe.Pointer(path16)), imageIcon, smW, smW, lrLoadFile)
	hBig, _, _ := wvProcLoadImage.Call(0, uintptr(unsafe.Pointer(path16)), imageIcon, bgW, bgW, lrLoadFile)
	if hBig == 0 {
		return
	}
	if hSmall == 0 {
		hSmall = hBig
	}
	wvProcSendMessage.Call(hwnd, wmSetIcon, 0, hSmall) // ICON_SMALL (taskbar)
	wvProcSendMessage.Call(hwnd, wmSetIcon, 1, hBig)   // ICON_BIG (alt-tab)
}

// showWindow — pencereyi göster ve öne getir (tray'den çağrılır).
// Minimize ise restore eder; SW_SHOW minimize pencereyi açmaz.
func showWindow() {
	if wv == nil {
		return
	}
	hwnd := uintptr(wv.Window())
	iconic, _, _ := wvProcIsIconic.Call(hwnd)
	if iconic != 0 {
		wvProcShowWindow.Call(hwnd, wvSwRestore)
	} else {
		wvProcShowWindow.Call(hwnd, wvSwShow)
	}
	wvProcBringWindowToTop.Call(hwnd)
	wvProcSetForegroundWindow.Call(hwnd)
}

// bringExistingToFront — başka bir process'ten çalışan örneği öne getirir.
// SwitchToThisWindow kullanır; SetForegroundWindow cross-process UIPI kısıtlamasını aşar.
func bringExistingToFront() {
	title, err := windows.UTF16PtrFromString("LunaProxy")
	if err != nil {
		return
	}
	hwnd, _, _ := wvProcFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd == 0 {
		return
	}
	wvProcSwitchToThisWindow.Call(hwnd, 1) // fAltTab=true → focus + öne getir
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
