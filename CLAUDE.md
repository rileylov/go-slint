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
make test    # make lib, then: go test . ./slintsys/
```
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
**M0–M5 complete** (the full data layer: scalars, structs, enums, models). Shim built
with `backend-winit` + `renderer-software` + the headless testing backend (`internal`
feature → `configure_test_fonts`).

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

Examples (`cmd/examples/`): `hello` (static, M1), `counter` (callbacks, M3),
`todo` (SliceModel + callbacks + two-way binding, M5). Each non-hello example embeds its
`ui.slint` and has a compile-smoke `*_test.go` (validates markup, no display needed).

Go: `slintsys` (Layer 1) and `slint` (Layer 2: `Compile`, `Compilation.Create`,
`Instance.Int/Float/Bool/Str/Set`, `OnCallback`/`OnGlobalCallback`/`Invoke`/
`InvokeGlobal`/`GetGlobal`/`SetGlobal`, `Run/Quit`, `DiagnosticError`). Example:
`cmd/examples/hello`. Conformance: `internal/conformance` (`make conformance`),
0 failures across the self-contained dirs.

**Next: M6** — graphics & timers: brush/color (rgba), images (path/SVG/buffer),
`Timer`. Then M7 (idiomatic polish + full corpus green + examples) and M8 (cross-platform
packaging) per PLAN.md §10. Introduce cbindgen when the header grows further (it's ~60
functions now).
