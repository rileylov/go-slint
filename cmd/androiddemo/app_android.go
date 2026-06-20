//go:build android

// Command androiddemo is the Go side of the Android app. It is built as a
// c-archive and linked into the Rust shim's cdylib; the Rust android_main entry
// (src/android.rs) installs Slint's Android platform and then calls
// goslint_android_main below, which runs the UI like a desktop main() would.
package main

import "C"

import (
	_ "embed"
	"runtime"

	"github.com/rileylov/go-slint"
)

//go:embed ui.slint
var ui string

//export goslint_android_main
func goslint_android_main() {
	// We're on the android-activity thread with the platform already set.
	runtime.LockOSThread()

	app, err := slint.Compile(ui)
	if err != nil {
		return
	}
	defer app.Close()

	win, err := app.Create("App")
	if err != nil {
		return
	}
	defer win.Close()

	_ = win.Run() // drives the Android event loop until the activity ends
}

func main() {} // required for c-archive; unused (entry is goslint_android_main)
