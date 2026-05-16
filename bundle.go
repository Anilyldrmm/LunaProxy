//go:build withbundle

package main

import (
	"embed"
	"io"
	"os"
	"path/filepath"
)

//go:embed assets/gdpi/goodbyedpi.exe assets/gdpi/WinDivert.dll assets/gdpi/WinDivert64.sys
var bundledFS embed.FS

func BundledGDPIAvailable() bool { return true }

func ExtractBundledGDPI() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	binDir := filepath.Join(dir, "LunaProxy", "bin")
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

func extractFile(fs embed.FS, src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	in, err := fs.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
