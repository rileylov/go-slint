package slint_test

// What Set / Invoke / callback returns accept beyond the canonical representation
// Get produces. Each of these used to fail with "unsupported value type", or — for
// a model wrapper anywhere but the top level, and a string holding a NUL on the way
// out — went wrong more quietly.

import (
	"reflect"
	"testing"
	"time"

	slint "github.com/rileylov/go-slint"
)

func compileValueMap(t *testing.T) *slint.Instance {
	t.Helper()
	lockSlint(t)
	app, err := slint.Compile(`
		export struct Row { name: string, n: int }
		export struct Page { title: string, items: [string] }
		export component T inherits Window {
			in-out property <int> n;
			in-out property <float> f;
			in-out property <bool> b;
			in-out property <string> s;
			in-out property <duration> d;
			in-out property <Row> rec;
			in-out property <Page> page;
			in-out property <[string]> names;
			in-out property <[int]> nums;
			in-out property <[Row]> rows;
			in-out property <[[string]]> grid;
			out property <string> nul: "a\u{0}b";
			pure callback items() -> [string];
			out property <int> item-count: root.items().length;
			callback echo(string) -> string;
			public function run-echo() -> string { return root.echo("x\u{0}y"); }
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

func mustGet(t *testing.T, inst *slint.Instance, name string) any {
	t.Helper()
	v, err := inst.Get(name)
	if err != nil {
		t.Fatalf("Get(%q): %v", name, err)
	}
	return v
}

// Every integer and float type is a number, as is a named type built on one.
func TestSetAcceptsEveryNumericKind(t *testing.T) {
	inst := compileValueMap(t)
	type Level int
	type Ratio float32

	for _, v := range []any{
		int(7), int8(7), int16(7), int32(7), int64(7),
		uint(7), uint8(7), uint16(7), uint32(7), uint64(7), uintptr(7),
		float32(7), float64(7), Level(7), Ratio(7),
	} {
		if err := inst.Set("n", 0); err != nil {
			t.Fatal(err)
		}
		if err := inst.Set("n", v); err != nil {
			t.Errorf("Set(n, %T): %v", v, err)
			continue
		}
		if got := mustGet(t, inst, "n"); got != 7.0 {
			t.Errorf("Set(n, %T(7)) read back %v", v, got)
		}
	}

	type Name string
	type Flag bool
	if err := inst.Set("s", Name("x")); err != nil {
		t.Errorf("named string: %v", err)
	} else if got := mustGet(t, inst, "s"); got != "x" {
		t.Errorf("named string read back %v", got)
	}
	if err := inst.Set("b", Flag(true)); err != nil {
		t.Errorf("named bool: %v", err)
	} else if got := mustGet(t, inst, "b"); got != true {
		t.Errorf("named bool read back %v", got)
	}
}

// A time.Duration is a Slint `duration` — milliseconds — not its raw nanosecond
// count, which is what a by-kind conversion of the underlying int64 would send.
func TestSetDuration(t *testing.T) {
	inst := compileValueMap(t)
	if err := inst.Set("d", 1500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, inst, "d"); got != 1500.0 {
		t.Errorf("d = %v ms, want 1500", got)
	}
}

// Typed slices, arrays and string-keyed maps cross as arrays and structs without
// first being copied into []any / map[string]any by hand.
func TestSetAcceptsTypedContainers(t *testing.T) {
	inst := compileValueMap(t)

	for _, tc := range []struct {
		prop string
		in   any
		want any
	}{
		{"names", []string{"a", "b"}, []any{"a", "b"}},
		{"names", [2]string{"a", "b"}, []any{"a", "b"}},
		{"names", []string(nil), []any{}},
		{"nums", []int{1, 2, 3}, []any{1.0, 2.0, 3.0}},
		{"nums", []float64{1, 2}, []any{1.0, 2.0}},
		{"nums", []uint8{1, 2}, []any{1.0, 2.0}},
		{"rows", []map[string]any{{"name": "a", "n": 1}}, []any{map[string]any{"name": "a", "n": 1.0}}},
		{"grid", [][]string{{"a"}, {"b", "c"}}, []any{[]any{"a"}, []any{"b", "c"}}},
		{"rec", map[string]string{"name": "x"}, map[string]any{"name": "x", "n": 0.0}},
	} {
		if err := inst.Set(tc.prop, tc.in); err != nil {
			t.Errorf("Set(%s, %T): %v", tc.prop, tc.in, err)
			continue
		}
		if got := mustGet(t, inst, tc.prop); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Set(%s, %#v) read back %#v, want %#v", tc.prop, tc.in, got, tc.want)
		}
	}

	// The element index survives into the error, as it does for []any.
	err := inst.Set("names", []string{"ok", badUTF8})
	wantErr(t, err, "bad element of a typed slice", `set property "names"`, "element 1", "UTF-8")

	// Still unsupported, and still said so plainly.
	for _, v := range []any{struct{ A int }{1}, make(chan int), map[int]string{1: "a"}, func() {}} {
		err := inst.Set("n", v)
		wantErr(t, err, "unsupported type", `set property "n"`, "unsupported value type")
	}
}

// A *SliceModel is a value wherever a value can appear, not only as the top-level
// argument of Set: it used to be rejected inside a struct field, an array element,
// and a callback's return.
func TestModelNestedInValue(t *testing.T) {
	inst := compileValueMap(t)

	m := slint.NewSliceModel("a", "b")
	defer m.Close()

	// In a struct field — and it stays live there.
	if err := inst.Set("page", map[string]any{"title": "t", "items": m}); err != nil {
		t.Fatalf("model in a struct field: %v", err)
	}
	page := mustGet(t, inst, "page").(map[string]any)
	if !reflect.DeepEqual(page["items"], []any{"a", "b"}) {
		t.Errorf("page.items = %#v", page["items"])
	}
	m.Append("c")
	page = mustGet(t, inst, "page").(map[string]any)
	if !reflect.DeepEqual(page["items"], []any{"a", "b", "c"}) {
		t.Errorf("page.items after Append = %#v: the nested model is not live", page["items"])
	}

	// As an array element.
	if err := inst.Set("grid", []any{m, []any{"z"}}); err != nil {
		t.Fatalf("model as an array element: %v", err)
	}
	if got := mustGet(t, inst, "grid"); !reflect.DeepEqual(got, []any{[]any{"a", "b", "c"}, []any{"z"}}) {
		t.Errorf("grid = %#v", got)
	}

	// As a callback's return value. This path has no error to return, so a
	// rejected value was reported to the panic handler and Slint got void.
	var reports []slint.PanicInfo
	slint.SetPanicHandler(func(p slint.PanicInfo) { reports = append(reports, p) })
	defer slint.SetPanicHandler(nil)
	if err := inst.OnCallback("items", func([]any) any { return m }); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, inst, "item-count"); got != 3.0 {
		t.Errorf("item-count = %v, want 3 (the callback's model did not arrive)", got)
	}
	if len(reports) != 0 {
		t.Errorf("returning a *SliceModel from a callback was reported as a problem: %+v", reports)
	}
}

// A nil or closed model wrapper is an error, not a nil dereference.
func TestModelWrapperNilOrClosed(t *testing.T) {
	inst := compileValueMap(t)

	err := inst.Set("names", (*slint.SliceModel)(nil))
	wantErr(t, err, "nil *SliceModel", `set property "names"`, "nil")

	m := slint.NewSliceModel("a")
	m.Close()
	err = inst.Set("page", map[string]any{"title": "t", "items": m})
	wantErr(t, err, "closed model in a struct", `set property "page"`, `struct field "items"`, "closed")
}

// A Slint string that holds a NUL byte reads back whole. It used to come back as
// "" with a nil error: the shim returned strings NUL-terminated, which cannot
// carry one, so it returned nothing at all.
func TestStringWithNULReadsBackWhole(t *testing.T) {
	inst := compileValueMap(t)

	if got := mustGet(t, inst, "nul"); got != "a\x00b" {
		t.Errorf("nul = %q, want %q", got, "a\x00b")
	}

	// ...and as a callback argument.
	var arg any
	if err := inst.OnCallback("echo", func(args []any) any { arg = args[0]; return "ok" }); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.Invoke("run-echo"); err != nil {
		t.Fatal(err)
	}
	if arg != "x\x00y" {
		t.Errorf("callback argument = %q, want %q", arg, "x\x00y")
	}
}
