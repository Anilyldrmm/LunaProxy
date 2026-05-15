package main

import (
	_ "embed"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

//go:embed icon.png
var rawLogoBytes []byte

// resizeLogo — box-filter ile src'yi sz×sz'ye küçültür.
func resizeLogo(src image.Image, sz int) *image.NRGBA {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, sz, sz))
	xScale := float64(sw) / float64(sz)
	yScale := float64(sh) / float64(sz)
	for dy := 0; dy < sz; dy++ {
		y0 := int(float64(dy) * yScale)
		y1 := int(float64(dy+1) * yScale)
		if y1 > sh {
			y1 = sh
		}
		for dx := 0; dx < sz; dx++ {
			x0 := int(float64(dx) * xScale)
			x1 := int(float64(dx+1) * xScale)
			if x1 > sw {
				x1 = sw
			}
			var rs, gs, bs, as, n int
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					r, g, b, a := src.At(sb.Min.X+sx, sb.Min.Y+sy).RGBA()
					rs += int(r >> 8); gs += int(g >> 8)
					bs += int(b >> 8); as += int(a >> 8)
					n++
				}
			}
			if n == 0 {
				n = 1
			}
			dst.SetNRGBA(dx, dy, color.NRGBA{
				R: uint8(rs / n), G: uint8(gs / n),
				B: uint8(bs / n), A: uint8(as / n),
			})
		}
	}
	return dst
}

// dimLogo — tam renkli logoyu gri tona çevirir (proxy durduruldu).
func dimLogo(src image.Image) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bv, a := src.At(x, y).RGBA()
			lum := uint8((19595*r + 38470*g + 7471*bv) >> 24)
			dst.SetNRGBA(x, y, color.NRGBA{R: lum, G: lum, B: lum, A: uint8(a >> 8)})
		}
	}
	return dst
}

// packICO — PNG dilimlerinden çok boyutlu ICO baytları üretir.
func packICO(sizes []int, pngs [][]byte) []byte {
	count := len(sizes)
	headerOffset := uint32(6 + 16*count)
	var ico bytes.Buffer
	binary.Write(&ico, binary.LittleEndian, uint16(0))
	binary.Write(&ico, binary.LittleEndian, uint16(1))
	binary.Write(&ico, binary.LittleEndian, uint16(count))
	off := headerOffset
	for i, sz := range sizes {
		w, h := uint8(sz), uint8(sz)
		if sz == 256 {
			w, h = 0, 0
		}
		ico.WriteByte(w); ico.WriteByte(h)
		ico.WriteByte(0); ico.WriteByte(0)
		binary.Write(&ico, binary.LittleEndian, uint16(1))
		binary.Write(&ico, binary.LittleEndian, uint16(32))
		binary.Write(&ico, binary.LittleEndian, uint32(len(pngs[i])))
		binary.Write(&ico, binary.LittleEndian, off)
		off += uint32(len(pngs[i]))
	}
	for _, p := range pngs {
		ico.Write(p)
	}
	return ico.Bytes()
}

// makeICOBytes — PNG logosundan çok boyutlu ICO üretir (Vista+ PNG-in-ICO).
func makeICOBytes(active bool) []byte {
	src, err := png.Decode(bytes.NewReader(rawLogoBytes))
	if err != nil {
		return nil
	}
	if !active {
		src = dimLogo(src)
	}
	sizes := []int{256, 48, 32, 16}
	var pngs [][]byte
	for _, sz := range sizes {
		var buf bytes.Buffer
		png.Encode(&buf, resizeLogo(src, sz))
		pngs = append(pngs, buf.Bytes())
	}
	return packICO(sizes, pngs)
}

// drawStatusDot — ikonun sağ üst köşesine yeşil/kırmızı durum noktası çizer.
func drawStatusDot(img *image.NRGBA, sz int, active bool) {
	r := sz / 6
	if r < 2 {
		r = 2
	}
	border := sz / 20
	if border < 1 {
		border = 1
	}
	cx := sz - r - border - 1
	cy := r + border + 1

	var dotCol color.NRGBA
	if active {
		dotCol = color.NRGBA{R: 72, G: 199, B: 116, A: 255} // yeşil
	} else {
		dotCol = color.NRGBA{R: 220, G: 75, B: 75, A: 255} // kırmızı
	}
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}

	outerR := r + border
	for dy := -outerR; dy <= outerR; dy++ {
		for dx := -outerR; dx <= outerR; dx++ {
			dist2 := dx*dx + dy*dy
			px, py := cx+dx, cy+dy
			if px < 0 || py < 0 || px >= sz || py >= sz {
				continue
			}
			if dist2 <= outerR*outerR {
				if dist2 <= r*r {
					img.SetNRGBA(px, py, dotCol)
				} else {
					img.SetNRGBA(px, py, white)
				}
			}
		}
	}
}

// makeTrayICOBytes — tray için durum noktası eklenmiş ICO üretir.
func makeTrayICOBytes(active bool) []byte {
	src, err := png.Decode(bytes.NewReader(rawLogoBytes))
	if err != nil {
		return nil
	}
	if !active {
		src = dimLogo(src)
	}
	sizes := []int{256, 48, 32, 16}
	var pngs [][]byte
	for _, sz := range sizes {
		resized := resizeLogo(src, sz)
		drawStatusDot(resized, sz, active)
		var buf bytes.Buffer
		png.Encode(&buf, resized)
		pngs = append(pngs, buf.Bytes())
	}
	return packICO(sizes, pngs)
}

// iconCacheDir — ICO dosyalarının kalıcı olarak saklandığı dizin.
func iconCacheDir() string {
	dir, _ := os.UserConfigDir()
	d := filepath.Join(dir, "SpAC3DPI", "cache")
	os.MkdirAll(d, 0755)
	return d
}

// logoBase64 — PNG logoyu base64 olarak döndürür (WebView2 logo inject için).
func logoBase64() string {
	return base64.StdEncoding.EncodeToString(rawLogoBytes)
}
