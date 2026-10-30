package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"
)

// Dahili DPI motoru (proxy girişi): sadece engelli listesindeki alan adlarına
// ClientHello parçalama uygular; diğer siteler (banka, 3D Secure) olduğu gibi geçer.

// Deneme sırasına göre stratejiler.
var dpiStrategies = []string{"tlsrec", "split", "disorder", "tlsrec_disorder"}

var activeStrategy atomic.Value // string

func currentStrategy() string {
	if s, ok := activeStrategy.Load().(string); ok {
		return s
	}
	return dpiStrategies[0]
}

func dpiEngineOn() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.dpiSource == "internal"
}

// isBlockedHost — host engelli listesinde mi (alt alan adları dahil, büyük/küçük harf duyarsız)?
func isBlockedHost(host string) bool {
	return hostInList(host, getConfig().BypassDomains)
}

func hostInList(host string, list []string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, d := range list {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" && (host == d || strings.HasSuffix(host, "."+d)) {
			return true
		}
	}
	return false
}

// validAutoHost — otomatik listeye girebilecek bir alan adı mı? IP ve tek etiketli adlar ("com")
// reddedilir; aksi halde suffix eşleşmesi koca bir TLD'yi listeye sokar.
func validAutoHost(host string) bool {
	return net.ParseIP(host) == nil && strings.Count(strings.Trim(host, "."), ".") >= 1
}

// addBlockedHost — doğrulanmış engelli alan adını listeye kalıcı ekler ve UI'daki listeye yansıtır.
func addBlockedHost(host string) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	added := false
	err := updateConfig(func(c *Config) {
		if !hostInList(host, c.BypassDomains) {
			c.BypassDomains = append(append([]string{}, c.BypassDomains...), host)
			added = true
		}
	})
	if err != nil {
		logWarn("engelli liste kaydedilemedi: " + err.Error())
	}
	if added {
		logInfo("DPI: engel tespit edildi, listeye eklendi → " + host)
		evalJS(fmt.Sprintf("appendBypassDomain(%q)", host))
	}
}

// desyncConn — ilk Write'taki ClientHello'yu stratejiye göre parçalar, sonrası düz geçer.
type desyncConn struct {
	net.Conn
	strategy string
	done     bool
}

func (c *desyncConn) Write(b []byte) (int, error) {
	if c.done || c.strategy == "none" {
		return c.Conn.Write(b)
	}
	c.done = true
	_, off, ok := parseSNI(b)
	if !ok {
		return c.Conn.Write(b)
	}
	rec, rest := b, []byte(nil)
	if n := 5 + (int(b[3])<<8 | int(b[4])); n < len(b) {
		rec, rest = b[:n], b[n:] // kayıttan sonra ek veri (nadir) en sona düz yazılır
	}
	at := off + 1 // SNI'nın ilk harfinden sonra böl
	var p1, p2 []byte
	if strings.HasPrefix(c.strategy, "tlsrec") {
		p1, p2 = splitTLSRecord(rec, at)
	} else {
		p1, p2 = rec[:at], rec[at:]
	}
	p2 = append(p2, rest...)
	if strings.HasSuffix(c.strategy, "disorder") {
		// İlk parça TTL=1 ile yolda düşer; kernel onu sonra normal TTL ile yeniden yollar,
		// DPI ikinci parçayı önce görür ve akışı birleştiremez.
		setTTL(c.Conn, 1)
		_, err := c.Conn.Write(p1)
		setTTL(c.Conn, 128) // Windows varsayılan TTL
		if err != nil {
			return 0, err
		}
	} else if _, err := c.Conn.Write(p1); err != nil {
		return 0, err
	}
	if _, err := c.Conn.Write(p2); err != nil {
		return 0, err
	}
	return len(b), nil
}

func setTTL(conn net.Conn, ttl int) {
	tc, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}
	raw, err := tc.SyscallConn()
	if err != nil {
		return
	}
	raw.Control(func(fd uintptr) {
		h := windows.Handle(fd)
		windows.SetsockoptInt(h, windows.IPPROTO_IP, windows.IP_TTL, ttl)
		windows.SetsockoptInt(h, windows.IPPROTO_IPV6, windows.IPV6_UNICAST_HOPS, ttl)
	})
}

// dialBypass — host'u DoH ile çözüp bağlanır, ilk yazımı desync'e sarar.
func dialBypass(host, port, strategy string) (net.Conn, error) {
	ips, err := dohResolve(host)
	if err != nil {
		logWarn(fmt.Sprintf("DoH %s: %v — sistem DNS'i deneniyor", host, err))
		ips = []string{host}
	}
	var lastErr error
	for _, ip := range ips {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, port), 10*time.Second)
		if err == nil {
			conn.(*net.TCPConn).SetNoDelay(true)
			return &desyncConn{Conn: conn, strategy: strategy}, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// readClientHello — istemciden ilk TLS kaydını tam okur; TLS değilse tamponda olanı döner.
// İstemci hiç konuşmazsa (sunucu önce konuşan protokol) nil, nil döner.
func readClientHello(src net.Conn, br *bufio.Reader) ([]byte, error) {
	src.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer src.SetReadDeadline(time.Time{})
	hdr, err := br.Peek(5)
	if err != nil || hdr[0] != 0x16 {
		if len(hdr) == 0 && err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return nil, nil
			}
			return nil, err
		}
		b := make([]byte, br.Buffered())
		_, err := br.Read(b)
		return b, err
	}
	rec := make([]byte, 5+(int(hdr[3])<<8|int(hdr[4])))
	_, err = io.ReadFull(br, rec)
	return rec, err
}

// handleConnectDPI — 443 CONNECT'leri dahili motorla işler.
func handleConnectDPI(src net.Conn, br *bufio.Reader, hostPort, ip string) {
	defer src.Close()
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return
	}
	ch, err := readClientHello(src, br)
	if err != nil {
		return
	}
	if len(ch) == 0 { // sunucu önce konuşuyor: düz tünel
		dst, err := net.DialTimeout("tcp", hostPort, 10*time.Second)
		if err != nil {
			stats.incError()
			return
		}
		pipeConns(src, br, dst, ip)
		return
	}
	_, _, isTLS := parseSNI(ch)
	strategy := currentStrategy()

	var dst net.Conn
	var first []byte
	if isTLS && isBlockedHost(host) {
		if divertCoversPC() {
			strategy = "none" // WinDivert bu bağlantıyı zaten paket seviyesinde parçalayacak
		}
		if dst, err = dialBypass(host, port, strategy); err == nil {
			_, err = dst.Write(ch)
		}
	} else if dst, err = net.DialTimeout("tcp", hostPort, 10*time.Second); err == nil {
		first, err = writeAwait(dst, ch, isTLS)
		if err != nil && isTLS && validAutoHost(host) {
			// Bağlantı kuruldu ama ClientHello'ya RST / sessizlik: DPI şüphesi. Bypass ile dene; sadece
			// tutarsa listeye ekle (site çökmesi, kopukluk vb. listeye girmesin).
			dst.Close()
			bd, berr := dialBypass(host, port, strategy)
			if berr == nil {
				if first, err = writeAwait(bd, ch, true); err == nil {
					addBlockedHost(host)
				}
			}
			dst = bd
		}
	}
	if err != nil {
		stats.incError()
		logWarn(fmt.Sprintf("DPI %s → hata: %v", hostPort, err))
		if dst != nil {
			dst.Close()
		}
		return
	}
	if len(first) > 0 {
		src.Write(first)
	}
	pipeConns(src, br, dst, ip)
}

// writeAwait — ilk veriyi yazar; wait ise sunucunun ilk cevabını bekleyip döndürür (engel tespiti için).
func writeAwait(dst net.Conn, ch []byte, wait bool) ([]byte, error) {
	if _, err := dst.Write(ch); err != nil || !wait {
		return nil, err
	}
	dst.SetReadDeadline(time.Now().Add(6 * time.Second))
	defer dst.SetReadDeadline(time.Time{})
	buf := make([]byte, 16*1024)
	n, err := dst.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// probeHosts — strateji testi için Türkiye'de engelli bilinen alan adları.
var probeHosts = []string{"discord.com", "www.roblox.com"}

// selectStrategy — proxy girişi için ISP'de çalışan ilk stratejiyi bulur ve aktif yapar.
func selectStrategy() {
	pickStrategy("proxy", dpiStrategies, func(s string) { activeStrategy.Store(s) }, probeTLS)
}

var errProbeAborted = errors.New("strateji testi iptal edildi")

// pickStrategy — test alan adına önce parçalamasız ("none"), engelliyse stratejileri sırayla dener.
func pickStrategy(label string, list []string, set func(string), try func(host, s string) error) {
	for _, host := range probeHosts {
		err := try(host, "none")
		if err == nil {
			continue // bu ISP'de engelli değil, sonraki host'la test et
		}
		for _, s := range list {
			if errors.Is(err, errProbeAborted) {
				return
			}
			if err = try(host, s); err == nil {
				set(s)
				logInfo(fmt.Sprintf("DPI [%s]: strateji seçildi → %s (%s ile test edildi)", label, s, host))
				return
			}
		}
		if errors.Is(err, errProbeAborted) {
			return
		}
		set(list[0])
		logWarn(fmt.Sprintf("DPI [%s]: hiçbir strateji %s engelini aşamadı, varsayılan → %s", label, host, list[0]))
		return
	}
	set(list[0])
	logInfo(fmt.Sprintf("DPI [%s]: test alan adları engelli değil, varsayılan → %s", label, list[0]))
}

// probeTLS — DoH ile bağlanıp verilen stratejiyle ("none" = parçalamasız) TLS el sıkışması dener.
func probeTLS(host, strategy string) error {
	conn, err := dialBypass(host, "443", strategy)
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	return tls.Client(conn, &tls.Config{ServerName: host}).HandshakeContext(ctx)
}
