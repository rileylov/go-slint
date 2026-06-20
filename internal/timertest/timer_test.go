// Package timertest verifies that timers actually fire via the event loop, using
// the integration-test backend (a simple loop on the system clock). Its own
// package/process: the backend may be initialized only once per process and is
// thread-affine, so everything runs on one locked OS thread.
package timertest

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rileylov/go-slint/slintsys"
)

// backstop quits the loop after d so a non-firing timer can't hang the test.
func backstop(d time.Duration) {
	go func() {
		time.Sleep(d)
		_ = slintsys.QuitEventLoop()
	}()
}

func TestTimers(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := slintsys.InitIntegration(); err != nil {
		t.Fatalf("InitIntegration: %v", err)
	}

	// repeated timer
	var fired int32
	tm := slintsys.NewTimer()
	tm.Start(slintsys.TimerRepeated, 5, func() {
		if atomic.AddInt32(&fired, 1) >= 3 {
			_ = slintsys.QuitEventLoop()
		}
	})
	backstop(5 * time.Second)
	if err := slintsys.RunEventLoop(); err != nil {
		t.Fatalf("RunEventLoop (repeated): %v", err)
	}
	tm.Stop()
	tm.Free()
	if n := atomic.LoadInt32(&fired); n < 3 {
		t.Fatalf("repeated timer fired %d times; want >= 3", n)
	}

	// single-shot timer
	var shot int32
	slintsys.SingleShot(5, func() {
		atomic.StoreInt32(&shot, 1)
		_ = slintsys.QuitEventLoop()
	})
	backstop(5 * time.Second)
	if err := slintsys.RunEventLoop(); err != nil {
		t.Fatalf("RunEventLoop (single-shot): %v", err)
	}
	if atomic.LoadInt32(&shot) != 1 {
		t.Fatal("single-shot timer did not fire")
	}

	// invoke from a background goroutine -> runs on the loop thread
	var invoked int32
	go func() {
		_ = slintsys.InvokeFromEventLoop(func() {
			atomic.StoreInt32(&invoked, 1)
			_ = slintsys.QuitEventLoop()
		})
	}()
	backstop(5 * time.Second)
	if err := slintsys.RunEventLoop(); err != nil {
		t.Fatalf("RunEventLoop (invoke): %v", err)
	}
	if atomic.LoadInt32(&invoked) != 1 {
		t.Fatal("InvokeFromEventLoop callback did not run")
	}
}
