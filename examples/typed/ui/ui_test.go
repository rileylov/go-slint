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

	_ = win.SetName("Ada")                                        // component setter
	_ = win.App().OnGreeting(func(string) string { return "hi" }) // global callback (records via t.Logic())
	_ = win.App().OnClicked(func() {})                            // global callback
	if win.rec == nil || len(win.rec.replays) != 3 {
		t.Fatalf("recorded %d setup calls; want 3 (incl. the globals)", len(win.rec.replays))
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

// TestGlobalHandleSurvivesReload reproduces the dev-only bug where a global handle
// captured before Run() pointed at the instance that live reload then swapped out
// ("set global property failed" on click). The fix: the handle holds a back-ref to
// the component, so it follows win.inner to the freshly-created instance.
func TestGlobalHandleSurvivesReload(t *testing.T) {
	runtime.LockOSThread()
	if err := slint.InitHeadless(); err != nil {
		t.Fatalf("InitHeadless: %v", err)
	}
	win, err := NewAppWindow()
	if err != nil {
		t.Fatalf("NewAppWindow: %v", err)
	}

	logic := win.App() // captured BEFORE the swap, like `counter := win.Counter()`
	if err := logic.SetCalls(1); err != nil {
		t.Fatalf("SetCalls (initial instance): %v", err)
	}

	// Simulate what dev live reload does: create a fresh instance and point the
	// component at it (LiveReload's bind sets c.inner = inst).
	old := win.inner
	app, err := compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	fresh, err := app.Create("AppWindow")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	win.inner = fresh
	defer old.Close()
	defer fresh.Close()

	// The captured handle must now operate on the fresh instance, not the dead one.
	// (Old snapshot behavior: this read the old instance and returned 1.)
	if n, _ := logic.Calls(); n != 0 {
		t.Fatalf("captured handle still bound to old instance: Calls = %d, want 0", n)
	}
	if err := logic.SetCalls(5); err != nil {
		t.Fatalf("SetCalls after reload: %v (this was the reported bug)", err)
	}
	if n, _ := logic.Calls(); n != 5 {
		t.Fatalf("Calls after reload = %d, want 5", n)
	}
}

// TestSetLogModelLive exercises the array-only typed live-model setter end to end:
// binding a *slint.SliceModel via SetLogModel keeps the binding live, so a later
// Append flows through to the `[string] log` property without re-setting it (read
// back through the snapshot getter). Both a SliceModel and a NewModel-wrapped model
// satisfy slint.LiveModel.
func TestSetLogModelLive(t *testing.T) {
	runtime.LockOSThread()
	_ = slint.InitHeadless() // ignore "already set" if this thread was reused

	win, err := NewAppWindow()
	if err != nil {
		t.Fatalf("NewAppWindow: %v", err)
	}
	defer win.Close()

	m := slint.NewSliceModel("a", "b")
	if err := win.SetLogModel(m); err != nil { // *SliceModel is a slint.LiveModel
		t.Fatalf("SetLogModel: %v", err)
	}
	if got, _ := win.Log(); len(got) != 2 {
		t.Fatalf("after bind, log = %v; want 2 rows", got)
	}
	m.Append("c") // live update — no re-set
	if got, _ := win.Log(); len(got) != 3 || got[2] != "c" {
		t.Fatalf("after Append, log = %v; want [a b c] — live binding didn't flow through", got)
	}

	// the *ModelHandle from NewModel also satisfies slint.LiveModel
	if err := win.SetLogModel(slint.NewModel(m)); err != nil {
		t.Fatalf("SetLogModel(NewModel): %v", err)
	}
}
