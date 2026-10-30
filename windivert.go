package main

import (
	"fmt"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WinDivert 2.x DLL bağlaması (cgo yok). Gömülü WinDivert.dll/WinDivert64.sys kullanılır.

const (
	wdLayerNetwork = 0
	wdFlagSniff    = 0x1
	wdFlagDrop     = 0x2
)

// wdAddress — WINDIVERT_ADDRESS (80 bayt).
type wdAddress struct {
	Timestamp int64
	Bits      uint32 // Layer:8 Event:8 Sniffed:1 Outbound:1 Loopback:1 Impostor:1 IPv6:1 ...
	Reserved2 uint32
	Data      [64]byte // NETWORK katmanında: IfIdx uint32, SubIfIdx uint32
}

const wdOutboundBit = 1 << 17

func (a *wdAddress) outbound() bool { return a.Bits&wdOutboundBit != 0 }
func (a *wdAddress) setOutbound(v bool) {
	if v {
		a.Bits |= wdOutboundBit
	} else {
		a.Bits &^= wdOutboundBit
	}
}

var (
	wdDLL       *windows.LazyDLL
	wdOpen      *windows.LazyProc
	wdRecv      *windows.LazyProc
	wdSend      *windows.LazyProc
	wdClose     *windows.LazyProc
	wdShutdown  *windows.LazyProc
	wdChecksums *windows.LazyProc
)

// loadWinDivert — gömülü WinDivert dosyalarını çıkarır ve DLL'i yükler.
func loadWinDivert() error {
	if wdDLL != nil {
		return nil
	}
	exe, err := ExtractBundledGDPI()
	if err != nil || exe == "" {
		return fmt.Errorf("WinDivert bulunamadı (withbundle build gerekli): %v", err)
	}
	dll := windows.NewLazyDLL(filepath.Join(filepath.Dir(exe), "WinDivert.dll"))
	if err := dll.Load(); err != nil {
		return fmt.Errorf("WinDivert.dll yüklenemedi: %w", err)
	}
	wdDLL = dll
	wdOpen = dll.NewProc("WinDivertOpen")
	wdRecv = dll.NewProc("WinDivertRecv")
	wdSend = dll.NewProc("WinDivertSend")
	wdClose = dll.NewProc("WinDivertClose")
	wdShutdown = dll.NewProc("WinDivertShutdown")
	wdChecksums = dll.NewProc("WinDivertHelperCalcChecksums")
	return nil
}

func divertOpen(filter string, priority int16, flags uint64) (windows.Handle, error) {
	f, err := windows.BytePtrFromString(filter)
	if err != nil {
		return 0, err
	}
	h, _, e := wdOpen.Call(uintptr(unsafe.Pointer(f)), wdLayerNetwork, uintptr(priority), uintptr(flags))
	if windows.Handle(h) == windows.InvalidHandle {
		return 0, fmt.Errorf("WinDivertOpen(%q): %w", filter, e)
	}
	return windows.Handle(h), nil
}

func divertRecv(h windows.Handle, buf []byte, addr *wdAddress) (int, error) {
	var n uint32
	r, _, e := wdRecv.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
		uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(addr)))
	if r == 0 {
		return 0, e
	}
	return int(n), nil
}

// divertSend — checksum'ları yeniden hesaplayıp paketi enjekte eder.
func divertSend(h windows.Handle, pkt []byte, addr *wdAddress) error {
	calcChecksums(pkt, addr)
	return divertSendRaw(h, pkt, addr)
}

func calcChecksums(pkt []byte, addr *wdAddress) {
	wdChecksums.Call(uintptr(unsafe.Pointer(&pkt[0])), uintptr(len(pkt)), uintptr(unsafe.Pointer(addr)), 0)
}

// divertSendRaw — checksum'a dokunmadan enjekte eder (bilerek bozuk checksum'lı sahte paket için).
func divertSendRaw(h windows.Handle, pkt []byte, addr *wdAddress) error {
	r, _, e := wdSend.Call(uintptr(h), uintptr(unsafe.Pointer(&pkt[0])), uintptr(len(pkt)), 0, uintptr(unsafe.Pointer(addr)))
	if r == 0 {
		return e
	}
	return nil
}

func divertShutdownClose(h windows.Handle) {
	wdShutdown.Call(uintptr(h), 3) // WINDIVERT_SHUTDOWN_BOTH
	wdClose.Call(uintptr(h))
}
