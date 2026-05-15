package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// GDPIManager — GoodbyeDPI sürecini yönetir.
// DPISource="disabled" veya harici kaynak varsa doğrudan çağrılmaz; uygulama yönetiminde başlatılır.
type GDPIManager struct {
	mu      sync.Mutex
	proc    *exec.Cmd
	running bool
}

var gdpi = &GDPIManager{}

// Start — verilen yol ve bayraklarla goodbyedpi.exe başlatır.
func (m *GDPIManager) Start(exePath, flags string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return nil
	}
	if exePath == "" {
		return fmt.Errorf("goodbyedpi.exe yolu belirtilmemiş")
	}
	if _, err := os.Stat(exePath); err != nil {
		return fmt.Errorf("dosya bulunamadı: %s", exePath)
	}

	args := strings.Fields(flags)
	cmd := exec.Command(exePath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000200, // CREATE_NEW_PROCESS_GROUP
	}
	// stdout/stderr GoodbyeDPI çıktısını log'a yönlendir
	cmd.Stdout = &logWriter{"GDPI"}
	cmd.Stderr = &logWriter{"GDPI"}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("başlatılamadı: %w", err)
	}

	m.proc = cmd
	m.running = true
	logInfo(fmt.Sprintf("GoodbyeDPI başlatıldı [%s %s]", filepath.Base(exePath), flags))

	// Süreç bitişini izle
	go func() {
		cmd.Wait()
		m.mu.Lock()
		m.running = false
		m.proc = nil
		m.mu.Unlock()
		logWarn("GoodbyeDPI beklenmedik şekilde durdu")
	}()

	return nil
}

// Stop — çalışan GoodbyeDPI sürecini durdurur.
func (m *GDPIManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.proc != nil && m.proc.Process != nil {
		m.proc.Process.Kill()
		m.proc = nil
	}
	if m.running {
		m.running = false
		logInfo("GoodbyeDPI durduruldu")
	}
}

// IsRunning — sürecin hâlâ çalışıp çalışmadığını döner.
func (m *GDPIManager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Restart — durdur + yeni bayraklarla yeniden başlat.
func (m *GDPIManager) Restart(exePath, flags string) error {
	m.Stop()
	return m.Start(exePath, flags)
}

// StopWindowsService — çakışmayı önlemek için GoodbyeDPI Windows servisini durdurur.
func StopWindowsService() {
	for _, name := range []string{"GoodbyeDPI", "goodbyedpi", "GoodbyeDPI Service"} {
		hiddenRun("sc", "stop", name)
		hiddenRun("net", "stop", name)
	}
}

// FindGDPIExe — yaygın konumlarda goodbyedpi.exe arar; x86_64 öncelikli.
func FindGDPIExe() string {
	exe, _ := os.Executable()
	profile := os.Getenv("USERPROFILE")
	appdata := os.Getenv("APPDATA")
	localappdata := os.Getenv("LOCALAPPDATA")

	// OneDrive klasörlerini dinamik bul
	var oneDrives []string
	for _, base := range []string{
		filepath.Join(profile, "OneDrive"),
		filepath.Join(profile, "OneDrive - Personal"),
	} {
		if _, err := os.Stat(base); err == nil {
			oneDrives = append(oneDrives, base)
		}
	}

	var candidates []string

	// x86_64 önce, sonra x86 (64-bit Windows tercih)
	for _, od := range oneDrives {
		candidates = append(candidates,
			filepath.Join(od, "Belgeler", "GoodByeDPI", "x86_64", "goodbyedpi.exe"),
			filepath.Join(od, "Documents", "GoodByeDPI", "x86_64", "goodbyedpi.exe"),
			filepath.Join(od, "Belgeler", "GoodByeDPI", "x86", "goodbyedpi.exe"),
			filepath.Join(od, "Documents", "GoodByeDPI", "x86", "goodbyedpi.exe"),
		)
	}

	candidates = append(candidates,
		filepath.Join(filepath.Dir(exe), "goodbyedpi.exe"),
		`C:\GoodbyeDPI\x86_64\goodbyedpi.exe`,
		`C:\GoodbyeDPI\goodbyedpi.exe`,
		`C:\Program Files\GoodbyeDPI\goodbyedpi.exe`,
		`C:\Program Files (x86)\GoodbyeDPI\goodbyedpi.exe`,
		filepath.Join(appdata, "GoodbyeDPI", "goodbyedpi.exe"),
		filepath.Join(localappdata, "GoodbyeDPI", "goodbyedpi.exe"),
		filepath.Join(profile, "GoodbyeDPI", "goodbyedpi.exe"),
	)

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// logWriter — exec.Cmd'in stdout/stderr'ini appLog'a yazar
type logWriter struct{ prefix string }

func (lw *logWriter) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if msg != "" {
		appLog.Add("INFO", fmt.Sprintf("[%s] %s", lw.prefix, msg))
	}
	return len(p), nil
}
