# go-slint

Go bindings for the [Slint](https://slint.dev) declarative UI toolkit.

Write your UI in Slint's `.slint` markup and drive it from Go: compile markup at
runtime, create components, read/write properties, handle callbacks, back models,
load images, and run timers — all from Go.

> Status: feature-complete data + graphics layer (compile → window → properties →
> callbacks → structs/enums → models → color/image/timers). Desktop Linux is the
> primary tested target. Cross-platform packaging (Windows/macOS/Android) is WIP.
> See [`PLAN.md`](PLAN.md) for the roadmap.

## Example

```go
package main

import (
	_ "embed"
	"runtime"

	"github.com/rileylov/go-slint"
)

func init() { runtime.LockOSThread() } // Slint is thread-affine

//go:embed app.slint
var ui string

func main() {
	app, _ := slint.Compile(ui, slint.WithStyle("fluent"))
	defer app.Close()
	win, _ := app.Create("AppWindow")
	defer win.Close()

	win.OnCallback("clicked", func([]any) any {
		n, _ := win.Int("counter")
		win.Set("counter", n+1)
		return nil
	})
	win.Run()
}
```

Runnable examples in [`cmd/examples`](cmd/examples): `hello`, `counter`,
`todo` (live model), `clock` (timer).

```sh
make lib                       # build the native shim into lib/<os>_<arch>/
go run ./cmd/examples/todo
```

## Cross-compiling for Windows (from Linux)

Slint cross-compiles cleanly to Windows (its text stack is pure Rust). You need the
Rust target and the mingw-w64 toolchain:

```sh
rustup target add x86_64-pc-windows-gnu
sudo pacman -S mingw-w64-gcc          # Arch (or: apt install gcc-mingw-w64-x86-64)

make build-windows                    # cross-builds goslint.dll + all examples to build/windows/
```

Copy `build/windows/` to a Windows machine and run an `.exe` — keep `goslint.dll`
in the same folder (or on `PATH`).

## Android

Builds a signed debug APK (x86_64 + arm64-v8a) of `cmd/androiddemo`. Slint's
Android backend (skia, via `android-activity`) renders straight to a NativeActivity.
The app ships as two libraries: `libgoslint.so` (Rust cdylib — the NativeActivity
entry + the `goslint_*` C ABI) and `libgoslintapp.so` (the Go app as a c-shared,
`dlopen`'d by the Rust `android_main`).

Prereqs: `rustup target add aarch64-linux-android x86_64-linux-android`, the Android
NDK, and SDK build-tools + a platform. Point `ANDROID_HOME` at an SDK that has them.

```sh
make android                                   # -> build/android/goslint-demo.apk
adb install -r build/android/goslint-demo.apk  # install on emulator/phone
```

Verified rendering on an x86_64 emulator (API 36); the same APK runs on arm64 phones.

## How it works

Three layers (details in [`CLAUDE.md`](CLAUDE.md)):

- **`rust/goslint-sys`** — a Rust shim exposing a flat C ABI over Slint's
  `slint-interpreter`. Built to `lib/<os>_<arch>/libgoslint.{so,a}` by `make lib`.
- **`slintsys`** — the low-level cgo package (1:1 with the C ABI).
- **`slint`** — the idiomatic Go API (this module's root package).

The interpreter approach (like Slint's Node and Python bindings) loads `.slint`
at runtime; embed it with `go:embed`.

## Building & testing

```sh
make lib          # cargo-build the shim + stage libs (do this before go build/test)
make test         # lib, then `go test`
make conformance  # run Slint's full .slint test corpus through the bindings
```

The conformance suite runs Slint's own 600+ `.slint` cases through the Go API
(mirroring Slint's interpreter test driver): currently 0 failures.

## Staying in sync with Slint

The bindings sit on `slint-interpreter`'s stable public Rust API, so tracking
upstream is one command — it bumps the pinned checkout, rebuilds, and verifies
against the full conformance corpus:

```sh
make update-slint                    # track main (latest origin/master)
make update-slint SLINT_REF=v1.18.0  # pin to a release tag (recommended for stability)
```

If an upstream change actually affects us, `make lib` fails with a localized Rust
compile error (pointing at the exact function) before anything is staged — a
broken update can't ship silently. In practice the shim builds unchanged across
Slint versions (verified compiling clean and conformance-green on both 1.16.1 and
1.17.0-dev).

## License

Slint is tri-licensed (GPL-3.0 / royalty-free / commercial); a linked application
inherits those terms. Settle your licensing posture before distributing.
