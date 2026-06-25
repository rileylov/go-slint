package main

import (
	"testing"

	"github.com/rileylov/go-slint"
)

// TestUICompiles validates the embedded markup without needing a display.
func TestUICompiles(t *testing.T) {
	app, err := slint.Compile(src, slint.WithStyle("fluent"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	app.Close()
}
