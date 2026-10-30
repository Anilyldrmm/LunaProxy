package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// ipv4TCP — 20 bayt IPv4 + 20 bayt TCP başlıklı test paketi.
func ipv4TCP(payload []byte) []byte {
	p := make([]byte, 40, 40+len(payload))
	p[0], p[9] = 0x45, protoTCP
	copy(p[12:], []byte{10, 0, 0, 2})
	copy(p[16:], []byte{1, 2, 3, 4})
	binary.BigEndian.PutUint16(p[20:], 50000)
	binary.BigEndian.PutUint16(p[22:], 443)
	binary.BigEndian.PutUint32(p[24:], 1000) // seq
	p[32] = 5 << 4                           // data offset
	p = append(p, payload...)
	binary.BigEndian.PutUint16(p[2:], uint16(len(p)))
	return p
}

func TestTCPSegmentSplit(t *testing.T) {
	ch := realClientHello(t, "discord.com")
	pkt := ipv4TCP(ch)
	in, ok := parsePacket(pkt)
	if !ok || in.proto != protoTCP || in.payload != 40 {
		t.Fatalf("parsePacket: %+v ok=%v", in, ok)
	}
	_, off, _ := parseSNI(pkt[in.payload:])
	s1 := tcpSegment(pkt, in, ch[:off+1], 0)
	s2 := tcpSegment(pkt, in, ch[off+1:], off+1)
	if got := binary.BigEndian.Uint32(s2[24:]); got != 1000+uint32(off+1) {
		t.Fatalf("seg2 seq=%d", got)
	}
	if int(binary.BigEndian.Uint16(s1[2:])) != len(s1) || int(binary.BigEndian.Uint16(s2[2:])) != len(s2) {
		t.Fatal("IP toplam uzunluk alanı yanlış")
	}
	if !bytes.Equal(append(append([]byte{}, s1[40:]...), s2[40:]...), ch) {
		t.Fatal("segment payload'ları orijinali vermiyor")
	}
}

func TestDNSQueryReply(t *testing.T) {
	// discord.com A sorgusu
	q := []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0,
		7, 'd', 'i', 's', 'c', 'o', 'r', 'd', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1}
	name, qtype, qEnd, ok := parseDNSQuery(q)
	if !ok || name != "discord.com" || qtype != 1 || qEnd != len(q) {
		t.Fatalf("parseDNSQuery: %q %d %d %v", name, qtype, qEnd, ok)
	}
	r := buildDNSReply(q, qEnd, []string{"162.159.136.232", "::1"})
	if r[0] != 0x12 || r[2]&0x80 == 0 || binary.BigEndian.Uint16(r[6:]) != 1 {
		t.Fatalf("cevap başlığı yanlış: % x", r[:12])
	}
	if !bytes.Equal(r[len(r)-4:], []byte{162, 159, 136, 232}) {
		t.Fatal("A kaydı yanlış")
	}

	// UDP cevap paketi: adres/port yer değiştirmeli
	pkt := make([]byte, 28)
	pkt[0], pkt[9] = 0x45, protoUDP
	copy(pkt[12:], []byte{10, 0, 0, 2})
	copy(pkt[16:], []byte{8, 8, 8, 8})
	binary.BigEndian.PutUint16(pkt[20:], 55555)
	binary.BigEndian.PutUint16(pkt[22:], 53)
	pkt = append(pkt, q...)
	in, _ := parsePacket(pkt)
	rp := udpReply(pkt, in, r)
	if !bytes.Equal(rp[12:16], []byte{8, 8, 8, 8}) || binary.BigEndian.Uint16(rp[22:]) != 55555 ||
		int(binary.BigEndian.Uint16(rp[24:])) != 8+len(r) || int(binary.BigEndian.Uint16(rp[2:])) != len(rp) {
		t.Fatal("udpReply başlıkları yanlış")
	}
}

func TestDNSEmptyReplyHasSOA(t *testing.T) {
	q := []byte{0, 1, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1, 'a', 0, 0, 28, 0, 1}
	_, _, qEnd, _ := parseDNSQuery(q)
	r := buildDNSReply(q, qEnd, nil)
	if binary.BigEndian.Uint16(r[6:]) != 0 || binary.BigEndian.Uint16(r[8:]) != 1 || len(r) != qEnd+12+22 {
		t.Fatalf("SOA'lı boş cevap yanlış: % x", r)
	}
}

func TestAutoFakeTTL(t *testing.T) {
	// Linux sunucu (64) 10 atlama uzakta → 54 gelir → sahte TTL 8; Windows (128) 3 atlama → 125 → alt sınır 3
	for recv, want := range map[byte]byte{54: 8, 125: 3, 240: 13} {
		if got := autoFakeTTL(recv); got != want {
			t.Errorf("autoFakeTTL(%d)=%d, beklenen %d", recv, got, want)
		}
	}
}
