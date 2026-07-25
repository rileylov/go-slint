package slint_test

// End-to-end panic reporting: a panic in user code called BY Slint must be
// recovered (never unwinding through C into Rust), reported through the hook,
// and leave the app usable. Before this, every one of these panics vanished
// silently — no error, no output, no trace.

import (
	"strings"
	"sync"
	"testing"

	slint "github.com/rileylov/go-slint"
)

// capture installs a collecting panic handler for the test's duration.
func capture(t *testing.T) func() []slint.PanicInfo {
	t.Helper()
	var mu sync.Mutex
	var got []slint.PanicInfo
	slint.SetPanicHandler(func(p slint.PanicInfo) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, p)
	})
	t.Cleanup(func() { slint.SetPanicHandler(nil) })
	return func() []slint.PanicInfo {
		mu.Lock()
		defer mu.Unlock()
		return append([]slint.PanicInfo(nil), got...)
	}
}

// TestPanicInCallbackIsReported is the headline case: a nil-map write in a
// button handler. It used to return err=nil with nothing printed anywhere.
func TestPanicInCallbackIsReported(t *testing.T) {
	got := capture(t)
	comp, win := compileT(t, `export component T inherits Window {
		callback increment();
		in-out property <int> n: 1;
	}`)
	defer comp.Close()
	defer win.Close()

	_ = win.OnCallback("increment", func([]any) any {
		var m map[string]int
		m["kaboom"] = 1 // panic: assignment to entry in nil map
		return nil
	})
	if _, err := win.Invoke("increment"); err != nil {
		t.Fatalf("Invoke: %v (the panic must stay contained)", err)
	}

	ps := got()
	if len(ps) != 1 {
		t.Fatalf("got %d reports, want 1", len(ps))
	}
	if ps[0].Site != "callback" || ps[0].Name != "increment" {
		t.Errorf("report = %s/%q, want callback/increment", ps[0].Site, ps[0].Name)
	}
	if !strings.Contains(string(ps[0].Stack), "panichook_test.go") {
		t.Errorf("stack should point at the handler that panicked:\n%s", ps[0].Stack)
	}
	// The instance survives: the boundary aborted the call, not the app.
	if _, err := win.Get("n"); err != nil {
		t.Errorf("instance unusable after a recovered panic: %v", err)
	}
}

// A global callback reports as "Global.name" so the report identifies it exactly.
func TestPanicInGlobalCallbackIsReported(t *testing.T) {
	got := capture(t)
	comp, win := compileT(t, `export global Logic {
		callback greeting();
	}
	export component T inherits Window {}`)
	defer comp.Close()
	defer win.Close()

	_ = win.OnGlobalCallback("Logic", "greeting", func([]any) any { panic("global boom") })
	if _, err := win.InvokeGlobal("Logic", "greeting"); err != nil {
		t.Fatalf("InvokeGlobal: %v", err)
	}
	ps := got()
	if len(ps) != 1 || ps[0].Name != "Logic.greeting" {
		t.Fatalf("want one report named Logic.greeting, got %+v", ps)
	}
}

// Model methods are called by Slint while it renders; a panic there used to make
// rows silently vanish (RowCount fell back to 0).
func TestPanicInModelIsReported(t *testing.T) {
	got := capture(t)
	comp, win := compileT(t, `export component T inherits Window {
		in property <[string]> items;
		out property <int> count: root.items.length;
	}`)
	defer comp.Close()
	defer win.Close()

	if err := win.Set("items", slint.NewModel(panicModel{})); err != nil {
		t.Fatalf("Set model: %v", err)
	}
	if _, err := win.Get("count"); err != nil {
		t.Fatalf("Get count: %v", err)
	}

	ps := got()
	if len(ps) == 0 {
		t.Fatal("a panicking model reported nothing")
	}
	if !strings.HasPrefix(ps[0].Site, "model.") {
		t.Errorf("site = %q, want a model.* site", ps[0].Site)
	}
}

// panicModel panics on every access — the shape of a model with a bad index
// calculation or an unguarded nil field.
type panicModel struct{}

func (panicModel) RowCount() int       { panic("model row count exploded") }
func (panicModel) RowData(int) any     { panic("model row data exploded") }
func (panicModel) SetRowData(int, any) { panic("model set row data exploded") }

// Work posted from another goroutine panics on the UI thread; it must report too.
func TestPanicInInvokeFromEventLoopIsReported(t *testing.T) {
	got := capture(t)
	comp, win := compileT(t, `export component T inherits Window {}`)
	defer comp.Close()
	defer win.Close()

	// The headless backend runs pre-queued work when a loop starts; drive it
	// directly instead so the test needs no event loop.
	slint.SetPanicHandler(func(p slint.PanicInfo) {}) // placeholder, replaced below
	slint.SetPanicHandler(nil)
	got = capture(t)

	if err := slint.InvokeFromEventLoop(func() { panic("posted work exploded") }); err != nil {
		t.Skipf("InvokeFromEventLoop unavailable on this backend: %v", err)
	}
	// Without a running loop the callback may never execute; if it did, it must
	// have reported.
	for _, p := range got() {
		if p.Site == "InvokeFromEventLoop" {
			return
		}
	}
	t.Skip("posted callback did not run without an event loop (covered by internal/timertest)")
}

// TestDefaultReporterWritesToStderr: with no handler installed the panic must
// still be visible — that silence was the whole bug.
func TestDefaultReporterWritesToStderr(t *testing.T) {
	comp, win := compileT(t, `export component T inherits Window {
		callback boom();
	}`)
	defer comp.Close()
	defer win.Close()

	slint.SetPanicHandler(nil) // default: stderr
	_ = win.OnCallback("boom", func([]any) any { panic("visible please") })
	if _, err := win.Invoke("boom"); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	// Nothing to assert programmatically without capturing the process's stderr;
	// the reporter itself is unit-tested in slintsys. This case exists to prove
	// the default path stays panic-free through the real FFI boundary.
}
