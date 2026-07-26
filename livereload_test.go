package slint

// The live-reload watcher's lifecycle. It used to be an unstoppable
// `for { sleep }` goroutine: every LiveReload leaked one, and after the call
// returned it kept polling and posting reloads that referenced an instance
// already closed. These tests are in-package because watchSlint is internal.

import (
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func writeSlint(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestWatchSlintNotifiesOnChange: an edit to any .slint beside the entry fires
// onChange exactly once per change.
func TestWatchSlintNotifiesOnChange(t *testing.T) {
	dir := t.TempDir()
	entry := writeSlint(t, dir, "app.slint", `export component App inherits Window {}`)

	var fired atomic.Int32
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchSlint(entry, 5*time.Millisecond, stop, func() { fired.Add(1) })
	}()

	// A sibling import counts too — that's why the watcher scans the directory.
	time.Sleep(20 * time.Millisecond)
	writeSlint(t, dir, "card.slint", `export component Card {}`)

	deadline := time.Now().Add(2 * time.Second)
	for fired.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fired.Load() == 0 {
		t.Fatal("watcher never reported the change")
	}

	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not return after stop was closed")
	}
}

// TestWatchSlintStopsPromptly: closing stop must return even while the watcher is
// waiting between polls — shutdown can't block for a poll interval.
func TestWatchSlintStopsPromptly(t *testing.T) {
	dir := t.TempDir()
	entry := writeSlint(t, dir, "app.slint", `export component App inherits Window {}`)

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchSlint(entry, time.Hour, stop, func() { t.Error("onChange fired unexpectedly") })
	}()

	time.Sleep(20 * time.Millisecond) // let it settle into the wait
	start := time.Now()
	close(stop)
	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("stop took %v; it must not wait out the poll interval", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watcher ignored stop while waiting between polls")
	}
}

// TestWatchSlintNoGoroutineLeak: repeated watchers must leave nothing behind, so
// calling LiveReload more than once (tests, embedded apps) doesn't accumulate
// pollers that all keep firing.
func TestWatchSlintNoGoroutineLeak(t *testing.T) {
	dir := t.TempDir()
	entry := writeSlint(t, dir, "app.slint", `export component App inherits Window {}`)

	settle := func() int {
		for i := 0; i < 50; i++ {
			runtime.Gosched()
			time.Sleep(2 * time.Millisecond)
		}
		return runtime.NumGoroutine()
	}
	before := settle()

	for i := 0; i < 5; i++ {
		stop := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			watchSlint(entry, 5*time.Millisecond, stop, func() {})
		}()
		time.Sleep(15 * time.Millisecond)
		close(stop)
		<-done
	}

	if after := settle(); after > before {
		t.Errorf("goroutines grew from %d to %d after 5 watcher cycles", before, after)
	}
}
