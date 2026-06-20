# go-slint (wip)

Go bindings for the [Slint](https://slint.dev) declarative UI toolkit.

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

## Quickstart

To use go-slint in your own project you need **Go** and a **C compiler** (for cgo)
— that's all. The native Slint library is downloaded prebuilt, so you do **not**
need Rust or a Slint checkout just to *use* the bindings.

1. Add the module and fetch the native library for your platform:

   ```sh
   go get github.com/rileylov/go-slint
   go run github.com/rileylov/go-slint/cmd/goslint setup
   ```

   `setup` downloads the prebuilt `libgoslint` for your OS/arch from this project's
   [Releases](../../releases) (checksum-verified) into `~/.cache/goslint/`, and
   writes a pkg-config file describing how to link it. Run `goslint doctor` any
   time to check your toolchain and the cached library.

2. Write a `.slint` UI and a `main.go` (see [Example](#example) above), then build:

   ```sh
   # easiest — the wrapper sets the linker flags + build tag for you:
   go run github.com/rileylov/go-slint/cmd/goslint build -o myapp .
   ./myapp

   # …or with plain `go build`, after pointing pkg-config at the cached lib:
   eval "$(go run github.com/rileylov/go-slint/cmd/goslint env)"
   go build -tags goslint_pkgconfig -o myapp .
   ```

The result is a **single self-contained binary**: `libgoslint` (and the Slint
interpreter inside it) is linked statically, leaving only ubiquitous system
libraries as runtime dependencies — on Linux that's OpenGL and fontconfig, present
on any desktop.

> Tip: `go install github.com/rileylov/go-slint/cmd/goslint@latest` once, then call
> `goslint setup` / `goslint build` / `goslint doctor` directly instead of via
> `go run`.

For Android, see [Android](#android) below — there the native library is bundled
into an APK rather than linked into a desktop binary.

## Platforms

Current platform status as of writing:

| Platform   | Tested           | Maintained                                                                                                  
| :--------- | :--------------- | :---------
| Linux      | ✅ 7.0.3-arch1-1       | ✅                                                                                            
| Android    | ✅ CinnamonBun      | ✅                                                                                 
| Windows    | ❌  	   | ✅
| iOS        | ❌       | ❌                                                                                                      
| macOS      | ❌       | ❌                                                             

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

Slint's Android backend (skia, via `android-activity`) renders straight to a
NativeActivity. An APK ships two libraries per ABI: `libgoslint.so` (the native
Slint shim — NativeActivity entry + the `goslint_*` C ABI) and `libgoslintapp.so`
(your Go package built as a c-shared, `dlopen`'d by the Rust `android_main`).

Your Go package needs an Android entry that exports `goslint_android_main` (a
`//go:build android` file calling `runtime.LockOSThread()` then `slint.Compile`/
`Create`/`Run`) — see [`cmd/examples/interop`](cmd/examples/interop) for a
cross-platform example.

Build a signed APK with the `goslint` tool (it downloads the prebuilt
`libgoslint.so` for each ABI, cross-builds your package, and packages + signs):

```sh
goslint android build -o myapp.apk ./path/to/pkg   # arm64-v8a + x86_64
adb install -r myapp.apk
```

Prereqs: the Android **NDK** and SDK **build-tools** + a **platform** (point
`ANDROID_HOME`/`ANDROID_NDK_HOME` at them); a JDK for signing. Flags let you set
`-package`, `-label`, `-abi`, `-min-sdk`, a custom `-manifest`, `-keystore`, etc.
(a debug keystore is created automatically). Verified rendering on an x86_64
emulator and on arm64 phones.

> Contributors building the bundled demo from source can instead use
> `make android` (it cargo-builds the shim rather than downloading it).

## How it works

Three layers (details in [`CLAUDE.md`](CLAUDE.md)):

- **`rust/goslint-sys`** — a Rust shim exposing a flat C ABI over Slint's
  `slint-interpreter`. Built to `lib/<os>_<arch>/libgoslint.{so,a}` by `make lib`.
- **`slintsys`** — the low-level cgo package (1:1 with the C ABI).
- **`slint`** — the idiomatic Go API (this module's root package).

The interpreter approach (like Slint's Node and Python bindings) loads `.slint`
at runtime; embed it with `go:embed`.

## Building from source (contributors)

Working *on* go-slint (rather than using it) builds the native shim yourself, which
needs the **Rust toolchain** and a Slint checkout in `./slint` (the version pinned
in `.slint-version`). This path stages the lib locally and skips `goslint setup`.

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

The **go-slint** binding code in this repository is licensed under the
[MIT License](LICENSE).

go-slint links the [Slint](https://slint.dev) UI toolkit, which is **separately
licensed** and **not** covered by this repository's MIT license. Slint is
tri-licensed under one of:

- **GPLv3** — free, but your application must be GPL-compatible (open source);
- a **royalty-free** license for desktop, mobile, and web applications — free,
  with an attribution requirement (a "Made with Slint" notice);
- a **commercial** license — paid; removes attribution and adds support.

Any application you build with go-slint — and the prebuilt `libgoslint` binaries
published in this project's [Releases](../../releases), which embed Slint — are
governed by Slint's license, and you must choose and comply with one of the
options above. The MIT grant on this repository applies only to the binding code
and does **not** extend to Slint.

For all of go-slint's supported targets the royalty-free license applies, see [LICENSES](https://github.com/slint-ui/slint/blob/master/LICENSES) for more information.

