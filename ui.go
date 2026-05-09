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

// ── Log tablo modeli ──────────────────────────────────────────────────────────

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

// ── Widget referansları ───────────────────────────────────────────────────────

type appUI struct {
	mw        *walk.MainWindow
	ni        *walk.NotifyIcon
	niMenu    *walk.Menu
	heroPanel *walk.Composite

	// Hero — her zaman görünür
	btnToggle *walk.PushButton
	lblStatus *walk.Label
	lblUptime *walk.Label
	lblActive *walk.Label
	lblBytes  *walk.Label

	// Durum sekmesi
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

	// Ayarlar sekmesi
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

	// Mobil sekmesi
	ivQR         *walk.ImageView
	leQRURL      *walk.LineEdit // router PAC URL (önerilen)
	lePCPACURL   *walk.LineEdit // PC direkt PAC URL

	// Kayıtlar sekmesi
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
		Title:    "SpAC3DPI — DPI Bypass Proxy",
		Icon:     getIcon(false),
		MinSize:  Size{Width: 800, Height: 540},
		Size:     Size{Width: 900, Height: 640},
		Layout:   VBox{MarginsZero: true, SpacingZero: true},
		Children: []Widget{
			u.heroSection(),
			TabWidget{
				Pages: []TabPage{
					{Title: "  Durum  ", Content: u.statusPage()},
					{Title: "  Ayarlar  ", Content: u.settingsPage()},
					{Title: "  Mobil  ", Content: u.mobilePage()},
					{Title: "  Kayıtlar  ", Content: u.logsPage()},
				},
			},
		},
	}).Create(); err != nil {
		panic(err)
	}

	u.setupTray()
	u.darkTitleBar()
	u.applyDarkHero()

	// X her zaman tray'e küçültür — çıkış yalnızca tray menüsünden
	u.mw.Closing().Attach(func(canceled *bool, _ walk.CloseReason) {
		*canceled = true
		u.mw.Hide()
	})

	u.loadSettingsForm()
	u.updateQR()
	u.refreshStatus()

	// İlk açılışta pencereyi hemen göster
	u.mw.Show()
	u.mw.Activate()

	// Proxy arka planda başlat
	go func() {
		if err := g.start(); err != nil {
			logError("Otomatik başlatma başarısız: " + err.Error())
		}
		u.mw.Synchronize(u.refreshStatus)
	}()

	// 2s'de bir durum yenile
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for range t.C {
			u.mw.Synchronize(u.refreshStatus)
		}
	}()

	u.mw.Run()
}

// ── Hero section (Windscribe ilhamlı, her zaman görünür) ─────────────────────

func (u *appUI) heroSection() Widget {
	return Composite{
		AssignTo: &u.heroPanel,
		Layout:   VBox{Margins: Margins{Left: 24, Right: 24, Top: 18, Bottom: 16}, Spacing: 14},
		Children: []Widget{
			// Başlık satırı
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 10},
				Children: []Widget{
					Label{
						Text:      "⚡ SpAC3DPI",
						Font:      Font{Bold: true, PointSize: 13},
						TextColor: walk.RGB(235, 235, 245),
					},
					Label{
						Text:      "DPI Bypass Proxy",
						Font:      Font{PointSize: 9},
						TextColor: walk.RGB(120, 120, 145),
					},
					HSpacer{},
				},
			},
			// Durum + mini istatistikler
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 28},
				Children: []Widget{
					Label{
						AssignTo:  &u.lblStatus,
						Text:      "● BAĞLI DEĞİL",
						Font:      Font{Bold: true, PointSize: 16},
						TextColor: walk.RGB(220, 75, 75),
					},
					HSpacer{},
					u.miniStat(&u.lblUptime, "SÜRE"),
					u.miniStat(&u.lblActive, "BAĞ"),
					u.miniStat(&u.lblBytes, "VERİ"),
				},
			},
			// Tam genişlik toggle butonu
			PushButton{
				AssignTo:  &u.btnToggle,
				Text:      "▶  BAŞLAT",
				OnClicked: u.onToggle,
				MinSize:   Size{Height: 42},
				Font:      Font{Bold: true, PointSize: 10},
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
				Font:      Font{Bold: true, PointSize: 11},
				TextColor: walk.RGB(235, 235, 245),
				Alignment: AlignHCenterVCenter,
			},
			Label{
				Text:      caption,
				Font:      Font{PointSize: 7},
				TextColor: walk.RGB(120, 120, 145),
				Alignment: AlignHCenterVCenter,
			},
		},
	}
}

// applyDarkHero — hero composite'e koyu arka plan uygular.
func (u *appUI) applyDarkHero() {
	bg, err := walk.NewSolidColorBrush(walk.RGB(28, 28, 35))
	if err != nil {
		return
	}
	u.heroPanel.SetBackground(bg)
}

// ── Durum sekmesi ────────────────────────────────────────────────────────────

func (u *appUI) statusPage() Widget {
	return ScrollView{
		Layout: VBox{Margins: Margins{Left: 12, Right: 12, Top: 10, Bottom: 10}, Spacing: 8},
		Children: []Widget{
			GroupBox{
				Title:  "İstatistikler",
				Layout: Grid{Columns: 3, Spacing: 6},
				Children: []Widget{
					u.statCard("Toplam İstek", &u.lblTotal),
					u.statCard("Hatalar", &u.lblErrors),
					u.statCard("Watchdog Restart", &u.lblRestarts),
				},
			},
			GroupBox{
				Title:  "Servis Durumu",
				Layout: Grid{Columns: 2, Spacing: 5},
				Children: []Widget{
					Label{Text: "HTTP Proxy"}, Label{AssignTo: &u.lblProxy, Text: "—"},
					Label{Text: "PAC Sunucu"}, Label{AssignTo: &u.lblPACSvc, Text: "—"},
					Label{Text: "GoodbyeDPI"}, Label{AssignTo: &u.lblGDPI, Text: "—"},
					Label{Text: "DNS"}, Label{AssignTo: &u.lblDNSSvc, Text: "—"},
					Label{Text: "Sistem Proxy"}, Label{AssignTo: &u.lblSysProxy, Text: "—"},
				},
			},
			GroupBox{
				Title:  "Ağ Bilgisi",
				Layout: Grid{Columns: 2, Spacing: 5},
				Children: []Widget{
					Label{Text: "PC IP"}, Label{AssignTo: &u.lblIP, Text: "—"},
					Label{Text: "Proxy Adresi"}, Label{AssignTo: &u.lblProxyAddr, Text: "—"},
					Label{Text: "PAC URL"}, Label{AssignTo: &u.lblPACURL, Text: "—"},
					Label{Text: "DPI Modu"}, Label{AssignTo: &u.lblDPIMode, Text: "—"},
					Label{Text: "Chunk"}, Label{AssignTo: &u.lblChunkSvc, Text: "—"},
					Label{Text: "ISP"}, Label{AssignTo: &u.lblISPSvc, Text: "—"},
					Label{Text: "GDPI Bayrakları"}, Label{AssignTo: &u.lblGDPIFlags, Text: "—"},
					Label{Text: "DNS Sağlayıcı"}, Label{AssignTo: &u.lblDNSMode, Text: "—"},
				},
			},
		},
	}
}

func (u *appUI) statCard(title string, ref **walk.Label) Widget {
	return GroupBox{
		Title:  title,
		Layout: VBox{Margins: Margins{Left: 8, Right: 8, Top: 4, Bottom: 6}},
		Children: []Widget{
			Label{
				AssignTo:  ref,
				Text:      "—",
				Font:      Font{Bold: true, PointSize: 20},
				Alignment: AlignHCenterVCenter,
			},
		},
	}
}

// ── Ayarlar sekmesi ──────────────────────────────────────────────────────────

func (u *appUI) settingsPage() Widget {
	return ScrollView{
		Layout: VBox{Margins: Margins{Left: 12, Right: 12, Top: 10, Bottom: 10}, Spacing: 8},
		Children: []Widget{
			GroupBox{
				Title:  "DPI Bypass Modu",
				Layout: VBox{},
				Children: []Widget{
					ComboBox{
						AssignTo:              &u.cbDPI,
						Model:                 []string{"⚡ Turbo — Hız öncelikli", "⚖ Dengeli — Standart (Önerilen)", "🔥 Güçlü — Paket parçalama", "🛠 Özel — Manuel bayraklar"},
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
							Label{Text: "Chunk:"},
							ComboBox{
								AssignTo:     &u.cbChunk,
								Model:        []string{"4 byte", "8 byte", "16 byte", "40 byte"},
								CurrentIndex: 3,
								MaxSize:      Size{Width: 90},
							},
							Label{Text: "ISP:"},
							ComboBox{
								AssignTo:     &u.cbISP,
								Model:        []string{"Otomatik", "Superonline / UltraNet", "Türk Telekom", "Vodafone TR", "Turkcell"},
								CurrentIndex: 0,
								MaxSize:      Size{Width: 180},
							},
							HSpacer{},
						},
					},
				},
			},
			GroupBox{
				Title:  "DNS Şifreleme",
				Layout: HBox{},
				Children: []Widget{
					ComboBox{
						AssignTo:     &u.cbDNS,
						Model:        []string{"🔒 Değiştirilmesin", "☁ Cloudflare (1.1.1.1)", "🌐 Google (8.8.8.8)", "🛡 AdGuard", "🔐 Quad9", "🌍 OpenDNS"},
						CurrentIndex: 0,
						MaxSize:      Size{Width: 270},
					},
					HSpacer{},
				},
			},
			GroupBox{
				Title:  "Sistem",
				Layout: VBox{Spacing: 4},
				Children: []Widget{
					CheckBox{AssignTo: &u.chkSysProxy, Text: "Windows sistem proxy'sini otomatik ayarla"},
					CheckBox{AssignTo: &u.chkAutoStart, Text: "Windows ile otomatik başlat"},
				},
			},
			GroupBox{
				Title:  "DPI Kaynağı",
				Layout: VBox{Spacing: 4},
				Children: []Widget{
					RadioButton{
						AssignTo:  &u.rbDPIAuto,
						Text:      "Otomatik (önerilen) — Servis → Proses → Manuel → Bundle",
						Value:     "auto",
						OnClicked: u.onDPISourceChange,
					},
					RadioButton{
						AssignTo:  &u.rbDPIService,
						Text:      "Sistem Servisi — Sadece Windows servisi kullanılır",
						Value:     "service",
						OnClicked: u.onDPISourceChange,
					},
					RadioButton{
						AssignTo:  &u.rbDPIManual,
						Text:      "Manuel Yol — Aşağıdaki goodbyedpi.exe başlatılır",
						Value:     "manual",
						OnClicked: u.onDPISourceChange,
					},
					Composite{
						AssignTo: &u.gdpiPathComp,
						Visible:  false,
						Layout:   HBox{MarginsZero: true, Spacing: 4},
						Children: []Widget{
							LineEdit{AssignTo: &u.leGDPIPath, CueBanner: `C:\GoodbyeDPI\goodbyedpi.exe`},
							PushButton{Text: "Bul", OnClicked: u.onAutoDetectGDPI, MaxSize: Size{Width: 60}},
						},
					},
					RadioButton{
						AssignTo:  &u.rbDPIDisabled,
						Text:      "Devre Dışı — Sadece Proxy + PAC çalışır",
						Value:     "disabled",
						OnClicked: u.onDPISourceChange,
					},
				},
			},
			GroupBox{
				Title:  "Ağ Portları",
				Layout: HBox{},
				Children: []Widget{
					Label{Text: "Proxy:"},
					NumberEdit{AssignTo: &u.neProxyPort, MinValue: 1, MaxValue: 65535, Decimals: 0, MaxSize: Size{Width: 80}},
					Label{Text: "  PAC:"},
					NumberEdit{AssignTo: &u.nePACPort, MinValue: 1, MaxValue: 65535, Decimals: 0, MaxSize: Size{Width: 80}},
					HSpacer{},
				},
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					PushButton{Text: "💾  Kaydet ve Uygula", OnClicked: u.onSaveSettings},
					Label{AssignTo: &u.lblSaveStatus},
					HSpacer{},
				},
			},
		},
	}
}

// ── Mobil sekmesi ─────────────────────────────────────────────────────────────

func (u *appUI) mobilePage() Widget {
	return ScrollView{
		Layout: VBox{Margins: Margins{Left: 12, Right: 12, Top: 10, Bottom: 10}, Spacing: 10},
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 20},
				Children: []Widget{
					// Sol — QR + URL
					Composite{
						Layout: VBox{MarginsZero: true, Spacing: 6},
						Children: []Widget{
							Label{Text: "Router PAC URL (Önerilen)", Font: Font{Bold: true}},
							LineEdit{AssignTo: &u.leQRURL, ReadOnly: true},
							PushButton{Text: "Kopyala", OnClicked: u.onCopyPACURL, MaxSize: Size{Width: 90}},
							Label{Text: "PC PAC URL (alternatif)"},
							LineEdit{AssignTo: &u.lePCPACURL, ReadOnly: true},
							PushButton{Text: "Kopyala", OnClicked: u.onCopyPCPACURL, MaxSize: Size{Width: 90}},
							ImageView{
								AssignTo: &u.ivQR,
								Mode:     ImageViewModeZoom,
								MinSize:  Size{Width: 200, Height: 200},
								MaxSize:  Size{Width: 200, Height: 200},
							},
						},
					},
					// Sağ — Kurulum rehberleri
					Composite{
						Layout: VBox{MarginsZero: true, Spacing: 8},
						Children: []Widget{
							GroupBox{
								Title:  "Android",
								Layout: VBox{Spacing: 3},
								Children: []Widget{
									Label{Text: "1. Telefon ve PC aynı Wi-Fi'da olsun"},
									Label{Text: "2. Wi-Fi'ye uzun bas → Ağı değiştir"},
									Label{Text: "3. Proxy → Otomatik"},
									Label{Text: "4. PAC URL'yi yapıştır → Kaydet"},
									Label{Text: "Samsung: Wi-Fi (i) → Daha fazla → Proxy", Font: Font{Italic: true}},
								},
							},
							GroupBox{
								Title:  "iOS",
								Layout: VBox{Spacing: 3},
								Children: []Widget{
									Label{Text: "1. Ayarlar → Wi-Fi → Ağın (i) simgesi"},
									Label{Text: "2. Proxy Yapılandırması → Otomatik"},
									Label{Text: "3. PAC URL'yi gir → Kaydet"},
								},
							},
							GroupBox{
								Title:  "Windows",
								Layout: VBox{Spacing: 3},
								Children: []Widget{
									Label{Text: "1. Ayarlar → Ağ → Proxy"},
									Label{Text: "2. Otomatik proxy kurulumu → Açık"},
									Label{Text: "3. Betik adresine PAC URL'yi girin"},
								},
							},
						},
					},
				},
			},
		},
	}
}

// ── Kayıtlar sekmesi ─────────────────────────────────────────────────────────

func (u *appUI) logsPage() Widget {
	return Composite{
		Layout: VBox{Margins: Margins{Left: 12, Right: 12, Top: 10, Bottom: 10}, Spacing: 6},
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 6},
				Children: []Widget{
					PushButton{Text: "Temizle", OnClicked: u.onClearLogs, MaxSize: Size{Width: 80}},
					PushButton{Text: "Kopyala", OnClicked: u.onCopyLogs, MaxSize: Size{Width: 80}},
					CheckBox{AssignTo: &u.chkAutoScroll, Text: "Otomatik kaydır", Checked: true},
					HSpacer{},
					Label{AssignTo: &u.lblLogCount, Text: "0 kayıt"},
				},
			},
			TableView{
				AssignTo:            &u.tvLog,
				Model:               u.logModel,
				LastColumnStretched: true,
				AlternatingRowBG:    true,
				Columns: []TableViewColumn{
					{Title: "Zaman", Width: 65},
					{Title: "Seviye", Width: 55},
					{Title: "Mesaj"},
				},
			},
		},
	}
}

// ── Tray ─────────────────────────────────────────────────────────────────────

func (u *appUI) setupTray() {
	ni, err := walk.NewNotifyIcon(u.mw)
	if err != nil {
		return
	}
	u.ni = ni
	if ico := getIcon(false); ico != nil {
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
	quitAct.Triggered().Attach(func() {
		watchdog.Stop()
		gdpi.Stop()
		// Önce PAC'ı DIRECT'e al — telefonlar bir sonraki fetch'te DIRECT görür.
		// Ardından kısa bekleme: aktif bağlantılar PAC'ı yeniden çekebilsin.
		setPACDirect()
		go func() {
			time.Sleep(400 * time.Millisecond)
			g.shutdown()
			ni.SetVisible(false)
			walk.App().Exit(0)
		}()
	})
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

// ── Koyu başlık çubuğu ───────────────────────────────────────────────────────

var (
	modDwmapi        = windows.NewLazySystemDLL("dwmapi.dll")
	dwmSetWindowAttr = modDwmapi.NewProc("DwmSetWindowAttribute")
)

func (u *appUI) darkTitleBar() {
	dark := uint32(1)
	hwnd := uintptr(u.mw.Handle())
	dwmSetWindowAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&dark)), 4)
	dwmSetWindowAttr.Call(hwnd, 19, uintptr(unsafe.Pointer(&dark)), 4)
}

// ── Durum yenileme ───────────────────────────────────────────────────────────

func (u *appUI) refreshStatus() {
	s := buildStatus()

	// Tray ikon + tooltip
	if u.ni != nil {
		if ico := getIcon(s.Running); ico != nil {
			u.ni.SetIcon(ico)
		}
		if s.Running {
			u.ni.SetToolTip(fmt.Sprintf("%s — Aktif (:%d)", appName, s.ProxyPort))
		} else {
			u.ni.SetToolTip(appName + " — Durduruldu")
		}
	}
	if ico := getIcon(s.Running); ico != nil {
		u.mw.SetIcon(ico)
	}

	// Hero status label + renk
	if u.lblStatus != nil {
		if s.Running {
			u.lblStatus.SetText("● BAĞLI")
			u.lblStatus.SetTextColor(walk.RGB(72, 199, 116))
		} else {
			u.lblStatus.SetText("● BAĞLI DEĞİL")
			u.lblStatus.SetTextColor(walk.RGB(220, 75, 75))
		}
	}

	// Toggle butonu metni
	if u.btnToggle != nil {
		if s.Running {
			u.btnToggle.SetText("■  DURDUR")
		} else {
			u.btnToggle.SetText("▶  BAŞLAT")
		}
	}

	// Hero mini istatistikler
	setLbl(u.lblUptime, s.Uptime)
	setLbl(u.lblActive, strconv.FormatInt(s.ActiveConns, 10))
	setLbl(u.lblBytes, s.TotalBytes)

	// Durum sekmesi istatistikler
	setLbl(u.lblTotal, strconv.FormatInt(s.TotalConns, 10))
	setLbl(u.lblErrors, strconv.FormatInt(s.Errors, 10))
	setLbl(u.lblRestarts, strconv.FormatInt(s.Restarts, 10))

	// Servis durumu
	if s.Running {
		setLbl(u.lblProxy, fmt.Sprintf("✔ Çalışıyor — :%d", s.ProxyPort))
		setLbl(u.lblPACSvc, fmt.Sprintf("✔ Çalışıyor — :%d", s.PACPort))
	} else {
		setLbl(u.lblProxy, "✘ Durduruldu")
		setLbl(u.lblPACSvc, "✘ Durduruldu")
	}

	if s.GDPIRunning {
		setLbl(u.lblGDPI, "✔ "+s.DPISourceLabel)
	} else if s.DPISourceLabel == "Devre Dışı" {
		setLbl(u.lblGDPI, "— Devre Dışı")
	} else {
		setLbl(u.lblGDPI, "— "+s.DPISourceLabel)
	}

	if s.DNSMode != "" && s.DNSMode != "unchanged" {
		setLbl(u.lblDNSSvc, "✔ "+s.DNSName)
	} else {
		setLbl(u.lblDNSSvc, "— Değiştirilmedi")
	}

	if s.SetSysProxy {
		if s.Running {
			setLbl(u.lblSysProxy, "✔ Aktif")
		} else {
			setLbl(u.lblSysProxy, "✘ Pasif")
		}
	} else {
		setLbl(u.lblSysProxy, "— Devre dışı")
	}

	// Ağ bilgisi
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

	// Log sayacı
	if u.lblLogCount != nil {
		u.lblLogCount.SetText(fmt.Sprintf("%d kayıt", appLog.Len()))
	}
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
			g.stop() // PAC→DIRECT, 3s bekle, proxy kapat
			u.mw.Synchronize(u.refreshStatus)
		}()
		// UI'yı hemen güncelle — proxy 3s sonra kapanacak ama durum "durduruldu" göster
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
		if u.rbDPIService != nil { u.rbDPIService.SetChecked(true) }
	case "manual":
		if u.rbDPIManual != nil { u.rbDPIManual.SetChecked(true) }
	case "disabled":
		if u.rbDPIDisabled != nil { u.rbDPIDisabled.SetChecked(true) }
	default:
		if u.rbDPIAuto != nil { u.rbDPIAuto.SetChecked(true) }
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

	// DPI kaynağı değişti — çalışıyorsa restart uygulansın
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

// ── Mobil tab aksiyonlar ──────────────────────────────────────────────────────

func (u *appUI) onCopyPACURL() {
	if u.leQRURL == nil {
		return
	}
	if txt := u.leQRURL.Text(); txt != "" {
		walk.Clipboard().SetText(txt)
	}
}

func (u *appUI) onCopyPCPACURL() {
	if u.lePCPACURL == nil {
		return
	}
	if txt := u.lePCPACURL.Text(); txt != "" {
		walk.Clipboard().SetText(txt)
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

// ── Log tab aksiyonlar ────────────────────────────────────────────────────────

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
