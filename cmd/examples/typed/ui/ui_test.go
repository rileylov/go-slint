package ui

import (
	"runtime"
	"testing"

	slint "github.com/rileylov/go-slint"
)

// TestGeneratedCompiles validates the generated wrapper's embedded markup (its
// lazy compile()) without needing a display — also a smoke test for `goslint
// generate` output staying valid.
func TestGeneratedCompiles(t *testing.T) {
	if _, err := compile(); err != nil {
		t.Fatalf("generated app.slint failed to compile: %v", err)
	}
}

// TestDevRecordReplayGlobals verifies live-reload record/replay covers component
// AND global bindings (this component has a Logic global) headlessly.
func TestDevRecordReplayGlobals(t *testing.T) {
	runtime.LockOSThread()
	if err := slint.InitHeadless(); err != nil {
		t.Fatalf("InitHeadless: %v", err)
	}
	t.Setenv("GOSLINT_DEV", "1")

	win, err := NewAppWindow()
	if err != nil {
		t.Fatalf("NewAppWindow: %v", err)
	}
	defer win.Close()

	_ = win.SetName("Ada")                                          // component setter
	_ = win.Logic().OnGreeting(func(string) string { return "hi" }) // global callback (records via t.Logic())
	_ = win.OnClicked(func() {})                                    // component callback
	if win.rec == nil || len(win.rec.replays) != 3 {
		t.Fatalf("recorded %d setup calls; want 3 (incl. the global)", len(win.rec.replays))
	}

	// Replay onto a fresh instance, as a live reload does. The global replay
	// (t.Logic().OnGreeting) must re-register without error, and the component
	// setter must reapply.
	app, err := compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	fresh, err := app.Create("AppWindow")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer fresh.Close()
	target := &AppWindow{inner: fresh}
	for _, replay := range win.rec.replays {
		if err := replay(target); err != nil {
			t.Fatalf("replay: %v", err)
		}
	}
	if n, _ := target.Name(); n != "Ada" {
		t.Fatalf("replayed name = %q; want Ada", n)
	}
}
