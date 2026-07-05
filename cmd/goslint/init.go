package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// cmdInit scaffolds a new go-slint project using the TYPED API: ui/app.slint with a
// co-located, pre-generated ui/app.slint.go (so it builds straight away, and
// //go:embed can reach the markup), app.go (shared wiring + a //go:generate
// directive to regenerate), and the desktop entry point (main.go). The Android
// entry point is created on demand by `goslint android build`, so a fresh project
// stays desktop-only — no //go:build android cgo file to make editors (gopls) spin
// up a broken cross-build view.
func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	module := fs.String("module", "", "Go module path (default: directory name)")
	_ = fs.Parse(args)

	dir := fs.Arg(0)
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	name := filepath.Base(abs)
	if name == "." || name == string(filepath.Separator) {
		name = "goslint-app"
	}
	if *module == "" {
		*module = name
	}

	if err := os.MkdirAll(filepath.Join(dir, "ui"), 0o755); err != nil {
		return err
	}
	if exists(filepath.Join(dir, "app.go")) {
		return fmt.Errorf("%s already contains app.go — refusing to overwrite", dir)
	}

	if !exists(filepath.Join(dir, "go.mod")) {
		if err := runIn(dir, "go", "mod", "init", *module); err != nil {
			return err
		}
	}
	if err := runIn(dir, "go", "mod", "edit", "-require="+modulePath+"@"+version()); err != nil {
		return err
	}

	files := map[string]string{
		"ui/app.slint":    appSlintTemplate,
		"ui/app.slint.go": uiTemplate,
		"app.go":          fmt.Sprintf(appGoTemplate, *module),
		"main.go":         mainTemplate,
		// The Android entry (android_main.go) is intentionally omitted — `goslint
		// android build` writes it on demand. See cmdInit's doc and ensureAndroidEntry.
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
			return err
		}
	}

	// best-effort: pull the dependency (needs the module to be reachable)
	if err := runIn(dir, "go", "mod", "tidy"); err != nil {
		fmt.Printf("\nnote: `go mod tidy` failed (is %s published/reachable yet?). Run it once it is.\n", modulePath)
	}

	fmt.Printf("\n✓ scaffolded %q (typed API) in %s\n\nNext:\n", name, dir)
	if dir != "." {
		fmt.Printf("  cd %s\n", dir)
	}
	fmt.Println("  goslint dev .            # run with auto-reload (fetches the native lib on first run)")
	fmt.Println("  goslint android build .  # build a signed APK")
	fmt.Println("\nJust edit ui/app.slint — goslint dev/run/build regenerate ui/app.slint.go for you.")
	return nil
}

func runIn(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

const appSlintTemplate = `import { Button, VerticalBox } from "std-widgets.slint";

export component AppWindow inherits Window {
    in-out property <int> count: 0;
    callback increment();

    title: "go-slint app";
    preferred-width: 320px;
    preferred-height: 200px;

    VerticalBox {
        alignment: center;
        spacing: 12px;
        Text {
            text: "Count: " + root.count;
            font-size: 28px;
            horizontal-alignment: center;
        }
        Button {
            text: "Increment";
            clicked => { root.increment(); }
        }
    }
}
`

// appGoTemplate holds the shared app logic (used by both desktop and Android). %s is
// the module path (for the ui import). Write your app once: New, bind, Run. Under
// ` + "`goslint dev`" + ` the same code live-reloads app.slint in-process; ` + "`goslint build`" + `
// produces a normal binary — no separate dev/prod code paths.
// goGenerateDirective is kept separate so the literal "//go:generate" line doesn't
// sit at column 0 in this file (which would make `go generate ./...` over the
// go-slint repo try to run it here, where there's no app.slint).
const goGenerateDirective = "//go:generate goslint generate -o ui/app.slint.go -package ui ui/app.slint"

const appGoTemplate = `package main

` + goGenerateDirective + `

import (
	"%s/ui"
)

func run() error {
	win, err := ui.NewAppWindow()
	if err != nil {
		return err
	}
	defer win.Close()

	if err := win.OnIncrement(func() {
		n, _ := win.Count()
		_ = win.SetCount(n + 1)
	}); err != nil {
		return err
	}

	return win.Run()
}
`

const mainTemplate = `//go:build !goslint_android

package main

import "runtime"

func init() { runtime.LockOSThread() } // Slint is thread-affine

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}
`

// androidTemplate is the Android entry point, written by ensureAndroidEntry on the
// first `goslint android build`. It calls run() from app.go.
//
// It's gated on a CUSTOM tag (goslint_android), not the android GOOS, and named
// android_main.go (not *_android.go) on purpose: a file constrained to the android
// GOOS — by an `//go:build android` line OR an `_android.go` filename — makes editors
// (gopls) spin up an android cross-build view that fails without the NDK, surfacing a
// confusing "android/386 requires external (cgo) linking" error. A custom tag is
// ignored by that machinery, so the file is simply excluded from the desktop build
// (no error). `goslint android build` passes -tags=goslint_android with GOOS=android.
const androidTemplate = `//go:build goslint_android

package main

/*
#cgo LDFLAGS: -llog
#include <android/log.h>
#include <stdlib.h>
static void goslint_report(const char *msg) {
	__android_log_write(ANDROID_LOG_ERROR, "goslintapp", msg);
}
*/
import "C"

import (
	"runtime"
	"unsafe"
)

//export goslint_android_main
func goslint_android_main(_ *C.char) {
	runtime.LockOSThread()
	if err := run(); err != nil {
		// Logcat is the only place an Android app can report failure:
		//   adb logcat -s goslintapp goslint
		msg := C.CString("run: " + err.Error())
		C.goslint_report(msg)
		C.free(unsafe.Pointer(msg))
	}
}

func main() {} // required for c-shared; unused (entry is goslint_android_main)
`

// uiTemplate is the typed wrapper for the scaffold's ui/app.slint, pre-generated by
// goslint-gen so the project builds before the user runs `go generate`. It's kept as
// an embedded file (not a string literal) to avoid escaping the generated code.
// Regenerate it with `make scaffold-template`, which generates it co-located with the
// markup (ui/app.slint + ui/app.slint.go) so the //go:embed directive resolves.
//
//go:embed templates/app.slint.go.tmpl
var uiTemplate string
