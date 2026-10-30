package main

import (
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WinDivert girişi: PC'nin kendi trafiğine (tüm PC veya seçili uygulamalar) dahili motoru uygular.
// Sadece engelli listesindeki alan adlarına dokunur; DNS'leri DoH ile cevaplar, QUIC'i TCP'ye düşürür.

// GoodbyeDPI (öncelik 0) PC'de çalışıyorsa paketleri önce o işlesin; bizim enjekte ettiğimiz
// DNS cevapları da daha düşük öncelikli handle'lara gittiği için onun DNS yönlendirmesine takılmaz.
const divertPriority = -100

// Paket seviyesinde tlsrec yok: payload boyu değişirse TCP seq'i bozulur.
// fakeauto: sahte ClientHello, TTL'i sunucuya kalan atlama sayısının 2 eksiği (SYN-ACK'ten ölçülür) —
// yoldaki DPI görür, sunucuya ulaşmaz. fakeN: sabit TTL=N. fake: bozuk checksum.
// Sunucuya ulaşan sahte paket el sıkışmasını bozar; test o stratejiyi eler.
var divertStrategies = []string{"fakeauto_disorder", "fakeauto_split", "split", "disorder", "fake_disorder",
	"fake4_disorder", "fake6_disorder", "fake8_disorder", "fake10_disorder"}

var (
	divertStrategy atomic.Value // string
	divertProbing  atomic.Bool
	blockedIPs     sync.Map // DoH ile çözülmüş engelli alan adı IP'leri → true

	divertGen     atomic.Uint64 // her startDivert'te artar; eski strateji testi kendini iptal eder
	divertMu      sync.Mutex
	divertHandles []windows.Handle
	divertScope   string
	divertApps    map[string]bool
)

func currentDivertStrategy() string {
	if s, ok := divertStrategy.Load().(string); ok {
		return s
	}
	return divertStrategies[0]
}

// divertCoversPC — WinDivert tüm PC'yi işliyorsa proxy girişi ayrıca desync yapmaz (çift parçalama olmasın).
func divertCoversPC() bool {
	divertMu.Lock()
	defer divertMu.Unlock()
	return len(divertHandles) > 0 && divertScope == "pc"
}

// startDivert — scope "pc" veya "apps" için WinDivert döngülerini başlatır.
func startDivert(scope string, apps []string) error {
	stopDivert() // önceki handle'lar sızmasın
	if err := loadWinDivert(); err != nil {
		return err
	}
	// Gelen SYN-ACK'ler de bu handle'dan geçer: TTL'i ölçülüp yeniden enjekte edildikten sonra
	// istemci ClientHello'yu yollar, yani ölçüm her zaman ClientHello'dan önce hazırdır.
	mh, err := divertOpen("!loopback and ((outbound and ("+
		"(tcp.DstPort == 443 and tcp.PayloadLength > 5 and tcp.Payload[0] == 22 and tcp.Payload[5] == 1)"+
		" or udp.DstPort == 53 or (udp.DstPort == 443 and udp.PayloadLength >= 1200)))"+
		" or (inbound and tcp.SrcPort == 443 and tcp.Syn and tcp.Ack))", divertPriority, 0)
	if err != nil {
		return err
	}
	rst, err := divertOpen("inbound and tcp.SrcPort == 443 and tcp.Rst", divertPriority, wdFlagSniff)
	if err != nil {
		divertShutdownClose(mh)
		return err
	}
	set := map[string]bool{}
	for _, a := range apps {
		if a = strings.ToLower(strings.TrimSpace(a)); a != "" {
			set[a] = true
		}
	}
	divertMu.Lock()
	divertHandles = []windows.Handle{mh, rst}
	divertScope, divertApps = scope, set
	divertMu.Unlock()

	go divertLoop(mh)
	go rstLoop(rst)
	logInfo(fmt.Sprintf("DPI: WinDivert girişi aktif (kapsam: %s)", scope))
	go selectDivertStrategy(divertGen.Add(1))
	return nil
}

func stopDivert() {
	divertMu.Lock()
	hs := divertHandles
	divertHandles = nil
	divertMu.Unlock()
	divertGen.Add(1)
	blockedIPs.Clear() // listeden çıkarılan alan adlarının IP'leri etkilenmeye devam etmesin
	for _, h := range hs {
		divertShutdownClose(h)
	}
	if len(hs) > 0 {
		logInfo("DPI: WinDivert girişi durduruldu")
	}
}

func divertLoop(h windows.Handle) {
	buf := make([]byte, 0xFFFF)
	for {
		var addr wdAddress
		n, err := divertRecv(h, buf, &addr)
		if err != nil {
			if divertHandleOpen(h) {
				// beklenmedik hata: handle açık kalırsa filtreye uyan paketler kuyrukta düşer
				logError("DPI: WinDivert okuma hatası, durduruluyor: " + err.Error())
				go stopDivert()
			}
			return
		}
		handleDivertPacket(h, append([]byte{}, buf[:n]...), addr)
	}
}

func divertActive() bool {
	divertMu.Lock()
	defer divertMu.Unlock()
	return len(divertHandles) > 0
}

func divertHandleOpen(h windows.Handle) bool {
	divertMu.Lock()
	defer divertMu.Unlock()
	for _, x := range divertHandles {
		if x == h {
			return true
		}
	}
	return false
}

func handleDivertPacket(h windows.Handle, pkt []byte, addr wdAddress) {
	defer func() {
		if r := recover(); r != nil {
			logError(fmt.Sprintf("DPI: paket işlenirken panik: %v", r))
			divertSendRaw(h, pkt, &addr) // paket kaybolmasın
		}
	}()
	in, ok := parsePacket(pkt)
	switch {
	case !ok:
		divertSendRaw(h, pkt, &addr)
	case !addr.outbound(): // SYN-ACK
		synackTTL.Store(binary.BigEndian.Uint16(pkt[in.l4+2:]), ipTTL(pkt, in))
		divertSendRaw(h, pkt, &addr)
	case in.proto == protoTCP:
		handleDivertTLS(h, pkt, in, &addr)
	case binary.BigEndian.Uint16(pkt[in.l4+2:]) == 53:
		handleDivertDNS(h, pkt, in, addr)
	default: // QUIC Initial: engelli IP'ye gidiyorsa düşür → tarayıcı TCP'ye döner
		// shortcut: "apps" kapsamında UDP sahibi bakılmadığı için QUIC'e dokunulmaz; gerekirse GetExtendedUdpTable ekle.
		_, blocked := blockedIPs.Load(dstIP(pkt, in).String())
		if !blocked || divertScopeIs("apps") {
			divertSendRaw(h, pkt, &addr)
		}
	}
}

func divertScopeIs(scope string) bool {
	divertMu.Lock()
	defer divertMu.Unlock()
	return divertScope == scope
}

// synackTTL — yerel port → sunucudan gelen SYN-ACK'in TTL'i (fakeauto için).
var synackTTL sync.Map // uint16 → byte

// autoFakeTTL — SYN-ACK TTL'inden sunucuya atlama sayısını tahmin eder, 2 eksiğini döndürür.
func autoFakeTTL(recv byte) byte {
	hops := 64 - int(recv)
	if recv > 64 {
		hops = 128 - int(recv)
	}
	if recv > 128 {
		hops = 255 - int(recv)
	}
	return byte(max(3, min(hops-2, 20)))
}

// recentCH — engel tespiti için: yerel port → listede olmayan SNI (RST gelirse listeye eklenir).
var recentCH sync.Map // uint16 → chEntry

type chEntry struct {
	sni  string
	seen time.Time
}

func handleDivertTLS(h windows.Handle, pkt []byte, in pktInfo, addr *wdAddress) {
	payload := pkt[in.payload:]
	sni, off, hasSNI := parseSNI(payload)
	localPort := binary.BigEndian.Uint16(pkt[in.l4:])
	probe := divertProbing.Load() && hasSNI && isProbeHost(sni)

	listed := probe || (hasSNI && isBlockedHost(sni))
	byIP := false
	if !listed && !hasSNI {
		// ClientHello birden fazla segmente yayıldı ve SNI ilk segmentte değil → IP'ye bak
		_, byIP = blockedIPs.Load(dstIP(pkt, in).String())
		listed = byIP
	}
	appOK := probe || divertAppAllowed(localPort, in.v6)
	if !listed || !appOK {
		if hasSNI && !listed && appOK {
			recentCH.Store(localPort, chEntry{sni, time.Now()})
		}
		divertSendRaw(h, pkt, addr)
		return
	}

	strategy := currentDivertStrategy()
	if byIP && strategy != "none" {
		// IP paylaşan (CDN) listede olmayan bir site olabilir: sadece sunucuya görünmez saf split, sahte paket yok
		strategy = "split"
	}
	if strategy == "none" {
		divertSendRaw(h, pkt, addr)
		return
	}
	at := 1 // SNI bulunamadıysa kayıt başlığını böl
	if hasSNI {
		at = off + 1
	}
	if at >= len(payload) {
		at = len(payload) / 2
	}
	seg1 := tcpSegment(pkt, in, payload[:at], 0)
	seg2 := tcpSegment(pkt, in, payload[at:], at)

	if strings.HasPrefix(strategy, "fake") {
		// Sahte ClientHello: sunucuya ulaşmaz (düşük TTL / bozuk checksum), DPI onu gerçek sanıp akışı serbest bırakır.
		fake := tcpSegment(pkt, in, fakeClientHello(), 0)
		var ttl int
		fmt.Sscanf(strategy, "fake%d", &ttl)
		if strings.HasPrefix(strategy, "fakeauto") {
			ttl = 8 // SYN-ACK görülmediyse (ör. motor bağlantı ortasında açıldı)
			if v, ok := synackTTL.Load(localPort); ok {
				ttl = int(autoFakeTTL(v.(byte)))
			}
		}
		if ttl > 0 {
			setIPTTL(fake, in, byte(ttl))
			divertSend(h, fake, addr)
		} else {
			calcChecksums(fake, addr)
			fake[in.l4+16] ^= 0xff
			divertSendRaw(h, fake, addr)
		}
	}
	if strings.HasSuffix(strategy, "disorder") {
		divertSend(h, seg2, addr)
		divertSend(h, seg1, addr)
	} else {
		divertSend(h, seg1, addr)
		divertSend(h, seg2, addr)
	}
}

func handleDivertDNS(h windows.Handle, pkt []byte, in pktInfo, addr wdAddress) {
	name, qtype, qEnd, ok := parseDNSQuery(pkt[in.payload:])
	if !ok || !isBlockedHost(name) {
		divertSendRaw(h, pkt, &addr)
		return
	}
	go func() {
		// A dışındaki her tip (AAAA, HTTPS/65…) boş cevaplanır: ISP'nin ele geçirdiği DNS'ten NXDOMAIN
		// gelip önbelleğe girmesin, engelli siteler IPv4'ten gitsin (DoH sadece A çözüyor).
		var ips []string
		if qtype == 1 {
			var err error
			if ips, err = dohResolve(name); err != nil {
				divertSendRaw(h, pkt, &addr) // DoH yoksa orijinal sorgu geçsin, internet gitmesin
				return
			}
			for _, ip := range ips {
				blockedIPs.Store(ip, true)
			}
		}
		reply := udpReply(pkt, in, buildDNSReply(pkt[in.payload:], qEnd, ips))
		addr.setOutbound(false)
		divertSend(h, reply, &addr)
	}()
}

// rstLoop — listede olmayan bir siteye ClientHello'dan hemen sonra RST gelirse engelli say.
func rstLoop(h windows.Handle) {
	buf := make([]byte, 0xFFFF)
	for {
		var addr wdAddress
		n, err := divertRecv(h, buf, &addr)
		if err != nil {
			return
		}
		in, ok := parsePacket(buf[:n])
		if !ok || in.proto != protoTCP {
			continue
		}
		port := binary.BigEndian.Uint16(buf[in.l4+2:])
		v, ok := recentCH.LoadAndDelete(port)
		if !ok || time.Since(v.(chEntry).seen) > 3*time.Second {
			continue
		}
		go confirmBlocked(v.(chEntry).sni)
	}
}

var confirming sync.Map // sni → true; doğrulama sürerken aynı host tekrar denenmesin

// confirmBlocked — RST gerçekten DPI engeli mi? Parçalamasız başarısız, parçalamalı (proxy desync) başarılıysa
// listeye ekler. Kapanan sekme / sunucunun kendi RST'si gibi durumlar böylece listeye girmez.
func confirmBlocked(sni string) {
	if _, busy := confirming.LoadOrStore(sni, true); busy {
		return
	}
	defer confirming.Delete(sni)
	if !validAutoHost(sni) || probeTLS(sni, "none") == nil || probeTLS(sni, currentStrategy()) != nil {
		return
	}
	addBlockedHost(sni)
	flushDNSCache() // zehirli DNS cevabı önbellekte kalmasın, sonraki deneme DoH'tan gelsin
}

func isProbeHost(sni string) bool {
	for _, p := range probeHosts {
		if sni == p {
			return true
		}
	}
	return false
}

// selectDivertStrategy — paket seviyesinde çalışan stratejiyi test edip seçer.
func selectDivertStrategy(gen uint64) {
	divertProbing.Store(true)
	defer divertProbing.Store(false)
	pickStrategy("WinDivert", divertStrategies, func(s string) { divertStrategy.Store(s) },
		func(host, s string) error {
			if divertGen.Load() != gen {
				return errProbeAborted // WinDivert durduruldu/yeniden başlatıldı
			}
			divertStrategy.Store(s)
			return probeTLS(host, "none") // DoH ile bağlan, parçalamayı WinDivert yapsın
		})
}

var (
	fakeCHOnce sync.Once
	fakeCH     []byte
)

// fakeClientHello — engelsiz bir alan adı için gerçek görünümlü ClientHello.
func fakeClientHello() []byte {
	fakeCHOnce.Do(func() {
		c1, c2 := net.Pipe()
		// Sadece X25519: ML-KEM'li ClientHello ~1.5KB olup başlıklarla MTU'yu aşıyor
		go tls.Client(c1, &tls.Config{ServerName: "www.google.com", CurvePreferences: []tls.CurveID{tls.X25519}}).Handshake()
		c2.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 4096)
		n, _ := c2.Read(buf)
		c1.Close()
		c2.Close()
		fakeCH = buf[:n]
	})
	return fakeCH
}

var (
	iphlpapi            = windows.NewLazySystemDLL("iphlpapi.dll")
	getExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
	dnsFlush            = windows.NewLazySystemDLL("dnsapi.dll").NewProc("DnsFlushResolverCache")
)

func flushDNSCache() { dnsFlush.Call() }

// divertAppAllowed — "apps" kapsamında yerel portun sahibi seçili uygulamalardan mı?
func divertAppAllowed(localPort uint16, v6 bool) bool {
	divertMu.Lock()
	scope, apps := divertScope, divertApps
	divertMu.Unlock()
	if scope != "apps" {
		return true
	}
	pid := tcpPortOwner(localPort, v6)
	if pid == 0 {
		return false
	}
	return apps[strings.ToLower(processName(pid))]
}

// tcpPortOwner — GetExtendedTcpTable(TCP_TABLE_OWNER_PID_ALL) ile yerel portun PID'i.
func tcpPortOwner(port uint16, v6 bool) uint32 {
	af, rowSize, portOff, pidOff := uintptr(windows.AF_INET), 24, 8, 20
	if v6 {
		af, rowSize, portOff, pidOff = windows.AF_INET6, 56, 20, 52
	}
	size := uint32(64 * 1024)
	for i := 0; i < 3; i++ {
		buf := make([]byte, size)
		r, _, _ := getExtendedTcpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, af, 5, 0)
		if r == uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
			continue
		}
		if r != 0 {
			return 0
		}
		n := int(binary.LittleEndian.Uint32(buf))
		for j := 0; j < n; j++ {
			row := buf[4+j*rowSize:]
			// port DWORD'ün ilk iki baytında ağ sırasıyla durur
			if binary.BigEndian.Uint16(row[portOff:]) == port {
				return binary.LittleEndian.Uint32(row[pidOff:])
			}
		}
		return 0
	}
	return 0
}

func processName(pid uint32) string {
	if pid == uint32(os.Getpid()) {
		return filepath.Base(os.Args[0])
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return filepath.Base(windows.UTF16ToString(buf[:n]))
}
