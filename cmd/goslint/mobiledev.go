package main

import (
	"fmt"
	"os"
	"os/signal"
	"time"
)

// mobileDev drives an edit → rebuild → reinstall → relaunch loop for a sandboxed
// target (an iOS simulator or Android emulator). Desktop `goslint dev` hot-reloads
// .slint edits *in-process* — the running binary re-reads the markup from disk — but
// a sandboxed app can't reach the host's project files, so mobile dev rebuilds and
// reinstalls instead. The watch loop, debounce, and Ctrl-C handling live here; the
// platform supplies build/install/launch/stop hooks, so iOS and Android `dev` share
// this driver and differ only in those four steps.
type mobileDev struct {
	pkg      string        // package dir (or file) to watch for .slint/.go edits
	rebuild  func() error  // cross-build + package the installable artifact
	install  func() error  // push it to the device/emulator
	launch   func() error  // (re)launch the app
	stop     func()        // best-effort terminate the running app before reinstall
	startLog func() func() // optional: begin streaming device logs; returns a stopper
}

func (m mobileDev) run() error {
	// A cycle builds first, so a failed rebuild leaves the currently-running app up
	// (only swap the app once the new build succeeds).
	cycle := func(reason string) error {
		if reason != "" {
			fmt.Println(reason)
		}
		if err := m.rebuild(); err != nil {
			return err
		}
		m.stop()
		if err := m.install(); err != nil {
			return err
		}
		return m.launch()
	}

	if err := cycle(""); err != nil {
		return err
	}
	var stopLog func()
	if m.startLog != nil {
		stopLog = m.startLog()
	}
	defer func() {
		if stopLog != nil {
			stopLog()
		}
		m.stop()
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	fmt.Println(">> watching — edit .slint or .go to rebuild + reload; Ctrl-C to stop")

	last := latestSourceMod(m.pkg)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-sig:
			fmt.Println("\n>> stopping")
			return nil
		case <-ticker.C:
			if now := latestSourceMod(m.pkg); now.After(last) {
				time.Sleep(250 * time.Millisecond) // debounce a burst of saves
				last = latestSourceMod(m.pkg)
				if err := cycle(">> change — rebuilding"); err != nil {
					fmt.Fprintln(os.Stderr, "reload failed:", err)
				}
			}
		}
	}
}

// latestSourceMod is the newest mtime among .slint and .go files under pkg's dir.
func latestSourceMod(pkg string) time.Time {
	s, g := newestExt(pkg, ".slint"), newestExt(pkg, ".go")
	if s.After(g) {
		return s
	}
	return g
}
