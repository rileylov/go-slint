package slintsys

/*
#include "goslint.h"
*/
import "C"

// InitHeadless installs the headless testing backend (mock time, no windows).
// Call once per process, before creating any component, on the UI thread.
func InitHeadless() error { return rc(C.goslint_testing_init_headless(), "init headless") }

// MockElapsedTime advances the testing backend's mock clock.
func MockElapsedTime(ms uint64) { C.goslint_testing_mock_elapsed_time(C.uint64_t(ms)) }

// ConfigureTestFonts installs deterministic embedded fonts (call after
// InitHeadless). Matches Slint's interpreter test driver.
func ConfigureTestFonts() { C.goslint_testing_configure_fonts() }

// RunEventLoop runs the Slint event loop until quit / last window closed.
// Blocks; must be called on the UI thread.
func RunEventLoop() error { return rc(C.goslint_run_event_loop(), "run event loop") }

// QuitEventLoop requests the running event loop to quit.
func QuitEventLoop() error { return rc(C.goslint_quit_event_loop(), "quit event loop") }
