package main

import (
	"fmt"
	"net"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const inetRegPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// proxySnapshot — Windows sistem proxy yedeği
type proxySnapshot struct {
	enable uint32
	server string
	pac    string
}

var origSystemProxy proxySnapshot
var proxyBackedUp bool

// ── Single-instance guard (Windows Named Mutex) ───────────────────────────────

var instanceMutex windows.Handle

var (
	modKernel32       = windows.NewLazySystemDLL("kernel32.dll")
	procCreateMutexW  = modKernel32.NewProc("CreateMutexW")
)

// ensureSingleInstance — Windows Named Mutex ile tek-örnek koruması.
// TCP port kilidinden çok daha güvenilir; process crash'te OS otomatik serbest bırakır.
func ensureSingleInstance() bool {
	name, _ := windows.UTF16PtrFromString("Local\\SpAC3DPI_v3_SingleInstance")
	r, _, lastErr := procCreateMutexW.Call(
		0,                             // lpMutexAttributes (nil → inherit default)
		0,                             // bInitialOwner (false)
		uintptr(unsafe.Pointer(name)), // lpName
	)
	if r == 0 {
		// CreateMutex tamamen başarısız — nadir durum, çalışmasına izin ver
		return true
	}
	// ERROR_ALREADY_EXISTS = 183 (0xB7) — mutex önceden oluşturulmuş → başka örnek var
	if errno, ok := lastErr.(syscall.Errno); ok && errno == 183 {
		windows.CloseHandle(windows.Handle(r))
		return false
	}
	instanceMutex = windows.Handle(r)
	return true
}

// ── System proxy backup / restore ────────────────────────────────────────────

// BackupSystemProxy — kapanmadan önce mevcut Windows sistem proxy ayarlarını saklar.
func BackupSystemProxy() {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetRegPath, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer k.Close()

	en, _, _ := k.GetIntegerValue("ProxyEnable")
	srv, _, _ := k.GetStringValue("ProxyServer")
	pac, _, _ := k.GetStringValue("AutoConfigURL")

	origSystemProxy = proxySnapshot{
		enable: uint32(en),
		server: srv,
		pac:    pac,
	}
	proxyBackedUp = true
	logInfo(fmt.Sprintf("Sistem proxy yedeği alındı (enable=%d server=%q)", en, srv))
}

// SetSystemProxy — Windows sistem proxy'sini SpAC3DPI'a yönlendirir.
func SetSystemProxy(addr string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetRegPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	k.SetDWordValue("ProxyEnable", 1)
	k.SetStringValue("ProxyServer", addr)
	k.DeleteValue("AutoConfigURL")
	notifyProxyChange()
	logInfo("Windows sistem proxy ayarlandı → " + addr)
	return nil
}

// RestoreSystemProxy — orijinal sistem proxy ayarlarını geri yükler.
func RestoreSystemProxy() {
	if !proxyBackedUp {
		ClearSystemProxy()
		return
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, inetRegPath, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()

	k.SetDWordValue("ProxyEnable", origSystemProxy.enable)
	if origSystemProxy.server != "" {
		k.SetStringValue("ProxyServer", origSystemProxy.server)
	} else {
		k.DeleteValue("ProxyServer")
	}
	if origSystemProxy.pac != "" {
		k.SetStringValue("AutoConfigURL", origSystemProxy.pac)
	} else {
		k.DeleteValue("AutoConfigURL")
	}
	notifyProxyChange()
	logInfo("Sistem proxy orijinal değerine döndürüldü")
}

// ClearSystemProxy — proxy ayarlarını kapatır (acil temizleme).
func ClearSystemProxy() {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetRegPath, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	k.SetDWordValue("ProxyEnable", 0)
	notifyProxyChange()
}

// notifyProxyChange — WinINet'e proxy ayarlarının değiştiğini bildirir.
// Tarayıcılar ve uygulamalar yeni ayarları anında kullanır.
func notifyProxyChange() {
	wininet := windows.NewLazySystemDLL("wininet.dll")
	inetSetOption := wininet.NewProc("InternetSetOptionW")
	inetSetOption.Call(0, 39, 0, 0) // INTERNET_OPTION_SETTINGS_CHANGED
	inetSetOption.Call(0, 37, 0, 0) // INTERNET_OPTION_REFRESH
}

// ── Dirty-state sentinel ──────────────────────────────────────────────────────

// SentinelCheck — uygulama başlarken kirli proxy durumunu tespit eder ve temizler.
// Bir önceki çalıştırmada crash/kapanma olduysa ve proxy temizlenmediyse
// kullanıcının interneti kesilmiş olabilir; bu fonksiyon bunu kurtarır.
func SentinelCheck() {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetRegPath, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer k.Close()

	en, _, _ := k.GetIntegerValue("ProxyEnable")
	srv, _, _ := k.GetStringValue("ProxyServer")

	if en == 0 || srv == "" {
		return // proxy kapalı, sorun yok
	}

	// Proxy açık — sunucu yanıt veriyor mu?
	conn, err := net.DialTimeout("tcp", srv, 1500*time.Millisecond)
	if err == nil {
		conn.Close()
		return // yanıt veriyor, sorun yok
	}

	// Yanıt vermiyor — kirli durum!
	logWarn(fmt.Sprintf("Sentinel: ölü proxy tespit edildi (%s) — temizleniyor", srv))
	ClearSystemProxy()
	logInfo("Sentinel: internet bağlantısı kurtarıldı")
}
