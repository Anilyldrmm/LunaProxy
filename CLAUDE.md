# mobilDPI (SpAC3DPI) — Claude Rehberi

## Proje Özeti
Windows için GoodbyeDPI tabanlı DPI bypass proxy. PC üzerinde çalışır; aynı ağdaki
tüm cihazlara (telefon, tablet) PAC URL ile şeffaf bypass sağlar.

**Durum:** v1 tamamlandı, v2 yeniden yazılıyor (mimari sorunlar nedeniyle)

## Build
```powershell
go build -tags withbundle -ldflags "-H windowsgui" -o SpAC3DPI.exe .
```
Test build (bundle olmadan):
```powershell
go build -ldflags "-H windowsgui" -o SpAC3DPI.exe .
```

## Dosya Haritası (v1)
| Dosya | Sorumluluk | Satır |
|-------|-----------|-------|
| main.go | Uygulama lifecycle, proxy/PAC başlatma, sistem proxy/DNS | 237 |
| ui.go | Native Walk arayüzü — TÜM paneller, tray, event handler'lar | 1527 |
| config.go | JSON config yapısı, DPI modu/ISP/DNS tanımları | 165 |
| proxy.go | HTTP/CONNECT tünel proxy (SSL terminasyonu YOK) | 156 |
| pac.go | PAC/WPAD sunucusu, /proxy.pac, /wpad.dat | 112 |
| sentinel.go | Named Mutex tek-örnek kilidi, sistem proxy backup/restore | 173 |
| dns.go | DNS değiştirme (PowerShell), DoH desteği (Win11+) | 169 |
| goodbyedpi.go | GoodbyeDPI process yönetimi, gizli pencere, log pipe | 162 |
| network.go | Yerel IP tespiti, firewall kuralları, tray ikon çizici | 78 |
| watchdog.go | 5s'de bir port kontrolü, otomatik yeniden başlatma | 136 |
| dpi_priority.go | 4-katman DPI öncelik tespiti | 105 |
| status.go | StatusPayload yapısı, buildStatus() | 99 |
| logs.go | 500-girişli döngüsel log tamponu | 59 |
| bundle.go | go:embed ile GoodbyeDPI binary gömme | 58 |

## v1 Sorunları (v2'de Çözülecek)
- ui.go 1527 satır — tek dosyada tüm UI (bakım imkansız)
- Walk (lxn/walk) kütüphanesi kısıtlı, custom rendering zor
- Config ve state management dağınık

## v2 Hedef Mimari
- Her dosya max ~300-400 satır
- UI dosyaları: ui_main.go, ui_tray.go, ui_settings.go, ui_status.go
- Clean separation: config, state, UI, network katmanları

## Okunmaması Gerekenler
- SpAC3DPI*.exe, SpAC3DPI*.exe~ — binary
- rsrc.syso — Windows resource binary
- out.txt, err.txt, err_v2.txt — log output
- assets/ içindeki binary dosyalar
