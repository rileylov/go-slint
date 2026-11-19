package slint_test

// Invoke with the wrong arguments. The interpreter trusts what it is handed: a
// `.slint` function (or a callback whose handler is written in `.slint`) reads its
// parameters by index and by declared type, so a missing argument or one of the
// wrong kind panicked inside it. The panic was contained, but the caller got
// "panic in goslint-sys" and a Rust backtrace on stderr for an ordinary mistake in
// their own call. These pin the check that now runs first: the call is rejected
// with an error that says what is wrong, nothing is invoked, and the instance
// carries on.

import (
	"strings"
	"testing"

	slint "github.com/rileylov/go-slint"
)

func compileInvoke(t *testing.T) *slint.Instance {
	t.Helper()
	lockSlint(t)
	app, err := slint.Compile(`
		export struct Point { x: int, y: int }
		export enum Mode { on, off }
		export enum Other { a, b }
		export global Logic {
			public function add(a: int, b: int) -> int { return a + b; }
		}
		export component T inherits Window {
			in-out property <string> last: "none";
			public function twice(n: int) -> int { return n * 2; }
			public function join(a: string, b: string) -> string { return a + b; }
			public function sum(p: Point) -> int { return p.x + p.y; }
			public function is-on(m: Mode) -> bool { return m == Mode.on; }
			public function count(items: [string]) -> int { return items.length; }
			callback note(string);
			note(s) => { root.last = s; }
			callback ping();
		}`)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := app.Create("T")
	if err != nil {
		app.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { inst.Close(); app.Close() })
	return inst
}

func TestInvokeRejectsWrongArguments(t *testing.T) {
	inst := compileInvoke(t)

	for _, tc := range []struct {
		what string
		call func() (any, error)
		want []string
	}{
		{"too few", func() (any, error) { return inst.Invoke("twice") },
			[]string{`invoke "twice"`, "takes 1 argument, got 0"}},
		{"too many", func() (any, error) { return inst.Invoke("twice", 1, 2) },
			[]string{`invoke "twice"`, "takes 1 argument, got 2"}},
		{"wrong kind", func() (any, error) { return inst.Invoke("twice", "three") },
			[]string{`invoke "twice"`, "argument 0", "a string", "int"}},
		{"second argument wrong", func() (any, error) { return inst.Invoke("join", "a", 2) },
			[]string{`invoke "join"`, "argument 1", "a number", "string"}},
		{"nil for a typed parameter", func() (any, error) { return inst.Invoke("twice", nil) },
			[]string{`invoke "twice"`, "argument 0", "void"}},
		{"struct with an unknown field", func() (any, error) {
			return inst.Invoke("sum", map[string]any{"x": 1, "z": 2})
		}, []string{`invoke "sum"`, "argument 0"}},
		{"struct field of the wrong kind", func() (any, error) {
			return inst.Invoke("sum", map[string]any{"x": "one", "y": 2})
		}, []string{`invoke "sum"`, "argument 0"}},
		{"enum of another type", func() (any, error) {
			return inst.Invoke("is-on", slint.Enum{Type: "Other", Value: "a"})
		}, []string{`invoke "is-on"`, "argument 0", "Other"}},
		{"scalar for an array", func() (any, error) { return inst.Invoke("count", "x") },
			[]string{`invoke "count"`, "argument 0", "a string"}},
		// A callback handled in .slint reads its argument just like a function does.
		{"callback, too few", func() (any, error) { return inst.Invoke("note") },
			[]string{`invoke "note"`, "takes 1 argument, got 0"}},
		{"callback, extra", func() (any, error) { return inst.Invoke("ping", 1) },
			[]string{`invoke "ping"`, "takes 0 arguments, got 1"}},
		{"global, too few", func() (any, error) { return inst.InvokeGlobal("Logic", "add", 1) },
			[]string{`invoke global "Logic.add"`, "takes 2 arguments, got 1"}},
		{"global, wrong kind", func() (any, error) { return inst.InvokeGlobal("Logic", "add", 1, "2") },
			[]string{`invoke global "Logic.add"`, "argument 1", "a string"}},
	} {
		_, err := tc.call()
		wantErr(t, err, tc.what, tc.want...)
		if err != nil && strings.Contains(err.Error(), "panic") {
			t.Errorf("%s: the call reached the interpreter and panicked: %v", tc.what, err)
		}
	}

	// Nothing above was invoked.
	if got, _ := inst.Str("last"); got != "none" {
		t.Errorf("last = %q: a rejected call still ran its handler", got)
	}
}

// The check must not get in the way of a correct call, in any spelling of the
// name, nor change what an unknown name reports.
func TestInvokeAcceptsCorrectArguments(t *testing.T) {
	inst := compileInvoke(t)

	for _, tc := range []struct {
		what string
		call func() (any, error)
		want any
	}{
		{"int", func() (any, error) { return inst.Invoke("twice", 21) }, 42.0},
		{"strings", func() (any, error) { return inst.Invoke("join", "a", "b") }, "ab"},
		{"struct", func() (any, error) { return inst.Invoke("sum", map[string]any{"x": 1, "y": 2}) }, 3.0},
		{"partial struct", func() (any, error) { return inst.Invoke("sum", map[string]any{"x": 5}) }, 5.0},
		{"enum", func() (any, error) { return inst.Invoke("is-on", slint.Enum{Type: "Mode", Value: "on"}) }, true},
		{"underscore spelling", func() (any, error) { return inst.Invoke("is_on", slint.Enum{Type: "Mode", Value: "off"}) }, false},
		{"array", func() (any, error) { return inst.Invoke("count", []any{"a", "b", "c"}) }, 3.0},
		{"live model", func() (any, error) { return inst.Invoke("count", slint.NewSliceModel("a", "b")) }, 2.0},
		{"global", func() (any, error) { return inst.InvokeGlobal("Logic", "add", 2, 3) }, 5.0},
	} {
		got, err := tc.call()
		if err != nil {
			t.Errorf("%s: %v", tc.what, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %v (%T), want %v", tc.what, got, got, tc.want)
		}
	}

	if _, err := inst.Invoke("note", "hello"); err != nil {
		t.Fatalf("note: %v", err)
	}
	if got, _ := inst.Str("last"); got != "hello" {
		t.Errorf("last = %q after note(\"hello\")", got)
	}

	_, err := inst.Invoke("no-such-thing", 1)
	wantErr(t, err, "unknown callable", `invoke "no-such-thing"`, "no such")
	_, err = inst.InvokeGlobal("Nope", "add", 1, 2)
	wantErr(t, err, "unknown global", `invoke global "Nope.add"`)
}
