# go-slint - Guide (wip)

How to build a UI with go-slint, step by step.

You write your UI in the **`.slint`** language, run **`goslint generate`** to turn
it into a typed Go package, and drive it with normal Go methods. Property and
callback names become compile-checked methods; structs and enums become Go types.
Under the hood a small Rust shim runs Slint's interpreter, so `.slint` is compiled
at run time - but `goslint generate` embeds the markup, so you still ship one
self-contained binary.

> New to the `.slint` language itself? See the
> [Slint Language Documentation](https://slint.dev/docs/slint).

---

## Quick start

**Prerequisites:** Go and a **C compiler** for cgo - gcc/clang on Linux, the Xcode
command-line tools on macOS, **MinGW-w64 gcc on Windows** (the prebuilt lib uses the
GNU toolchain, so MSVC won't link). You do *not* need Rust or pkg-config. `goslint
doctor` checks all of this.

**1. Scaffold a project** (creates `go.mod`, `app.slint`, `main.go`, and the
generated `ui/` package):

```sh
go install github.com/rileylov/go-slint/cmd/goslint@latest
goslint init myapp && cd myapp
```

`goslint dev`/`run`/`build` download the prebuilt native lib for your platform on
first use (cached, once per version) - no separate install step. (To pre-fetch it
explicitly, e.g. in CI, run `goslint setup`.)

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
goslint generate          # whole project: writes <name>.slint.go next to each entry .slint
# …or target one file explicitly:
goslint generate -o ui/app.slint.go -package ui app.slint
```

With no arguments, `goslint generate` finds every **entry** `.slint` (one not imported by
another - imported components/widgets are skipped) and generates the typed `<name>.slint.go`
beside it, packaged after its directory. If your project already has `//go:generate goslint
generate …` directives, it runs those instead, so a custom output path/package still wins.

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

`goslint dev`/`run`/`build` regenerate the typed wrappers from your `.slint` first,
so you can just edit `app.slint` and see your changes. The scaffold adds a
`//go:generate goslint generate …` directive (which they honour), but it's optional:
without one they fall back to the same convention as bare `goslint generate` -
generate `<name>.slint.go` beside each entry `.slint`.

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
| `brush` | `any` - a `slint.Color` or `slint.Gradient` (see [Dynamic API](#dynamic-runtime-api)) |

### Images

An `image` property is a `*slint.Image`. Load one from a file, or build one from
pixels you generated/decoded in Go (Slint's `SharedPixelBuffer` equivalent):

```go
img, _ := slint.LoadImage("logo.png")          // from a file
img, _ := slint.NewImage(goImage)              // any image.Image: decoded, drawn, generated
img, _ := slint.NewImageRGBA(pixels, w, h)     // raw RGBA8 bytes (w*h*4)
img, _ := slint.NewImageFromSVG(svgBytes)      // SVG bytes (e.g. go:embed'd) — Slint rasterizes
img, _ := slint.NewImageFromData(pngBytes, "") // encoded bytes (PNG/JPEG/…); "" auto-detects
win.SetIcon(img)
defer img.Close()
```

`NewImage` accepts any `image.Image`, so the whole Go ecosystem works - decode with
`image/png`/`image/jpeg`, draw with `image/draw` or a plotting library, or generate
procedurally. (It handles Go's premultiplied-alpha `image.RGBA` correctly.) See
[`examples/image`](examples/image).

**Embedded SVGs.** `NewImageFromSVG` takes raw SVG bytes and lets Slint rasterize
them at render size — so a `go:embed`'d vector asset stays crisp at any scale and
needs no file on disk. Prefer it to `@image-url` when shipping a self-contained
binary or an APK, where `@image-url` can't resolve a path (it would render blank).

### Custom fonts

The simplest way is to **import the font in your `.slint`** - the interpreter
registers it for you, and you reference it by family name:

```slint
import "fonts/Inter.ttf";
export component AppWindow inherits Window {
    Text { text: "Hi"; font-family: "Inter"; }
}
```

To register a font from Go at runtime (e.g. a `go:embed`'d or user-supplied font),
call it on the window before the text is shown - it applies to all windows:

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

For lists that update **row-by-row at runtime** (insert/remove/edit while shown),
bind a *live model* instead of resending the whole slice. Every array property also
gets a typed `Set<Name>Model` setter that takes a `slint.LiveModel` (a `*SliceModel`,
or `slint.NewModel(yourModel)`):

```go
m := slint.NewSliceModel("a", "b")
win.SetItemsModel(m)   // bind once; the model stays live
m.Append("c")          // one row added → Slint updates just that row (no re-set)
m.SetRowData(0, "A")   // one row changed → only row 0 re-renders
```

`SetItems([]T)` replaces the list from a snapshot (full re-render); `SetItemsModel`
keeps a persistent model whose per-row changes flow to the UI incrementally — use it
for progressively-filled or frequently-edited lists.

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
built binary is still self-contained - no `.slint` tree is needed at run time.
(`@library` imports are the exception: they still resolve via library paths.) See
[`examples/multifile`](examples/multifile).

---

## Live development

You write your app **one way** - create, bind, `Run()` - and it does the right thing
in dev and in production.

```go
win, _ := ui.NewAppWindow()
defer win.Close()
win.OnIncrement(func() { n, _ := win.Count(); _ = win.SetCount(n + 1) })
win.Run()
```

`goslint dev .` runs this and watches the project:

- editing a **`.slint`** **hot-reloads in-process** - the app recompiles the markup
  and swaps its UI into the *same* window live, **no rebuild**, so cosmetic/layout
  iteration is instant (~100–500ms per save);
- editing a **`.go`** rebuilds and restarts (regenerating the wrapper first if the
  `.slint`'s interface changed).

Dynamic-API projects (markup embedded next to `package main` and compiled with
`slint.Compile` — no generated wrapper) can't swap markup in-process: the `.slint`
is baked into the binary by `go:embed`. For those, `goslint dev` rebuilds and
restarts on `.slint` edits too, so a save still shows without leaving the loop.
(An app that drives `slint.LiveReload` itself is left alone — it already re-reads
its markup from disk.)

How it works: under `GOSLINT_DEV`, the generated wrapper *records* your `New`-to-`Run`
setup calls (`Set*`, `On*`) and `Run()` replays them onto the freshly-compiled
instance on each reload - so your same inline code drives the live reload. Property
state resets on reload (it replays your initial setup, like a page reload). `goslint
build`/`run` produce a normal binary from the same code (no recording, no reload).

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

**Frameless windows.** With `Window { no-frame: true; }` the OS draws no title bar,
so moving and resizing become your job — and the right way to do them is to hand the
gesture back to the OS, not to move the window by hand from pointer deltas. From a
`TouchArea` pointer-event callback (while the button is down), call:

```go
win.OnCallback("start-move", func([]any) any {
	_ = win.StartSystemMove() // OS tracks the pointer until release
	return nil
})
win.OnCallback("start-resize", func(a []any) any {
	_ = win.StartSystemResize(slint.ResizeSouthEast) // or any edge/corner
	return nil
})
```

One call handles the whole gesture with native motion (it's winit's `drag_window`
escape hatch). Desktop only — Windows, macOS, X11, Wayland; on other backends the
calls return an error. Give the grab areas a matching `mouse-cursor` (`ew-resize`,
`ns-resize`, …) so the pointer advertises the resize. Full wiring — custom title
bar, all eight edges/corners — in [`examples/frameless`](examples/frameless).

**Closing.** Intercept the window's close (the X button) to confirm or save first -
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
one (non-blocking) and drive a **single** shared event loop with `slint.Run()` - not
`win.Run()` per window. `Run()` returns when the last window closes; `slint.RunUntilQuit()`
keeps the loop running across windows opening/closing (until `slint.Quit()`).

```go
mainWin.Show()
dialog.Show()        // opened on demand; both share one loop
slint.Run()          // or slint.RunUntilQuit() for tray-style / dynamic-window apps
```

See [`examples/multiwindow`](examples/multiwindow). (One `.slint` with several
window components works via the dynamic API today; the typed generator currently wraps
a single component per file.)

**Snapshot.** Render the window's current contents to a Go image - for screenshots
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
goroutine through `slint.InvokeFromEventLoop(func(){ … })`:

```go
go func() {
    img := decode(path)                       // heavy work off the UI thread
    slint.InvokeFromEventLoop(func() {        // back on the UI thread to touch the UI
        win.SetPreview(img)
    })
}()
```

A bare `win.SetPreview(img)` from that goroutine compiles fine but corrupts or
crashes at runtime - Slint is thread-affine. To catch this, **`goslint dev` runs a
guard**: a property set, model mutation, or invoke made off the event-loop thread
**panics** with a clear message instead of corrupting silently. The guard is active
only under `GOSLINT_DEV` (so a shipped build has zero overhead) - develop with
`goslint dev` and off-thread mistakes surface immediately.

---

## Shipping

```sh
goslint build -o myapp .                 # one self-contained desktop binary
goslint android build -o myapp.apk .     # signed APK (arm64-v8a + x86_64)
```

On **Windows**, `goslint build` links the app as a GUI subsystem binary, so
double-clicking it shows only your window - no console pops up alongside. (A side
effect: stdout/stderr go nowhere in a built app, as with any GUI program. Need a
console build for debugging? Pass your own `-ldflags` - e.g. `goslint build
-ldflags= .` - and goslint leaves the subsystem at the default.)

Prefer plain `go`? `eval "$(goslint env)"` exports `CGO_LDFLAGS`, then
`go build -tags goslint_extlib .` - no pkg-config required. (If you'd rather use
pkg-config, the `goslint_pkgconfig` tag still works with the `goslint.pc` that
`setup` also writes.) On Windows add `-ldflags=-H=windowsgui` yourself to suppress
the console.

**Android.** Your package needs an entry exporting `goslint_android_main`;
`goslint android build` writes one (`android_main.go`) for you on first run. It's
gated on a custom `goslint_android` build tag rather than the android GOOS (and
isn't named `*_android.go`) so editors don't try to cross-build it and flag a
spurious cgo error — the build passes `-tags=goslint_android` with `GOOS=android`.
The build downloads the prebuilt `libgoslint.so` per ABI, cross-builds your package
as a c-shared, and packages + signs. Point `ANDROID_HOME`/`ANDROID_NDK_HOME` at your SDK
and NDK; flags set `-package`, `-label`, `-abi`, `-min-sdk`, `-keystore`, etc. (a
debug keystore is created automatically).

**Prerequisites (one-time host setup).** You need a JDK (17+) and the Android
command-line tools. On macOS via Homebrew:

```sh
brew install --cask temurin@17 android-commandlinetools
sdkmanager "platform-tools" "build-tools;35.0.1" "platforms;android-34" \
           "ndk;29.0.14206865" "emulator" "system-images;android-34;default;arm64-v8a"
```

(On Linux, install a JDK + the SDK via Android Studio or your package manager, then
the same `sdkmanager` components.) `goslint android build` auto-finds the SDK at the
usual locations — including Homebrew's `.../share/android-commandlinetools` and apt's
`/usr/lib/android-sdk` — so `ANDROID_HOME` is only needed if your SDK lives elsewhere;
set `JAVA_HOME` if the JDK isn't picked up.

---

## Cross-compiling

Build for another OS/arch from one machine with `-target`:

```sh
goslint build -target windows_amd64 -o myapp.exe .
goslint build -target linux_arm64   -o myapp     .
```

This uses [**zig**](https://ziglang.org/download/) as the C toolchain — `cgo` needs a
C compiler, and zig ships one that targets every platform. Install zig and put it on
`PATH`; that's all Windows and Linux targets need. The matching prebuilt `libgoslint`
is downloaded automatically, exactly like a native build. Supported targets:

| `-target` | Notes |
|---|---|
| `windows_amd64` | links the LLVM-ABI lib — no MinGW or MSVC required |
| `linux_amd64`, `linux_arm64` | fontconfig is loaded at runtime, so nothing external to link |
| `darwin_amd64`, `darwin_arm64` | needs an Apple SDK (see below) |

**macOS targets need an Apple SDK.** Apple's license means goslint can't ship one, so
point `GOSLINT_MACOS_SDK` at a copy — e.g. download and unpack a `MacOSX*.sdk` from
[macosx-sdks](https://github.com/joseluisq/macosx-sdks):

```sh
export GOSLINT_MACOS_SDK=/path/to/MacOSX14.sdk
goslint build -target darwin_arm64 -o myapp .
```

Only `build` takes `-target` (you can't run a cross-built binary on the host, so
`run`/`dev` don't). `goslint doctor` shows whether zig is installed.

> zig doubles as a **fallback C compiler** for *native* builds too: if you have no
> `cc`/`gcc`/`clang` but zig is on `PATH`, `goslint build`/`run`/`dev` use it
> automatically — handy on a fresh machine (Windows especially).

---

## Custom OpenGL (underlays, overlays, zero-copy textures)

For GPU workloads — video frames, game views, custom visualizations — goslint
exposes Slint's stable OpenGL interop (GL renderer only, i.e. the femtovg
default; under `SLINT_BACKEND=software` these return an error):

```go
win.SetRenderingNotifier(func(state slint.RenderingState) {
    switch state {
    case slint.RenderingSetup:      // GL context created:
        gl.InitWithProcAddrFunc(slint.GLProcAddress)   // go-gl via Slint's loader
    case slint.BeforeRendering:     // draw UNDER the UI, upload textures
    case slint.AfterRendering:      // draw OVER the UI
    case slint.RenderingTeardown:   // release GL resources
    }
})
```

The callback runs on the UI thread with the window's GL context current — GL
calls (and `slint.GLProcAddress`) are only valid inside it. Give the `Window` a
`background: transparent;` so a `BeforeRendering` underlay shows through
(see [`examples/glunderlay`](examples/glunderlay)).

For streaming pixels, wrap a texture you own as an image — zero-copy, Slint
samples the live texture every frame:

```go
img, _ := slint.NewImageFromGLTexture(textureID, w, h, false)
win.Set("frame", img)   // set once; glTexSubImage2D updates show live
```

Create/update the texture inside the notifier (same GL context), keep it alive
while any property shows the image, and Close the image before deleting the
texture. [`examples/glvideo`](examples/glvideo) streams 720p frames and shows a
live cost comparison against the per-frame `NewImageRGBA` copy path.

---

## Renderers

go-slint ships Slint's **GPU renderer** (femtovg/OpenGL - the default, best on most
machines) and a **software renderer**. On **low-end / integrated GPUs**, OpenGL
window *resize* can stutter badly (a Slint/driver limitation, not the bindings - it
reproduces in Slint's own examples). Switch to the software renderer via an env var,
no rebuild:

```sh
SLINT_BACKEND=software   ./myapp           # or `winit-software`
# PowerShell:  $env:SLINT_BACKEND="software"; .\myapp.exe
```

On a discrete GPU the default is smooth and you don't need this. (Set the env var
before launching; you can also `os.Setenv("SLINT_BACKEND", "software")` in `init()`
before creating a window if you want to force it for your app.)

---

## Reference

- [`examples`](examples) - runnable examples (typed: `counter`, `clock`,
  `typed`, `multifile`, `image`; dynamic: `todo`, `window`, `gradient`, `interop`,
  `multiwindow`, `threadcheck`, `dragdrop`, `systray`). The last four show the
  thread-affinity guard and the Slint 1.17 features (drag & drop, system tray).
- [README.md](README.md) - overview, install, platform support, license.
