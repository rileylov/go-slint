//go:build goslint_android

// Android entry for the interop demo (gated on the custom goslint_android tag, not
// the android GOOS, so editors don't cross-build it and flag a spurious cgo error).
// The Rust android_main (android.rs) installs
// Slint's platform and calls goslint_android_main with the app's writable data dir
// (unused here — the phone UI is a single embedded file, no extraction needed).
// It runs the SAME interop logic as the desktop build (interop.go), just with the
// phone-stacked markup and the material style.
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
	"runtime"
	"unsafe"
)

//go:embed ui_android.slint
var ui string

func logf(msg string) {
	c := C.CString(msg)
	C.goslint_log(c)
	C.free(unsafe.Pointer(c))
}

//export goslint_android_main
func goslint_android_main(_ *C.char) {
	runtime.LockOSThread()
	logf("interop: starting")
	if err := runApp(ui, "material"); err != nil {
		logf("interop FAILED: " + err.Error())
	}
}

func main() {} // required for c-shared; unused (entry is goslint_android_main)
