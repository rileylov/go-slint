// Command multifile is a TYPED example whose .slint imports another .slint
// (components/card.slint). `goslint generate` embeds every imported file, so the
// built binary compiles the whole UI from memory — no .slint tree on disk.
//
//	make lib
//	go generate ./cmd/examples/multifile   # regenerates ui/app.slint.go
//	go run ./cmd/examples/multifile
package main

//go:generate goslint generate -o ui/app.slint.go -package ui ui/app.slint

import (
	"runtime"

	"github.com/rileylov/go-slint/cmd/examples/multifile/ui"
)

func init() { runtime.LockOSThread() } // Slint is thread-affine

func main() {
	win, err := ui.NewAppWindow()
	if err != nil {
		panic(err)
	}
	defer win.Close()

	if err := win.OnBump(func() {
		n, _ := win.Count()
		_ = win.SetCount(n + 1)
	}); err != nil {
		panic(err)
	}

	if err := win.Run(); err != nil {
		panic(err)
	}
}
