package timertest

import (
	"os"
	"os/exec"
	"runtime"
	"testing"

	slint "github.com/rileylov/go-slint"
	"github.com/rileylov/go-slint/slintsys"
)

// TestCloseInsideEventLoop pins the concrete re-entrant-Close dangling case:
// slint's run() is show + event loop + hide, and a callback dispatched by the
// RUNNING loop that Closes the instance used to leave the trailing hide()
// executing on the freed Box. The scenario runs in a subprocess because the
// integration backend is once-per-process and thread-affine — a fresh process
// owns it end to end on one locked thread, and the parent (which must not
// touch slint at all, TestTimers owns this process's backend) just asserts a
// clean exit.
func TestCloseInsideEventLoop(t *testing.T) {
	if os.Getenv("GOSLINT_REENTRANT_LOOP_HELPER") == "1" {
		runtime.LockOSThread()
		if err := slintsys.InitIntegration(); err != nil {
			t.Fatalf("init: %v", err)
		}
		comp, err := slint.Compile(`export component T inherits Window {}`, slint.WithStyle("fluent"))
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		defer comp.Close()
		win, err := comp.Create("T")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		// Queued before Run; the loop drains it once it starts.
		if err := slint.InvokeFromEventLoop(func() {
			win.Close() // frees the Go handle while goslint_instance_run is live
			_ = slint.Quit()
		}); err != nil {
			t.Fatalf("InvokeFromEventLoop: %v", err)
		}
		if err := win.Run(); err != nil {
			t.Fatalf("Run after in-loop close: %v", err)
		}
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run", "^TestCloseInsideEventLoop$", "-test.v")
	cmd.Env = append(os.Environ(), "GOSLINT_REENTRANT_LOOP_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("in-loop close crashed or failed: %v\n%s", err, out)
	}
}
