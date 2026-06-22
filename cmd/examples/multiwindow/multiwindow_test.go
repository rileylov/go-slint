package main

import (
	"slices"
	"testing"

	"github.com/rileylov/go-slint"
)

// TestUICompiles validates the embedded markup and that both window components are
// present, without needing a display.
func TestUICompiles(t *testing.T) {
	app, err := slint.Compile(src, slint.WithStyle("fluent"))
	if err != nil {
		t.Fatalf("compile app.slint: %v", err)
	}
	defer app.Close()
	names := app.ComponentNames()
	for _, want := range []string{"MainWindow", "Palette"} {
		if !slices.Contains(names, want) {
			t.Fatalf("%s not found in %v", want, names)
		}
	}
}
