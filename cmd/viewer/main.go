// Command viewer loads and runs any .slint file through the Go bindings, like
// Slint's own slint-viewer. Widgets are interactive on their own, so widget
// showcases work without their Rust logic.
//
//	make lib && go run ./cmd/viewer path/to/file.slint [Component] [style]
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/rileylov/go-slint"
)

func init() { runtime.LockOSThread() }

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("usage: %s <file.slint> [Component] [style]", filepath.Base(os.Args[0]))
	}
	path := os.Args[1]
	style := "fluent"
	if len(os.Args) > 3 && os.Args[3] != "" {
		style = os.Args[3]
	}

	app, err := slint.CompileFile(path,
		slint.WithStyle(style),
		slint.WithIncludePaths(filepath.Dir(path)))
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	names := app.ComponentNames()
	if len(names) == 0 {
		log.Fatal("no components exported by " + path)
	}
	comp := names[len(names)-1] // last exported, like slint-viewer
	if len(os.Args) > 2 && os.Args[2] != "" {
		comp = os.Args[2]
	}
	fmt.Printf("components: %v\nrunning: %s (style %s)\n", names, comp, style)

	win, err := app.Create(comp)
	if err != nil {
		log.Fatalf("create %q: %v", comp, err)
	}
	defer win.Close()
	if err := win.Run(); err != nil {
		log.Fatal(err)
	}
}
