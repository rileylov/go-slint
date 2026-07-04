package slintsys

/*
#include "goslint.h"
*/
import "C"

import "sync/atomic"

// loopState tracks whether a Slint event loop has run and quit, purely for
// better diagnostics: posting work before the FIRST run is legitimate (Slint
// executes pre-queued callbacks at loop start), but posting after a loop quit
// only runs if a loop is started again — so dev mode flags it (see
// InvokeFromEventLoop). Never used to reject work: queue-then-rerun is valid.
const (
	loopNeverRan int32 = iota
	loopRunning
	loopQuit
)

var loopState atomic.Int32

// withLoopRunning brackets a blocking event-loop entry point with state
// tracking. Every loop runner (RunEventLoop, RunEventLoopUntilQuit,
// Instance.Run) must go through it.
func withLoopRunning(run func() error) error {
	loopState.Store(loopRunning)
	defer loopState.Store(loopQuit)
	return run()
}

// InitHeadless installs the headless testing backend (mock time, no windows).
// Call once per process, before creating any component, on the UI thread.
func InitHeadless() error {
	MarkUIThread() // this thread owns Slint's context for tests
	return rc(C.goslint_testing_init_headless(), "init headless")
}

// MockElapsedTime advances the testing backend's mock clock.
func MockElapsedTime(ms uint64) { C.goslint_testing_mock_elapsed_time(C.uint64_t(ms)) }

// ConfigureTestFonts installs deterministic embedded fonts (call after
// InitHeadless). Matches Slint's interpreter test driver.
func ConfigureTestFonts() { C.goslint_testing_configure_fonts() }

// SetTestingOSWindows forces the reported OS to Windows (for OS-dependent cases
// like dialog button order). Matches Slint's interpreter test driver.
func SetTestingOSWindows() { C.goslint_testing_set_os_windows() }

// RunEventLoop runs the Slint event loop until quit / last window closed.
// Blocks; must be called on the UI thread.
func RunEventLoop() error {
	MarkUIThread() // the calling thread is the UI thread
	return withLoopRunning(func() error { return rc(C.goslint_run_event_loop(), "run event loop") })
}

// RunEventLoopUntilQuit runs the event loop until QuitEventLoop, without quitting
// when the last window closes. Blocks; UI thread only.
func RunEventLoopUntilQuit() error {
	MarkUIThread() // the calling thread is the UI thread
	return withLoopRunning(func() error { return rc(C.goslint_run_event_loop_until_quit(), "run event loop until quit") })
}

// QuitEventLoop requests the running event loop to quit.
func QuitEventLoop() error { return rc(C.goslint_quit_event_loop(), "quit event loop") }
