package main

import (
	"slices"
	"testing"

	"github.com/rileylov/go-slint"
)

// TestUICompiles validates the 1.17 drag & drop markup (DragArea/DropArea/data-transfer)
// compiles, without needing a display.
func TestUICompiles(t *testing.T) {
	app, err := slint.Compile(src, slint.WithStyle("fluent"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer app.Close()
	if names := app.ComponentNames(); !slices.Contains(names, "DndDemo") {
		t.Fatalf("DndDemo not found in %v", names)
	}
}
