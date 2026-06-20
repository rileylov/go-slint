package slint_test

import (
	"runtime"
	"testing"

	"github.com/rileylov/go-slint"
)

func TestVersion(t *testing.T) {
	if got := slint.Version(); got != "1.17.0" {
		t.Fatalf("Version() = %q, want 1.17.0", got)
	}
}

func TestCompileErrorsReported(t *testing.T) {
	_, err := slint.Compile(`export component Broken { this is not valid slint }`)
	if err == nil {
		t.Fatal("expected a compilation error, got nil")
	}
	if _, ok := err.(*slint.DiagnosticError); !ok {
		t.Fatalf("expected *slint.DiagnosticError, got %T: %v", err, err)
	}
}

// TestHeadlessRoundTrip exercises the whole M1 path on a single locked OS thread
// (Slint's platform/context is thread-local, and the headless backend may be
// initialized only once per process).
func TestHeadlessRoundTrip(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := slint.InitHeadless(); err != nil {
		t.Fatalf("InitHeadless: %v", err)
	}

	const src = `
		export component App inherits Window {
			in-out property <int> counter: 7;
			in-out property <string> title-text: "hi";
			in-out property <bool> active: true;
		}`

	app, err := slint.Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer app.Close()

	if names := app.ComponentNames(); len(names) != 1 || names[0] != "App" {
		t.Fatalf("ComponentNames() = %v, want [App]", names)
	}

	inst, err := app.Create("App")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer inst.Close()

	// int round-trip
	if n, err := inst.Int("counter"); err != nil || n != 7 {
		t.Fatalf("Int(counter) = %d, %v; want 7", n, err)
	}
	if err := inst.Set("counter", 9); err != nil {
		t.Fatalf("Set(counter, 9): %v", err)
	}
	if n, _ := inst.Int("counter"); n != 9 {
		t.Fatalf("Int(counter) after Set = %d; want 9", n)
	}

	// string round-trip
	if s, err := inst.Str("title-text"); err != nil || s != "hi" {
		t.Fatalf("Str(title-text) = %q, %v; want \"hi\"", s, err)
	}
	if err := inst.Set("title-text", "hello"); err != nil {
		t.Fatalf("Set(title-text): %v", err)
	}
	if s, _ := inst.Str("title-text"); s != "hello" {
		t.Fatalf("Str(title-text) after Set = %q; want \"hello\"", s)
	}

	// bool round-trip
	if b, err := inst.Bool("active"); err != nil || b != true {
		t.Fatalf("Bool(active) = %v, %v; want true", b, err)
	}
	if err := inst.Set("active", false); err != nil {
		t.Fatalf("Set(active): %v", err)
	}
	if b, _ := inst.Bool("active"); b != false {
		t.Fatalf("Bool(active) after Set = %v; want false", b)
	}

	// unknown property surfaces an error
	if _, err := inst.Int("nope"); err == nil {
		t.Fatal("expected error reading unknown property")
	}
}
