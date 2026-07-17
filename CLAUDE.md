# go-slint — architecture & contributor notes

Go bindings for [Slint](https://slint.dev), built on the interpreter approach
(load `.slint` markup at runtime) over a thin Rust C-ABI shim. Users don't need
this file or Rust — see the README and the `goslint` CLI. This is for working *on*
the bindings.

## Layers

- **`rust/goslint-sys/`** — the Rust shim: a flat C ABI (`extern "C"`) over
  `slint-interpreter`, built to `libgoslint.{a,so}`. Features: `backend-winit` +
  `renderer-femtovg` (GPU, desktop default) + `renderer-software` + the headless
  testing backend. Android pulls `i-slint-backend-android-activity` (skia). iOS needs
  no Cargo change: Slint's winit `build.rs` auto-enables the Skia/Metal renderer and
  compiles out femtovg/GL (`ios_and_friends` cfg), so the same crate builds for
  `aarch64-apple-ios{,-sim}` with the renderer-femtovg feature harmlessly inert.
- **`include/goslint.h`** — the hand-written C header (the ABI contract).
- **`slintsys/`** — Layer 1: low-level cgo, 1:1 with the C ABI.
- **`slint.go` / `slint_dev.go` / `doc.go`** (package `slint`) — Layer 2: the
  idiomatic Go API.
- **`cmd/goslint/`** — the user-facing CLI (init/setup/dev/build/run/android/ios/doctor).

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
android, so the Rust `android_main` `dlopen`s the Go lib). `goslint android dev`
builds, installs, and launches on a running device or a booted emulator (building
only that device's ABI for speed), then rebuilds/reinstalls on each edit — the shared
`mobileDev` driver, same as `goslint ios dev`.

**iOS** ships static `.a`s (`ios_arm64` device + `ios_sim_arm64` Apple-Silicon
simulator; `make lib-ios` builds them, the release matrix publishes them). Unlike
android, iOS needs **no entry-point inversion**: winit calls `UIApplicationMain`
itself when the event loop runs, so a plain desktop-style `func main()` (open a
window, `Run()`, with `runtime.LockOSThread`) *is* the iOS app. `goslint ios build`
cross-compiles that package (`GOOS=ios GOARCH=arm64`, `CC=clang -target
arm64-apple-ios<min>{-simulator} -isysroot <sdk>`, the extlib link path), links the
prebuilt shim + recorded frameworks, and wraps the binary in a signed `.app`
(ad-hoc for the simulator; `-device` needs a real identity). `-run` installs +
launches it in the booted simulator via `simctl`. Needs a full Xcode selected
(`xcode-select -p`).

**Windows static lib:** the `windows` crate (via winit) lists umbrella import libs
(`-lwindows.0.52.0` …) in `native-static-libs`; those `.a` files live inside the
`windows_*_gnu` crate and aren't on a user's machine. The release step merges them
into `libgoslint.a` (an `ar -M` MRI `ADDLIB`) and strips the `-lwindows.*` tokens
from the published link line, so the shipped archive is self-contained.

**Windows gnullvm lib (for zig):** the release ships a *second* Windows lib built for
`x86_64-pc-windows-gnullvm` (target key `windows_gnullvm_amd64`). Same ABI as gnu but
its exception-handling runtime is LLVM libunwind/compiler-rt instead of libgcc — so
`zig cc` links it with no external toolchain (the gnu lib needs libgcc's
`_GCC_specific_handler`, which zig can't supply). This is the lib goslint uses when it
builds Windows with zig: cross-compiling from Linux/macOS, or a Windows box with only
zig and no MinGW/MSVC. The staticlib is **pure Rust** (no C toolchain to build it — the
only `-sys` dep, `windows-sys`, is bindings). Its umbrella import libs are LLVM
*short-import* archives that GNU `ar` mangles, so the release folds them in with
`zig ar` (llvm-ar) via an MRI `CREATE` script; `-lunwind` stays on the published link
line (the user's zig provides libunwind at link time). The published link line must
name `libgoslint.a` by path — zig's `lld-link` rejects GNU `-l:` and `--start-group`.

**Cross-compiling with zig (`goslint build -target <goos>_<goarch>`).** Sets
`GOOS`/`GOARCH` + `CC="zig cc -target <triple>"` and links the right prebuilt lib. Each
OS needs a different thing to be zig-linkable, and the reason is always *where its
unwinder / external deps live*:
- **windows_amd64** → the `windows_gnullvm_amd64` lib (LLVM unwinder zig bundles; the
  gnu lib's libgcc can't be supplied by zig).
- **linux_amd64 / linux_arm64** → the normal linux lib. Its only non-glibc link deps were
  `fontconfig`/`freetype`; the `fontconfig-dlopen` feature (in goslint-sys' Cargo.toml)
  makes fontconfig load at runtime via `dlopen`, so nothing external is left to resolve
  at link time (system fonts still work; falls back to the embedded font if fontconfig is
  absent). Linux/BSD-gated, so it's a no-op on macOS/Windows.
- **darwin_amd64 / darwin_arm64** → the normal darwin lib, but macOS needs an Apple SDK
  (frameworks/headers) that can't be bundled (Apple license). The user sets
  `GOSLINT_MACOS_SDK` to one (e.g. from github.com/joseluisq/macosx-sdks); goslint passes
  `-isysroot`/`-F`/`-L`. The default `-s -w` skips DWARF, avoiding the host-`strip` step
  Go otherwise runs on darwin.

The archive-reference form is chosen per-TARGET by `linkByPath` (main.go): macOS, linux,
and windows-gnullvm name the archive by path (zig's linkers reject GNU `-l:`); only the
windows-gnu/mingw lib keeps the `--start-group -l:` form. Codegen always runs on the host
(the host lib), even for a cross build.

## Tests

`internal/conformance` runs Slint's full `.slint` corpus (auto-discovers all case
dirs) through the Go API, mirroring Slint's interpreter test driver
(SLINT_ENABLE_EXPERIMENTAL_FEATURES + test fonts + OS=Windows): 0 failures.
`internal/timertest` verifies timers fire via the integration backend. Each
`examples/*` has a compile-smoke `*_test.go` (validates markup, no display).

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
  `examples/dragdrop`, which mirrors Slint's own `dnd-kanban`).

## Toward v1.0

Tracked breaking changes to make when cutting **v1.0** (don't do them in 0.x):

- **Drop the Layer-1 type aliases.** `slint.Image` and `slint.Timer` are currently
  `= slintsys.Image` / `= slintsys.Timer` aliases, which leaks Layer 1 into the public
  v1 contract. Make them real `slint` structs wrapping the `slintsys` handle. This is
  non-trivial: images/timers flow through the dynamic Value system *as* `slintsys`
  types, so it needs wrapping/unwrapping at every boundary where one surfaces — `Set`
  (via `toSys`), `Get`/typed getters (`goValue` returns `*slintsys.Image`), callback
  args, model rows, and values nested inside structs. Keep the finalizer leak-watch
  (the wrapper's `Close` must still drive the inner handle's release) and the
  image-read path (#2) working. See the `TODO(v1.0)` at the `Image`/`Timer` aliases in
  `slint.go`. Note: the copy-safety restructuring already moved these types onto a
  shared owner cell (`imageOwner`/`timerOwner`/`modelOwner`, leak-watch included), so
  the v1 wrappers mostly need the boundary wrapping/unwrapping — ownership mechanics
  are done.
- **Remove the deprecated `Free()` methods.** `Image`/`Timer`/`ModelHandle` kept
  `Free()` as a `// Deprecated:` alias for `Close()` (added in 0.x for a consistent
  release verb without a break). Deleting `Free()` at v1.0 is the breaking change — tag
  that commit accordingly.