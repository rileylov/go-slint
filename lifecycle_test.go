package slint_test

// Instance lifetime and Close semantics: what closing a window does (and does
// not) release, and the contract that a Go callback may Close the very instance
// whose call put it on the stack.

import (
	"testing"

	slint "github.com/rileylov/go-slint"
)

// compileT compiles ui headlessly and creates its "T" component. The caller
// closes the returned compilation; the instance is the test's to close (several
// of these tests close it from inside a callback on purpose).
func compileT(t *testing.T, ui string) (*slint.Compilation, *slint.Instance) {
	t.Helper()
	lockSlint(t) // shared with slint_test.go: locks the UI thread, installs headless
	comp, err := slint.Compile(ui, slint.WithStyle("fluent"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	inst, err := comp.Create("T")
	if err != nil {
		comp.Close()
		t.Fatalf("create: %v", err)
	}
	return comp, inst
}

// TestWindowCloseDoesNotRelease pins the lifecycle rule documented in doc.go and
// DOCS.md: closing a window (the user's close button, or RequestClose) runs the
// close handler and HIDES the window — it does not release the instance. The old
// docs claimed the instance was "also released when the window closes", which
// invited both leaks (nobody calls Close) and use-after-free reasoning.
func TestWindowCloseDoesNotRelease(t *testing.T) {
	comp, win := compileT(t, `export component T inherits Window {
		in-out property <int> n: 5;
	}`)
	defer comp.Close()
	defer win.Close()

	handled := false
	win.OnCloseRequested(func() bool { handled = true; return true }) // allow the close
	win.RequestClose()
	if !handled {
		t.Fatal("close handler did not run — RequestClose should drive the close path")
	}

	// The instance must still be fully usable: closing hid the window, nothing more.
	if err := win.Set("n", 99); err != nil {
		t.Fatalf("Set after close: %v (the instance must survive a window close)", err)
	}
	v, err := win.Get("n")
	if err != nil {
		t.Fatalf("Get after close: %v", err)
	}
	if got, _ := v.(float64); got != 99 {
		t.Errorf("n = %v after close, want 99", v)
	}
	if err := win.Show(); err != nil { // and it can be shown again
		t.Errorf("Show after close: %v", err)
	}
}

// TestWindowCloseRequested covers OnCloseRequested + RequestClose: the handler runs
// on a close request, and returning false vetoes the close.
func TestWindowCloseRequested(t *testing.T) {
	lockSlint(t)
	app, err := slint.Compile(`export component W inherits Window {}`)
	if err != nil {
		t.Fatalf("Compile W: %v", err)
	}
	defer app.Close()
	inst, err := app.Create("W")
	if err != nil {
		t.Fatalf("Create W: %v", err)
	}
	defer inst.Close()
	if err := inst.Show(); err != nil {
		t.Fatalf("Show: %v", err)
	}

	calls := 0
	allow := false
	inst.OnCloseRequested(func() bool {
		calls++
		return allow
	})

	inst.RequestClose() // vetoed
	if calls != 1 {
		t.Fatalf("handler calls = %d; want 1 after first RequestClose", calls)
	}
	allow = true
	inst.RequestClose() // allowed (window hides)
	if calls != 2 {
		t.Fatalf("handler calls = %d; want 2 after second RequestClose", calls)
	}
}

// The tests below pin the re-entrant Close contract: a Go callback may Close the
// very instance whose call put it on the stack. The shim takes a strong clone at
// entry to every re-entrant call (see instance.rs RE-ENTRANCY RULE), so the
// native component outlives the call even though the Go handle released it —
// previously the freed Box left dangling &self references (most concretely
// run()'s trailing hide()). After the callback, further use of the closed
// instance must degrade to clean errors, never crash. The fourth case — closing
// from a callback dispatched by a RUNNING event loop — needs a real loop, so it
// lives in internal/timertest.

// Close from a handler running inside Invoke, before any Show.
func TestCloseInsideInvokedCallback(t *testing.T) {
	comp, win := compileT(t, `export component T inherits Window {
		callback boom();
		in-out property <int> n: 7;
	}`)
	defer comp.Close()
	_ = win.OnCallback("boom", func([]any) any {
		win.Close() // frees the Go handle while goslint_instance_invoke is on the stack
		return nil
	})
	if _, err := win.Invoke("boom"); err != nil {
		t.Fatalf("Invoke returned an error (want clean completion): %v", err)
	}
	if _, err := win.Get("n"); err == nil {
		t.Error("Get on the closed instance should error, not succeed")
	}
	win.Close() // idempotent
}

// Close from a Go-implemented pure callback evaluated inside a property binding:
// the Get call itself puts user code on the stack.
func TestCloseInsideBindingEvaluation(t *testing.T) {
	comp, win := compileT(t, `export component T inherits Window {
		pure callback compute() -> int;
		out property <int> val: root.compute();
	}`)
	defer comp.Close()
	_ = win.OnCallback("compute", func([]any) any {
		win.Close() // frees the handle while goslint_instance_get_property evaluates the binding
		return 42
	})
	v, err := win.Get("val")
	if err != nil {
		t.Fatalf("Get returned an error (want the binding's value): %v", err)
	}
	if n, _ := v.(float64); n != 42 {
		t.Errorf("val = %v, want 42", v)
	}
}

// Close from a global callback handler during InvokeGlobal.
func TestCloseInsideGlobalCallback(t *testing.T) {
	comp, win := compileT(t, `export global Logic {
		callback boom();
	}
	export component T inherits Window {}`)
	defer comp.Close()
	_ = win.OnGlobalCallback("Logic", "boom", func([]any) any {
		win.Close()
		return nil
	})
	if _, err := win.InvokeGlobal("Logic", "boom"); err != nil {
		t.Fatalf("InvokeGlobal returned an error (want clean completion): %v", err)
	}
}
