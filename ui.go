package main

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	qrcode "github.com/skip2/go-qrcode"
	"golang.org/x/sys/windows"
)

// ── Log tablo modeli ─────────────────────────────────────────────────────────

type logTableModel struct {
	walk.TableModelBase
	entries []LogEntry
}

func (m *logTableModel) RowCount() int { return len(m.entries) }
func (m *logTableModel) Value(row, col int) interface{} {
	if row >= len(m.entries) {
		return ""
	}
	e := m.entries[row]
	switch col {
	case 0:
		return e.Time
	case 1:
		return e.Level
	case 2:
		return e.Message
	}
	return ""
}

// ── Sabitler ─────────────────────────────────────────────────────────────────

var dpiModeValues = []string{"turbo", "balanced", "powerful", "custom"}
var ispValues = []string{"auto", "superonline", "ttnet", "vodafone", "turkcell"}
var dnsValues = []string{"unchanged", "cloudflare", "google", "adguard", "quad9", "opendns"}
var chunkValues = []int{4, 8, 16, 40}

// ── Renk paleti (koyu lacivert, okunabilir beyaz metin) ─────────────────────

var (
	clrBg      = walk.RGB(13, 17, 28)    // #0d111c — ana arka plan
	clrSidebar = walk.RGB(9, 12, 20)     // #090c14 — sidebar
	clrCard    = walk.RGB(20, 26, 42)    // #141a2a — kart arka planı
	clrNavAct  = walk.RGB(24, 33, 55)    // #182137 — aktif nav
	clrBtnOff  = walk.RGB(16, 185, 129)  // #10b981 — BAŞLAT (yeşil)
	clrBtnOn   = walk.RGB(220, 60, 60)   // #dc3c3c — DURDUR (kırmızı)
	clrText    = walk.RGB(235, 240, 252)  // #ebf0fc — birincil metin
	clrSub     = walk.RGB(90, 110, 155)   // #5a6e9b — ikincil metin
	clrGreen   = walk.RGB(16, 185, 129)  // #10b981
	clrRed     = walk.RGB(220, 60, 60)   // #dc3c3c
)

// ── Win32 sabitleri ───────────────────────────────────────────────────────────

const (
	gwlStyle    = -16
	gwlExStyle  = -20
	wsCaption   = uint32(0x00C00000)
	wsSysMenu   = uint32(0x00080000)
	wsThickFrame = uint32(0x00040000)
	wsMinBox    = uint32(0x00020000)
	wsMaxBox    = uint32(0x00010000)
	wsBorder    = uint32(0x00800000)
	wsPopup     = uint32(0x80000000)
	wsExAppWindow = uint32(0x00040000)

	swpNoZOrder    = uint32(0x0004)
	swpFrameChg    = uint32(0x0020)
	swpNoCopyBits  = uint32(0x0100)

	wmNcLBtnDown = uint32(0x00A1)
	htCaption    = uintptr(2)

	// DWM attributes (Windows 11+)
	dwmwaBorderColor  = uint32(34)
	dwmwaCaptionColor = uint32(35)
	dwmwaColorNone    = uint32(0xFFFFFFFE)
)

var (
	modDwmapi        = windows.NewLazySystemDLL("dwmapi.dll")
	dwmSetWindowAttr = modDwmapi.NewProc("DwmSetWindowAttribute")

	modUser32          = windows.NewLazySystemDLL("user32.dll")
	procGetWindowLongW = modUser32.NewProc("GetWindowLongW")
	procSetWindowLongW = modUser32.NewProc("SetWindowLongW")
	procSetWindowPos   = modUser32.NewProc("SetWindowPos")
	procReleaseCapture = modUser32.NewProc("ReleaseCapture")
	procSendMessageW   = modUser32.NewProc("SendMessageW")
)

func winGetLong(hwnd uintptr, idx int32) uint32 {
	r, _, _ := procGetWindowLongW.Call(hwnd, uintptr(uint32(idx)))
	return uint32(r)
}

func winSetLong(hwnd uintptr, idx int32, val uint32) {
	procSetWindowLongW.Call(hwnd, uintptr(uint32(idx)), uintptr(val))
}

// ── Widget referansları ───────────────────────────────────────────────────────

type appUI struct {
	mw     *walk.MainWindow
	ni     *walk.NotifyIcon
	niMenu *walk.Menu

	// Custom titlebar
	titleBar    *walk.Composite
	ivTitleLogo *walk.ImageView
	lblTitleApp *walk.Label
	lblTitleSub *walk.Label
	btnMinimize *walk.Composite
	lblMinimize *walk.Label
	btnClose    *walk.Composite
	lblClose    *walk.Label

	// Sidebar
	sidebarComp *walk.Composite
	navItems    [4]*walk.Composite
	navLabels   [4]*walk.Label
	activeNav   int

	// Panel containers
	panelStatus   *walk.Composite
	panelSettings *walk.Composite
	panelMobile   *walk.Composite
	panelLogs     *walk.Composite

	// ScrollView refs (arka plan için)
	svStatus   *walk.ScrollView
	svSettings *walk.ScrollView
	svMobile   *walk.ScrollView

	// Status panel
	ivLogo       *walk.ImageView
	lblStatus    *walk.Label
	lblIPInfo    *walk.Label
	btnPanel     *walk.Composite
	lblToggle    *walk.Label
	lblUptime    *walk.Label
	lblActive    *walk.Label
	lblBytes     *walk.Label
	lblStatusBar *walk.Label

	lblTotal     *walk.Label
	lblErrors    *walk.Label
	lblRestarts  *walk.Label
	lblProxy     *walk.Label
	lblPACSvc    *walk.Label
	lblGDPI      *walk.Label
	lblDNSSvc    *walk.Label
	lblSysProxy  *walk.Label
	lblIP        *walk.Label
	lblProxyAddr *walk.Label
	lblPACURL    *walk.Label
	lblDPIMode   *walk.Label
	lblChunkSvc  *walk.Label
	lblISPSvc    *walk.Label
	lblGDPIFlags *walk.Label
	lblDNSMode   *walk.Label

	// Ayarlar paneli
	cbDPI         *walk.ComboBox
	leCustomFlags *walk.LineEdit
	cbChunk       *walk.ComboBox
	cbISP         *walk.ComboBox
	cbDNS         *walk.ComboBox
	chkSysProxy   *walk.CheckBox
	chkAutoStart  *walk.CheckBox
	rbDPIAuto     *walk.RadioButton
	rbDPIService  *walk.RadioButton
	rbDPIManual   *walk.RadioButton
	rbDPIDisabled *walk.RadioButton
	gdpiPathComp  *walk.Composite
	leGDPIPath    *walk.LineEdit
	neProxyPort   *walk.NumberEdit
	nePACPort     *walk.NumberEdit
	lblSaveStatus *walk.Label

	// Mobil paneli
	ivQR       *walk.ImageView
	leQRURL    *walk.LineEdit
	lePCPACURL *walk.LineEdit

	// Kayıtlar paneli
	tvLog         *walk.TableView
	logModel      *logTableModel
	chkAutoScroll *walk.CheckBox
	lblLogCount   *walk.Label
}

var theUI = &appUI{}

// ── Ana giriş noktası ────────────────────────────────────────────────────────

func runUI() {
	u := theUI
	u.logModel = &logTableModel{}

	if err := (MainWindow{
		AssignTo: &u.mw,
		Title:    appName,
		Icon:     getIcon(false),
		MinSize:  Size{Width: 440, Height: 660},
		Size:     Size{Width: 440, Height: 660},
		Layout:   VBox{MarginsZero: true, SpacingZero: true},
		Children: []Widget{
			u.buildTitleBar(),
			Composite{
				Layout: HBox{MarginsZero: true, SpacingZero: true},
				Children: []Widget{
					u.buildSidebar(),
					u.buildContent(),
				},
			},
		},
	}).Create(); err != nil {
		panic(err)
	}

	u.makeFrameless()
	u.applyTheme()
	u.wireTitleBar()

	// Nav handler'lar
	for i := range u.navItems {
		idx := i
		if u.navItems[idx] != nil {
			u.navItems[idx].MouseDown().Attach(func(x, y int, button walk.MouseButton) {
				if button == walk.LeftButton {
					u.mw.Synchronize(func() { u.switchPanel(idx) })
				}
			})
		}
		if u.navLabels[idx] != nil {
			u.navLabels[idx].MouseDown().Attach(func(x, y int, button walk.MouseButton) {
				if button == walk.LeftButton {
					u.mw.Synchronize(func() { u.switchPanel(idx) })
				}
			})
		}
	}

	u.switchPanel(0)

	u.mw.Closing().Attach(func(canceled *bool, _ walk.CloseReason) {
		if appExiting {
			return
		}
		*canceled = true
		go u.mw.Synchronize(u.mw.Hide)
	})

	u.setupTray()
	u.loadSettingsForm()
	u.updateQR()
	u.refreshStatus()

	u.mw.Show()
	u.mw.Activate()

	go func() {
		if err := g.start(); err != nil {
			logError("Otomatik başlatma başarısız: " + err.Error())
		}
		u.mw.Synchronize(u.refreshStatus)
	}()

	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for range t.C {
			u.mw.Synchronize(u.refreshStatus)
		}
	}()

	u.mw.Run()
}

// ── Frameless pencere ─────────────────────────────────────────────────────────

func (u *appUI) makeFrameless() {
	hwnd := uintptr(u.mw.Handle())

	// Native chrome kaldır, WS_POPUP ekle
	style := winGetLong(hwnd, gwlStyle)
	style &^= wsCaption | wsSysMenu | wsThickFrame | wsMinBox | wsMaxBox | wsBorder
	style |= wsPopup
	winSetLong(hwnd, gwlStyle, style)

	// Görev çubuğunda görünür
	exStyle := winGetLong(hwnd, gwlExStyle)
	exStyle |= wsExAppWindow
	winSetLong(hwnd, gwlExStyle, exStyle)

	// DWM: caption rengini arka planla eşleştir (BGR — sarı/turuncu kaybolur)
	// clrSidebar = RGB(9,12,20) → BGR COLORREF = 0x140c09
	captionBGR := uint32(0x140c09)
	dwmSetWindowAttr.Call(hwnd, uintptr(dwmwaCaptionColor),
		uintptr(unsafe.Pointer(&captionBGR)), 4)

	// DWM: border kaldır
	borderNone := dwmwaColorNone
	dwmSetWindowAttr.Call(hwnd, uintptr(dwmwaBorderColor),
		uintptr(unsafe.Pointer(&borderNone)), 4)

	// DWM dark mode
	dark := uint32(1)
	dwmSetWindowAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&dark)), 4)
	dwmSetWindowAttr.Call(hwnd, 19, uintptr(unsafe.Pointer(&dark)), 4)

	// Style değişikliğini uygula + ekran merkezine al
	cx := int32(win.GetSystemMetrics(win.SM_CXSCREEN))
	cy := int32(win.GetSystemMetrics(win.SM_CYSCREEN))
	wx := (cx - 440) / 2
	wy := (cy - 660) / 2
	procSetWindowPos.Call(hwnd, 0,
		uintptr(wx), uintptr(wy), 440, 660,
		uintptr(swpNoZOrder|swpFrameChg|swpNoCopyBits))
}

// ── Custom titlebar ───────────────────────────────────────────────────────────

func (u *appUI) buildTitleBar() Widget {
	return Composite{
		AssignTo: &u.titleBar,
		MinSize:  Size{Height: 44},
		MaxSize:  Size{Height: 44},
		Layout:   HBox{Margins: Margins{Left: 12, Right: 4, Top: 0, Bottom: 0}, Spacing: 8},
		Children: []Widget{
			ImageView{
				AssignTo: &u.ivTitleLogo,
				MinSize:  Size{Width: 26, Height: 26},
				MaxSize:  Size{Width: 26, Height: 26},
				Mode:     ImageViewModeZoom,
			},
			Composite{
				Layout: VBox{MarginsZero: true, SpacingZero: true},
				Children: []Widget{
					Label{
						AssignTo:  &u.lblTitleApp,
						Text:      "SpAC3DPI",
						Font:      Font{Family: "Segoe UI", Bold: true, PointSize: 10},
						TextColor: clrText,
					},
					Label{
						AssignTo:  &u.lblTitleSub,
						Text:      "DPI Bypass Proxy",
						Font:      Font{Family: "Segoe UI", PointSize: 7},
						TextColor: clrSub,
					},
				},
			},
			HSpacer{},
			// Minimize
			Composite{
				AssignTo: &u.btnMinimize,
				MinSize:  Size{Width: 38, Height: 44},
				MaxSize:  Size{Width: 38, Height: 44},
				Layout:   VBox{MarginsZero: true},
				Children: []Widget{
					Label{
						AssignTo:  &u.lblMinimize,
						Text:      "—",
						Font:      Font{Family: "Segoe UI", PointSize: 10},
						TextColor: clrSub,
						Alignment: AlignHCenterVCenter,
					},
				},
			},
			// Kapat
			Composite{
				AssignTo: &u.btnClose,
				MinSize:  Size{Width: 38, Height: 44},
				MaxSize:  Size{Width: 38, Height: 44},
				Layout:   VBox{MarginsZero: true},
				Children: []Widget{
					Label{
						AssignTo:  &u.lblClose,
						Text:      "✕",
						Font:      Font{Family: "Segoe UI", PointSize: 10},
						TextColor: walk.RGB(200, 80, 80),
						Alignment: AlignHCenterVCenter,
					},
				},
			},
		},
	}
}

func (u *appUI) wireTitleBar() {
	hwnd := uintptr(u.mw.Handle())

	beginDrag := func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			procReleaseCapture.Call()
			procSendMessageW.Call(hwnd, uintptr(wmNcLBtnDown), htCaption, 0)
		}
	}

	if u.titleBar != nil {
		u.titleBar.MouseDown().Attach(beginDrag)
	}
	if u.lblTitleApp != nil {
		u.lblTitleApp.MouseDown().Attach(beginDrag)
	}
	if u.lblTitleSub != nil {
		u.lblTitleSub.MouseDown().Attach(beginDrag)
	}
	if u.ivTitleLogo != nil {
		u.ivTitleLogo.MouseDown().Attach(beginDrag)
	}

	doMin := func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			win.ShowWindow(u.mw.Handle(), win.SW_MINIMIZE)
		}
	}
	if u.btnMinimize != nil {
		u.btnMinimize.MouseDown().Attach(doMin)
	}
	if u.lblMinimize != nil {
		u.lblMinimize.MouseDown().Attach(doMin)
	}

	doClose := func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			u.mw.Synchronize(u.onQuit)
		}
	}
	if u.btnClose != nil {
		u.btnClose.MouseDown().Attach(doClose)
	}
	if u.lblClose != nil {
		u.lblClose.MouseDown().Attach(doClose)
	}

	if u.ivTitleLogo != nil {
		if bmp := getLogoBitmap(false); bmp != nil {
			u.ivTitleLogo.SetImage(bmp)
		}
	}
}

// ── Sidebar ───────────────────────────────────────────────────────────────────

var navIcons = [4]string{"◎", "⚙", "◈", "☰"}
var navTips  = [4]string{"Durum", "Ayarlar", "Mobil", "Kayıtlar"}

func (u *appUI) buildSidebar() Widget {
	return Composite{
		AssignTo: &u.sidebarComp,
		MinSize:  Size{Width: 52},
		MaxSize:  Size{Width: 52},
		Layout:   VBox{MarginsZero: true, SpacingZero: true},
		Children: []Widget{
			u.navItem(navIcons[0], 0),
			u.navItem(navIcons[1], 1),
			u.navItem(navIcons[2], 2),
			u.navItem(navIcons[3], 3),
			VSpacer{},
		},
	}
}

// navItem — HSpacer yok, Label tüm alanı kaplar (click güvenilir çalışır).
func (u *appUI) navItem(icon string, idx int) Widget {
	return Composite{
		AssignTo: &u.navItems[idx],
		MinSize:  Size{Width: 52, Height: 50},
		MaxSize:  Size{Width: 52, Height: 50},
		Layout:   VBox{MarginsZero: true},
		Children: []Widget{
			Label{
				AssignTo:  &u.navLabels[idx],
				Text:      icon,
				Font:      Font{Family: "Segoe UI", PointSize: 15},
				TextColor: clrSub,
				Alignment: AlignHCenterVCenter,
			},
		},
	}
}

func (u *appUI) switchPanel(idx int) {
	panels := []*walk.Composite{u.panelStatus, u.panelSettings, u.panelMobile, u.panelLogs}
	for i, p := range panels {
		if p != nil {
			p.SetVisible(i == idx)
		}
	}
	for i := range u.navItems {
		if u.navItems[i] == nil {
			continue
		}
		if i == idx {
			setBrush(u.navItems[i], clrNavAct)
			if u.navLabels[i] != nil {
				u.navLabels[i].SetTextColor(clrText)
			}
		} else {
			setBrush(u.navItems[i], clrSidebar)
			if u.navLabels[i] != nil {
				u.navLabels[i].SetTextColor(clrSub)
			}
		}
	}
	u.activeNav = idx
}

// ── Content area ──────────────────────────────────────────────────────────────

func (u *appUI) buildContent() Widget {
	return Composite{
		Layout: VBox{MarginsZero: true, SpacingZero: true},
		Children: []Widget{
			u.buildStatusPanel(),
			u.buildSettingsPanel(),
			u.buildMobilePanel(),
			u.buildLogsPanel(),
		},
	}
}

// ── Status paneli ─────────────────────────────────────────────────────────────

func (u *appUI) buildStatusPanel() Widget {
	return Composite{
		AssignTo: &u.panelStatus,
		Layout:   VBox{MarginsZero: true, SpacingZero: true},
		Children: []Widget{
			ScrollView{
				AssignTo: &u.svStatus,
				Layout:   VBox{Margins: Margins{Left: 14, Right: 14, Top: 14, Bottom: 14}, Spacing: 12},
				Children: []Widget{
					// Logo
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							HSpacer{},
							ImageView{
								AssignTo: &u.ivLogo,
								MinSize:  Size{Width: 72, Height: 72},
								MaxSize:  Size{Width: 72, Height: 72},
								Mode:     ImageViewModeZoom,
							},
							HSpacer{},
						},
					},
					// Durum metni
					Label{
						AssignTo:  &u.lblStatus,
						Text:      "●  BAĞLI DEĞİL",
						Font:      Font{Family: "Segoe UI", Bold: true, PointSize: 16},
						TextColor: clrRed,
						Alignment: AlignHCenterVCenter,
					},
					Label{
						AssignTo:  &u.lblIPInfo,
						Text:      "—",
						Font:      Font{Family: "Segoe UI", PointSize: 9},
						TextColor: clrSub,
						Alignment: AlignHCenterVCenter,
					},
					// Start/Stop butonu — HSpacer YOK, label tüm alanı kaplar
					Composite{
						AssignTo: &u.btnPanel,
						MinSize:  Size{Height: 44},
						Layout:   VBox{MarginsZero: true},
						Children: []Widget{
							Label{
								AssignTo:  &u.lblToggle,
								Text:      "▶   BAŞLAT",
								Font:      Font{Family: "Segoe UI", Bold: true, PointSize: 12},
								TextColor: walk.RGB(255, 255, 255),
								Alignment: AlignHCenterVCenter,
							},
						},
					},
					// Mini istatistikler
					Composite{
						Layout: Grid{Columns: 3, Spacing: 4},
						Children: []Widget{
							u.miniStat(&u.lblUptime, "SÜRE"),
							u.miniStat(&u.lblActive, "BAĞ"),
							u.miniStat(&u.lblBytes, "VERİ"),
						},
					},
					// Alt durum çubuğu
					Label{
						AssignTo:  &u.lblStatusBar,
						Text:      "Proxy: —  PAC: —  DPI: —",
						Font:      Font{Family: "Segoe UI", PointSize: 8},
						TextColor: clrSub,
						Alignment: AlignHCenterVCenter,
					},
					// Servis durumu kartı
					u.cardSection("Servis Durumu",
						Composite{
							Layout: VBox{MarginsZero: true, Spacing: 3},
							Children: []Widget{
								u.kvRow("HTTP Proxy", &u.lblProxy),
								u.kvRow("PAC Sunucu", &u.lblPACSvc),
								u.kvRow("GoodbyeDPI", &u.lblGDPI),
								u.kvRow("DNS", &u.lblDNSSvc),
								u.kvRow("Sistem Proxy", &u.lblSysProxy),
							},
						},
					),
					// İstatistikler kartı
					u.cardSection("İstatistikler",
						Composite{
							Layout: Grid{Columns: 3, Spacing: 4},
							Children: []Widget{
								u.statChip("Toplam", &u.lblTotal),
								u.statChip("Hata", &u.lblErrors),
								u.statChip("Watchdog", &u.lblRestarts),
							},
						},
					),
					// Ağ bilgisi kartı
					u.cardSection("Ağ Bilgisi",
						Composite{
							Layout: VBox{MarginsZero: true, Spacing: 3},
							Children: []Widget{
								u.kvRow("PC IP", &u.lblIP),
								u.kvRow("Proxy", &u.lblProxyAddr),
								u.kvRow("PAC URL", &u.lblPACURL),
								u.kvRow("DPI Modu", &u.lblDPIMode),
								u.kvRow("Chunk", &u.lblChunkSvc),
								u.kvRow("ISP", &u.lblISPSvc),
								u.kvRow("DNS", &u.lblDNSMode),
								u.kvRow("GDPI Flags", &u.lblGDPIFlags),
							},
						},
					),
				},
			},
		},
	}
}

func (u *appUI) cardSection(title string, content Widget) Widget {
	return Composite{
		Layout: VBox{Margins: Margins{Left: 0, Right: 0, Top: 4, Bottom: 4}, Spacing: 6},
		Children: []Widget{
			Label{
				Text:      strings.ToUpper(title),
				Font:      Font{Family: "Segoe UI", PointSize: 8, Bold: true},
				TextColor: clrSub,
			},
			content,
		},
	}
}

// kvRow — sabit genişlik key + değer label satırı
func (u *appUI) kvRow(key string, ref **walk.Label) Widget {
	return Composite{
		Layout: HBox{MarginsZero: true, Spacing: 8},
		Children: []Widget{
			Label{
				Text:      key,
				TextColor: clrSub,
				Font:      Font{Family: "Segoe UI", PointSize: 9},
				MinSize:   Size{Width: 88},
				MaxSize:   Size{Width: 88},
			},
			Label{
				AssignTo:  ref,
				Text:      "—",
				TextColor: clrText,
				Font:      Font{Family: "Segoe UI", PointSize: 9},
			},
		},
	}
}

func (u *appUI) statChip(title string, ref **walk.Label) Widget {
	return Composite{
		Layout: VBox{Margins: Margins{Left: 4, Right: 4, Top: 6, Bottom: 6}, Spacing: 2},
		Children: []Widget{
			Label{
				AssignTo:  ref,
				Text:      "—",
				Font:      Font{Family: "Segoe UI", Bold: true, PointSize: 18},
				TextColor: clrText,
				Alignment: AlignHCenterVCenter,
			},
			Label{
				Text:      title,
				Font:      Font{Family: "Segoe UI", PointSize: 7},
				TextColor: clrSub,
				Alignment: AlignHCenterVCenter,
			},
		},
	}
}

func (u *appUI) miniStat(ref **walk.Label, caption string) Widget {
	return Composite{
		Layout: VBox{MarginsZero: true, SpacingZero: true},
		Children: []Widget{
			Label{
				AssignTo:  ref,
				Text:      "—",
				Font:      Font{Family: "Segoe UI", Bold: true, PointSize: 12},
				TextColor: clrText,
				Alignment: AlignHCenterVCenter,
			},
			Label{
				Text:      caption,
				Font:      Font{Family: "Segoe UI", PointSize: 7},
				TextColor: clrSub,
				Alignment: AlignHCenterVCenter,
			},
		},
	}
}

// ── Ayarlar paneli ────────────────────────────────────────────────────────────

func (u *appUI) buildSettingsPanel() Widget {
	return Composite{
		AssignTo: &u.panelSettings,
		Layout:   VBox{MarginsZero: true, SpacingZero: true},
		Visible:  false,
		Children: []Widget{
			ScrollView{
				AssignTo: &u.svSettings,
				Layout:   VBox{Margins: Margins{Left: 12, Right: 12, Top: 10, Bottom: 10}, Spacing: 10},
				Children: []Widget{
					u.settingsGroup("DPI Bypass Modu", []Widget{
						ComboBox{
							AssignTo:              &u.cbDPI,
							Model:                 []string{"Turbo — Hız öncelikli", "Dengeli — Standart (Önerilen)", "Güçlü — Paket parçalama", "Özel — Manuel bayraklar"},
							CurrentIndex:          1,
							OnCurrentIndexChanged: u.onDPIModeChange,
						},
						LineEdit{
							AssignTo:  &u.leCustomFlags,
							CueBanner: "-1 -p -q -r -s -e 40 --new-mode",
							Visible:   false,
						},
						Composite{
							Layout: HBox{MarginsZero: true, Spacing: 6},
							Children: []Widget{
								Label{Text: "Chunk:", TextColor: clrText, Font: Font{Family: "Segoe UI", PointSize: 9}},
								ComboBox{
									AssignTo:     &u.cbChunk,
									Model:        []string{"4 byte", "8 byte", "16 byte", "40 byte"},
									CurrentIndex: 3,
									MaxSize:      Size{Width: 90},
								},
								Label{Text: "ISP:", TextColor: clrText, Font: Font{Family: "Segoe UI", PointSize: 9}},
								ComboBox{
									AssignTo:     &u.cbISP,
									Model:        []string{"Otomatik", "Superonline / UltraNet", "Türk Telekom", "Vodafone TR", "Turkcell"},
									CurrentIndex: 0,
								},
								HSpacer{},
							},
						},
					}),
					u.settingsGroup("DNS Şifreleme", []Widget{
						ComboBox{
							AssignTo:     &u.cbDNS,
							Model:        []string{"Değiştirilmesin", "Cloudflare (1.1.1.1)", "Google (8.8.8.8)", "AdGuard", "Quad9", "OpenDNS"},
							CurrentIndex: 0,
						},
					}),
					u.settingsGroup("DPI Kaynağı", []Widget{
						RadioButton{
							AssignTo:  &u.rbDPIAuto,
							Text:      "Otomatik (önerilen)",
							Value:     "auto",
							OnClicked: u.onDPISourceChange,
						},
						RadioButton{
							AssignTo:  &u.rbDPIService,
							Text:      "Sistem Servisi",
							Value:     "service",
							OnClicked: u.onDPISourceChange,
						},
						RadioButton{
							AssignTo:  &u.rbDPIManual,
							Text:      "Manuel Yol",
							Value:     "manual",
							OnClicked: u.onDPISourceChange,
						},
						Composite{
							AssignTo: &u.gdpiPathComp,
							Visible:  false,
							Layout:   HBox{MarginsZero: true, Spacing: 4},
							Children: []Widget{
								LineEdit{AssignTo: &u.leGDPIPath, CueBanner: `C:\GoodbyeDPI\goodbyedpi.exe`},
								PushButton{Text: "Bul", OnClicked: u.onAutoDetectGDPI, MaxSize: Size{Width: 55}},
							},
						},
						RadioButton{
							AssignTo:  &u.rbDPIDisabled,
							Text:      "Devre Dışı — Sadece Proxy + PAC",
							Value:     "disabled",
							OnClicked: u.onDPISourceChange,
						},
					}),
					u.settingsGroup("Ağ Portları", []Widget{
						Composite{
							Layout: HBox{MarginsZero: true, Spacing: 8},
							Children: []Widget{
								Label{Text: "Proxy:", TextColor: clrText, Font: Font{Family: "Segoe UI", PointSize: 9}},
								NumberEdit{AssignTo: &u.neProxyPort, MinValue: 1, MaxValue: 65535, Decimals: 0, MaxSize: Size{Width: 70}},
								Label{Text: "PAC:", TextColor: clrText, Font: Font{Family: "Segoe UI", PointSize: 9}},
								NumberEdit{AssignTo: &u.nePACPort, MinValue: 1, MaxValue: 65535, Decimals: 0, MaxSize: Size{Width: 70}},
								HSpacer{},
							},
						},
					}),
					u.settingsGroup("Sistem", []Widget{
						CheckBox{AssignTo: &u.chkSysProxy, Text: "Windows sistem proxy'sini otomatik ayarla"},
						CheckBox{AssignTo: &u.chkAutoStart, Text: "Windows ile otomatik başlat"},
					}),
					Composite{
						Layout: HBox{MarginsZero: true, Spacing: 8},
						Children: []Widget{
							PushButton{Text: "Kaydet ve Uygula", OnClicked: u.onSaveSettings, MinSize: Size{Width: 140}},
							Label{AssignTo: &u.lblSaveStatus, TextColor: clrGreen, Font: Font{Family: "Segoe UI", PointSize: 9}},
							HSpacer{},
						},
					},
				},
			},
		},
	}
}

func (u *appUI) settingsGroup(title string, children []Widget) Widget {
	all := make([]Widget, 0, len(children)+1)
	all = append(all, Label{
		Text:      strings.ToUpper(title),
		Font:      Font{Family: "Segoe UI", PointSize: 8, Bold: true},
		TextColor: clrSub,
	})
	all = append(all, children...)
	return Composite{
		Layout:   VBox{Margins: Margins{Left: 0, Right: 0, Top: 2, Bottom: 6}, Spacing: 6},
		Children: all,
	}
}

// ── Mobil paneli ──────────────────────────────────────────────────────────────

func (u *appUI) buildMobilePanel() Widget {
	return Composite{
		AssignTo: &u.panelMobile,
		Layout:   VBox{MarginsZero: true, SpacingZero: true},
		Visible:  false,
		Children: []Widget{
			ScrollView{
				AssignTo: &u.svMobile,
				Layout:   VBox{Margins: Margins{Left: 12, Right: 12, Top: 12, Bottom: 12}, Spacing: 10},
				Children: []Widget{
					Label{
						Text:      "ROUTER PAC URL (ÖNERİLEN)",
						Font:      Font{Family: "Segoe UI", PointSize: 8, Bold: true},
						TextColor: clrSub,
					},
					LineEdit{AssignTo: &u.leQRURL, ReadOnly: true},
					PushButton{Text: "Kopyala", OnClicked: u.onCopyPACURL, MaxSize: Size{Width: 80}},
					Label{
						Text:      "PC PAC URL (ALTERNATİF)",
						Font:      Font{Family: "Segoe UI", PointSize: 8, Bold: true},
						TextColor: clrSub,
					},
					LineEdit{AssignTo: &u.lePCPACURL, ReadOnly: true},
					PushButton{Text: "Kopyala", OnClicked: u.onCopyPCPACURL, MaxSize: Size{Width: 80}},
					// QR
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							HSpacer{},
							ImageView{
								AssignTo: &u.ivQR,
								Mode:     ImageViewModeZoom,
								MinSize:  Size{Width: 180, Height: 180},
								MaxSize:  Size{Width: 180, Height: 180},
							},
							HSpacer{},
						},
					},
					u.settingsGroup("Android", []Widget{
						Label{Text: "1. Telefon ve PC aynı Wi-Fi'da olsun", TextColor: clrText, Font: Font{Family: "Segoe UI", PointSize: 9}},
						Label{Text: "2. Wi-Fi'ye uzun bas → Ağı değiştir", TextColor: clrText, Font: Font{Family: "Segoe UI", PointSize: 9}},
						Label{Text: "3. Proxy → Otomatik → PAC URL'yi gir", TextColor: clrText, Font: Font{Family: "Segoe UI", PointSize: 9}},
					}),
					u.settingsGroup("iOS", []Widget{
						Label{Text: "1. Ayarlar → Wi-Fi → (i) simgesi", TextColor: clrText, Font: Font{Family: "Segoe UI", PointSize: 9}},
						Label{Text: "2. Proxy Yapılandırması → Otomatik", TextColor: clrText, Font: Font{Family: "Segoe UI", PointSize: 9}},
						Label{Text: "3. PAC URL'yi gir → Kaydet", TextColor: clrText, Font: Font{Family: "Segoe UI", PointSize: 9}},
					}),
				},
			},
		},
	}
}

// ── Kayıtlar paneli ───────────────────────────────────────────────────────────

func (u *appUI) buildLogsPanel() Widget {
	return Composite{
		AssignTo: &u.panelLogs,
		Layout:   VBox{Margins: Margins{Left: 10, Right: 10, Top: 8, Bottom: 8}, Spacing: 6},
		Visible:  false,
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 6},
				Children: []Widget{
					PushButton{Text: "Temizle", OnClicked: u.onClearLogs, MaxSize: Size{Width: 75}},
					PushButton{Text: "Kopyala", OnClicked: u.onCopyLogs, MaxSize: Size{Width: 75}},
					CheckBox{AssignTo: &u.chkAutoScroll, Text: "Oto. kaydır", Checked: true},
					HSpacer{},
					Label{AssignTo: &u.lblLogCount, Text: "0 kayıt", TextColor: clrSub,
						Font: Font{Family: "Segoe UI", PointSize: 9}},
				},
			},
			TableView{
				AssignTo:            &u.tvLog,
				Model:               u.logModel,
				LastColumnStretched: true,
				AlternatingRowBG:    true,
				Columns: []TableViewColumn{
					{Title: "Zaman", Width: 65},
					{Title: "Seviye", Width: 52},
					{Title: "Mesaj"},
				},
			},
		},
	}
}

// ── Tema uygulama ─────────────────────────────────────────────────────────────

func setBrush(c *walk.Composite, col walk.Color) {
	if c == nil {
		return
	}
	if br, err := walk.NewSolidColorBrush(col); err == nil {
		c.SetBackground(br)
	}
}

func (u *appUI) applyTheme() {
	if br, err := walk.NewSolidColorBrush(clrBg); err == nil {
		u.mw.SetBackground(br)
	}
	setBrush(u.titleBar, clrSidebar)
	setBrush(u.sidebarComp, clrSidebar)
	setBrush(u.btnPanel, clrBtnOff)

	// ScrollView arka planları
	for _, sv := range []*walk.ScrollView{u.svStatus, u.svSettings, u.svMobile} {
		if sv == nil {
			continue
		}
		if br, err := walk.NewSolidColorBrush(clrBg); err == nil {
			sv.SetBackground(br)
		}
	}

	// Başlat/durdur click handler'ı
	if u.btnPanel != nil {
		u.btnPanel.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
			if button == walk.LeftButton {
				u.mw.Synchronize(u.onToggle)
			}
		})
	}
	if u.lblToggle != nil {
		u.lblToggle.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
			if button == walk.LeftButton {
				u.mw.Synchronize(u.onToggle)
			}
		})
	}

	if u.ivLogo != nil {
		if bmp := getLogoBitmap(false); bmp != nil {
			u.ivLogo.SetImage(bmp)
		}
	}
}

// ── Tray ─────────────────────────────────────────────────────────────────────

func (u *appUI) setupTray() {
	ni, err := walk.NewNotifyIcon(u.mw)
	if err != nil {
		return
	}
	u.ni = ni
	if ico := getTrayIcon(false); ico != nil {
		ni.SetIcon(ico)
	}
	ni.SetToolTip(appName + " — DPI Bypass Proxy")
	ni.SetVisible(true)

	menu := ni.ContextMenu()
	u.niMenu = menu

	openAct := walk.NewAction()
	openAct.SetText("Arayüzü Aç")
	openAct.Triggered().Attach(u.showWindow)
	menu.Actions().Add(openAct)
	menu.Actions().Add(walk.NewSeparatorAction())

	toggleAct := walk.NewAction()
	toggleAct.SetText("Başlat / Durdur")
	toggleAct.Triggered().Attach(func() { u.mw.Synchronize(u.onToggle) })
	menu.Actions().Add(toggleAct)
	menu.Actions().Add(walk.NewSeparatorAction())

	quitAct := walk.NewAction()
	quitAct.SetText("Çıkış")
	quitAct.Triggered().Attach(func() { u.mw.Synchronize(u.onQuit) })
	menu.Actions().Add(quitAct)

	ni.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			u.showWindow()
		}
	})
}

func (u *appUI) showWindow() {
	u.mw.Show()
	u.mw.Activate()
	win.ShowWindow(u.mw.Handle(), win.SW_RESTORE)
}

func (u *appUI) onQuit() {
	appExiting = true
	u.mw.Hide()
	if u.ni != nil {
		u.ni.SetVisible(false)
	}
	watchdog.Stop()
	if gdpi.IsRunning() {
		gdpi.Stop()
	}
	c := getConfig()
	if c.SetSystemProxy {
		RestoreSystemProxy()
	}
	if c.DNSMode != "unchanged" && c.DNSMode != "" {
		RestoreDNS()
	}
	localIP := g.localIP
	go func() {
		setPACDirect()
		done := make(chan struct{}, 1)
		go func() { pushRouterPAC(localIP, "direct", 0); done <- struct{}{} }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		os.Exit(0)
	}()
}

// ── Durum yenileme ───────────────────────────────────────────────────────────

func (u *appUI) refreshStatus() {
	s := buildStatus()

	trayIco := getTrayIcon(s.Running)
	if u.ni != nil {
		if trayIco != nil {
			u.ni.SetIcon(trayIco)
		}
		if s.Running {
			u.ni.SetToolTip(fmt.Sprintf("%s — Aktif (:%d)", appName, s.ProxyPort))
		} else {
			u.ni.SetToolTip(appName + " — Durduruldu")
		}
	}
	if winIco := getIcon(s.Running); winIco != nil {
		u.mw.SetIcon(winIco)
	}

	if bmp := getLogoBitmap(s.Running); bmp != nil {
		if u.ivLogo != nil {
			u.ivLogo.SetImage(bmp)
		}
		if u.ivTitleLogo != nil {
			u.ivTitleLogo.SetImage(bmp)
		}
	}

	if u.lblStatus != nil {
		if s.Running {
			u.lblStatus.SetText("●  BAĞLI")
			u.lblStatus.SetTextColor(clrGreen)
		} else {
			u.lblStatus.SetText("●  BAĞLI DEĞİL")
			u.lblStatus.SetTextColor(clrRed)
		}
	}

	if u.lblToggle != nil {
		if s.Running {
			u.lblToggle.SetText("■   DURDUR")
			setBrush(u.btnPanel, clrBtnOn)
		} else {
			u.lblToggle.SetText("▶   BAŞLAT")
			setBrush(u.btnPanel, clrBtnOff)
		}
	}

	if u.lblIPInfo != nil {
		if s.Running && s.LocalIP != "" {
			u.lblIPInfo.SetText(fmt.Sprintf("%s : %d", s.LocalIP, s.ProxyPort))
		} else {
			u.lblIPInfo.SetText("—")
		}
	}

	setLbl(u.lblUptime, s.Uptime)
	setLbl(u.lblActive, strconv.FormatInt(s.ActiveConns, 10))
	setLbl(u.lblBytes, s.TotalBytes)

	setLbl(u.lblTotal, strconv.FormatInt(s.TotalConns, 10))
	setLbl(u.lblErrors, strconv.FormatInt(s.Errors, 10))
	setLbl(u.lblRestarts, strconv.FormatInt(s.Restarts, 10))

	if s.Running {
		setLbl(u.lblProxy, fmt.Sprintf("✔  :%d", s.ProxyPort))
		setLbl(u.lblPACSvc, fmt.Sprintf("✔  :%d", s.PACPort))
	} else {
		setLbl(u.lblProxy, "✘  Durduruldu")
		setLbl(u.lblPACSvc, "✘  Durduruldu")
	}

	if s.GDPIRunning {
		setLbl(u.lblGDPI, "✔  "+s.DPISourceLabel)
	} else if s.DPISourceLabel == "Devre Dışı" {
		setLbl(u.lblGDPI, "—  Devre Dışı")
	} else {
		setLbl(u.lblGDPI, "—  "+s.DPISourceLabel)
	}

	if s.DNSMode != "" && s.DNSMode != "unchanged" {
		setLbl(u.lblDNSSvc, "✔  "+s.DNSName)
	} else {
		setLbl(u.lblDNSSvc, "—  Değiştirilmedi")
	}

	if s.SetSysProxy {
		if s.Running {
			setLbl(u.lblSysProxy, "✔  Aktif")
		} else {
			setLbl(u.lblSysProxy, "✘  Pasif")
		}
	} else {
		setLbl(u.lblSysProxy, "—  Devre dışı")
	}

	setLbl(u.lblIP, s.LocalIP)
	if s.LocalIP != "" {
		setLbl(u.lblProxyAddr, fmt.Sprintf("%s:%d", s.LocalIP, s.ProxyPort))
	}
	setLbl(u.lblPACURL, s.PACUrl)
	setLbl(u.lblDPIMode, s.DPIModeName)
	setLbl(u.lblChunkSvc, fmt.Sprintf("%d byte", s.ChunkSize))
	setLbl(u.lblISPSvc, s.ISPName)
	setLbl(u.lblGDPIFlags, s.GDPIFlags)
	setLbl(u.lblDNSMode, s.DNSName)

	if u.lblStatusBar != nil {
		proxyStr, pacStr := "✘", "✘"
		if s.Running {
			proxyStr, pacStr = "✔", "✔"
		}
		dpiStr := s.DPISourceLabel
		if dpiStr == "" {
			dpiStr = "—"
		}
		u.lblStatusBar.SetText(fmt.Sprintf("Proxy: %s  PAC: %s  DPI: %s", proxyStr, pacStr, dpiStr))
	}

	u.refreshLogs()
}

func setLbl(lbl *walk.Label, text string) {
	if lbl != nil {
		lbl.SetText(text)
	}
}

// ── Toggle ────────────────────────────────────────────────────────────────────

func (u *appUI) onToggle() {
	if g.running {
		watchdog.Stop()
		go func() {
			g.stop()
			u.mw.Synchronize(u.refreshStatus)
		}()
		u.refreshStatus()
	} else {
		if err := g.start(); err != nil {
			walk.MsgBox(u.mw, "Başlatma Hatası", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
			return
		}
		u.refreshStatus()
	}
}

// ── Ayarlar yükleme / kaydetme ───────────────────────────────────────────────

func (u *appUI) loadSettingsForm() {
	c := getConfig()

	for i, m := range dpiModeValues {
		if m == c.DPIMode {
			u.cbDPI.SetCurrentIndex(i)
			break
		}
	}
	if u.leCustomFlags != nil {
		u.leCustomFlags.SetText(c.CustomFlags)
	}
	u.onDPIModeChange()

	for i, v := range chunkValues {
		if v == c.ChunkSize {
			u.cbChunk.SetCurrentIndex(i)
			break
		}
	}
	for i, v := range ispValues {
		if v == c.ISP {
			u.cbISP.SetCurrentIndex(i)
			break
		}
	}
	for i, v := range dnsValues {
		if v == c.DNSMode {
			u.cbDNS.SetCurrentIndex(i)
			break
		}
	}

	u.chkSysProxy.SetChecked(c.SetSystemProxy)
	u.chkAutoStart.SetChecked(startupEnabled())

	switch c.DPISource {
	case "service":
		if u.rbDPIService != nil {
			u.rbDPIService.SetChecked(true)
		}
	case "manual":
		if u.rbDPIManual != nil {
			u.rbDPIManual.SetChecked(true)
		}
	case "disabled":
		if u.rbDPIDisabled != nil {
			u.rbDPIDisabled.SetChecked(true)
		}
	default:
		if u.rbDPIAuto != nil {
			u.rbDPIAuto.SetChecked(true)
		}
	}
	if u.leGDPIPath != nil {
		u.leGDPIPath.SetText(c.GDPIPath)
	}
	u.onDPISourceChange()

	u.neProxyPort.SetValue(float64(c.ProxyPort))
	u.nePACPort.SetValue(float64(c.PACPort))
}

func (u *appUI) onDPIModeChange() {
	isCustom := u.cbDPI != nil && u.cbDPI.CurrentIndex() == 3
	if u.leCustomFlags != nil {
		u.leCustomFlags.SetVisible(isCustom)
	}
}

func (u *appUI) onDPISourceChange() {
	isManual := u.rbDPIManual != nil && u.rbDPIManual.Checked()
	if u.gdpiPathComp != nil {
		u.gdpiPathComp.SetVisible(isManual)
	}
}

func (u *appUI) onSaveSettings() {
	if u.lblSaveStatus != nil {
		u.lblSaveStatus.SetText("")
	}

	idx := 0
	if u.cbDPI != nil {
		idx = u.cbDPI.CurrentIndex()
	}
	var dpiMode string
	if idx >= 0 && idx < len(dpiModeValues) {
		dpiMode = dpiModeValues[idx]
	} else {
		dpiMode = "balanced"
	}

	chunk := 40
	if u.cbChunk != nil {
		ci := u.cbChunk.CurrentIndex()
		if ci >= 0 && ci < len(chunkValues) {
			chunk = chunkValues[ci]
		}
	}

	isp := "auto"
	if u.cbISP != nil {
		ci := u.cbISP.CurrentIndex()
		if ci >= 0 && ci < len(ispValues) {
			isp = ispValues[ci]
		}
	}

	dnsMode := "unchanged"
	if u.cbDNS != nil {
		ci := u.cbDNS.CurrentIndex()
		if ci >= 0 && ci < len(dnsValues) {
			dnsMode = dnsValues[ci]
		}
	}

	customFlags := ""
	if u.leCustomFlags != nil {
		customFlags = strings.TrimSpace(u.leCustomFlags.Text())
	}

	dpiSource := "auto"
	switch {
	case u.rbDPIService != nil && u.rbDPIService.Checked():
		dpiSource = "service"
	case u.rbDPIManual != nil && u.rbDPIManual.Checked():
		dpiSource = "manual"
	case u.rbDPIDisabled != nil && u.rbDPIDisabled.Checked():
		dpiSource = "disabled"
	}

	gdpiPath := ""
	if u.leGDPIPath != nil {
		gdpiPath = strings.TrimSpace(u.leGDPIPath.Text())
	}

	nc := Config{
		DPIMode:        dpiMode,
		ChunkSize:      chunk,
		ISP:            isp,
		CustomFlags:    customFlags,
		DNSMode:        dnsMode,
		SetSystemProxy: u.chkSysProxy != nil && u.chkSysProxy.Checked(),
		DPISource:      dpiSource,
		GDPIPath:       gdpiPath,
		ProxyPort:      int(u.neProxyPort.Value()),
		PACPort:        int(u.nePACPort.Value()),
	}
	if nc.ProxyPort < 1 || nc.ProxyPort > 65535 {
		nc.ProxyPort = 8888
	}
	if nc.PACPort < 1 || nc.PACPort > 65535 {
		nc.PACPort = 8080
	}

	if err := setConfig(nc); err != nil {
		if u.lblSaveStatus != nil {
			u.lblSaveStatus.SetText("✘ " + err.Error())
		}
		return
	}

	setStartup(u.chkAutoStart != nil && u.chkAutoStart.Checked())

	if g.running {
		go g.restart()
	}

	logInfo(fmt.Sprintf("Ayarlar: DPI=%s chunk=%d ISP=%s DNS=%s", nc.DPIMode, nc.ChunkSize, nc.ISP, nc.DNSMode))

	if u.lblSaveStatus != nil {
		u.lblSaveStatus.SetText("✔ Kaydedildi")
		go func() {
			time.Sleep(3 * time.Second)
			u.mw.Synchronize(func() {
				if u.lblSaveStatus != nil {
					u.lblSaveStatus.SetText("")
				}
			})
		}()
	}
}

func (u *appUI) onAutoDetectGDPI() {
	path := FindGDPIExe()
	if path != "" {
		if u.leGDPIPath != nil {
			u.leGDPIPath.SetText(path)
		}
		walk.MsgBox(u.mw, "Bulundu", "goodbyedpi.exe:\n"+path, walk.MsgBoxIconInformation|walk.MsgBoxOK)
	} else {
		walk.MsgBox(u.mw, "Bulunamadı", "Standart konumlarda bulunamadı.", walk.MsgBoxIconWarning|walk.MsgBoxOK)
	}
}

// ── Mobil aksiyonlar ─────────────────────────────────────────────────────────

func (u *appUI) onCopyPACURL() {
	if u.leQRURL != nil {
		if txt := u.leQRURL.Text(); txt != "" {
			walk.Clipboard().SetText(txt)
		}
	}
}

func (u *appUI) onCopyPCPACURL() {
	if u.lePCPACURL != nil {
		if txt := u.lePCPACURL.Text(); txt != "" {
			walk.Clipboard().SetText(txt)
		}
	}
}

func (u *appUI) updateQR() {
	c := getConfig()
	ip := g.localIP
	if ip == "" {
		ip = "127.0.0.1"
	}
	gateway := guessGatewayIP(ip)
	routerPACURL := fmt.Sprintf("http://%s:8090/proxy.pac", gateway)
	pcPACURL := fmt.Sprintf("http://%s:%d/proxy.pac", ip, c.PACPort)

	if u.leQRURL != nil {
		u.leQRURL.SetText(routerPACURL)
	}
	if u.lePCPACURL != nil {
		u.lePCPACURL.SetText(pcPACURL)
	}

	pngBytes, err := qrcode.Encode(routerPACURL, qrcode.High, 200)
	if err != nil {
		return
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return
	}
	tmpFile, err := os.CreateTemp("", "spac3dpi_qr_*.png")
	if err != nil {
		return
	}
	png.Encode(tmpFile, img)
	tmpFile.Close()
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	bmp, err := walk.NewBitmapFromFile(tmpPath)
	if err != nil {
		return
	}
	if u.ivQR != nil {
		old := u.ivQR.Image()
		u.ivQR.SetImage(bmp)
		if old != nil {
			old.Dispose()
		}
	}
}

// ── Log aksiyonlar ───────────────────────────────────────────────────────────

func (u *appUI) onClearLogs() {
	appLog.Clear()
	u.logModel.entries = nil
	u.logModel.PublishRowsReset()
	if u.lblLogCount != nil {
		u.lblLogCount.SetText("0 kayıt")
	}
}

func (u *appUI) onCopyLogs() {
	entries := appLog.All()
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("[%s] %s %s\n", e.Time, e.Level, e.Message))
	}
	walk.Clipboard().SetText(sb.String())
}

func (u *appUI) refreshLogs() {
	entries := appLog.All()
	u.logModel.entries = entries
	u.logModel.PublishRowsReset()
	if u.lblLogCount != nil {
		u.lblLogCount.SetText(fmt.Sprintf("%d kayıt", len(entries)))
	}
	if u.chkAutoScroll != nil && u.chkAutoScroll.Checked() && len(entries) > 0 {
		if u.tvLog != nil {
			u.tvLog.EnsureItemVisible(len(entries) - 1)
		}
	}
}
