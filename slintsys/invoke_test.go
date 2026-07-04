package slintsys

import (
	"fmt"
	"runtime/cgo"
	"strings"
	"testing"
)

// TestInvokeDropReporting pins the §3.8 fix: a posted callback dropped without
// running (the event loop quit before executing it) warns in dev mode; one that
// ran releases silently; a stale handle is absorbed (§3.5 containment).
func TestInvokeDropReporting(t *testing.T) {
	prevEnabled, prevReport := leakWatchEnabled, leakReportf
	var msgs []string
	leakWatchEnabled = true
	leakReportf = func(format string, a ...any) { msgs = append(msgs, fmt.Sprintf(format, a...)) }
	defer func() { leakWatchEnabled, leakReportf = prevEnabled, prevReport }()

	// A callback that ran must release silently.
	ran := &invokePending{fn: func() {}, ran: true}
	dropInvokePending(uintptr(cgo.NewHandle(ran)))
	if len(msgs) != 0 {
		t.Errorf("a callback that ran must release silently, got %v", msgs)
	}

	// One dropped un-run must warn.
	pending := &invokePending{fn: func() {}}
	dropInvokePending(uintptr(cgo.NewHandle(pending)))
	if len(msgs) != 1 || !strings.Contains(msgs[0], "dropped without running") {
		t.Errorf("expected a dropped-without-running warning, got %v", msgs)
	}

	// A stale handle is absorbed, never panicking through Rust Drop into C.
	dropInvokePending(1 << 40)
}

// TestInvokePostAfterQuitAdvisory: once the loop state is Quit, posting warns in
// dev mode that the callback only runs if a loop starts again. (Only the advisory
// is asserted — whether the post itself succeeds depends on which backend earlier
// tests initialized, which is test-order dependent.)
func TestInvokePostAfterQuitAdvisory(t *testing.T) {
	prevEnabled, prevReport := leakWatchEnabled, leakReportf
	prevState := loopState.Load()
	var msgs []string
	leakWatchEnabled = true
	leakReportf = func(format string, a ...any) { msgs = append(msgs, fmt.Sprintf(format, a...)) }
	defer func() {
		leakWatchEnabled, leakReportf = prevEnabled, prevReport
		loopState.Store(prevState)
	}()

	loopState.Store(loopQuit)
	_ = InvokeFromEventLoop(func() {})
	if joined := strings.Join(msgs, "\n"); !strings.Contains(joined, "after the event loop quit") {
		t.Errorf("expected the post-after-quit advisory, got %v", msgs)
	}
}

// TestWithLoopRunningStates pins the state bracket every loop runner uses:
// Running while the loop body executes, Quit once it returns.
func TestWithLoopRunningStates(t *testing.T) {
	prev := loopState.Load()
	defer loopState.Store(prev)

	var during int32 = -1
	_ = withLoopRunning(func() error { during = loopState.Load(); return nil })
	if during != loopRunning {
		t.Errorf("state during run = %d, want loopRunning", during)
	}
	if got := loopState.Load(); got != loopQuit {
		t.Errorf("state after run = %d, want loopQuit", got)
	}
}
