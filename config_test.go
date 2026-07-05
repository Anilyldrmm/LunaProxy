package main

import "testing"

func TestIsMITMExempt(t *testing.T) {
	// Save prior config before modifying it
	cfgMu.Lock()
	saved := current
	current = Config{MITMExemptDomains: []string{"apple.com"}}
	cfgMu.Unlock()

	// Restore config after test completes
	t.Cleanup(func() {
		cfgMu.Lock()
		current = saved
		cfgMu.Unlock()
	})

	if !isMITMExempt("gsp-ssl.ls.apple.com") {
		t.Error("apple.com alt-domaini muaf sayılmalıydı")
	}
	if !isMITMExempt("apple.com") {
		t.Error("apple.com'un kendisi muaf sayılmalıydı")
	}
	if isMITMExempt("ads.example.com") {
		t.Error("ads.example.com muaf olmamalıydı")
	}
}

func TestDefaultConfig_HasMITMExemptDomains(t *testing.T) {
	c := defaultConfig()
	if len(c.MITMExemptDomains) == 0 {
		t.Error("varsayılan MITMExemptDomains boş olmamalı")
	}
	if c.AdBlockEnabled {
		t.Error("AdBlockEnabled varsayılan false olmalı")
	}
}
