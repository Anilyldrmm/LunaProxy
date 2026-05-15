//go:build !withbundle

package main

func BundledGDPIAvailable() bool { return false }

func ExtractBundledGDPI() (string, error) {
	return "", nil
}
