# go-slint — agent context

Go bindings for Slint. **Read `PLAN.md` first** — it holds the architecture decision,
the C-ABI contract, the milestone plan, and the rationale.

## Layout
- `slint/` — pinned upstream Slint checkout (v1.17.0). Build dependency + reference. No Go files.
- `rust/goslint-sys/` — Layer 0: the Rust C-ABI shim over `slint-interpreter`.
- `include/goslint.h` — the C header (hand-written through M0; cbindgen later).
- `lib/<goos>_<goarch>/` — staged prebuilt shim libraries (built by `make lib`).
- `slintsys/` — Layer 1: low-level cgo package, 1:1 with the C ABI.
- `slint.go` (package `slint`) — Layer 2: idiomatic Go API.

## Build & test
```sh
make lib     # cargo build the shim + stage libgoslint.{a,so} into lib/<os>_<arch>/
make test    # make lib, then: go test . ./slintsys/ ./internal/conformance/
make update-slint               # bump pinned slint/ to origin/master, rebuild, verify
make update-slint SLINT_REF=v1.18.0   # pin a release tag instead
```
Updating Slint is low-friction by design (shim binds only the stable
`slint-interpreter` API): a breaking change shows up as a localized Rust compile
error in `make lib`, not a silent runtime break. Verified building clean +
conformance-green on both 1.16.1 and 1.17.0-dev.
The Rust lib MUST be staged in `lib/<os>_<arch>/` before `go build`/`go test`, because
cgo links it via `#cgo LDFLAGS`. The first cargo build is slow (it compiles
`i-slint-compiler`).

## Conventions (Layer 0, enforced)
- Every `extern "C"` body runs inside `guard` (catch_unwind) — never unwind across C.
- Returned `char*`/handles are library-owned; callers free via the matching `_free`.
  NULL = failure; detail via `goslint_last_error()`.
- Inbound strings are borrowed (copied in Rust).
- Go callbacks cross into C only via `runtime/cgo.Handle` — never raw Go pointers.

## Current state
**M0–M6 complete + M7 mostly done.** Shim built with `backend-winit` +
`renderer-femtovg` (GPU, desktop default) + `renderer-software` (fallback) + the
headless testing backend (`internal` feature → `configure_test_fonts`).
NOTE: software-renderer-only hit a winit/softbuffer buffer-size panic on Windows
(`software/lib.rs:584`, "buffer too small") — femtovg renders straight to the window
and avoids it. femtovg loads OpenGL at runtime (build needs libGL/opengl32).

C ABI (`include/goslint.h`): version/last_error/string_free; testing init + mock-time
+ configure-fonts; run/quit event loop; Compiler (style, include paths,
build_from_source/path); CompilationResult + diagnostics; ComponentDefinition
(name/create); ComponentInstance (get/set property, show/hide/run, invoke,
set_callback, globals: get/set property + set_callback + invoke_global); Value
scalars (void/number/bool/string) **+ structs (GoStruct: new/free/get/set/iter) and
enums (new/as)**.

**Value <-> Go mapping:** Void↔nil, Number↔float64, Bool↔bool, String↔string,
Struct↔map[string]any (recursive), Enum↔slint.Enum{Type,Value}, **Model↔[]any (read
snapshot) / slint.SliceModel|NewModel(Model) (write, live)**. Set/Get and callbacks
accept/return all of these (models: top-level via `toSys` unwrap in Set/Invoke).

**Model bridge (M5):** a Go `Model` (RowCount/RowData/SetRowData) is wrapped by a Rust
`GoModelInner: i_slint_core::model::Model` holding a `ModelNotify`; the opaque GoModel =
boxed `ModelRc<Value>` shared with the property. Trampolines in `model.go`, bridge in
`model_bridge.go`, both via cgo.Handle. `slint.SliceModel` is a built-in auto-notifying
model. goslint-sys now also depends directly on `i-slint-core` (to name Model/ModelRc/
ModelNotify).

**Callback model (M3):** the C ABI uses `uintptr_t user_data` (cgo.Handle pattern).
Go stores the closure via `cgo.NewHandle`, passes the handle; one exported
`goslintCallbackTrampoline` recovers + invokes it; `goslintDropHandle` frees it when
Slint releases the handler. Static C bridges live in `callback_bridge.go` (a no-export
file); the `//export` functions in `callback.go`. Never pass a raw Go pointer into C.

Compile options: `slint.Compile(src, slint.WithStyle("fluent"), slint.WithIncludePaths(...))`
— WithStyle is required for `import "std-widgets.slint"`.

Color↔slint.Color{R,G,B,A}, Image (slint.LoadImage → assign to `image` prop),
Timer (slint.NewTimer/Start/Stop/Running/Free, slint.SingleShot). Timers fire only while
the loop runs; `internal/timertest` verifies real firing via the integration backend's
run_event_loop.

**FFI closure gotcha (cost a long debug session):** in a `move ||` closure that drives a
host callback, call a *method* on the Drop-guard struct (`data.call()`), never access its
`Copy` fields directly (`(data.cb)(data.handle)`). Rust 2021 disjoint capture would move
only the Copy fields and drop the guard immediately, releasing the cgo.Handle early →
"misuse of an invalid Handle". See timer.rs / TimerCallback::call. Applies to any future
FFI callback closure (file loaders, custom platform, etc.).

Examples (`cmd/examples/`): `hello` (static, M1), `counter` (callbacks, M3),
`todo` (SliceModel + callbacks + two-way binding, M5), `clock` (Timer, M6). Each non-hello
example embeds its `ui.slint` and has a compile-smoke `*_test.go` (validates markup, no
display needed).

Go: `slintsys` (Layer 1) and `slint` (Layer 2: `Compile`, `Compilation.Create`,
`Instance.Int/Float/Bool/Str/Set`, `OnCallback`/`OnGlobalCallback`/`Invoke`/
`InvokeGlobal`/`GetGlobal`/`SetGlobal`, `Run/Quit`, `DiagnosticError`). Example:
`cmd/examples/hello`. Also `Run/Quit`, `InvokeFromEventLoop` (cross-goroutine → UI),
package docs in `doc.go`, `README.md`.

**Conformance: FULL corpus by default** (`internal/conformance` auto-discovers all 27
case dirs = 614 cases, **0 failures**). The driver sets SLINT_ENABLE_EXPERIMENTAL_FEATURES
+ configure-fonts + OS=Windows to match Slint's interpreter test driver. Non-passes are
noTest (no `test` bool) and 1 compileErr (`@library` import; library-paths unwired).
`make conformance`.

**M8 in progress.** **Windows cross-compile WORKS** (`make build-windows` from Linux →
`build/windows/{hello,counter,todo,clock}.exe` + goslint.dll, all valid PE32+). Needs the
rust target `x86_64-pc-windows-gnu` + mingw-w64 (`sudo pacman -S mingw-w64-gcc`, provides
dlltool). Per-platform cgo via `#cgo windows,amd64 → lib/windows_amd64`. Awaiting on-device
Windows validation. **Next in M8: Android** (the hard one — needs NDK install + the
android-activity/NativeActivity glue + on-device testing).

**M7 mostly done** (full corpus green, InvokeFromEventLoop, docs). Deferred: switching
`goslint.h` to cbindgen-generated (~85 fns; hand-written works fine), wiring library-paths
(1 case), Window API (title/size from Go). **Next: M8** — cross-platform packaging
(Windows/macOS libs, build tags, CI). PLAN.md §10. Licensing posture still open.
