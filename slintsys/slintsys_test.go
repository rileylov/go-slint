package slintsys

import "testing"

func TestVersion(t *testing.T) {
	if got := Version(); got != "1.17.0" {
		t.Fatalf("Version() = %q, want 1.17.0", got)
	}
}

func TestSmokeCompile(t *testing.T) {
	if got := SmokeCompile(); got != "Smoke" {
		t.Fatalf("SmokeCompile() = %q, want %q (last error: %q)", got, "Smoke", LastError())
	}
}
