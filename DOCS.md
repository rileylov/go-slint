# go-slint — Guide

How to build a UI with go-slint, step by step.

You write your UI in the **`.slint`** language, run **`goslint generate`** to turn
it into a typed Go package, and drive it with normal Go methods. Property and
callback names become compile-checked methods; structs and enums become Go types.
Under the hood a small Rust shim runs Slint's interpreter, so `.slint` is compiled
at run time — but `goslint generate` embeds the markup, so you still ship one
self-contained binary.

> New to the `.slint` language itself? See the
> [Slint Language Documentation](https://slint.dev/docs/slint).

---

## Quick start

**Prerequisites:** Go and a **C compiler** for cgo — gcc/clang on Linux, the Xcode
command-line tools on macOS, **MinGW-w64 gcc on Windows** (the prebuilt lib uses the
GNU toolchain, so MSVC won't link). You do *not* need Rust or pkg-config. `goslint
doctor` checks all of this.

**1. Scaffold a project** (creates `go.mod`, `app.slint`, `main.go`, and the
generated `ui/` package):

```sh
go install github.com/rileylov/go-slint/cmd/goslint@latest
goslint init myapp && cd myapp
goslint setup          # download the prebuilt native lib for your platform
```

**2. Write `app.slint`:**

```slint
import { Button, VerticalBox } from "std-widgets.slint";

export component AppWindow inherits Window {
    in-out property <int> counter: 0;
    callback increment();

    VerticalBox {
        Text { text: "Counter: " + root.counter; }
        Button { text: "+1"; clicked => { root.increment(); } }
    }
}
```

**3. Generate the typed API and write `main.go`:**

```sh
goslint generate -o ui/app.slint.go -package ui app.slint
```

```go
package main

import (
	"runtime"

	"myapp/ui"
)

func init() { runtime.LockOSThread() } // Slint is thread-affine

func main() {
	win, err := ui.NewAppWindow()
	if err != nil {
		panic(err)
	}
	defer win.Close()

	win.OnIncrement(func() {
		n, _ := win.Counter()
		_ = win.SetCounter(n + 1)
	})

	win.Run()
}
```

**4. Run it:** `goslint dev .` (or `goslint run .`).

The scaffold puts a `//go:generate goslint generate …` directive atop `main.go`, and
`goslint dev`/`run`/`build` run it for you — so you can just edit `app.slint` and
see your changes. (Manual `goslint generate` is only needed if you build outside
those commands.)

---

## API overview

For a component named `AppWindow`, the generated package gives you:

### Instantiating a component

`New<Component>` compiles the markup once and creates an instance.

```go
win, err := ui.NewAppWindow()
defer win.Close()

win.Show()   // show without blocking
win.Run()    // show and run the event loop (blocks until the window closes)
win.Hide()
```

Each exported component that inherits `Window` gets its own `New…` constructor.

### Properties

`in`, `out`, and `in-out` properties become a typed getter and (unless `out`) a
setter. Getters return `(value, error)`; setters return `error`.

```slint
in-out property <string> name;
```

```go
name, err := win.Name()
err = win.SetName("Gophers")
```

### Callbacks

A `callback` becomes an `On<Name>` method taking a typed handler. Arguments are
positional (`a0`, `a1`, …); a return type maps to the handler's return.

```slint
callback increment();
callback validate(string) -> bool;
```

```go
win.OnIncrement(func() { /* … */ })
win.OnValidate(func(a0 string) bool { return a0 != "" })
```

### Functions

A `function` (or `pure function`) becomes a method you **call** (not `On…`). It
returns `(result, error)`, or just `error` when it returns nothing.

```slint
pure function area(a: int, b: int) -> int { return a * b; }
```

```go
a, err := win.Area(3, 4) // a == 12
```

### Globals

A `global` becomes an accessor returning a typed handle with the same
property/callback/function methods.

```slint
export global Logic {
    in-out property <int> score;
    pure callback greeting(string) -> string;
}
```

```go
win.Logic().SetScore(10)
win.Logic().OnGreeting(func(a0 string) string { return "Hi " + a0 })
```

### Type mappings

| `.slint` type | Go type |
| --- | --- |
| `int` | `int` |
| `float`, `length`, `physical-length`, `duration`, `angle` | `float64` |
| `string` | `string` |
| `bool` | `bool` |
| `color` | `slint.Color` |
| `image` | `*slint.Image` |
| `[T]` (array) | `[]T` |
| struct | a generated struct type (e.g. `ui.Point`) |
| enum | a generated string type (e.g. `ui.Mode`) |
| `brush` | `any` — a `slint.Color` or `slint.Gradient` (see [Dynamic API](#dynamic-runtime-api)) |

### Images

An `image` property is a `*slint.Image`. Load one from a file, or build one from
pixels you generated/decoded in Go (Slint's `SharedPixelBuffer` equivalent):

```go
img, _ := slint.LoadImage("logo.png")          // from a file
img, _ := slint.NewImage(goImage)              // any image.Image: decoded, drawn, generated
img, _ := slint.NewImageRGBA(pixels, w, h)     // raw RGBA8 bytes (w*h*4)
win.SetIcon(img)
defer img.Free()
```

`NewImage` accepts any `image.Image`, so the whole Go ecosystem works — decode with
`image/png`/`image/jpeg`, draw with `image/draw` or a plotting library, or generate
procedurally. (It handles Go's premultiplied-alpha `image.RGBA` correctly.) See
[`cmd/examples/image`](cmd/examples/image).

### Custom fonts

The simplest way is to **import the font in your `.slint`** — the interpreter
registers it for you, and you reference it by family name:

```slint
import "fonts/Inter.ttf";
export component AppWindow inherits Window {
    Text { text: "Hi"; font-family: "Inter"; }
}
```

To register a font from Go at runtime (e.g. a `go:embed`'d or user-supplied font),
call it on the window before the text is shown — it applies to all windows:

```go
win.RegisterFontFromPath("fonts/Inter.ttf")
win.RegisterFontFromMemory(embeddedTTF) // []byte; kept for the process
```

### Arrays

Array properties map to Go slices. The setter takes a whole slice (a snapshot);
the getter returns a snapshot.

```slint
in-out property <[string]> items;
```

```go
win.SetItems([]string{"a", "b", "c"})
items, _ := win.Items()
```

For lists that update row-by-row at runtime (insert/remove/edit while shown), use a
live model via the [dynamic API](#dynamic-runtime-api).

### Structs

An exported `struct` becomes a Go struct with exported fields. Build it with a
struct literal and pass it to a setter.

```slint
export struct Point { x: int, y: int }
export component AppWindow inherits Window {
    in-out property <Point> origin;
}
```

```go
win.SetOrigin(ui.Point{X: 1, Y: 2})
p, _ := win.Origin() // p.X, p.Y
```

### Enums

An exported `enum` becomes a string-typed constant set.

```slint
export enum Mode { idle, active }
export component AppWindow inherits Window {
    in-out property <Mode> mode;
}
```

```go
win.SetMode(ui.ModeActive)
```

---

## Multi-file projects

A `.slint` may import other `.slint` files:

```slint
import { Card } from "components/card.slint";
```

`goslint generate` walks the import graph and **embeds every imported file**, so the
built binary is still self-contained — no `.slint` tree is needed at run time.
(`@library` imports are the exception: they still resolve via library paths.) See
[`cmd/examples/multifile`](cmd/examples/multifile).

---

## Live development

`goslint dev .` runs your app and watches the project:

- editing a **`.slint`** re-runs `go generate` (refreshing the typed wrapper from
  the markup), then rebuilds and restarts — so **both** cosmetic changes and
  *interface* changes (new/renamed properties and callbacks) show up automatically;
- editing a **`.go`** rebuilds and restarts.

`goslint build` and `goslint run` regenerate first too, so a produced binary always
reflects the current `.slint`. You rarely need to run `goslint generate` by hand —
do it only when generating outside these commands (e.g. for editor completion).

---

## Dynamic (runtime) API

The typed API is generated on top of a dynamic runtime in the `slint` package. Use
it directly when you need things codegen can't give you: compiling `.slint` chosen
or edited **at run time**, **live, mutating models**, or gradient **brushes**.

```go
app, _ := slint.Compile(markup)        // or CompileFile / CompileSource
win, _ := app.Create("AppWindow")
defer win.Close()

win.Set("counter", 42)                  // values are mapped automatically
n, _ := win.Int("counter")              // typed read helpers: Int/Float/Bool/Str
win.OnCallback("increment", func(args []any) any { return nil })
win.Run()
```

Value mapping: numbers ↔ `float64`, `string`, `bool`, struct ↔ `map[string]any`,
enum ↔ `slint.Enum`, color ↔ `slint.Color`, image ↔ `*slint.Image`, array ↔ `[]any`
(read) / `slint.SliceModel` (live, write).

**Live models.** Back a list with a `slint.SliceModel` (or implement `slint.Model`)
and mutate it while the UI is shown:

```go
m := slint.NewSliceModel("a", "b")
win.Set("items", m)
m.Append("c")        // the view updates
```

**Brushes.** A `brush` property accepts a `slint.Color` or a `slint.Gradient`:

```go
win.Set("bg", slint.Gradient{Angle: 90, Stops: []slint.GradientStop{
	{Pos: 0, Color: slint.Color{R: 0, G: 0, B: 0, A: 255}},
	{Pos: 1, Color: slint.Color{R: 0, G: 120, B: 255, A: 255}},
}})
```

Globals and functions have `…Global` variants: `GetGlobal`/`SetGlobal`,
`OnGlobalCallback`, `InvokeGlobal`.

---

## Window, timers, and threading

```go
win.SetWindowSize(800, 600)
win.SetFullscreen(true)            // also SetMaximized / SetMinimized
win.RequestRedraw()
```

**Closing.** Intercept the window's close (the X button) to confirm or save first —
return `true` to let it close, `false` to keep it open. `RequestClose()` triggers
the same path (e.g. from a Quit button):

```go
win.OnCloseRequested(func() bool {
	if hasUnsavedChanges() {
		showSavePrompt()
		return false // keep open
	}
	return true     // allow close
})
```

**Clipboard.** Read and write the system clipboard (package-level; needs a backend,
so use it once a window exists):

```go
slint.SetClipboardText("copied!")
text := slint.ClipboardText()
```

**Translations.** Mark strings with `@tr("…")` in `.slint`, then provide the
translations from Go. Call `SetTranslator` again to switch languages at runtime
(visible `@tr` strings re-render); return the original for anything untranslated:

```slint
Text { text: @tr("Hello"); }
```

```go
es := map[string]string{"Hello": "Hola"}
slint.SetTranslator(func(msgid string) string {
	if v, ok := es[msgid]; ok {
		return v
	}
	return msgid
})
```

**Multiple windows.** Each `Create` (or `New…`) is an independent window. Show each
one (non-blocking) and drive a **single** shared event loop with `slint.Run()` — not
`win.Run()` per window. `Run()` returns when the last window closes; `slint.RunUntilQuit()`
keeps the loop running across windows opening/closing (until `slint.Quit()`).

```go
mainWin.Show()
dialog.Show()        // opened on demand; both share one loop
slint.Run()          // or slint.RunUntilQuit() for tray-style / dynamic-window apps
```

See [`cmd/examples/multiwindow`](cmd/examples/multiwindow). (One `.slint` with several
window components works via the dynamic API today; the typed generator currently wraps
a single component per file.)

**Snapshot.** Render the window's current contents to a Go image — for screenshots
or export:

```go
img, err := win.Snapshot()      // *image.NRGBA; or SnapshotRGBA() for raw bytes
png.Encode(file, img)
```

Snapshot needs a live renderer, so call it on a shown window (it uses the GPU or
software renderer). It is not available under the headless test backend.

Timers run on the event loop:

```go
slint.SingleShot(500, func() { /* once, after 500ms */ })

t := slint.NewTimer()
t.Start(slint.TimerRepeated, 1000, func() { /* every second */ })
```

**Threading.** Slint's context is thread-local. Call `runtime.LockOSThread()` in
`init` (the scaffold does this), and marshal any UI change made from another
goroutine through `slint.InvokeFromEventLoop(func(){ … })`.

---

## Shipping

```sh
goslint build -o myapp .                 # one self-contained desktop binary
goslint android build -o myapp.apk .     # signed APK (arm64-v8a + x86_64)
```

Prefer plain `go`? `eval "$(goslint env)"` exports `CGO_LDFLAGS`, then
`go build -tags goslint_extlib .` — no pkg-config required. (If you'd rather use
pkg-config, the `goslint_pkgconfig` tag still works with the `goslint.pc` that
`setup` also writes.)

**Android.** Your package needs a `//go:build android` entry exporting
`goslint_android_main`; `goslint init` scaffolds it (`app_android.go`). The build
downloads the prebuilt `libgoslint.so` per ABI, cross-builds your package as a
c-shared, and packages + signs. Point `ANDROID_HOME`/`ANDROID_NDK_HOME` at your SDK
and NDK; flags set `-package`, `-label`, `-abi`, `-min-sdk`, `-keystore`, etc. (a
debug keystore is created automatically).

---

## Reference

- [`cmd/examples`](cmd/examples) — runnable examples (typed: `counter`, `clock`,
  `typed`, `multifile`; dynamic: `todo`, `window`, `gradient`, `interop`).
- [README.md](README.md) — overview, install, platform support, license.
