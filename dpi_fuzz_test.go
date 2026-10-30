package main

import (
	"math/rand"
	"net"
	"testing"
)

// TestParsersNoPanic — WinDivert/istemciden gelen bozuk veride ayrıştırıcılar panik atmamalı.
func TestParsersNoPanic(t *testing.T) {
	ch := realClientHello(t, "discord.com")
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 200000; i++ {
		var b []byte
		if i%2 == 0 { // gerçek ClientHello'nun bozulmuş / kesilmiş hali
			b = append([]byte{}, ch[:r.Intn(len(ch)+1)]...)
			for j := 0; j < 3 && len(b) > 0; j++ {
				b[r.Intn(len(b))] = byte(r.Intn(256))
			}
		} else {
			b = make([]byte, r.Intn(120))
			r.Read(b)
			if len(b) > 0 {
				b[0] = []byte{0x45, 0x60, 0x16}[r.Intn(3)]
			}
		}
		if _, off, ok := parseSNI(b); ok {
			splitTLSRecord(b, off+1)
			(&desyncConn{Conn: nopConn{}, strategy: "tlsrec"}).Write(b)
		}
		if in, ok := parsePacket(b); ok && in.proto == protoUDP {
			if _, _, qEnd, ok := parseDNSQuery(b[in.payload:]); ok {
				udpReply(b, in, buildDNSReply(b[in.payload:], qEnd, []string{"1.2.3.4"}))
			}
		} else if ok {
			tcpSegment(b, in, b[in.payload:], 0)
		}
		parseDNSQuery(b)
	}
}

type nopConn struct{ net.Conn }

func (nopConn) Write(b []byte) (int, error) { return len(b), nil }
