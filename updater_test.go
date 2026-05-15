package main

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"0.1.0", "0.2.0", true},
		{"1.0.0", "1.0.0", false},
		{"1.2.0", "0.9.9", false},
		{"0.1.0", "v0.2.0", true}, // tag v-prefix
	}
	for _, tc := range tests {
		got := newerVersion(tc.current, tc.latest)
		if got != tc.want {
			t.Errorf("newerVersion(%q,%q)=%v want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}
