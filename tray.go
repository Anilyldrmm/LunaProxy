//go:build windows

package main

import (
	"bytes"
	"image"
	"image/png"

	"github.com/getlantern/systray"
)

var (
	trayStart *systray.MenuItem
	trayStop  *systray.MenuItem
)

func initTray() {
	go systray.Run(onTrayReady, onTrayExit)
}

func onTrayReady() {
	systray.SetIcon(makeIconPNG(false))
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
			}
		}
	}()
}

func onTrayExit() {}

// updateTrayState — proxy durumuna göre tray ikonunu ve menüsünü günceller.
func updateTrayState(running bool) {
	systray.SetIcon(makeIconPNG(running))
	if running {
		trayStart.Hide()
		trayStop.Show()
	} else {
		trayStop.Hide()
		trayStart.Show()
	}
}

// makeIconPNG — tray için 32×32 PNG ikonu üretir.
func makeIconPNG(active bool) []byte {
	src, err := png.Decode(bytes.NewReader(rawLogoBytes))
	if err != nil {
		return rawLogoBytes
	}
	if !active {
		src = dimLogo(src)
	}
	resized := resizeLogo(src, 32)
	return encodePNG(resized)
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}
