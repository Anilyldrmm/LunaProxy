//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestRouterScriptContents(t *testing.T) {
	if !strings.Contains(routerProxyPac, "#!/bin/sh") {
		t.Error("proxy.pac: shebang eksik")
	}
	if !strings.Contains(routerProxyPac, "/opt/bin:/opt/sbin") {
		t.Error("proxy.pac: PATH eksik")
	}
	if !strings.Contains(routerProxyPac, "DIRECT") {
		t.Error("proxy.pac: DIRECT fallback eksik")
	}
	if !strings.Contains(routerHbSh, "date +%s > /tmp/lunaproxy_hb") {
		t.Error("hb.sh: heartbeat yazma komutu eksik")
	}
	if !strings.Contains(routerUpdateSh, "lunaproxy_proxy") {
		t.Error("update.sh: proxy dosyası yazma eksik")
	}
	if !strings.Contains(routerLighttpdConf, "8090") {
		t.Error("lighttpd.conf: port 8090 eksik")
	}
}
