package main

import (
	"bytes"
	"crypto/tls"
	"net"
	"testing"
	"time"
)

// realClientHello — crypto/tls'in ürettiği gerçek ClientHello kaydını yakalar.
func realClientHello(t *testing.T, host string) []byte {
	c1, c2 := net.Pipe()
	defer c1.Close()
	go tls.Client(c1, &tls.Config{ServerName: host}).Handshake()
	c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 16*1024)
	n, err := c2.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	c2.Close()
	return buf[:n]
}

func TestParseSNIAndSplit(t *testing.T) {
	ch := realClientHello(t, "gateway.discord.gg")
	name, off, ok := parseSNI(ch)
	if !ok || name != "gateway.discord.gg" || string(ch[off:off+len(name)]) != name {
		t.Fatalf("parseSNI: ok=%v name=%q off=%d", ok, name, off)
	}

	r1, r2 := splitTLSRecord(ch, off+1)
	// iki kaydın payload'ı birleşince orijinal handshake verisi çıkmalı
	if got := append(append([]byte{}, r1[5:]...), r2[5:]...); !bytes.Equal(got, ch[5:]) {
		t.Fatal("splitTLSRecord payload bozuldu")
	}
	if l1 := int(r1[3])<<8 | int(r1[4]); l1 != len(r1)-5 {
		t.Fatalf("r1 uzunluk alanı %d, gerçek %d", l1, len(r1)-5)
	}

	if _, _, ok := parseSNI([]byte("GET / HTTP/1.1\r\n\r\n")); ok {
		t.Fatal("HTTP isteği ClientHello sanıldı")
	}
}

func TestIsBlockedHostSubdomain(t *testing.T) {
	cfgMu.Lock()
	prev := current
	current = Config{BypassDomains: []string{"discord.com"}}
	cfgMu.Unlock()
	defer func() { cfgMu.Lock(); current = prev; cfgMu.Unlock() }()

	for host, want := range map[string]bool{
		"discord.com": true, "cdn.discord.com": true, "notdiscord.com": false, "google.com": false,
		"Discord.COM.": true,
	} {
		if got := isBlockedHost(host); got != want {
			t.Errorf("isBlockedHost(%q)=%v, beklenen %v", host, got, want)
		}
	}
}

func TestValidAutoHost(t *testing.T) {
	for host, want := range map[string]bool{
		"gateway.discord.gg": true, "com": false, "com.": false, "1.2.3.4": false, "::1": false,
	} {
		if got := validAutoHost(host); got != want {
			t.Errorf("validAutoHost(%q)=%v, beklenen %v", host, got, want)
		}
	}
}
