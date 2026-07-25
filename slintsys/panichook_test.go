package slintsys

import (
	"strings"
	"sync"
	"testing"
)

// capturePanics installs a collecting handler for the duration of a test and
// returns a func that yields what it collected.
func capturePanics(t *testing.T) func() []PanicInfo {
	t.Helper()
	var mu sync.Mutex
	var got []PanicInfo
	SetPanicHandler(func(p PanicInfo) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, p)
	})
	t.Cleanup(func() { SetPanicHandler(nil) })
	return func() []PanicInfo {
		mu.Lock()
		defer mu.Unlock()
		return append([]PanicInfo(nil), got...)
	}
}

// TestReportPanicDeliversDetail pins what a handler receives: the site, the name
// where one applies, the panic value, and a stack that names the function that
// actually panicked (not just the trampoline).
func TestReportPanicDeliversDetail(t *testing.T) {
	got := capturePanics(t)

	func() {
		defer func() {
			if r := recover(); r != nil {
				reportPanic("callback", "increment", r)
			}
		}()
		panicHelperForTest()
	}()

	ps := got()
	if len(ps) != 1 {
		t.Fatalf("handler got %d panics, want 1", len(ps))
	}
	p := ps[0]
	if p.Site != "callback" || p.Name != "increment" {
		t.Errorf("site/name = %q/%q, want callback/increment", p.Site, p.Name)
	}
	if s, _ := p.Value.(string); s != "boom" {
		t.Errorf("Value = %v, want \"boom\"", p.Value)
	}
	if !strings.Contains(string(p.Stack), "panicHelperForTest") {
		t.Errorf("stack should name the function that panicked:\n%s", p.Stack)
	}
	if want := `panic in callback "increment": boom`; p.String() != want {
		t.Errorf("String() = %q, want %q", p.String(), want)
	}
}

func panicHelperForTest() { panic("boom") }

// TestPanicHandlerPanicIsContained: reportPanic runs inside a deferred recover at
// the C boundary, so a handler that panics must not escape — that panic would
// unwind into Rust.
func TestPanicHandlerPanicIsContained(t *testing.T) {
	SetPanicHandler(func(PanicInfo) { panic("handler is broken too") })
	t.Cleanup(func() { SetPanicHandler(nil) })
	reportPanic("callback", "x", "original") // must simply return
}

// TestSetPanicHandlerNilRestoresDefault: passing nil goes back to the built-in
// stderr reporter rather than leaving a stale handler installed.
func TestSetPanicHandlerNilRestoresDefault(t *testing.T) {
	called := false
	SetPanicHandler(func(PanicInfo) { called = true })
	SetPanicHandler(nil)
	reportPanic("timer", "", "ignored") // goes to stderr, not the old handler
	if called {
		t.Error("handler still called after SetPanicHandler(nil)")
	}
}
