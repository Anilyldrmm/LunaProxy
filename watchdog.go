package main

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ── İstatistikler ────────────────────────────────────────────────────────────

type appStats struct {
	startTime   time.Time
	activeConns int64
	totalConns  int64
	totalBytes  int64
	errors      int64
}

var stats = &appStats{}

func (s *appStats) reset() {
	s.startTime = time.Now()
	atomic.StoreInt64(&s.activeConns, 0)
	atomic.StoreInt64(&s.totalConns, 0)
	atomic.StoreInt64(&s.totalBytes, 0)
	atomic.StoreInt64(&s.errors, 0)
}

func (s *appStats) incConn() {
	atomic.AddInt64(&s.activeConns, 1)
	atomic.AddInt64(&s.totalConns, 1)
}
func (s *appStats) decConn()       { atomic.AddInt64(&s.activeConns, -1) }
func (s *appStats) addBytes(n int64) { atomic.AddInt64(&s.totalBytes, n) }
func (s *appStats) incError()      { atomic.AddInt64(&s.errors, 1) }

func (s *appStats) uptimeStr() string {
	if s.startTime.IsZero() {
		return "—"
	}
	d := time.Since(s.startTime).Truncate(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	sec := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%ds %02dm %02ds", h, m, sec)
	}
	return fmt.Sprintf("%dm %02ds", m, sec)
}

func (s *appStats) bytesStr() string {
	b := atomic.LoadInt64(&s.totalBytes)
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// ── Watchdog ─────────────────────────────────────────────────────────────────

// Watchdog her 5 saniyede bir proxy ve GoodbyeDPI sağlığını kontrol eder.
// Servis yanıt vermiyorsa otomatik olarak yeniden başlatır.
type Watchdog struct {
	stop    chan struct{}
	restart int64 // yeniden başlatma sayısı
}

var watchdog = &Watchdog{}

func (w *Watchdog) Start() {
	w.stop = make(chan struct{})
	go w.loop()
	logInfo("Watchdog başlatıldı (5s aralık)")
}

func (w *Watchdog) Stop() {
	if w.stop != nil {
		select {
		case <-w.stop: // zaten kapalı
		default:
			close(w.stop)
		}
	}
}

func (w *Watchdog) loop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.check()
		case <-w.stop:
			return
		}
	}
}

func (w *Watchdog) check() {
	if !g.running {
		return
	}
	c := getConfig()

	// 1. Proxy portu yanıt veriyor mu?
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", c.ProxyPort), 2*time.Second)
	if err != nil {
		n := atomic.AddInt64(&w.restart, 1)
		logWarn(fmt.Sprintf("Watchdog: proxy yanıt yok → yeniden başlatılıyor (#%d)", n))
		stats.incError()
		go g.restart() // ayrı goroutine; çakışma olmaz
		return
	}
	conn.Close()

	// 2. Bizim başlattığımız DPI prosesi durduysa yeniden başlat (servis/harici prosese dokunma)
	if !gdpi.IsRunning() && (g.dpiSource == "manual" || g.dpiSource == "bundle") {
		logWarn("Watchdog: GoodbyeDPI durmuş → yeniden başlatılıyor")
		if result, err := ResolveDPI(c); err == nil && result.ExePath != "" {
			if err := gdpi.Start(result.ExePath, activeGDPIFlags()); err != nil {
				logError("GoodbyeDPI yeniden başlatılamadı: " + err.Error())
			}
		}
	}
}

func (w *Watchdog) RestartCount() int64 {
	return atomic.LoadInt64(&w.restart)
}

// ── Bant genişliği geçmişi (grafik için) ─────────────────────────────────────

type bwHistory struct {
	mu     sync.Mutex
	points [60]float64 // son 60 örnek, 2s aralık → 2 dakika penceresi
	head   int
	last   int64
}

var bwHist = &bwHistory{}

// sample — son 2s'deki MB/s değerini ring buffer'a ekler; IPC ticker'dan çağrılır.
func (h *bwHistory) sample() {
	cur := atomic.LoadInt64(&stats.totalBytes)
	h.mu.Lock()
	delta := cur - h.last
	if delta < 0 {
		delta = 0
	}
	h.last = cur
	h.points[h.head] = float64(delta) / (1024 * 1024 * 2) // 2s window → MB/s
	h.head = (h.head + 1) % 60
	h.mu.Unlock()
}

// Snapshot — en eskiden en yeniye 60 MB/s değeri döner.
func (h *bwHistory) Snapshot() []float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]float64, 60)
	for i := 0; i < 60; i++ {
		out[i] = h.points[(h.head+i)%60]
	}
	return out
}
