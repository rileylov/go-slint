package main

import (
	"strings"
	"testing"
)

func TestUnexportedGlobalWarnings(t *testing.T) {
	iface := &Interface{
		Component: "App",
		Globals:   []GlobalInfo{{Name: "Theme"}}, // the entry exposes Theme
	}
	files := map[string]string{
		"theme.slint":        "export global Theme { in-out property <int> n; }", // exposed -> no warning
		"export_panel.slint": "export global Export { callback run(); }",         // reachable but not exposed -> warn
		"helpers.slint":      "global Internal { }",                              // not exported -> never reachable, no warn
	}
	warns := unexportedGlobalWarnings(iface, files)
	if len(warns) != 1 {
		t.Fatalf("got %d warnings, want exactly 1: %v", len(warns), warns)
	}
	w := warns[0]
	if !strings.Contains(w, `"Export"`) || !strings.Contains(w, "export_panel.slint") {
		t.Errorf("warning should name the global and its file: %q", w)
	}
	if !strings.Contains(w, `export { Export } from "export_panel.slint"`) {
		t.Errorf("warning should suggest the re-export fix: %q", w)
	}
	// must NOT warn about the exposed global or the private (non-exported) one
	if strings.Contains(w, "Theme") || strings.Contains(w, "Internal") {
		t.Errorf("unexpected global flagged: %q", w)
	}
}
