package slintsys

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestLeakWatch verifies the dev-only leak detector: a native object GC'd without
// Free warns, and a Freed one does not.
func TestLeakWatch(t *testing.T) {
	prevEnabled, prevReport := leakWatchEnabled, leakReportf
	msgs := make(chan string, 8)
	leakWatchEnabled = true
	leakReportf = func(format string, a ...any) { msgs <- fmt.Sprintf(format, a...) }
	defer func() { leakWatchEnabled, leakReportf = prevEnabled, prevReport }()

	// 1. an un-Freed image must warn (naming the type) when collected.
	func() {
		img, err := ImageFromRGBA([]byte{1, 2, 3, 4}, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		_ = img // dropped without Free
	}()
	if got := waitLeak(msgs); !strings.Contains(got, "slint.Image") {
		t.Errorf("expected a leak warning naming slint.Image, got %q", got)
	}

	// 2. a Freed image must NOT warn.
	func() {
		img, err := ImageFromRGBA([]byte{1, 2, 3, 4}, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		img.Close()
	}()
	if got := waitLeak(msgs); got != "" {
		t.Errorf("a Freed image should not warn, got %q", got)
	}
}

// waitLeak forces GC and returns the first leak warning, or "" if none arrives.
func waitLeak(msgs chan string) string {
	for i := 0; i < 10; i++ {
		runtime.GC()
		runtime.Gosched()
		select {
		case m := <-msgs:
			return m
		case <-time.After(20 * time.Millisecond):
		}
	}
	return ""
}

func benchImageCreate(b *testing.B, dev bool) {
	prevEnabled, prevReport := leakWatchEnabled, leakReportf
	leakWatchEnabled = dev
	leakReportf = func(string, ...any) {} // suppress warnings during the bench
	defer func() { leakWatchEnabled, leakReportf = prevEnabled, prevReport }()
	pix := make([]byte, 16*16*4)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		img, err := ImageFromRGBA(pix, 16, 16)
		if err != nil {
			b.Fatal(err)
		}
		img.Close()
	}
}

// BenchmarkImageCreateProd is the production path (finalizer off).
func BenchmarkImageCreateProd(b *testing.B) { benchImageCreate(b, false) }

// BenchmarkImageCreateDev is the dev path (GOSLINT_DEV — finalizer armed each create).
func BenchmarkImageCreateDev(b *testing.B) { benchImageCreate(b, true) }

// leakTestModel is a minimal Model that deliberately does NOT reference its
// ModelHandle — the detectable-leak shape (a self-referencing model like
// SliceModel is pinned by its own cgo.Handle and can never be finalized).
type leakTestModel struct{}

func (leakTestModel) RowCount() int       { return 0 }
func (leakTestModel) RowData(int) any     { return nil }
func (leakTestModel) SetRowData(int, any) {}

// TestLeakWatchModelHandle verifies §3.8 coverage: a ModelHandle GC'd without
// Close warns; a Closed one doesn't.
func TestLeakWatchModelHandle(t *testing.T) {
	prevEnabled, prevReport := leakWatchEnabled, leakReportf
	msgs := make(chan string, 8)
	leakWatchEnabled = true
	leakReportf = func(format string, a ...any) { msgs <- fmt.Sprintf(format, a...) }
	defer func() { leakWatchEnabled, leakReportf = prevEnabled, prevReport }()

	func() {
		_ = NewModelHandle(leakTestModel{}) // dropped without Close
	}()
	if got := waitLeak(msgs); !strings.Contains(got, "ModelHandle") {
		t.Errorf("expected a leak warning naming ModelHandle, got %q", got)
	}

	func() {
		mh := NewModelHandle(leakTestModel{})
		mh.Close()
	}()
	if got := waitLeak(msgs); got != "" {
		t.Errorf("a Closed ModelHandle should not warn, got %q", got)
	}
}
