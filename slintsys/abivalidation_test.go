package slintsys

import (
	"strings"
	"testing"
)

// Go ints are signed; several ABI slots are not (size_t row counts, uint32
// dimensions). An unchecked conversion turns -1 into ~1.8e19 or ~4.3e9, which
// Slint takes at face value — a negative model row count freezes the app trying
// to render 18 quintillion rows. These tests pin that such values are rejected
// at the boundary and reported instead of being passed through mangled.

// collectReports installs a handler gathering the boundary's reports.
func collectReports(t *testing.T) func() []PanicInfo {
	t.Helper()
	var got []PanicInfo
	SetPanicHandler(func(p PanicInfo) { got = append(got, p) })
	t.Cleanup(func() { SetPanicHandler(nil) })
	return func() []PanicInfo { return got }
}

// negRowModel is the classic accident: len(items)-1 on an empty slice.
type negRowModel struct{}

func (negRowModel) RowCount() int       { return -1 }
func (negRowModel) RowData(int) any     { return nil }
func (negRowModel) SetRowData(int, any) {}

// The conversion the model trampoline performs, tested without cgo (the same
// split as pixelBufferLen/snapshotLen).
func TestRowCountForABI(t *testing.T) {
	if _, err := rowCountForABI(-1); err == nil {
		t.Error("RowCount of -1 must be rejected; unchecked it becomes ~1.8e19 rows and hangs the app")
	}
	if _, err := rowCountForABI(-1 << 40); err == nil {
		t.Error("a large negative RowCount must be rejected")
	}
	n, err := rowCountForABI(7)
	if err != nil || n != 7 {
		t.Errorf("rowCountForABI(7) = %d, %v; want 7, nil", n, err)
	}
	if n, err := rowCountForABI(0); err != nil || n != 0 {
		t.Errorf("rowCountForABI(0) = %d, %v; want 0, nil", n, err)
	}
}

// And the trampoline reports it rather than silently rendering an empty model.
func TestNegativeRowCountReported(t *testing.T) {
	got := collectReports(t)
	reportInvalid("model.RowCount", "", errFromRowCount(t))
	ps := got()
	if len(ps) != 1 || ps[0].Kind != InvalidArgument {
		t.Fatalf("want one InvalidArgument report, got %+v", ps)
	}
	if !strings.Contains(ps[0].String(), "-1") || !strings.Contains(ps[0].String(), "invalid argument") {
		t.Errorf("report should name the bad value and its kind: %s", ps[0])
	}
}

func errFromRowCount(t *testing.T) error {
	t.Helper()
	_, err := rowCountForABI(negRowModel{}.RowCount())
	if err == nil {
		t.Fatal("expected an error for a negative row count")
	}
	return err
}

func TestNegativeNotificationsRejected(t *testing.T) {
	got := collectReports(t)
	mh := NewModelHandle(negRowModel{})
	defer mh.Close()

	mh.NotifyRowChanged(-1)
	mh.NotifyRowAdded(-1, 1)
	mh.NotifyRowAdded(0, -1)
	mh.NotifyRowRemoved(-5, 2)
	mh.NotifyRowRemoved(0, -2)

	ps := got()
	if len(ps) != 5 {
		t.Fatalf("want 5 rejections, got %d: %+v", len(ps), ps)
	}
	for _, p := range ps {
		if p.Kind != InvalidArgument {
			t.Errorf("kind = %v, want InvalidArgument (%s)", p.Kind, p)
		}
		if !strings.HasPrefix(p.Site, "model.Notify") {
			t.Errorf("site = %q, want a model.Notify* site", p.Site)
		}
	}
	// Valid notifications still pass through untouched.
	before := len(got())
	mh.NotifyRowChanged(0)
	mh.NotifyRowAdded(0, 3)
	mh.NotifyReset()
	if after := len(got()); after != before {
		t.Errorf("valid notifications produced %d reports, want none", after-before)
	}
}

func TestGLTextureDimensionsRejected(t *testing.T) {
	for _, tc := range []struct{ w, h int }{{-1, 64}, {64, -1}, {0, 64}, {64, 0}, {1 << 33, 64}} {
		if _, err := ImageFromGLTexture(7, tc.w, tc.h, false); err == nil {
			t.Errorf("ImageFromGLTexture(%d, %d) succeeded; want an error", tc.w, tc.h)
		}
	}
}

// validDim guards the uint32 window-size ABI the same way.
func TestValidDim(t *testing.T) {
	got := collectReports(t)
	for _, tc := range []struct{ w, h int }{{-1, 100}, {100, -1}, {0, 100}, {100, 0}, {1 << 33, 100}} {
		if validDim("window size", tc.w, tc.h) {
			t.Errorf("validDim(%d, %d) = true; want false", tc.w, tc.h)
		}
	}
	if !validDim("window size", 800, 600) {
		t.Error("validDim(800, 600) = false; want true")
	}
	if n := len(got()); n != 5 {
		t.Errorf("got %d reports, want 5 (one per rejection)", n)
	}
}
