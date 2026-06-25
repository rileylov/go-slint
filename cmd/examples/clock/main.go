// Command clock is an interactive go-slint example using the TYPED API: a
// repeating Timer driven from Go updates the typed `ticks` property once a
// second. (The Timer is part of the dynamic runtime — typed components and the
// runtime API compose freely.)
//
//	make lib
//	go run ./cmd/examples/clock
package main

//go:generate goslint generate -o ui/app.slint.go -package ui ui/app.slint

import (
	"fmt"
	"log"
	"runtime"

	"github.com/rileylov/go-slint"
	"github.com/rileylov/go-slint/cmd/examples/clock/ui"
)

// Slint is thread-affine: pin the main goroutine.
func init() { runtime.LockOSThread() }

func main() {
	win, err := ui.NewClock()
	if err != nil {
		log.Fatal(err)
	}
	defer win.Close()

	timer := slint.NewTimer()
	defer timer.Free()
	timer.Start(slint.TimerRepeated, 1000, func() {
		n, _ := win.Ticks()
		fmt.Println(n + 1)
		_ = win.SetTicks(n + 1)
	})

	if err := win.Run(); err != nil {
		log.Fatal(err)
	}
}
