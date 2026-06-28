package ui

import (
	"os"
	"testing"

	slint "github.com/rileylov/go-slint"
)

// TestGeneratedCompiles validates the generated wrapper compiles.
func TestGeneratedCompiles(t *testing.T) {
	if _, err := compile(); err != nil {
		t.Fatalf("generated app.slint failed to compile: %v", err)
	}
}

// TestEmbeddedCompiles exercises the self-contained path a shipped binary uses:
// compile the entry + its subdir import purely from the embedded FS, from an EMPTY
// working directory so there is no on-disk fallback.
func TestEmbeddedCompiles(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	c, err := slint.CompileFS(slintFS, "app.slint", slint.WithStyle("fluent"))
	if err != nil {
		t.Fatalf("embedded compile (no disk) failed: %v", err)
	}
	c.Close()
}
