package main

import (
	"slices"
	"testing"

	"github.com/rileylov/go-slint"
)

// TestUICompiles validates the window + SystemTrayIcon markup (1.17) compiles and both
// top-level components are present, without needing a display/tray.
func TestUICompiles(t *testing.T) {
	app, err := slint.Compile(src, slint.WithStyle("fluent"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer app.Close()
	names := app.ComponentNames()
	for _, want := range []string{"MainWindow", "Tray"} {
		if !slices.Contains(names, want) {
			t.Fatalf("%s not found in %v", want, names)
		}
	}
}
