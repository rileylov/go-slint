// Command hello shows a minimal Slint window from Go.
//
// Run on a machine with a display:
//
//	make lib && go run ./cmd/examples/hello
package main

import (
	"log"
	"runtime"

	"github.com/rileylov/go-slint"
)

// Slint is thread-affine: pin the main goroutine to one OS thread.
func init() { runtime.LockOSThread() }

const ui = `
export component App inherits Window {
    title: "go-slint";
    preferred-width: 320px;
    preferred-height: 200px;
    VerticalLayout {
        alignment: center;
        Text {
            text: "Hello from Go + Slint";
            horizontal-alignment: center;
            font-size: 20px;
        }
    }
}`

func main() {
	app, err := slint.Compile(ui)
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	win, err := app.Create("App")
	if err != nil {
		log.Fatal(err)
	}
	defer win.Close()

	if err := win.Run(); err != nil {
		log.Fatal(err)
	}
}
