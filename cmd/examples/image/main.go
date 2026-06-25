// Command image generates an image in Go and displays it in the UI via
// slint.NewImage (the SharedPixelBuffer path) — no image file on disk. Any
// image.Image works: decoded PNG/JPEG, a plot, or procedural content like here.
//
//	make lib
//	go generate ./cmd/examples/image
//	go run ./cmd/examples/image
package main

//go:generate goslint generate -o ui/app.slint.go -package ui ui/app.slint

import (
	"image"
	"image/color"
	"runtime"

	slint "github.com/rileylov/go-slint"
	"github.com/rileylov/go-slint/cmd/examples/image/ui"
)

func init() { runtime.LockOSThread() } // Slint is thread-affine

func main() {
	win, err := ui.NewAppWindow()
	if err != nil {
		panic(err)
	}
	defer win.Close()

	// draw a procedural gradient with the Go standard library
	const w, h = 240, 240
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 160, 255})
		}
	}

	frame, err := slint.NewImage(img)
	if err != nil {
		panic(err)
	}
	defer frame.Free()
	if err := win.SetFrame(frame); err != nil {
		panic(err)
	}

	if err := win.Run(); err != nil {
		panic(err)
	}
}
