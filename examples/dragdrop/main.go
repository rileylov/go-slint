// Command dragdrop demonstrates Slint 1.17 drag & drop (DragArea/DropArea) with a real
// payload bridged to Go. Slint's `data-transfer` is opaque; go-slint carries its
// plain-text content, so make-data encodes the dragged tile's index and dropped reads
// it back to reorder. Uses the dynamic API (the new DropEvent/data-transfer types are
// simplest there).
//
//	make lib && go run ./examples/dragdrop   # then drag a tile onto another
package main

import (
	_ "embed"
	"log"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/rileylov/go-slint"
)

func init() { runtime.LockOSThread() } // Slint is thread-affine

//go:embed app.slint
var src string

// appSlintPath returns this example's app.slint on disk, so @image-url("card-drag.svg")
// resolves to the SVG sitting beside it. The drag-image preview is loaded by Slint
// relative to the source file's dir, so we compile with CompileSource (not Compile),
// which anchors that resolution — exactly how an on-disk .slint app behaves.
func appSlintPath() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "app.slint")
}

func main() {
	app, err := slint.CompileSource(appSlintPath(), src, slint.WithStyle("fluent"))
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()
	win, err := app.Create("DndDemo")
	if err != nil {
		log.Fatal(err)
	}
	defer win.Close()

	items := []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo"}
	render := func() {
		rows := make([]any, len(items))
		for i, v := range items {
			rows[i] = v
		}
		_ = win.Set("items", rows)
	}

	// make-data: the drag payload carries the source index as text.
	win.OnGlobalCallback("Api", "make-data", func(a []any) any {
		return slint.NewDataTransfer(strconv.Itoa(int(a[0].(float64))))
	})
	// dropped: read the source index out of event.data and reorder onto the target.
	win.OnGlobalCallback("Api", "dropped", func(a []any) any {
		ev, _ := a[0].(map[string]any)
		dt, _ := ev["data"].(slint.DataTransfer)
		from, _ := strconv.Atoi(dt.Text)
		to := int(a[1].(float64))
		if from < 0 || from >= len(items) || to < 0 || to >= len(items) || from == to {
			return nil
		}
		v := items[from]
		items = append(items[:from], items[from+1:]...)
		if to > from {
			to--
		}
		items = append(items[:to], append([]string{v}, items[to:]...)...)
		render()
		log.Printf("moved %q -> %v", v, items)
		return nil
	})

	render()
	if err := win.Show(); err != nil {
		log.Fatal(err)
	}
	if err := slint.Run(); err != nil {
		log.Fatal(err)
	}
}
