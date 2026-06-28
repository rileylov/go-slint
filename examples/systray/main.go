// Command systray demonstrates Slint 1.17's SystemTrayIcon: the app puts an icon in
// the system tray with a menu (Show/Hide, Quit). Uses the dynamic API because it has
// two top-level components (a Window and the tray); the tray icon is generated in Go.
//
//	make lib && go run ./examples/systray
//
// SystemTrayIcon is cross-platform (macOS / Windows / Linux), but the icon only shows
// where the desktop has a tray/status area. Notably, modern GNOME does NOT display
// tray icons out of the box — it dropped legacy StatusNotifier support, so you need
// the "AppIndicator and KStatusNotifierItem Support" extension to see it. It shows
// natively on macOS, Windows, and KDE Plasma. (The icon + menu code runs regardless;
// only the visibility depends on the desktop environment.)
package main

import (
	_ "embed"
	"log"
	"runtime"

	"github.com/rileylov/go-slint"
)

func init() { runtime.LockOSThread() } // Slint is thread-affine

//go:embed app.slint
var src string

func main() {
	app, err := slint.Compile(src, slint.WithStyle("fluent"))
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	win, err := app.Create("MainWindow")
	if err != nil {
		log.Fatal(err)
	}
	defer win.Close()

	tray, err := app.Create("Tray")
	if err != nil {
		log.Fatal(err)
	}
	defer tray.Close()

	// The tray icon is embedded in the markup (icon: @image-url("data:...")), so it's
	// present when the component inits — that's when the tray registers with the OS.
	_ = tray.Set("tray-tooltip", "go-slint system tray demo")

	visible := true
	show := func(v bool) {
		visible = v
		if v {
			_ = win.Show()
		} else {
			_ = win.Hide()
		}
	}

	win.OnCallback("hide-to-tray", func([]any) any { show(false); return nil })
	tray.OnCallback("show-hide", func([]any) any { show(!visible); return nil })
	tray.OnCallback("quit", func([]any) any { _ = slint.Quit(); return nil })

	if err := win.Show(); err != nil {
		log.Fatal(err)
	}
	// RunUntilQuit (not Run): the app keeps running while the window is hidden in the tray.
	if err := slint.RunUntilQuit(); err != nil {
		log.Fatal(err)
	}
}
