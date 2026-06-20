//go:build !android

// Command chartstress is a throughput harness for the Go ⇄ Slint boundary and
// Slint's renderer. A goroutine generates a multi-series sine waveform, packs it
// into a single SVG path string, and pushes it into the UI as fast as the event
// loop can apply it (depth-1 backpressure keeps the queue bounded).
//
// Two load knobs — points-per-frame and series — scale the work:
//
//   - "applied/s" is how many full-frame property updates the binding + event loop
//     sustain per second: the BINDING throughput ceiling.
//   - Slint's own render FPS is separate; surface it with
//       SLINT_DEBUG_PERFORMANCE=refresh_full_speed,overlay go run ./cmd/examples/chartstress
//
//	go run ./cmd/examples/chartstress                 # interactive (buttons)
//	GOSLINT_BENCH=1 go run ./cmd/examples/chartstress # auto-ramp the load, log throughput
//
// The same logic runs on Android via app_android.go (shares chartstress.go).
package main

import (
	_ "embed"
	"os"
	"runtime"
	"time"
)

func init() { runtime.LockOSThread() }

//go:embed ui.slint
var ui string

func main() {
	bench := os.Getenv("GOSLINT_BENCH") != ""
	if err := runApp(ui, "fluent", bench, 3*time.Second); err != nil {
		panic(err)
	}
}
