package main

import (
	"net/http"
	"sync"
	"time"
)

var (
	hbMu   sync.Mutex
	hbStop chan struct{}
)

// startRouterHeartbeat — proxy çalıştığı sürece router'a her 10s POST atar.
// Router heartbeat > 30s gelmezse otomatik DIRECT'e döner.
// Yeni kurulum /cgi-bin/hb.sh, eski kurulum /hb.sh — hangisi varsa onu kullanır.
func startRouterHeartbeat(gateway string) {
	hbMu.Lock()
	if hbStop != nil {
		close(hbStop)
	}
	stop := make(chan struct{})
	hbStop = stop
	hbMu.Unlock()

	urls := []string{
		"http://" + gateway + ":8090/cgi-bin/hb.sh",
		"http://" + gateway + ":8090/hb.sh",
	}
	client := &http.Client{Timeout: 3 * time.Second}

	go func() {
		sendHeartbeat(client, urls)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				sendHeartbeat(client, urls)
			}
		}
	}()
}

func sendHeartbeat(client *http.Client, urls []string) {
	for _, url := range urls {
		resp, err := client.Post(url, "text/plain", nil)
		if err == nil {
			ok := resp.StatusCode == 200
			resp.Body.Close()
			if ok {
				return
			}
		}
	}
}

// stopRouterHeartbeat — heartbeat goroutine'i durdurur.
func stopRouterHeartbeat() {
	hbMu.Lock()
	defer hbMu.Unlock()
	if hbStop != nil {
		close(hbStop)
		hbStop = nil
	}
}
