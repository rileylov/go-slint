package main

import (
	"slices"
	"testing"

	"github.com/rileylov/go-slint"
)

// TestUICompiles validates the embedded markup without needing a display.
func TestUICompiles(t *testing.T) {
	app, err := slint.Compile(ui, slint.WithStyle("fluent"))
	if err != nil {
		t.Fatalf("compile ui.slint: %v", err)
	}
	defer app.Close()
	if names := app.ComponentNames(); !slices.Contains(names, "Clock") {
		t.Fatalf("component Clock not found in %v", names)
	}
}
