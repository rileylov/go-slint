package slint_test

// End-to-end panic reporting: a panic in user code called BY Slint must be
// recovered (never unwinding through C into Rust), reported through the hook,
// and leave the app usable. Before this, every one of these panics vanished
// silently — no error, no output, no trace.

import (
	"io"
	"os"
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

// panicModel panics on every access — the shape of a model with a bad index
// calculation or an unguarded nil field.
type panicModel struct{}

func (panicModel) RowCount() int       { panic("model row count exploded") }
func (panicModel) RowData(int) any     { panic("model row data exploded") }
func (panicModel) SetRowData(int, any) { panic("model set row data exploded") }

// Model methods are called by Slint while it renders; a panic there used to make
// rows silently vanish (RowCount fell back to 0 with no explanation).
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

// TestDefaultReporterWritesToStderr: with no handler installed the panic must
// still be visible — that silence was the whole bug. Captures the process's
// stderr around a real callback panic.
func TestDefaultReporterWritesToStderr(t *testing.T) {
	comp, win := compileT(t, `export component T inherits Window {
		callback boom();
	}`)
	defer comp.Close()
	defer win.Close()
	slint.SetPanicHandler(nil) // default reporter

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w

	_ = win.OnCallback("boom", func([]any) any { panic("visible please") })
	_, invokeErr := win.Invoke("boom")

	os.Stderr = orig
	w.Close()
	out, _ := io.ReadAll(r)

	if invokeErr != nil {
		t.Fatalf("Invoke: %v", invokeErr)
	}
	for _, want := range []string{"goslint", "callback", "boom", "visible please"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("default stderr report missing %q; got:\n%s", want, out)
		}
	}
}
