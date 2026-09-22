package slint_test

// Slint 1.18 lets .slint code call `push`, `insert` and `remove` on a model. For a
// Go-backed model those reach Go through the RowMutator bridge: SliceModel out of
// the box, a custom Model by implementing RowMutator. A Model without it is
// read-only to them (a call Slint logs and drops — never a panic, never a hole).

import (
	"testing"

	slint "github.com/rileylov/go-slint"
)

func compileMutate(t *testing.T) *slint.Instance {
	t.Helper()
	lockSlint(t)
	app, err := slint.Compile(`
		export component T inherits Window {
			in-out property <[string]> items;
			// A binding on the model: only a row-added/removed notification from
			// the Go side makes it re-evaluate, so it proves the notify contract.
			out property <int> n: items.length;
			callback push(string);
			callback insert(int, string);
			callback remove(int);
			push(v) => { items.push(v); }
			insert(i, v) => { items.insert(i, v); }
			remove(i) => { items.remove(i); }
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

func mustInvoke(t *testing.T, inst *slint.Instance, name string, args ...any) {
	t.Helper()
	if _, err := inst.Invoke(name, args...); err != nil {
		t.Fatalf("Invoke %s%v: %v", name, args, err)
	}
}

// wantRows checks the Go model, Slint's view of it (a Get snapshot pulls every row
// back through RowData), and the `items.length` binding, which only updates if the
// change was notified.
func wantRows(t *testing.T, inst *slint.Instance, m slint.Model, want ...string) {
	t.Helper()
	if n := m.RowCount(); n != len(want) {
		t.Fatalf("Go model has %d rows, want %d %v", n, len(want), want)
	}
	for i, w := range want {
		if got := m.RowData(i); got != w {
			t.Errorf("Go row %d = %v, want %q", i, got, w)
		}
	}
	v, err := inst.Get("items")
	if err != nil {
		t.Fatal(err)
	}
	rows := v.([]any)
	if len(rows) != len(want) {
		t.Fatalf("Slint sees %v, want %v", rows, want)
	}
	for i, w := range want {
		if rows[i] != w {
			t.Errorf("Slint row %d = %v, want %q", i, rows[i], w)
		}
	}
	if n, err := inst.Int("n"); err != nil || n != len(want) {
		t.Errorf("items.length binding = %d (%v), want %d: the change was not notified", n, err, len(want))
	}
}

func TestSliceModelMutatedFromSlint(t *testing.T) {
	inst := compileMutate(t)
	m := slint.NewSliceModel("a", "b")
	defer m.Close()
	if err := inst.Set("items", m); err != nil {
		t.Fatal(err)
	}

	mustInvoke(t, inst, "push", "c")
	wantRows(t, inst, m, "a", "b", "c")
	mustInvoke(t, inst, "insert", 1, "x")
	wantRows(t, inst, m, "a", "x", "b", "c")
	mustInvoke(t, inst, "insert", 4, "end") // insert at length == push
	wantRows(t, inst, m, "a", "x", "b", "c", "end")
	mustInvoke(t, inst, "remove", 0)
	wantRows(t, inst, m, "x", "b", "c", "end")
	mustInvoke(t, inst, "remove", 3)
	wantRows(t, inst, m, "x", "b", "c")

	// Out of range (including negative, which Slint rejects before reaching Go):
	// logged by Slint, model untouched, no panic, nothing reported on the Go side.
	got := capture(t)
	mustInvoke(t, inst, "remove", 10)
	mustInvoke(t, inst, "insert", 10, "y")
	mustInvoke(t, inst, "remove", -1)
	mustInvoke(t, inst, "insert", -1, "y")
	wantRows(t, inst, m, "x", "b", "c")
	if ps := got(); len(ps) != 0 {
		t.Errorf("an out-of-range mutation is Slint's to log, not a Go report: %+v", ps)
	}

	// The Go-side mutators still work alongside, and Insert bounds match InsertRow.
	m.Insert(0, "first")
	m.Insert(m.Len(), "last")
	m.Insert(99, "dropped")
	m.Insert(-1, "dropped")
	wantRows(t, inst, m, "first", "x", "b", "c", "last")
}

// mutableModel is a custom Model that opts in to RowMutator; it keeps its own
// handle to notify, as the contract requires.
type mutableModel struct {
	rows   []any
	handle *slint.ModelHandle
}

func (m *mutableModel) RowCount() int { return len(m.rows) }
func (m *mutableModel) RowData(i int) any {
	if i < 0 || i >= len(m.rows) {
		return nil
	}
	return m.rows[i]
}
func (m *mutableModel) SetRowData(i int, v any) {
	if i >= 0 && i < len(m.rows) {
		m.rows[i] = v
		m.handle.NotifyRowChanged(i)
	}
}
func (m *mutableModel) InsertRow(i int, v any) bool {
	if i < 0 || i > len(m.rows) {
		return false
	}
	m.rows = append(m.rows[:i], append([]any{v}, m.rows[i:]...)...)
	m.handle.NotifyRowAdded(i, 1)
	return true
}
func (m *mutableModel) RemoveRow(i int) bool {
	if i < 0 || i >= len(m.rows) {
		return false
	}
	m.rows = append(m.rows[:i], m.rows[i+1:]...)
	m.handle.NotifyRowRemoved(i, 1)
	return true
}

func TestCustomRowMutatorFromSlint(t *testing.T) {
	inst := compileMutate(t)
	m := &mutableModel{rows: []any{"a"}}
	m.handle = slint.NewModel(m)
	defer m.handle.Close()
	if err := inst.Set("items", m.handle); err != nil {
		t.Fatal(err)
	}
	mustInvoke(t, inst, "push", "b")
	wantRows(t, inst, m, "a", "b")
	mustInvoke(t, inst, "insert", 0, "z")
	wantRows(t, inst, m, "z", "a", "b")
	mustInvoke(t, inst, "remove", 1)
	wantRows(t, inst, m, "z", "b")
	mustInvoke(t, inst, "remove", 5)
	wantRows(t, inst, m, "z", "b")
}

// readOnlyModel implements only Model.
type readOnlyModel struct{ rows []any }

func (m *readOnlyModel) RowCount() int { return len(m.rows) }
func (m *readOnlyModel) RowData(i int) any {
	if i < 0 || i >= len(m.rows) {
		return nil
	}
	return m.rows[i]
}
func (m *readOnlyModel) SetRowData(int, any) {}

func TestReadOnlyModelIgnoresSlintMutation(t *testing.T) {
	got := capture(t)
	inst := compileMutate(t)
	m := &readOnlyModel{rows: []any{"a", "b"}}
	h := slint.NewModel(m)
	defer h.Close()
	if err := inst.Set("items", h); err != nil {
		t.Fatal(err)
	}
	mustInvoke(t, inst, "push", "c")
	mustInvoke(t, inst, "insert", 0, "c")
	mustInvoke(t, inst, "remove", 0)
	wantRows(t, inst, m, "a", "b")
	if ps := got(); len(ps) != 0 {
		t.Errorf("an unsupported mutation is Slint's to log, not a Go report: %+v", ps)
	}
}

// A plain array snapshot (Set with a []any, a VecModel on the Rust side) is
// Slint's own model, so the new functions work on it without any Go bridge.
func TestSnapshotArrayMutatedFromSlint(t *testing.T) {
	inst := compileMutate(t)
	if err := inst.Set("items", []any{"a"}); err != nil {
		t.Fatal(err)
	}
	mustInvoke(t, inst, "push", "b")
	mustInvoke(t, inst, "remove", 0)
	v, err := inst.Get("items")
	if err != nil {
		t.Fatal(err)
	}
	if rows := v.([]any); len(rows) != 1 || rows[0] != "b" {
		t.Errorf("rows = %v, want [b]", rows)
	}
	if n, _ := inst.Int("n"); n != 1 {
		t.Errorf("items.length = %d, want 1", n)
	}
}
