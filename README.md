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

## License

Slint is tri-licensed (GPL-3.0 / royalty-free / commercial); a linked application
inherits those terms. Settle your licensing posture before distributing.
