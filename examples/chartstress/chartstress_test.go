package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/rileylov/go-slint"
)

func TestUICompiles(t *testing.T) {
	app, err := slint.Compile(ui, slint.WithStyle("fluent"))
	if err != nil {
		t.Fatalf("compile ui.slint: %v", err)
	}
	defer app.Close()
	if names := app.ComponentNames(); !slices.Contains(names, "AppWindow") {
		t.Fatalf("AppWindow not found in %v", names)
	}
}

func TestBuildPath(t *testing.T) {
	p := buildPath(4, 2, 0.0)
	if !strings.HasPrefix(p, "M ") {
		t.Fatalf("path should start with a moveto: %q", p)
	}
	// 2 series, each 1 moveto + 3 linetos
	if got := strings.Count(p, "M "); got != 2 {
		t.Fatalf("want 2 subpaths (M), got %d in %q", got, p)
	}
	if got := strings.Count(p, "L "); got != 6 {
		t.Fatalf("want 6 linetos, got %d", got)
	}
}
