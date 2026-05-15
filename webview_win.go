//go:build windows

package main

import (
	"encoding/binary"
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
	wvProcLoadImage        = wvModUser32.NewProc("LoadImageW")

	wvProcReleaseCapture           = wvModUser32.NewProc("ReleaseCapture")
	wvProcSendMessage              = wvModUser32.NewProc("SendMessageW")
	wvProcCreateIconFromResourceEx = wvModUser32.NewProc("CreateIconFromResourceEx")

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
	wvSetTaskbarIcon(hwnd)

	// Logo inject — sayfa yüklenmeden önce window.__logoB64 hazır olsun
	logoB64 := logoBase64()
	wv.Init(fmt.Sprintf(`window.__logoB64 = "%s";`, logoB64))

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

// icoEntryHICON — ICO bayt dizisinden istenen boyuta en yakın girdiye HICON üretir.
// CreateIconFromResourceEx kullanır — temp dosya yok, bellek içi, DPI-safe.
func icoEntryHICON(icoBytes []byte, wantSize int) uintptr {
	if len(icoBytes) < 6 {
		return 0
	}
	count := int(binary.LittleEndian.Uint16(icoBytes[4:6]))
	if count == 0 || len(icoBytes) < 6+count*16 {
		return 0
	}
	bestIdx, bestDiff := -1, 999
	for i := 0; i < count; i++ {
		e := icoBytes[6+i*16:]
		w := int(e[0])
		if w == 0 {
			w = 256
		}
		if d := abs(w - wantSize); d < bestDiff {
			bestDiff, bestIdx = d, i
		}
	}
	if bestIdx < 0 {
		return 0
	}
	e := icoBytes[6+bestIdx*16:]
	dataSize := binary.LittleEndian.Uint32(e[8:12])
	dataOff := binary.LittleEndian.Uint32(e[12:16])
	if int(dataOff)+int(dataSize) > len(icoBytes) {
		return 0
	}
	img := icoBytes[dataOff : dataOff+dataSize]
	hicon, _, _ := wvProcCreateIconFromResourceEx.Call(
		uintptr(unsafe.Pointer(&img[0])),
		uintptr(dataSize),
		1,          // fIcon = TRUE
		0x00030000, // dwVersion = Windows 3.0+
		uintptr(wantSize),
		uintptr(wantSize),
		0, // LR_DEFAULTCOLOR
	)
	return hicon
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// wvSetTaskbarIcon — CreateIconFromResourceEx ile HICON üretir, WM_SETICON gönderir.
// DPI-aware: GetSystemMetrics(SM_CXSMICON/SM_CXICON) ile gerçek boyutu sorgular.
func wvSetTaskbarIcon(hwnd uintptr) {
	icoBytes := rawICOBytes
	if len(icoBytes) == 0 {
		icoBytes = makeICOBytes(true)
	}
	if len(icoBytes) == 0 {
		return
	}

	smW, _, _ := wvProcGetSystemMetrics.Call(wvSmCxSmIcon) // küçük ikon (16px @100%)
	bgW, _, _ := wvProcGetSystemMetrics.Call(wvSmCxIcon)   // büyük ikon (32px @100%)

	hSmall := icoEntryHICON(icoBytes, int(smW))
	hBig := icoEntryHICON(icoBytes, int(bgW))

	const wmSetIcon = 0x0080
	if hBig == 0 {
		return
	}
	if hSmall == 0 {
		hSmall = hBig
	}
	wvProcSendMessage.Call(hwnd, wmSetIcon, 0, hSmall) // ICON_SMALL
	wvProcSendMessage.Call(hwnd, wmSetIcon, 1, hBig)   // ICON_BIG
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
