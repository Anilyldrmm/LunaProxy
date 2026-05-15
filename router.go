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
func startRouterHeartbeat(gateway string) {
	hbMu.Lock()
	if hbStop != nil {
		close(hbStop)
	}
	stop := make(chan struct{})
	hbStop = stop
	hbMu.Unlock()

	url := "http://" + gateway + ":8090/hb.sh"
	client := &http.Client{Timeout: 3 * time.Second}

	go func() {
		client.Post(url, "text/plain", nil) // ilk heartbeat anında
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				client.Post(url, "text/plain", nil)
			}
		}
	}()
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
