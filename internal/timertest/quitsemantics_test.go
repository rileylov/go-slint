package timertest

import (
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	slint "github.com/rileylov/go-slint"
	"github.com/rileylov/go-slint/slintsys"
)

// TestTimersSurviveQuit pins the documented Quit semantics (doc.go "Quit
// semantics", DOCS.md): Quit stops the loop and cancels nothing — a timer stays
// registered and resumes firing if a loop runs again, and only Stop ends it.
// Apps that read Quit as "everything stops" get surprise callbacks on a second
// Run, so the behavior is documented and pinned here against silent drift.
//
// Runs in a subprocess (same reason as TestCloseInsideEventLoop): Slint's
// platform is installed once per process on one locked thread, and a test
// goroutine that locks a thread kills it on exit — so each loop-owning test
// needs its own process.
func TestTimersSurviveQuit(t *testing.T) {
	if os.Getenv("GOSLINT_QUIT_SEMANTICS_HELPER") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run", "^TestTimersSurviveQuit$", "-test.v")
		cmd.Env = append(os.Environ(), "GOSLINT_QUIT_SEMANTICS_HELPER=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("quit-semantics helper failed: %v\n%s", err, out)
		}
		return
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := slintsys.InitIntegration(); err != nil {
		t.Fatalf("InitIntegration: %v", err)
	}

	var fired int32
	tm := slint.NewTimer()
	defer tm.Close()
	tm.Start(slint.TimerRepeated, 5, func() {
		if atomic.AddInt32(&fired, 1) >= 2 {
			_ = slint.Quit()
		}
	})
	backstop(5 * time.Second)
	if err := slint.Run(); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if atomic.LoadInt32(&fired) < 2 {
		t.Fatalf("timer fired %d times in the first run, want >= 2", fired)
	}

	// Second loop, timer never restarted: it must resume on its own.
	atomic.StoreInt32(&fired, 0)
	go func() { time.Sleep(200 * time.Millisecond); _ = slint.Quit() }()
	if err := slint.Run(); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if atomic.LoadInt32(&fired) == 0 {
		t.Error("timer did not fire in the second run — Quit must not cancel timers (docs say they resume)")
	}

	// Stop is what actually ends it: a third loop stays quiet.
	tm.Stop()
	atomic.StoreInt32(&fired, 0)
	go func() { time.Sleep(200 * time.Millisecond); _ = slint.Quit() }()
	if err := slint.Run(); err != nil {
		t.Fatalf("third run: %v", err)
	}
	if n := atomic.LoadInt32(&fired); n != 0 {
		t.Errorf("stopped timer fired %d times in the third run, want 0", n)
	}
}
