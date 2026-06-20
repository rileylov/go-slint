//go:build android

// Android entry for the throughput harness. Always runs the auto-ramp benchmark
// (we can't click buttons during a headless capture), routes log output to logcat
// (tag "goslintapp"), and asks Slint for an on-screen FPS overlay so render FPS can
// be read from a screenshot. Setting SLINT_DEBUG_PERFORMANCE via os.Setenv works
// because cgo keeps the C environ in sync, and Slint (Rust) reads it at renderer
// init — which happens later, inside runApp.
package main

/*
#include <android/log.h>
#include <stdlib.h>
static void goslint_log(const char *msg) {
    __android_log_write(ANDROID_LOG_INFO, "goslintapp", msg);
}
*/
import "C"

import (
	_ "embed"
	"log"
	"os"
	"runtime"
	"strings"
	"time"
	"unsafe"
)

//go:embed ui.slint
var ui string

func logf(msg string) {
	c := C.CString(msg)
	C.goslint_log(c)
	C.free(unsafe.Pointer(c))
}

// androidLog routes the standard logger (used by stats/benchRamp) to logcat.
type androidLog struct{}

func (androidLog) Write(p []byte) (int, error) {
	logf(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

//export goslint_android_main
func goslint_android_main(_ *C.char) {
	runtime.LockOSThread()
	log.SetOutput(androidLog{})
	log.SetFlags(0)

	// render continuously + draw the FPS overlay so it's visible in screenshots
	os.Setenv("SLINT_DEBUG_PERFORMANCE", "refresh_full_speed,overlay")

	logf("chartstress: starting bench")
	if err := runApp(ui, "material", true, 5*time.Second); err != nil {
		logf("chartstress FAILED: " + err.Error())
	}
}

func main() {} // required for c-shared; unused (entry is goslint_android_main)
