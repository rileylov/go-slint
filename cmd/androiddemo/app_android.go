//go:build android

// Command androiddemo is the Go side of the Android app. The Rust android_main
// entry (rust/goslint-sys/src/android.rs) installs Slint's Android platform,
// sets TMPDIR to the app's writable data dir, then calls goslint_android_main.
//
// It runs Slint's multi-file "energy-monitor" demo: the .slint files and images
// are embedded with go:embed, extracted to a temp dir at startup (the interpreter
// resolves imports/@image-url from the filesystem), then compiled. The demo is
// adaptive — its MainWindow switches to a phone layout below a 444px width
// breakpoint, so it fills the screen properly on a phone.
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
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/rileylov/go-slint"
)

// The demo tree, mirroring its on-disk repo layout so relative imports and
// @image-url paths (e.g. "../../../logo/...") resolve once extracted.
//
//go:embed all:appassets
var assets embed.FS

const (
	embedRoot  = "appassets"
	entry      = "demos/energy-monitor/ui/desktop_window.slint"
	includeDir = "demos/energy-monitor/ui"
)

func logf(msg string) {
	c := C.CString(msg)
	C.goslint_log(c)
	C.free(unsafe.Pointer(c))
}

//export goslint_android_main
func goslint_android_main(dir *C.char) {
	// We're on the android-activity thread with the platform already set.
	runtime.LockOSThread()

	dataDir := C.GoString(dir)
	logf("goslint_android_main start; dataDir=" + dataDir)

	root, err := extractAssets(dataDir)
	if err != nil {
		logf("extractAssets FAILED: " + err.Error())
		return
	}
	logf("extracted to " + root)

	app, err := slint.CompileFile(filepath.Join(root, entry),
		slint.WithStyle("material"), // std-widgets needs a style; material = Android-native
		slint.WithIncludePaths(filepath.Join(root, includeDir)))
	if err != nil {
		logf("CompileFile FAILED: " + err.Error())
		return
	}
	defer app.Close()

	names := app.ComponentNames()
	logf("compiled; components=" + filepath.Join(names...))
	if len(names) == 0 {
		logf("no components")
		return
	}
	win, err := app.Create(names[len(names)-1])
	if err != nil {
		logf("Create FAILED: " + err.Error())
		return
	}
	defer win.Close()

	logf("running " + names[len(names)-1])
	_ = win.Run() // drives the Android event loop until the activity ends
}

// extractAssets writes the embedded tree under dataDir (the app's writable data
// path, passed in from the Rust entry) and returns the extraction root.
func extractAssets(dataDir string) (string, error) {
	root, err := os.MkdirTemp(dataDir, "goslint-gallery")
	if err != nil {
		return "", err
	}
	err = fs.WalkDir(assets, embedRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(embedRoot, p)
		dst := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := assets.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		return "", err
	}
	return root, nil
}

func main() {} // required for c-shared; unused (entry is goslint_android_main)
