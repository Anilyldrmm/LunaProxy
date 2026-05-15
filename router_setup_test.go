package main

import "testing"

func TestRouterScriptContents(t *testing.T) {
	if !strContains(routerProxyPac, "#!/bin/sh") {
		t.Error("proxy.pac: shebang eksik")
	}
	if !strContains(routerProxyPac, "/opt/bin:/opt/sbin") {
		t.Error("proxy.pac: PATH eksik")
	}
	if !strContains(routerProxyPac, "DIRECT") {
		t.Error("proxy.pac: DIRECT fallback eksik")
	}
	if !strContains(routerHbSh, "date +%s > /tmp/spac3dpi_hb") {
		t.Error("hb.sh: heartbeat yazma komutu eksik")
	}
	if !strContains(routerUpdateSh, "spac3dpi_proxy") {
		t.Error("update.sh: proxy dosyası yazma eksik")
	}
	if !strContains(routerLighttpdConf, "8090") {
		t.Error("lighttpd.conf: port 8090 eksik")
	}
}

func strContains(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
