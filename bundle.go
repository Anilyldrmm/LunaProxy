//go:build withbundle

package main

import (
	"bytes"
	"embed"
	"os"
	"path/filepath"
)

//go:embed assets/gdpi/goodbyedpi.exe assets/gdpi/WinDivert.dll assets/gdpi/WinDivert64.sys
var bundledFS embed.FS

func BundledGDPIAvailable() bool { return true }

// ExtractBundledGDPI — gömülü GoodbyeDPI/WinDivert dosyalarını exe'nin yanındaki bin/ klasörüne çıkarır.
// Yönetici olarak çalışan süreç bu DLL'i yükleyip exe'yi çalıştırdığı için kullanıcının yazabildiği
// %AppData% kullanılmaz (sahte DLL ile yetki yükseltme); kurulumda exe Program Files'tadır, oraya
// sadece yönetici yazabilir. Var olan dosyalar her seferinde gömülü kopyayla karşılaştırılır.
func ExtractBundledGDPI() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	binDir := filepath.Join(filepath.Dir(exe), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return "", err
	}

	files := []string{
		"assets/gdpi/goodbyedpi.exe",
		"assets/gdpi/WinDivert.dll",
		"assets/gdpi/WinDivert64.sys",
	}
	for _, src := range files {
		dst := filepath.Join(binDir, filepath.Base(src))
		if err := extractFile(bundledFS, src, dst); err != nil {
			return "", err
		}
	}
	return filepath.Join(binDir, "goodbyedpi.exe"), nil
}

// extractFile — dst gömülü içerikle birebir aynı değilse (yok, eski sürüm veya değiştirilmiş) üzerine yazar.
func extractFile(fs embed.FS, src, dst string) error {
	want, err := fs.ReadFile(src)
	if err != nil {
		return err
	}
	if have, err := os.ReadFile(dst); err == nil && bytes.Equal(have, want) {
		return nil
	}
	return os.WriteFile(dst, want, 0755)
}
