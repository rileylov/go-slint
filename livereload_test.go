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

// TestWatchSlintSkipsUnrelatedTrees: the poll walks the entry's directory tree on
// every tick, so it must stay out of the trees that hold no markup and can be huge
// (.git, node_modules, build output) while still seeing a real subdirectory.
func TestWatchSlintSkipsUnrelatedTrees(t *testing.T) {
	dir := t.TempDir()
	entry := writeSlint(t, dir, "app.slint", `export component App inherits Window {}`)
	base := newestMod(entry)

	future := time.Now().Add(time.Hour)
	touch := func(rel string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(`export component X {}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, future, future); err != nil {
			t.Fatal(err)
		}
	}

	for _, rel := range []string{".git/x.slint", "node_modules/pkg/x.slint", "target/x.slint", "vendor/x.slint", ".cache/deep/x.slint"} {
		touch(rel)
	}
	if got := newestMod(entry); !got.Equal(base) {
		t.Errorf("a .slint under a skipped directory moved the watch time (%v -> %v)", base, got)
	}

	touch("components/card.slint")
	if got := newestMod(entry); !got.After(base) {
		t.Error("a .slint in an ordinary subdirectory must still be seen")
	}
}

// TestReloadAdoptsInstanceWhenBindFails: creating the reloaded instance hands it
// the window, so when bind then fails there is no old UI left to "keep". loadReuse
// used to close the new instance and report failure, leaving the window attached
// to a released component while the old, detached one stayed "current". It must
// hand the new instance back, alive, alongside the bind error.
func TestReloadAdoptsInstanceWhenBindFails(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	_ = InitHeadless()

	dir := t.TempDir()
	entry := writeSlint(t, dir, "app.slint", `export component App inherits Window {
		in-out property <int> v: 1;
		callback clicked();
	}`)
	bind := func(i *Instance) error { return i.OnCallback("clicked", func([]any) any { return nil }) }

	cur, err := loadAndShow(entry, "", bind, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cur.close() }()

	// The markup renames the callback before the Go side catches up.
	writeSlint(t, dir, "app.slint", `export component App inherits Window {
		in-out property <int> v: 2;
		callback pressed();
	}`)
	next, bindErr, err := loadReuse(entry, "", bind, nil, cur.inst)
	if err != nil {
		t.Fatalf("the markup compiles, so this is not a failed reload: %v", err)
	}
	if bindErr == nil {
		t.Fatal("bind should have failed: the callback it wires no longer exists")
	}
	if next == nil {
		t.Fatal("the new instance owns the window and must be returned")
	}
	old := cur
	cur = next
	old.close()
	if v, err := cur.inst.Int("v"); err != nil || v != 2 {
		t.Errorf("the adopted instance must be live and be the new markup: v = %d, err = %v", v, err)
	}

	// A reload that doesn't compile changes nothing and reports err.
	writeSlint(t, dir, "app.slint", `export component App inherits Window { oops }`)
	if next, _, err := loadReuse(entry, "", bind, nil, cur.inst); err == nil || next != nil {
		t.Errorf("a compile error must fail the reload outright (next = %v, err = %v)", next, err)
	}
	if v, err := cur.inst.Int("v"); err != nil || v != 2 {
		t.Errorf("after a failed compile the current instance must be untouched: v = %d, err = %v", v, err)
	}
}
