package slint_test

import (
	"runtime"
	"strings"
	"testing"

	slint "github.com/rileylov/go-slint"
)

// These tests pin the re-entrant Close contract: a Go callback may Close the
// very instance whose call put it on the stack. The shim takes a strong clone
// at entry to every re-entrant call (see instance.rs RE-ENTRANCY RULE), so the
// native component outlives the call even though the Go handle released it —
// previously the freed Box left dangling &self references (most concretely
// run()'s trailing hide()). After the callback, further use of the closed
// instance must degrade to clean errors, never crash.

func compileT(t *testing.T, ui string) (*slint.Compilation, *slint.Instance) {
	t.Helper()
	runtime.LockOSThread()
	if err := slint.InitHeadless(); err != nil && !strings.Contains(err.Error(), "lready") {
		t.Fatalf("InitHeadless: %v", err)
	}
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
