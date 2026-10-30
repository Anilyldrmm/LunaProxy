package main

import (
	"encoding/binary"
	"net"
	"strings"
)

// Ham IP paketi ayrıştırma / üretme (WinDivert girişi için).

const (
	protoTCP = 6
	protoUDP = 17
)

type pktInfo struct {
	v6      bool
	proto   byte
	l4      int // L4 başlık ofseti
	payload int // payload ofseti
}

// parsePacket — IPv4/IPv6 + TCP/UDP başlıklarını ayrıştırır.
// shortcut: IPv6 uzantı başlıkları desteklenmiyor; TCP/UDP doğrudan IPv6 başlığından sonra gelmeli.
func parsePacket(p []byte) (pktInfo, bool) {
	var in pktInfo
	if len(p) < 20 {
		return in, false
	}
	switch p[0] >> 4 {
	case 4:
		in.l4 = int(p[0]&0x0f) * 4
		in.proto = p[9]
		if in.l4 < 20 {
			return in, false
		}
	case 6:
		in.v6, in.l4, in.proto = true, 40, p[6]
	default:
		return in, false
	}
	switch in.proto {
	case protoTCP:
		if len(p) < in.l4+20 {
			return in, false
		}
		if p[in.l4+12]>>4 < 5 {
			return in, false
		}
		in.payload = in.l4 + int(p[in.l4+12]>>4)*4
	case protoUDP:
		in.payload = in.l4 + 8
	default:
		return in, false
	}
	return in, in.payload <= len(p)
}

func dstIP(p []byte, in pktInfo) net.IP {
	if in.v6 {
		return net.IP(p[24:40])
	}
	return net.IP(p[16:20])
}

func ipTTL(p []byte, in pktInfo) byte {
	if in.v6 {
		return p[7]
	}
	return p[8]
}

func setIPTTL(p []byte, in pktInfo, ttl byte) {
	if in.v6 {
		p[7] = ttl
	} else {
		p[8] = ttl
	}
}

// setIPLen — IP uzunluk alanını paket boyuna göre günceller.
func setIPLen(p []byte, in pktInfo) {
	if in.v6 {
		binary.BigEndian.PutUint16(p[4:], uint16(len(p)-40))
	} else {
		binary.BigEndian.PutUint16(p[2:], uint16(len(p)))
	}
}

// tcpSegment — orijinal paketin başlıklarıyla yeni payload'lı segment üretir; seq += off.
func tcpSegment(p []byte, in pktInfo, payload []byte, off int) []byte {
	seg := append(append([]byte{}, p[:in.payload]...), payload...)
	seq := binary.BigEndian.Uint32(seg[in.l4+4:])
	binary.BigEndian.PutUint32(seg[in.l4+4:], seq+uint32(off))
	setIPLen(seg, in)
	return seg
}

// parseDNSQuery — tek sorulu DNS sorgusunun adını, tipini ve soru bölümü sonunu döndürür.
func parseDNSQuery(d []byte) (name string, qtype uint16, qEnd int, ok bool) {
	if len(d) < 12 || d[2]&0x80 != 0 || binary.BigEndian.Uint16(d[4:]) != 1 {
		return "", 0, 0, false
	}
	var labels []string
	p := 12
	for p < len(d) && d[p] != 0 {
		l := int(d[p])
		if l&0xc0 != 0 || p+1+l > len(d) {
			return "", 0, 0, false
		}
		labels = append(labels, string(d[p+1:p+1+l]))
		p += 1 + l
	}
	if p+5 > len(d) {
		return "", 0, 0, false
	}
	return strings.ToLower(strings.Join(labels, ".")), binary.BigEndian.Uint16(d[p+1:]), p + 5, true
}

// buildDNSReply — sorguya verilen IPv4 adresleriyle cevap üretir (ips boşsa SOA'lı boş NOERROR).
func buildDNSReply(q []byte, qEnd int, ips []string) []byte {
	r := append([]byte{}, q[:qEnd]...)
	r[2], r[3] = 0x81, 0x80 // QR, RD, RA, NOERROR
	binary.BigEndian.PutUint16(r[6:], 0)
	binary.BigEndian.PutUint16(r[8:], 0)
	binary.BigEndian.PutUint16(r[10:], 0)
	n := 0
	for _, s := range ips {
		ip := net.ParseIP(s).To4()
		if ip == nil {
			continue
		}
		// ad işaretçisi 0xc00c, tip A, sınıf IN, TTL 300, uzunluk 4
		r = append(r, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0x01, 0x2c, 0, 4)
		r = append(r, ip...)
		n++
	}
	binary.BigEndian.PutUint16(r[6:], uint16(n))
	if n == 0 {
		// Boş cevaba SOA ekle: Windows SOA'sız NODATA'yı geçersiz sayıp sorguyu tekrarlıyor (lookup zaman aşımı)
		r = append(r, 0xc0, 0x0c, 0, 6, 0, 1, 0, 0, 0x01, 0x2c, 0, 22, 0, 0)
		r = append(r, make([]byte, 20)...) // serial, refresh, retry, expire, minimum
		binary.BigEndian.PutUint16(r[8:], 1)
	}
	return r
}

// udpReply — giden UDP paketine, adresleri/portları ters çevrilmiş cevap paketi üretir.
func udpReply(p []byte, in pktInfo, payload []byte) []byte {
	r := append(append([]byte{}, p[:in.payload]...), payload...)
	if in.v6 {
		copy(r[8:24], p[24:40])
		copy(r[24:40], p[8:24])
		r[7] = 64
	} else {
		copy(r[12:16], p[16:20])
		copy(r[16:20], p[12:16])
		r[8] = 64
	}
	copy(r[in.l4:in.l4+2], p[in.l4+2:in.l4+4])
	copy(r[in.l4+2:in.l4+4], p[in.l4:in.l4+2])
	binary.BigEndian.PutUint16(r[in.l4+4:], uint16(len(r)-in.l4))
	setIPLen(r, in)
	return r
}
