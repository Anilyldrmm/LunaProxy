//go:build windows

package main

import (
	"fmt"
	"net"
	"strings"
	"time"
)

// mdnsLookup — cihazın IP'sine doğrudan UDP:5353 ile PTR sorgusu gönderir.
// iPhone (Bonjour/mDNS) ve diğer mDNS destekli cihazlar için hostname döndürür.
func mdnsLookup(ip string) string {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return ""
	}
	arpa := fmt.Sprintf("%s.%s.%s.%s.in-addr.arpa", parts[3], parts[2], parts[1], parts[0])

	pkt := buildDNSPTRQuery(arpa)

	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP(ip), Port: 5353})
	if err != nil {
		return ""
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(1200 * time.Millisecond))

	if _, err := conn.Write(pkt); err != nil {
		return ""
	}

	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil {
		return ""
	}
	return parseDNSPTRResponse(buf[:n])
}

func buildDNSPTRQuery(name string) []byte {
	pkt := []byte{
		0, 1, // ID
		0, 0, // Flags: standard query
		0, 1, // QDCOUNT = 1
		0, 0, 0, 0, 0, 0, // ANCOUNT, NSCOUNT, ARCOUNT = 0
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" {
			continue
		}
		pkt = append(pkt, byte(len(label)))
		pkt = append(pkt, []byte(label)...)
	}
	pkt = append(pkt, 0)      // root label
	pkt = append(pkt, 0, 12)  // QTYPE = PTR
	pkt = append(pkt, 0, 1)   // QCLASS = IN
	return pkt
}

func parseDNSPTRResponse(msg []byte) string {
	if len(msg) < 12 {
		return ""
	}
	ancount := int(msg[6])<<8 | int(msg[7])
	if ancount == 0 {
		return ""
	}
	pos := 12
	pos = dnsSkipName(msg, pos)
	pos += 4 // QTYPE + QCLASS

	for i := 0; i < ancount && pos < len(msg); i++ {
		pos = dnsSkipName(msg, pos)
		if pos+10 > len(msg) {
			break
		}
		rtype := int(msg[pos])<<8 | int(msg[pos+1])
		rdlen := int(msg[pos+8])<<8 | int(msg[pos+9])
		pos += 10
		if rtype == 12 { // PTR
			name := dnsDecodeName(msg, pos)
			name = strings.TrimSuffix(name, ".local")
			if name != "" {
				return name
			}
		}
		pos += rdlen
	}
	return ""
}

func dnsSkipName(msg []byte, pos int) int {
	for pos < len(msg) {
		b := msg[pos]
		if b == 0 {
			return pos + 1
		}
		if b&0xC0 == 0xC0 {
			return pos + 2
		}
		pos += int(b) + 1
	}
	return pos
}

func dnsDecodeName(msg []byte, pos int) string {
	var labels []string
	seen := make(map[int]bool)
	for pos < len(msg) {
		if seen[pos] {
			break
		}
		seen[pos] = true
		b := msg[pos]
		if b == 0 {
			break
		}
		if b&0xC0 == 0xC0 {
			if pos+1 >= len(msg) {
				break
			}
			pos = (int(b&0x3F) << 8) | int(msg[pos+1])
			continue
		}
		length := int(b)
		pos++
		if pos+length > len(msg) {
			break
		}
		labels = append(labels, string(msg[pos:pos+length]))
		pos += length
	}
	return strings.Join(labels, ".")
}
