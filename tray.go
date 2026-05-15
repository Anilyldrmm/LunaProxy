//go:build windows

package main

import (
	"os"

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
	systray.SetTooltip("SpAC3DPI — DPI Bypass")

	mOpen := systray.AddMenuItem("Arayüzü Aç", "Pencereyi göster")
	systray.AddSeparator()
	trayStart = systray.AddMenuItem("Başlat", "Proxy'yi başlat")
	trayStop = systray.AddMenuItem("Durdur", "Proxy'yi durdur")
	trayStop.Hide()
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Çıkış", "Programı kapat")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				showWindow()
			case <-trayStart.ClickedCh:
				if err := g.start(); err != nil {
					logError("Tray başlatma hatası: " + err.Error())
				}
				updateTrayState(true)
				pushStatus()
			case <-trayStop.ClickedCh:
				g.stop()
				updateTrayState(false)
				pushStatus()
			case <-mQuit.ClickedCh:
				appExiting = true
				g.shutdown()
				systray.Quit()
				os.Exit(0)
			}
		}
	}()
}

func onTrayExit() {}

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
