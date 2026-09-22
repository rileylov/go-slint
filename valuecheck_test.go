package slint_test

// Strings that can't cross the C boundary unchanged — invalid UTF-8 (the shim reads
// inbound text as UTF-8) and interior NUL bytes (C strings end at the first NUL) —
// used to be lost quietly: a struct field vanished, an array element was dropped, a
// model row read as missing, a scalar Set failed with the misleading "value is NULL",
// and "a\x00b" was stored as "a". These pin the fix: every such value is rejected
// with an error that says what is wrong AND where to look (the property or callback,
// the element or argument index, the struct field), the target is left untouched,
// and the paths with no error channel report instead.

import (
	"strings"
	"testing"

	slint "github.com/rileylov/go-slint"
)

const badUTF8 = "bad\xff"

func compileCheck(t *testing.T) (*slint.Compilation, *slint.Instance) {
	t.Helper()
	lockSlint(t)
	app, err := slint.Compile(`
		export struct Row { name: string, n: int }
		export enum Mode { on, off }
		export global Cfg {
			in-out property <string> label: "orig";
			callback take(string);
		}
		export component T inherits Window {
			in-out property <string> s: "orig";
			in-out property <Row> rec: { name: "orig", n: 1 };
			in-out property <[Row]> rows;
			in-out property <[string]> names;
			in-out property <Mode> mode: on;
			callback cb() -> string;
			callback take(string);
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
	return app, inst
}

// wantErr checks that err is non-nil, mentions every substring — the reason and
// each place the caller has to look at — and carries exactly one "slint:" prefix,
// at the front (an inner layer that added its own would read "slint: ...: slint: ...").
func wantErr(t *testing.T, err error, what string, substrs ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an error", what)
	}
	s := err.Error()
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			t.Errorf("%s: error %q should mention %q", what, s, sub)
		}
	}
	if !strings.HasPrefix(s, "slint: ") || strings.Count(s, "slint:") != 1 {
		t.Errorf("%s: error %q should carry a single leading \"slint: \" prefix", what, s)
	}
}

func TestInvalidStringRejectedWithPreciseError(t *testing.T) {
	_, inst := compileCheck(t)

	wantErr(t, inst.Set("s", badUTF8), "Set invalid UTF-8", `set property "s"`, "not valid UTF-8")
	wantErr(t, inst.Set("s", "a\x00b"), "Set interior NUL", `set property "s"`, "NUL")
	if v, _ := inst.Get("s"); v != "orig" {
		t.Errorf("a rejected Set must leave the property untouched, got %q", v)
	}

	// The field name and the field value both cross as C strings; the message names
	// the property, the field, and the reason.
	wantErr(t, inst.Set("rec", map[string]any{"name": badUTF8, "n": 2}), "struct field value",
		`set property "rec"`, `struct field "name"`, "not valid UTF-8")
	wantErr(t, inst.Set("rec", map[string]any{"na\x00me": "x", "n": 2}), "struct field name",
		`set property "rec"`, `struct field "na\x00me"`, "NUL")
	if v, _ := inst.Get("rec"); v.(map[string]any)["name"] != "orig" {
		t.Errorf("a rejected struct Set must leave the property untouched, got %v", v)
	}

	// Previously this silently produced a ONE-element model; now the index is named.
	wantErr(t, inst.Set("names", []any{"ok", badUTF8}), "array element",
		`set property "names"`, "element 1", "not valid UTF-8")
	if v, _ := inst.Get("names"); len(v.([]any)) != 0 {
		t.Errorf("a rejected array Set must leave the property untouched, got %v", v)
	}
	// Nested: the path reads outermost-first down to the failing leaf.
	wantErr(t, inst.Set("rows", []any{map[string]any{"name": "ok", "n": 1}, map[string]any{"name": badUTF8, "n": 2}}),
		"array of structs", `set property "rows": element 1: struct field "name": string is not valid UTF-8`)

	wantErr(t, inst.Set("mode", slint.Enum{Type: "Mode", Value: badUTF8}), "enum value",
		`set property "mode"`, "enum value", "not valid UTF-8")
	wantErr(t, inst.Set("mode", slint.Enum{Type: "Mo\x00de", Value: "off"}), "enum type",
		`set property "mode"`, "enum type", "NUL")
	if v, _ := inst.Get("mode"); v.(slint.Enum).Value != "on" {
		t.Errorf("a rejected enum Set must leave the property untouched, got %v", v)
	}

	// An unsupported Go type is the other conversion failure; same shape.
	wantErr(t, inst.Set("s", struct{}{}), "unsupported type", `set property "s"`, "unsupported value type struct {}")

	// A valid string still round-trips, so the check isn't over-eager.
	if err := inst.Set("s", "héllo ✓"); err != nil {
		t.Fatalf("valid UTF-8 rejected: %v", err)
	}
	if v, _ := inst.Get("s"); v != "héllo ✓" {
		t.Errorf("round trip = %q", v)
	}
}

// The three other Layer-1 entry points that take Go values — Invoke arguments and
// the global setters/invokers — add the same context.
func TestInvalidStringRejectedOnEveryEntryPoint(t *testing.T) {
	_, inst := compileCheck(t)

	_, err := inst.Invoke("take", badUTF8)
	wantErr(t, err, "Invoke argument", `invoke "take"`, "argument 0", "not valid UTF-8")
	_, err = inst.Invoke("take", "a\x00b")
	wantErr(t, err, "Invoke argument NUL", `invoke "take"`, "argument 0", "NUL")

	wantErr(t, inst.SetGlobal("Cfg", "label", badUTF8), "SetGlobal",
		`set global property "Cfg.label"`, "not valid UTF-8")
	if v, _ := inst.GetGlobal("Cfg", "label"); v != "orig" {
		t.Errorf("a rejected SetGlobal must leave the property untouched, got %q", v)
	}
	_, err = inst.InvokeGlobal("Cfg", "take", "a\x00b")
	wantErr(t, err, "InvokeGlobal argument", `invoke global "Cfg.take"`, "argument 0", "NUL")

	// Valid values still go through on every one of those paths.
	if _, err := inst.Invoke("take", "héllo"); err != nil {
		t.Errorf("Invoke with valid UTF-8: %v", err)
	}
	if err := inst.SetGlobal("Cfg", "label", "héllo"); err != nil {
		t.Errorf("SetGlobal with valid UTF-8: %v", err)
	}
	if v, _ := inst.GetGlobal("Cfg", "label"); v != "héllo" {
		t.Errorf("SetGlobal round trip = %q", v)
	}
	if _, err := inst.InvokeGlobal("Cfg", "take", "héllo"); err != nil {
		t.Errorf("InvokeGlobal with valid UTF-8: %v", err)
	}
}

// A callback has no error channel, so an unconvertible return is reported through
// the panic handler (it used to become void with nothing said) and Slint sees void.
func TestCallbackBadReturnIsReported(t *testing.T) {
	got := capture(t)
	_, inst := compileCheck(t)
	_ = inst.OnCallback("cb", func([]any) any { return badUTF8 })
	v, err := inst.Invoke("cb")
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if v != nil && v != "" {
		t.Errorf("Slint should receive void for an unconvertible return, got %#v", v)
	}
	ps := got()
	if len(ps) != 1 || ps[0].Kind != slint.InvalidArgument || ps[0].Site != "callback" || ps[0].Name != "cb" {
		t.Fatalf("want one InvalidArgument report for callback \"cb\", got %+v", ps)
	}
	if s := ps[0].String(); !strings.Contains(s, "return value") || !strings.Contains(s, "UTF-8") {
		t.Errorf("report should say what and why: %s", s)
	}
}

type badRowModel struct{}

func (badRowModel) RowCount() int       { return 2 }
func (badRowModel) RowData(row int) any { return []string{"ok", badUTF8}[row] }
func (badRowModel) SetRowData(int, any) {}

// A model row that can't cross reads as missing on the Slint side; the model
// trampoline now reports it instead of leaving a silent hole.
func TestModelBadRowIsReported(t *testing.T) {
	got := capture(t)
	_, inst := compileCheck(t)
	m := slint.NewModel(badRowModel{})
	defer m.Close()
	if err := inst.Set("names", m); err != nil {
		t.Fatalf("Set model: %v", err)
	}
	v, err := inst.Get("names") // snapshot pulls every row through RowData
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rows := v.([]any); len(rows) != 2 || rows[0] != "ok" || rows[1] != nil {
		t.Errorf("rows = %#v, want [ok <missing>]", rows)
	}
	// Slint pulls a row as often as it likes (the Set and the snapshot both read
	// here), so expect one report PER pull rather than exactly one.
	ps := got()
	if len(ps) == 0 {
		t.Fatal("want an InvalidArgument report from model.RowData, got none")
	}
	for _, p := range ps {
		if p.Kind != slint.InvalidArgument || p.Site != "model.RowData" {
			t.Fatalf("unexpected report %+v", p)
		}
		if s := p.String(); !strings.Contains(s, "row 1") || !strings.Contains(s, "UTF-8") {
			t.Errorf("report should name the row and the reason: %s", s)
		}
	}
}

// A hard compile failure (no result handle at all) must carry the shim's message.
// It used to come back as a bare "slint: " because a deferred Compiler.Free ran —
// and cleared the error slot — before the message was read.
func TestCompileHardFailureKeepsMessage(t *testing.T) {
	lockSlint(t)
	_, err := slint.Compile("export component A inherits Window { /* " + badUTF8 + " */ }")
	wantErr(t, err, "Compile invalid UTF-8 source", "UTF-8")
	if strings.TrimSpace(strings.TrimPrefix(err.Error(), "slint:")) == "" {
		t.Errorf("empty message: %q", err)
	}
	_, err = slint.CompileSource("x.slint", "export component A inherits Window {}", slint.WithStyle("fluent"))
	if err != nil {
		t.Fatalf("a plain compile must still succeed: %v", err)
	}
}
