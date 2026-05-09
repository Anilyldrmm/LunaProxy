package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"

	"github.com/lxn/walk"
)

// ── İkon üretimi ─────────────────────────────────────────────────────────────

func makeIconImage(active bool) image.Image {
	const sz = 32
	img := image.NewNRGBA(image.Rect(0, 0, sz, sz))

	bgCol := color.NRGBA{R: 13, G: 18, B: 30, A: 255}
	for y := 0; y < sz; y++ {
		for x := 0; x < sz; x++ {
			img.SetNRGBA(x, y, bgCol)
		}
	}

	var fg color.NRGBA
	if active {
		fg = color.NRGBA{R: 0, G: 210, B: 175, A: 255}
	} else {
		fg = color.NRGBA{R: 70, G: 125, B: 210, A: 255}
	}

	cx, baseY := 16.0, 25.0
	for dy := -2.0; dy <= 2.0; dy++ {
		for dx := -2.0; dx <= 2.0; dx++ {
			if dx*dx+dy*dy <= 4 {
				setpx(img, int(cx+dx), int(baseY+dy), fg)
			}
		}
	}
	for _, r := range []float64{6.0, 10.5, 15.0} {
		for t := 0.0; t <= math.Pi; t += 0.03 {
			px := cx - r*math.Cos(t)
			py := baseY - r*math.Sin(t)
			setpx(img, int(math.Round(px)), int(math.Round(py)), fg)
			setpx(img, int(math.Round(px)), int(math.Round(py))-1, fg)
		}
	}
	return img
}

func setpx(img *image.NRGBA, x, y int, col color.NRGBA) {
	b := img.Bounds()
	if x >= b.Min.X && x < b.Max.X && y >= b.Min.Y && y < b.Max.Y {
		img.SetNRGBA(x, y, col)
	}
}

// makeICOBytes — tek-resim ICO formatında bayt dizisi döner (Windows Vista+ PNG-in-ICO destekler).
func makeICOBytes(active bool) []byte {
	img := makeIconImage(active)
	var pngBuf bytes.Buffer
	png.Encode(&pngBuf, img)
	pngData := pngBuf.Bytes()

	var ico bytes.Buffer
	// GRPICONDIR header
	binary.Write(&ico, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&ico, binary.LittleEndian, uint16(1)) // type = icon
	binary.Write(&ico, binary.LittleEndian, uint16(1)) // count = 1

	// ICONDIRENTRY
	ico.WriteByte(32) // width
	ico.WriteByte(32) // height
	ico.WriteByte(0)  // color count
	ico.WriteByte(0)  // reserved
	binary.Write(&ico, binary.LittleEndian, uint16(1))               // planes
	binary.Write(&ico, binary.LittleEndian, uint16(32))              // bit count
	binary.Write(&ico, binary.LittleEndian, uint32(len(pngData)))    // size
	binary.Write(&ico, binary.LittleEndian, uint32(6+16))            // offset = header(6) + one entry(16)

	// PNG image data
	ico.Write(pngData)
	return ico.Bytes()
}

// loadWalkIcon — ICO baytlarını geçici dosyaya yazar, walk.Icon olarak yükler.
func loadWalkIcon(active bool) *walk.Icon {
	data := makeICOBytes(active)
	tmp, err := os.CreateTemp("", "spac3dpi_*.ico")
	if err != nil {
		return nil
	}
	tmp.Write(data)
	tmp.Close()
	path := tmp.Name()

	ico, err := walk.NewIconFromFile(path)
	os.Remove(path) // LoadImage memoria yükledi, dosyaya gerek yok
	if err != nil {
		return nil
	}
	return ico
}

var (
	icoDefault *walk.Icon
	icoActive  *walk.Icon
)

func getIcon(active bool) *walk.Icon {
	if active {
		if icoActive == nil {
			icoActive = loadWalkIcon(true)
		}
		return icoActive
	}
	if icoDefault == nil {
		icoDefault = loadWalkIcon(false)
	}
	return icoDefault
}
