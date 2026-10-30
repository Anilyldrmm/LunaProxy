//go:build windows

package main

import (
	"fmt"
	"os"
	"time"

	"fyne.io/systray"
)

var (
	trayStart *systray.MenuItem
	trayStop  *systray.MenuItem
)

func initTray() {
	go systray.Run(onTrayReady, onTrayExit)
}

func onTrayReady() {
	systray.SetIcon(trayIcon(false))
	systray.SetTooltip("LunaProxy — DPI Bypass")
	// Sol tık (tek veya çift) pencereyi açar; menü sağ tıkta (Windows tepsi standardı).
	systray.SetOnTapped(showWindow)

	mOpen := systray.AddMenuItem("Arayüzü Aç", "Pencereyi göster")
	systray.AddSeparator()
	trayStart = systray.AddMenuItem("Başlat", "Proxy'yi başlat")
	trayStop = systray.AddMenuItem("Durdur", "Proxy'yi durdur")
	trayStop.Hide()
	systray.AddSeparator()
	mUpdate := systray.AddMenuItem("Güncellemeleri Kontrol Et", "GitHub'da yeni sürüm ara")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Çıkış", "Programı kapat")

	// Otomatik başlatma durumunu yansıt (g.start() biraz sonra tamamlanır)
	go func() {
		for i := 0; i < 20; i++ {
			time.Sleep(100 * time.Millisecond)
			if g.running {
				updateTrayState(true)
				return
			}
		}
	}()

	go func() {
		for {
			func() {
				defer func() { recover() }() //nolint:errcheck
				select {
				case <-mOpen.ClickedCh:
					showWindow()
			case <-trayStart.ClickedCh:
				toggleProxy(true)
			case <-trayStop.ClickedCh:
				toggleProxy(false)
			case <-mUpdate.ClickedCh:
				go func() {
					tag, url, err := CheckUpdate()
					if err != nil {
						logWarn("Güncelleme kontrolü başarısız: " + err.Error())
						return
					}
					if tag != "" {
						pendingUpdateTag.Store(tag)
						pendingUpdateURL.Store(url)
						pushStatus()
						showWindow()
					} else {
						logInfo("Güncel sürüm kullanılıyor: " + Version)
					}
				}()
			case <-mQuit.ClickedCh:
				appExiting = true
				systray.Quit()
				os.Exit(0)
			}
			}()
		}
	}()
}

func onTrayExit() {}

// toggleProxy — proxy'yi arka planda başlatır/durdurur. Start/stop saniyeler sürebilir; çağıran
// (tepsi döngüsü, UI) bloklanırsa o sürede gelen tıklamalar kaybolur ve uygulama donmuş görünür.
// Tepsi ve UI aynı kilidi kullanır: önceki işlem bitmeden gelen istek yok sayılır.
func toggleProxy(start bool) {
	if !toggleBusy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer toggleBusy.Store(false)
		if start {
			if err := g.start(); err != nil {
				logError("Başlatma hatası: " + err.Error())
				evalJS(fmt.Sprintf(`showError(%s)`, jsonEscape(err.Error())))
			}
		} else {
			g.stop()
		}
		updateTrayState(g.running)
		pushStatus()
	}()
}

// updateTrayState — proxy durumuna göre tray ikonunu ve menüsünü günceller.
func updateTrayState(running bool) {
	systray.SetIcon(trayIcon(running))
	if running {
		trayStart.Hide()
		trayStop.Show()
	} else {
		trayStop.Hide()
		trayStart.Show()
	}
}

// trayIcon — Windows systray için ICO formatı döner (PNG yerine ICO daha güvenilir).
func trayIcon(active bool) []byte {
	return makeTrayICOBytes(active)
}
