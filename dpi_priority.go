package main

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// DPILaunchResult — ResolveDPI'ın döndürdüğü sonuç.
// ExePath boşsa GDPI zaten dışarıdan çalışıyor (dokunma).
// Source: "service" | "process" | "manual" | "bundle" | "disabled" | "none"
type DPILaunchResult struct {
	Source  string
	ExePath string
}

// IsGDPIServiceRunning — "GoodbyeDPI" Windows servisi çalışıyor mu?
func IsGDPIServiceRunning() bool {
	for _, name := range []string{"GoodbyeDPI", "goodbyedpi"} {
		out, err := hiddenOutput("sc", "query", name)
		if err != nil {
			continue
		}
		if parseServiceRunning(out) {
			return true
		}
	}
	return false
}

// parseServiceRunning — sc query çıktısından RUNNING durumunu parse eder.
func parseServiceRunning(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "STATE") && strings.Contains(line, "RUNNING") {
			return true
		}
	}
	return false
}

// FindGDPIProcess — herhangi bir goodbyedpi.exe prosesi çalışıyor mu?
func FindGDPIProcess() bool {
	out, err := hiddenOutput("tasklist", "/FI", "IMAGENAME eq goodbyedpi.exe", "/NH")
	if err != nil {
		return false
	}
	return parseProcessFound(out)
}

// parseProcessFound — tasklist çıktısında proses adı geçiyor mu?
func parseProcessFound(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "goodbyedpi.exe")
}

// ResolveDPI — config'e göre DPI kaynağını belirler.
// "auto" modunda öncelik sırası: servis → proses → manuel → bundle → yok.
// Servis veya mevcut proses bulunursa ExePath="" döner (dokunma).
func ResolveDPI(c Config) (DPILaunchResult, error) {
	switch c.DPISource {
	case "disabled":
		return DPILaunchResult{Source: "disabled"}, nil

	case "service":
		if !IsGDPIServiceRunning() {
			return DPILaunchResult{Source: "none"}, fmt.Errorf("GoodbyeDPI servisi çalışmıyor")
		}
		return DPILaunchResult{Source: "service"}, nil

	case "manual":
		if c.GDPIPath == "" {
			return DPILaunchResult{Source: "none"}, fmt.Errorf("manuel yol belirtilmemiş")
		}
		return DPILaunchResult{Source: "manual", ExePath: c.GDPIPath}, nil

	default: // "auto" veya boş
		if IsGDPIServiceRunning() {
			return DPILaunchResult{Source: "service"}, nil
		}
		if FindGDPIProcess() {
			return DPILaunchResult{Source: "process"}, nil
		}
		if c.GDPIPath != "" {
			return DPILaunchResult{Source: "manual", ExePath: c.GDPIPath}, nil
		}
		if BundledGDPIAvailable() {
			exePath, err := ExtractBundledGDPI()
			if err != nil {
				return DPILaunchResult{Source: "none"}, fmt.Errorf("bundle çıkartılamadı: %w", err)
			}
			return DPILaunchResult{Source: "bundle", ExePath: exePath}, nil
		}
		return DPILaunchResult{Source: "none"}, nil
	}
}

// hiddenOutput — komutu gizli pencere ile çalıştırır, stdout döner.
func hiddenOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.Output()
	return string(out), err
}
