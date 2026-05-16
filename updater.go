package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
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
		name := strings.ToLower(a.Name)
		if strings.HasSuffix(name, ".exe") {
			return rel.TagName, a.BrowserDownloadURL, nil
		}
	}
	return rel.TagName, "", nil
}

// DownloadAndReplace — güncelleme dosyasını indirir ve uygular.
// Setup installer ise /VERYSILENT ile çalıştırır.
// Raw exe ise PS1 replace script ile mevcut exe'yi değiştirir.
func DownloadAndReplace(downloadURL string) error {
	tmpDir := os.TempDir()

	// URL'den dosya adını çıkar
	urlBase := downloadURL[strings.LastIndex(downloadURL, "/")+1:]
	tmpFile := filepath.Join(tmpDir, urlBase)

	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("indirme hatası: %w", err)
	}
	defer resp.Body.Close()
	f, err := os.Create(tmpFile)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	f.Close()

	shell32 := windows.NewLazySystemDLL("shell32.dll")
	shellExecW := shell32.NewProc("ShellExecuteW")
	op, _ := windows.UTF16PtrFromString("open")

	isSetup := strings.Contains(strings.ToLower(urlBase), "setup")

	if isSetup {
		// Installer'ı doğrudan ShellExecuteW ile başlat.
		// requireAdministrator manifest'i UAC'ı otomatik tetikler — PS1 wrapper gereksiz.
		// Installer /VERYSILENT modunda [Run] Check:WizardSilent entry'si ile uygulamayı yeniden başlatır.
		exePtr, _ := windows.UTF16PtrFromString(tmpFile)
		argsPtr, _ := windows.UTF16PtrFromString("/VERYSILENT /NORESTART /CLOSEAPPLICATIONS")
		r, _, _ := shellExecW.Call(
			0,
			uintptr(unsafe.Pointer(op)),
			uintptr(unsafe.Pointer(exePtr)),
			uintptr(unsafe.Pointer(argsPtr)),
			0,
			1,
		)
		if r <= 32 {
			return fmt.Errorf("installer başlatılamadı (ShellExecute: %d)", r)
		}
		os.Exit(0)
		return nil
	}

	// Raw exe: PS1 replace script ile değiştir ve yeniden başlat.
	selfExe, err := os.Executable()
	if err != nil {
		return err
	}
	ps1 := filepath.Join(tmpDir, "LunaProxy_update.ps1")
	const utf8BOM = "\xEF\xBB\xBF"
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
    Start-Process -FilePath $dst
}
Remove-Item $ps1path -ErrorAction SilentlyContinue
`, tmpFile, selfExe, ps1)
	if err := os.WriteFile(ps1, []byte(script), 0644); err != nil {
		return err
	}
	exe, _ := windows.UTF16PtrFromString("powershell.exe")
	args, _ := windows.UTF16PtrFromString("-ExecutionPolicy Bypass -WindowStyle Hidden -File " + ps1)
	r, _, _ := shellExecW.Call(
		0,
		uintptr(unsafe.Pointer(op)),
		uintptr(unsafe.Pointer(exe)),
		uintptr(unsafe.Pointer(args)),
		0,
		1,
	)
	if r <= 32 {
		return fmt.Errorf("güncelleme script başlatılamadı (ShellExecute: %d)", r)
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
