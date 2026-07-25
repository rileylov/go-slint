package timertest

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	slint "github.com/rileylov/go-slint"
	"github.com/rileylov/go-slint/slintsys"
)

// TestPanicInLoopDrivenCallbacks covers the panic sites that only a running
// event loop can reach: a timer callback and work posted with
// InvokeFromEventLoop. Both used to swallow panics silently; both must now
// report and leave the loop running (the app keeps going, the bug is visible).
//
// Subprocess for the usual reason: the integration backend is once-per-process
// and thread-affine.
func TestPanicInLoopDrivenCallbacks(t *testing.T) {
	if os.Getenv("GOSLINT_PANIC_LOOP_HELPER") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run", "^TestPanicInLoopDrivenCallbacks$", "-test.v")
		cmd.Env = append(os.Environ(), "GOSLINT_PANIC_LOOP_HELPER=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("helper failed: %v\n%s", err, out)
		}
		return
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := slintsys.InitIntegration(); err != nil {
		t.Fatalf("InitIntegration: %v", err)
	}

	var mu sync.Mutex
	var sites []string
	slint.SetPanicHandler(func(p slint.PanicInfo) {
		mu.Lock()
		defer mu.Unlock()
		sites = append(sites, p.Site)
	})
	defer slint.SetPanicHandler(nil)

	tm := slint.NewTimer()
	defer tm.Close()
	tm.Start(slint.TimerSingleShot, 5, func() { panic("timer callback exploded") })
	if err := slint.InvokeFromEventLoop(func() { panic("posted work exploded") }); err != nil {
		t.Fatalf("InvokeFromEventLoop: %v", err)
	}
	// Quit a little later, so both panics happen inside the live loop first.
	go func() { time.Sleep(400 * time.Millisecond); _ = slint.Quit() }()
	backstop(5 * time.Second)
	if err := slint.Run(); err != nil {
		t.Fatalf("Run: %v (a recovered panic must not break the loop)", err)
	}

	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(sites, ",")
	for _, want := range []string{"timer", "InvokeFromEventLoop"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no report from %s; got [%s]", want, joined)
		}
	}
}
