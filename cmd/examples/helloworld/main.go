package main

import (
	"log"
	"runtime"

	"github.com/rileylov/go-slint/cmd/examples/helloworld/ui"
)

func init() { runtime.LockOSThread() }

func main() {
	win, err := ui.NewHelloWorld()
	if err != nil {
		log.Fatal(err)
	}
	defer win.Close()
	win.OnGreet(func() {
		win.SetGreeting("Hello")
	})
	if err := win.Run(); err != nil {
		log.Fatal(err)
	}
}
