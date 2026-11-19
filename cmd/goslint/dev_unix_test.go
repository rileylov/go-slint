//go:build unix

package main

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// TestNotifyStopCatchesSIGTERM: the dev loops listened for Ctrl-C only, so a SIGTERM
// (a supervisor, an IDE's stop button, plain `kill`) took goslint down without
// running its cleanup and orphaned the app it had launched. Unix-only: this delivers
// a real signal to the test process, which Windows has no equivalent of.
func TestNotifyStopCatchesSIGTERM(t *testing.T) {
	sig := make(chan os.Signal, 1)
	notifyStop(sig)
	defer signal.Stop(sig)

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-sig:
		if got != syscall.SIGTERM {
			t.Errorf("got %v, want SIGTERM", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SIGTERM was not delivered to the dev loop's stop channel")
	}
}
