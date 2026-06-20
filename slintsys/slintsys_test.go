package slintsys

import (
	"regexp"
	"testing"
)

// Version is checked for shape, not an exact value, so it survives Slint bumps
// (the pinned version lives in .slint-version).
func TestVersion(t *testing.T) {
	got := Version()
	if !regexp.MustCompile(`^\d+\.\d+\.\d+`).MatchString(got) {
		t.Fatalf("Version() = %q, want a semver-like string", got)
	}
}

func TestSmokeCompile(t *testing.T) {
	if got := SmokeCompile(); got != "Smoke" {
		t.Fatalf("SmokeCompile() = %q, want %q (last error: %q)", got, "Smoke", LastError())
	}
}
