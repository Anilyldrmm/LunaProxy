package main

// Version — build sırasında ldflags ile enjekte edilir:
// go build -ldflags "-X main.Version=1.2.0"
var Version = "1.0.9"

const (
	githubOwner = "Anilyldrmm"
	githubRepo  = "LunaProxy"
)
