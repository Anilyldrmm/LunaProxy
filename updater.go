package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// newerVersion — latest, current'tan büyükse true döner.
func newerVersion(current, latest string) bool {
	cur := parseVer(strings.TrimPrefix(current, "v"))
	lat := parseVer(strings.TrimPrefix(latest, "v"))
	for i := 0; i < 3; i++ {
		if lat[i] > cur[i] {
			return true
		}
		if lat[i] < cur[i] {
			return false
		}
	}
	return false
}

func parseVer(s string) [3]int {
	parts := strings.SplitN(s, ".", 3)
	var v [3]int
	for i, p := range parts {
		if i >= 3 {
			break
		}
		n, _ := strconv.Atoi(p)
		v[i] = n
	}
	return v
}

// CheckUpdate — GitHub'dan son sürümü kontrol eder.
// Güncelleme varsa (tagName, downloadURL) döner; yoksa ("","") döner.
func CheckUpdate() (tagName, downloadURL string, err error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest",
		githubOwner, githubRepo)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return "", "", nil // henüz release yok
	}
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("GitHub API: %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", err
	}
	if !newerVersion(Version, rel.TagName) {
		return "", "", nil
	}
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, "LunaProxy.exe") {
			return rel.TagName, a.BrowserDownloadURL, nil
		}
	}
	return rel.TagName, "", nil
}

// DownloadAndReplace — yeni exe'yi indirir, PS1 replace script çalıştırır, çıkar.
func DownloadAndReplace(downloadURL string) error {
	tmpDir := os.TempDir()
	newExe := filepath.Join(tmpDir, "LunaProxy_update.exe")

	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("indirme hatası: %w", err)
	}
	defer resp.Body.Close()
	f, err := os.Create(newExe)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	f.Close()

	selfExe, err := os.Executable()
	if err != nil {
		return err
	}

	ps1 := filepath.Join(tmpDir, "LunaProxy_update.ps1")
	// UTF-8 BOM zorunlu: PowerShell 5.x BOM olmadan Windows-1252 okur,
	// non-ASCII yollar (örn. "Masaüstü") bozulur ve Copy-Item başarısız olur.
	const utf8BOM = "\xEF\xBB\xBF"
	// WScript.Shell.Run — hidden PowerShell'den GUI app başlatmak için
	// Start-Process yerine kullanılır; desktop/window-station gerektirmez.
	script := utf8BOM + fmt.Sprintf(`$src = '%s'
$dst = '%s'
$ps1path = '%s'
Start-Sleep -Seconds 2
$ok = $false
for ($i = 0; $i -lt 10; $i++) {
    try {
        Copy-Item -Force $src $dst -ErrorAction Stop
        $ok = $true
        break
    } catch {
        Start-Sleep -Seconds 1
    }
}
if ($ok) {
    $cmd = '"' + $dst + '"'
    $s = New-Object -COM 'WScript.Shell'
    $s.Run($cmd, 1, $false)
}
Remove-Item $ps1path -ErrorAction SilentlyContinue
`, newExe, selfExe, ps1)
	if err := os.WriteFile(ps1, []byte(script), 0644); err != nil {
		return err
	}

	cmd := exec.Command("powershell.exe", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", ps1)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("güncelleme script başlatılamadı: %w", err)
	}
	os.Exit(0)
	return nil
}

// StartUpdateChecker — arka planda her 6 saatte bir güncelleme kontrol eder.
// Güncelleme bulunursa onNotify(tagName, downloadURL) çağrılır.
func StartUpdateChecker(onNotify func(tagName, downloadURL string)) {
	go func() {
		check := func() {
			tag, url, err := CheckUpdate()
			if err != nil {
				logWarn("Güncelleme kontrolü başarısız: " + err.Error())
				return
			}
			if tag != "" {
				logInfo("Yeni sürüm mevcut: " + tag)
				onNotify(tag, url)
			}
		}
		check()
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			check()
		}
	}()
}
