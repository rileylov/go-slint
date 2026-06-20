//go:build !android

// On non-Android platforms this is a no-op stub so `go build ./...` works; the
// real entry point is in app_android.go (built only for android).
package main

func main() {}
