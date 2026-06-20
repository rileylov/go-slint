// Command clock is an interactive go-slint example: a repeating Timer driven
// from Go updates the UI once per second.
//
//	make lib && go run ./cmd/examples/clock
package main

import (
	_ "embed"
	"fmt"
	"log"
	"runtime"

	"github.com/rileylov/go-slint"
)

// Slint is thread-affine: pin the main goroutine.
func init() { runtime.LockOSThread() }

//go:embed ui.slint
var ui string

func main() {
	app, err := slint.Compile(ui, slint.WithStyle("fluent"))
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	win, err := app.Create("Clock")
	if err != nil {
		log.Fatal(err)
	}
	defer win.Close()

	timer := slint.NewTimer()
	defer timer.Free()
	timer.Start(slint.TimerRepeated, 1000, func() {
		n, _ := win.Int("ticks")
		fmt.Println(n + 1)
		win.Set("ticks", n+1)
	})

	if err := win.Run(); err != nil {
		log.Fatal(err)
	}
}
