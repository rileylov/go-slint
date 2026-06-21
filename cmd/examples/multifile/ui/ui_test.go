package ui

import (
	"testing"

	slint "github.com/rileylov/go-slint"
)

// TestGeneratedCompiles validates the generated wrapper (disk branch in-repo).
func TestGeneratedCompiles(t *testing.T) {
	if _, err := compile(); err != nil {
		t.Fatalf("generated app.slint failed to compile: %v", err)
	}
}

// TestEmbeddedCompiles exercises the self-contained path a shipped binary uses:
// compile the entry purely from the embedded source + file loader, no disk access.
func TestEmbeddedCompiles(t *testing.T) {
	c, err := slint.CompileSource(generatedEntryName, generatedSource,
		slint.WithStyle("fluent"), slint.WithFileLoader(embeddedLoader))
	if err != nil {
		t.Fatalf("embedded compile failed: %v", err)
	}
	c.Close()
}
