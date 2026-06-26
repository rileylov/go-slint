# go-slint — architecture & contributor notes

Go bindings for [Slint](https://slint.dev), built on the interpreter approach
(load `.slint` markup at runtime) over a thin Rust C-ABI shim. Users don't need
this file or Rust — see the README and the `goslint` CLI. This is for working *on*
the bindings.

## Layers

- **`rust/goslint-sys/`** — the Rust shim: a flat C ABI (`extern "C"`) over
  `slint-interpreter`, built to `libgoslint.{a,so}`. Features: `backend-winit` +
  `renderer-femtovg` (GPU, desktop default) + `renderer-software` + the headless
  testing backend. Android pulls `i-slint-backend-android-activity` (skia).
- **`include/goslint.h`** — the hand-written C header (the ABI contract).
- **`slintsys/`** — Layer 1: low-level cgo, 1:1 with the C ABI.
- **`slint.go` / `slint_dev.go` / `doc.go`** (package `slint`) — Layer 2: the
  idiomatic Go API.
- **`cmd/goslint/`** — the user-facing CLI (init/setup/dev/build/run/android/doctor).

## Build & test (from source)

```sh
make slint    # clone/checkout upstream Slint at .slint-version (gitignored)
make lib      # cargo-build the shim + stage libgoslint.{a,so} into lib/<os>_<arch>/
make test     # lib, then go test . ./slintsys/ ./internal/conformance/
make conformance
make update-slint [SLINT_REF=v1.x]   # bump the pin, rebuild, verify, record .slint-version
```

Three link modes (one cgo file each, tag-gated): `link_dev.go` (default, no tag —
links the lib `make lib` stages, for in-repo build/test); `link_extlib.go` (`-tags
goslint_extlib` — link flags come from `CGO_LDFLAGS`, which the `goslint` CLI sets
from the downloaded lib; **this is what build/run/dev/generate use**, so no
pkg-config is needed on any platform); `link_pkgconfig.go` (`-tags goslint_pkgconfig`
— `#cgo pkg-config: goslint`, for users who prefer pkg-config). `goslint setup`
writes both `goslint.pc` and a `cgo_ldflags` file; the CLI reads the latter and
exports `CGO_ENABLED=1` + `CGO_LDFLAGS`. Updating Slint is low-friction: the shim binds
only the stable `slint-interpreter` API, so a breaking change surfaces as a
localized Rust compile error in `make lib`, not a silent runtime break.

## Layer 0 conventions (enforced)

- Every `extern "C"` body runs inside `guard` (catch_unwind) — never unwind across C.
- Returned `char*`/handles are library-owned; callers free via the matching `_free`.
  NULL = failure; detail via `goslint_last_error()`.
- Inbound strings are borrowed (copied in Rust).
- Go callbacks cross into C only via `runtime/cgo.Handle` — never raw Go pointers.
  (Static C bridges in `*_bridge.go`; `//export` funcs in their own files.)

**FFI closure gotcha (cost a long debug session):** in a `move ||` closure that
drives a host callback, call a *method* on the Drop-guard struct (`data.call()`),
never read its `Copy` fields directly (`(data.cb)(data.handle)`). Rust 2021 disjoint
capture would move only the Copy fields and drop the guard immediately, releasing
the cgo.Handle early → "misuse of an invalid Handle". See `timer.rs`
(`TimerCallback::call`). Applies to any FFI callback closure.

## Value ↔ Go mapping

Void↔nil, Number↔float64, Bool↔bool, String↔string, Struct↔map[string]any
(recursive), Enum↔`slint.Enum{Type,Value}`, Color↔`slint.Color{R,G,B,A}`,
Image↔`slint.Image`, Model↔[]any (read snapshot) / `slint.SliceModel` |
`NewModel(Model)` (live, write). Set/Get and callbacks accept/return all of these
(models unwrap via `toSys` in Set/Invoke).

**Model bridge:** a Go `Model` (RowCount/RowData/SetRowData) is wrapped by a Rust
`GoModelInner: i_slint_core::model::Model` holding a `ModelNotify`; the opaque
handle is a boxed `ModelRc<Value>`. `slint.SliceModel` is the built-in
auto-notifying model. **Threading:** every UI mutation (Set, model
Append/SetRowData) from a background goroutine must go through
`InvokeFromEventLoop` — Slint's context is thread-local (lock with
`runtime.LockOSThread`).

## Distribution

`goslint setup` reads the go-slint version from the user's `go.mod` and downloads
the matching prebuilt `libgoslint` from GitHub Releases (checksum-verified) into
`~/.cache/goslint/`, writing a pkg-config file. This is now **optional** —
`build`/`run`/`dev` call the same provisioning (`ensureProvisioned`) automatically
when `cgoLDFLAGS` finds the lib uncached, so a fresh checkout just works; `setup`
remains for explicit pre-fetch, CI, `-force`, and `-target` cross-provisioning. The release is produced by
`.github/workflows/release.yml` (matrix → `manifest.json` + `SHA256SUMS`) on a
`v*` tag. **Cutting a release:** push a `vX.Y.Z` tag; the published `manifest.json`
version = the tag, and `setup` matches it from `go.mod`. Android targets ship the
cdylib `.so`; `goslint android build` cross-builds the user's package as a
c-shared, bundles both `.so` per ABI, and signs the APK (Go has no c-archive on
android, so the Rust `android_main` `dlopen`s the Go lib).

**Windows static lib:** the `windows` crate (via winit) lists umbrella import libs
(`-lwindows.0.52.0` …) in `native-static-libs`; those `.a` files live inside the
`windows_*_gnu` crate and aren't on a user's machine. The release step merges them
into `libgoslint.a` (an `ar -M` MRI `ADDLIB`) and strips the `-lwindows.*` tokens
from the published link line, so the shipped archive is self-contained.

## Tests

`internal/conformance` runs Slint's full `.slint` corpus (auto-discovers all case
dirs) through the Go API, mirroring Slint's interpreter test driver
(SLINT_ENABLE_EXPERIMENTAL_FEATURES + test fonts + OS=Windows): 0 failures.
`internal/timertest` verifies timers fire via the integration backend. Each
`cmd/examples/*` has a compile-smoke `*_test.go` (validates markup, no display).

### Known platform limitations

- **Skia-only properties don't render.** go-slint ships femtovg + software, not Skia,
  so `drop-shadow-spread` and the `inner-shadow-*` properties on `Rectangle` (Slint
  1.17) are ignored. Plain `drop-shadow-*` works everywhere; only the spread/inner
  variants need Skia.
- **`DragArea.drag-image` must point at a file on disk, not a `data:` URL or a
  Go-set image.** The cursor-following drag overlay (Slint 1.17) is loaded from the
  `@image-url` resolved relative to the `.slint` source file's directory — so compile
  with [`CompileSource`] (or the generated/`CompileFS` path with the source present),
  not bare `Compile` with an empty path, and ship the asset alongside the markup. A
  `data:` URL or an image pushed in from Go renders as a black box. With a real file
  via `@image-url`, the overlay renders on every platform including Wayland (see
  `cmd/examples/dragdrop`, which mirrors Slint's own `dnd-kanban`).