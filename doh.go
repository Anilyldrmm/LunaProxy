package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// DoH sağlayıcıları IP ile yazılı — bootstrap için sistem (ISP) DNS'ine gerek yok.
var dohServers = []string{
	"https://1.1.1.1/dns-query",
	"https://8.8.8.8/resolve",
	"https://9.9.9.9:5053/dns-query",
}

var dohClient = &http.Client{Timeout: 4 * time.Second}

type dohEntry struct {
	ips     []string
	expires time.Time
}

var dohCache sync.Map // host → dohEntry

// dohResolve — host'u DoH ile IPv4 adreslerine çözer (önbellekli). Sağlayıcılar paralel sorgulanır;
// hepsi 2 sn içinde başarısızsa hata 60 sn önbelleğe alınır, çağıran o sürede hemen sistem DNS'ine düşer.
// shortcut: sadece A kaydı, IPv6 (AAAA) yok; ISP'ler v6'da da SNI engellerse ekle.
func dohResolve(host string) ([]string, error) {
	if net.ParseIP(host) != nil {
		return []string{host}, nil
	}
	if v, ok := dohCache.Load(host); ok && time.Now().Before(v.(dohEntry).expires) {
		if e := v.(dohEntry); e.ips != nil {
			return e.ips, nil
		}
		return nil, fmt.Errorf("doh: %s yakın zamanda çözülemedi", host)
	}
	type result struct {
		ips []string
		ttl int
		err error
	}
	ch := make(chan result, len(dohServers))
	for _, srv := range dohServers {
		go func(srv string) {
			ips, ttl, err := dohQuery(srv, host)
			ch <- result{ips, ttl, err}
		}(srv)
	}
	timeout := time.After(2 * time.Second)
	var lastErr error = fmt.Errorf("doh: %s zaman aşımı", host)
	for range dohServers {
		select {
		case r := <-ch:
			if r.err != nil {
				lastErr = r.err
				continue
			}
			dohCache.Store(host, dohEntry{r.ips, time.Now().Add(time.Duration(max(r.ttl, 60)) * time.Second)})
			return r.ips, nil
		case <-timeout:
			dohCache.Store(host, dohEntry{nil, time.Now().Add(60 * time.Second)})
			return nil, lastErr
		}
	}
	dohCache.Store(host, dohEntry{nil, time.Now().Add(60 * time.Second)})
	return nil, lastErr
}

func dohQuery(srv, host string) ([]string, int, error) {
	req, _ := http.NewRequest("GET", srv+"?type=A&name="+url.QueryEscape(host), nil)
	req.Header.Set("Accept", "application/dns-json")
	resp, err := dohClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var r struct {
		Answer []struct {
			Type int    `json:"type"`
			TTL  int    `json:"TTL"`
			Data string `json:"data"`
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, 0, err
	}
	var ips []string
	ttl := 300
	for _, a := range r.Answer {
		if a.Type == 1 { // A
			ips = append(ips, a.Data)
			if a.TTL < ttl {
				ttl = a.TTL
			}
		}
	}
	if len(ips) == 0 {
		return nil, 0, fmt.Errorf("doh: %s için A kaydı yok", host)
	}
	return ips, ttl, nil
}
