package ui

import "testing"

// TestGeneratedCompiles validates the generated wrapper's embedded markup (its
// lazy compile()) without needing a display — also a smoke test for `goslint
// generate` output staying valid.
func TestGeneratedCompiles(t *testing.T) {
	if _, err := compile(); err != nil {
		t.Fatalf("generated app.slint failed to compile: %v", err)
	}
}
