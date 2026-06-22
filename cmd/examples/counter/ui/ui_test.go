package ui

import (
	"runtime"
	"testing"

	slint "github.com/rileylov/go-slint"
)

// TestGeneratedCompiles validates the generated wrapper's embedded markup.
func TestGeneratedCompiles(t *testing.T) {
	if _, err := compile(); err != nil {
		t.Fatalf("generated app.slint failed to compile: %v", err)
	}
}

// TestDevRecordReplay verifies the live-reload record/replay mechanism headlessly:
// under GOSLINT_DEV, setup calls (Set/On) are recorded, and replaying them onto a
// fresh instance reproduces the state — which is what a live reload does per save.
func TestDevRecordReplay(t *testing.T) {
	runtime.LockOSThread()
	if err := slint.InitHeadless(); err != nil {
		t.Fatalf("InitHeadless: %v", err)
	}
	t.Setenv("GOSLINT_DEV", "1") // makes NewCounter start recording

	win, err := NewCounter()
	if err != nil {
		t.Fatalf("NewCounter: %v", err)
	}
	defer win.Close()
	if win.rec == nil {
		t.Fatal("expected a recorder under GOSLINT_DEV")
	}

	// setup: these record AND apply
	if err := win.SetValue(7); err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if err := win.OnIncrement(func() {}); err != nil {
		t.Fatalf("OnIncrement: %v", err)
	}
	if len(win.rec.replays) != 2 {
		t.Fatalf("recorded %d setup calls; want 2", len(win.rec.replays))
	}

	// replay onto a fresh instance, as a live reload would
	app, err := compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	fresh, err := app.Create("Counter")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer fresh.Close()
	target := &Counter{inner: fresh}
	for _, replay := range win.rec.replays {
		if err := replay(target); err != nil {
			t.Fatalf("replay: %v", err)
		}
	}
	if v, _ := target.Value(); v != 7 {
		t.Fatalf("replayed value = %d; want 7", v)
	}
	// replaying onto a non-recording target must not re-record
	if target.rec != nil {
		t.Fatal("replay target should not be recording")
	}
}
