package main

import "testing"

func TestDeviceTracking(t *testing.T) {
	devices.Range(func(k, v any) bool { devices.Delete(k); return true })

	trackDevice("192.168.1.105", 1024)
	trackDevice("192.168.1.105", 2048)
	trackDevice("192.168.1.112", 512)

	list := GetDevices()
	if len(list) != 2 {
		t.Fatalf("beklenen 2 cihaz, got %d", len(list))
	}

	var found105 bool
	for _, d := range list {
		if d.IP == "192.168.1.105" {
			found105 = true
			if d.Bytes != 3072 {
				t.Errorf("192.168.1.105 bytes=%d, beklenen 3072", d.Bytes)
			}
		}
	}
	if !found105 {
		t.Error("192.168.1.105 listede yok")
	}
}
