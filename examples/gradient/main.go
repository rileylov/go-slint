// Command gradient demonstrates building gradient brushes in Go (slint.Gradient)
// and assigning them to a `brush` property: linear at various angles, radial,
// a multi-stop rainbow, and an animated rotating gradient driven by a timer.
//
//	make lib && go run ./examples/gradient
package main

import (
	_ "embed"
	"fmt"
	"log"
	"runtime"

	"github.com/rileylov/go-slint"
)

func init() { runtime.LockOSThread() } // Slint is thread-affine

//go:embed ui.slint
var ui string

func rgb(r, g, b uint8) slint.Color { return slint.Color{R: r, G: g, B: b, A: 0xff} }

func main() {
	app, err := slint.Compile(ui, slint.WithStyle("fluent"))
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()
	win, err := app.Create("AppWindow")
	if err != nil {
		log.Fatal(err)
	}
	defer win.Close()

	set := func(g slint.Gradient, label string) {
		win.Set("swatch", g)
		win.Set("label", label)
	}

	// two-stop linear gradient at the given angle
	win.OnCallback("linear", func(a []any) any {
		angle := a[0].(float64)
		set(slint.Gradient{Angle: float32(angle), Stops: []slint.GradientStop{
			{Pos: 0, Color: rgb(0xff, 0x6b, 0x6b)},
			{Pos: 1, Color: rgb(0x4d, 0x9d, 0xff)},
		}}, fmt.Sprintf("linear  %.0f°", angle))
		return nil
	})

	win.OnCallback("radial", func([]any) any {
		set(slint.Gradient{Radial: true, Stops: []slint.GradientStop{
			{Pos: 0, Color: rgb(0xff, 0xff, 0xff)},
			{Pos: 1, Color: rgb(0x22, 0x22, 0x55)},
		}}, "radial")
		return nil
	})

	win.OnCallback("rainbow", func([]any) any {
		set(slint.Gradient{Angle: 90, Stops: []slint.GradientStop{
			{Pos: 0.0, Color: rgb(0xff, 0x00, 0x00)},
			{Pos: 0.2, Color: rgb(0xff, 0xa5, 0x00)},
			{Pos: 0.4, Color: rgb(0xff, 0xff, 0x00)},
			{Pos: 0.6, Color: rgb(0x00, 0xc8, 0x00)},
			{Pos: 0.8, Color: rgb(0x00, 0x70, 0xff)},
			{Pos: 1.0, Color: rgb(0x8a, 0x2b, 0xe2)},
		}}, "rainbow (6 stops)")
		return nil
	})

	// Animate: rotate a linear gradient by rebuilding it ~30×/second from Go.
	anim := slint.NewTimer()
	defer anim.Close()
	angle := 0.0
	win.OnCallback("toggle-animate", func([]any) any {
		if anim.Running() {
			anim.Stop()
			win.Set("label", "paused")
			return nil
		}
		anim.Start(slint.TimerRepeated, 33, func() {
			angle += 5
			if angle >= 360 {
				angle -= 360
			}
			set(slint.Gradient{Angle: float32(angle), Stops: []slint.GradientStop{
				{Pos: 0, Color: rgb(0xff, 0x00, 0x88)},
				{Pos: 0.5, Color: rgb(0x88, 0x00, 0xff)},
				{Pos: 1, Color: rgb(0x00, 0xcc, 0xff)},
			}}, fmt.Sprintf("animating  %.0f°", angle))
		})
		return nil
	})

	// start with something on screen
	set(slint.Gradient{Angle: 45, Stops: []slint.GradientStop{
		{Pos: 0, Color: rgb(0xff, 0x6b, 0x6b)},
		{Pos: 1, Color: rgb(0x4d, 0x9d, 0xff)},
	}}, "linear  45°  — pick a brush below")

	if err := win.Run(); err != nil {
		log.Fatal(err)
	}
}
