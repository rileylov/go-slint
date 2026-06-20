# go-slint — Implementation Plan

Go bindings for [Slint](https://slint.dev), enabling Go programs to build Slint UIs and
ship them on Linux, Windows, macOS, and Android.

Status: **planning complete, implementation not started.**
Target Slint version: **1.17.0** (pinned; the local checkout is at `slint/`, git
`v1.14.1-2454-ga3b8095eb`). Toolchain present: cargo 1.93.1, rustc 1.93.1, go 1.26.2.

---

## 1. The decision (read this first)

**Build interpreter-based bindings via cgo, over a purpose-built Rust C-ABI shim
crate, distributed as a prebuilt shared library per platform. Add an optional typed
code-generation layer later.**

There are two ways to bind Slint, and they differ by ~10× in effort:

| | **A. Interpreter (CHOSEN)** | **B. Compiler backend (codegen)** |
|---|---|---|
| How | Load/compile `.slint` at runtime; talk to it reflectively (get/set property by name, set callback by name) | Generate Go source from `.slint` at build time, like the Rust/C++ generators |
| Rust surface we depend on | `slint-interpreter` public API — small, stable, complete | The entire low-level `i-slint-core` item-tree / property / vtable C ABI — huge, the *least* stable surface in the repo |
| Extra components to write | A thin C shim + a Go package | A whole new codegen backend **plus** a cgo binding to all of core's internal vtables |
| Type safety | Runtime, string-addressed (typed layer optional, later) | Compile-time typed Go structs |
| Precedent | **This is what Node *and* Python actually do** | Nobody ships this for a dynamic language |
| Effort | Weeks to MVP, months to parity | Multi-person-year, brutal to maintain |

The Slint maintainer (`hunger`) mused that Go "would better fit the compiler backend
model" ([#5009](https://github.com/slint-ui/slint/issues/5009)), and the team's
*aspirational* preference is generated Go code for autocomplete
([#5049](https://github.com/slint-ui/slint/discussions/5049),
[#503](https://github.com/slint-ui/slint/discussions/503)). But **both** dynamic
bindings they actually shipped (Node via napi-rs, Python via PyO3) use the
interpreter. We do the same, and we reach the "typed Go" end-goal as an *optional layer
on top* (§9 Layer 3) — without paying for route B's core-ABI binding.

Also from those discussions, two pieces of concrete guidance we adopt:
- "A C library wrapper for Go can help get this started; more work is needed to feel native." → exactly our layered design.
- **"Windows cgo compilation is too slow; compile Slint as a shared dynamic library per platform for easier cross-compilation."** → we ship prebuilt `.so`/`.dll`/`.dylib`, not a static rebuild on every `go build` (§7).

### Why "tick off every feature with tests" is realistic here
- The interpreter's public surface is small and **enumerable** (one `Value` enum, ~30
  methods on `ComponentInstance`/`Compiler`/`ComponentDefinition`).
- Slint ships **619 `.slint` conformance cases** under `slint/tests/cases/` grouped by
  feature (`callbacks/`, `models/`, `globals/`, `layout/`, `types/`, `widgets/`, …),
  each exposing a `bool` result property. We mirror Slint's `test-driver-interpreter`
  with a **Go** test-driver and drive that corpus headlessly.
- The Node and Python bindings are a behavioral **oracle** — we port their unit tests.
- A headless **testing backend** already exists (`i-slint-backend-testing`:
  `init_no_event_loop()`, `init_integration_test_with_mock_time()`,
  `mock_elapsed_time()`), so tests run in CI with no display.

---

## 2. Architecture at a glance

```
┌──────────────────────────────────────────────────────────────────┐
│ Go application      import "…/go-slint/slint"                       │
├──────────────────────────────────────────────────────────────────┤
│ Layer 3 (optional)  slintc — codegen: .slint → typed Go accessors  │
├──────────────────────────────────────────────────────────────────┤
│ Layer 2  package `slint`   idiomatic Go: Value↔any, Model iface,   │
│                            typed helpers, event loop, lifetimes     │
├──────────────────────────────────────────────────────────────────┤
│ Layer 1  package `slintsys` (cgo)   1:1 with the C ABI; handles,    │
│                            marshalling, cgo.Handle callback registry │
├──────────────────────────────────────────────────────────────────┤
│ Layer 0  crate `goslint-sys` (Rust → staticlib + cdylib)           │
│          flat extern "C" over `slint_interpreter::*`               │
│          (char*, double, void*, fn-ptrs — NO Slint internal ABI)   │
├──────────────────────────────────────────────────────────────────┤
│ Upstream  slint-interpreter → i-slint-{core,compiler,backend-*}     │
└──────────────────────────────────────────────────────────────────┘
```

**Critical principle:** Layer 0 depends only on the *safe Rust* API of
`slint-interpreter` (`Compiler`, `CompilationResult`, `ComponentDefinition`,
`ComponentInstance`, `Value`, `Struct`, `run_event_loop`, `invoke_from_event_loop`,
`quit_event_loop`). It converts to/from plain C types at the boundary. It does **not**
link against `slint`'s existing `internal/interpreter/ffi.rs`, because that speaks the
internal ABI (`SharedString`, `SharedVector`, `Slice`, `Box<Value>`, size-asserted
opaque blobs) which `AGENTS.md` declares **not semver-stable**. We use `ffi.rs` only as
a *reference implementation* — it proves every operation we need is reachable and shows
the exact callback (`user_data`+`drop`) and model-vtable (`ModelAdaptorVTable`) patterns.

---

## 3. Reference map (where to copy patterns from)

| Need | Authoritative source in `slint/` |
|---|---|
| Public API to wrap | `internal/interpreter/api.rs` (`Value` enum @ line ~98; `Compiler` @ ~833; `CompilationResult` @ ~1010; `ComponentDefinition` @ ~1108; `ComponentInstance` get/set/callback/invoke/globals @ ~1393; `run_event_loop` @ ~1821) |
| C-ABI patterns (callback userdata+drop, model vtable, struct iter) | `internal/interpreter/ffi.rs` (whole file — `CallbackUserData` @ ~425, `ModelAdaptorVTable` @ ~640) |
| FFI conventions, cbindgen, opaque types, feature gating | `docs/development/ffi-language-bindings.md` |
| Backend/renderer feature wiring | `internal/interpreter/Cargo.toml` (`default = backend-default, renderer-femtovg, renderer-software, …`); `api/python/slint/Cargo.toml` (closest template: `crate-type=["cdylib","rlib"]`, depends on `slint-interpreter` default features + backend-selector) |
| Value↔host conversions (every variant) | `api/python/slint/value.rs`, `api/node/rust/interpreter/value.rs` |
| Go-backed model bridge | `api/node/rust/types/model.rs` (`JsModel impl Model`), `api/python/slint/models.rs` |
| Callback across boundary | `api/node/.../component_instance.rs::make_callback_handler`, `api/python/.../interpreter.rs::GcVisibleCallbacks` |
| Event-loop integration (pump vs block) | `api/node/rust/uv_event_loop.rs` (`process_events`), `api/python/slint/lib.rs` (`run_event_loop` + `py.detach`) |
| Headless test driving | `internal/backends/testing/lib.rs`; test event injection seen in `api/node/.../component_instance.rs` (`send_mouse_click`, `send_keyboard_string_sequence`, `send_key_combo`) |
| Conformance corpus | `slint/tests/cases/**/*.slint` (619 files) + `slint/tests/run_tests.sh` |
| Android entry | `api/cpp/lib.rs` (@47 `android_main` → `set_platform(AndroidPlatform::new(app))` → `slint_main()`); `internal/backends/android-activity/lib.rs` |

---

## 4. Layer 0 — the Rust shim crate `goslint-sys`

`crate-type = ["staticlib", "cdylib"]`. Header generated with cbindgen into
`include/goslint.h`.

```toml
# rust/goslint-sys/Cargo.toml  (sketch)
[dependencies]
slint-interpreter = { path = "../../slint/internal/interpreter", default-features = true,
                      features = ["display-diagnostics", "internal"] }   # default pulls backend+femtovg+software
spin_on = "0.1"                                                          # drive the async build_from_*
[target.'cfg(target_os="android")'.dependencies]
i-slint-backend-android-activity = { path = "../../slint/internal/backends/android-activity",
                                     features = ["native-activity"] }
```

### 4.1 Mandatory conventions
- **Every `extern "C"` body wraps its work in `std::panic::catch_unwind`** and converts a
  panic into an error status. Unwinding across the C boundary is UB — non-negotiable.
- **Errors:** handle/Value-returning functions return `NULL` on failure; status-returning
  functions return `int` (0=ok). Detail via thread-local
  `goslint_last_error() -> *const c_char` (+ `goslint_string_free`).
- **String ownership:** inbound `const char*` are borrowed (Rust copies). Outbound
  `char*` are heap-owned by Rust and **must** be freed with `goslint_string_free`.
- **Opaque handles:** `*mut Compiler`, `*mut CompilationResult`, `*mut ComponentDefinition`,
  `*mut ComponentInstance`, `*mut Value`, `*mut GoStruct`, `*mut GoModel`, `*mut Timer`.
  Each has a `_free`/`_destroy`. Document thread-affinity (most are UI-thread-only).

### 4.2 C ABI surface (the contract to implement)

```c
/* ---- global / event loop ---- */
const char* goslint_version(void);
int   goslint_testing_init_headless(void);        /* i_slint_backend_testing::init_no_event_loop */
int   goslint_testing_init_mock_time(void);
void  goslint_testing_mock_elapsed_time(uint64_t ms);
int   goslint_run_event_loop(bool quit_on_last_window_closed);   /* blocks; UI thread only */
int   goslint_quit_event_loop(void);
int   goslint_invoke_from_event_loop(void (*cb)(void* ud), void* ud);   /* thread-safe post */
int   goslint_process_events(int64_t timeout_ms);                /* non-blocking pump (Node-style) */
const char* goslint_last_error(void);
void  goslint_string_free(char*);

/* ---- compiler ---- */
Compiler* goslint_compiler_new(void);
void      goslint_compiler_free(Compiler*);
void      goslint_compiler_set_style(Compiler*, const char*);
void      goslint_compiler_set_include_paths(Compiler*, const char* const* paths, size_t n);
void      goslint_compiler_set_library_paths(Compiler*, const char* const* names,
                                             const char* const* paths, size_t n);
void      goslint_compiler_set_translation_domain(Compiler*, const char*);
CompilationResult* goslint_compiler_build_from_source(Compiler*, const char* src, const char* path);
CompilationResult* goslint_compiler_build_from_path(Compiler*, const char* path);

/* ---- compilation result / diagnostics ---- */
bool   goslint_result_has_errors(const CompilationResult*);
size_t goslint_result_diagnostic_count(const CompilationResult*);
void   goslint_result_diagnostic(const CompilationResult*, size_t i, int* level,
                                 char** message, char** file, uint32_t* line, uint32_t* col);
size_t goslint_result_component_count(const CompilationResult*);
char*  goslint_result_component_name(const CompilationResult*, size_t i);
ComponentDefinition* goslint_result_component(const CompilationResult*, const char* name);
void   goslint_result_free(CompilationResult*);

/* ---- definition (introspection + factory) ---- */
char*  goslint_definition_name(const ComponentDefinition*);
/* properties()/callbacks()/functions()/globals()/global_* -> count + getter pairs */
ComponentInstance* goslint_definition_create(const ComponentDefinition*);
void   goslint_definition_free(ComponentDefinition*);

/* ---- instance ---- */
Value* goslint_instance_get_property(const ComponentInstance*, const char* name);
int    goslint_instance_set_property(const ComponentInstance*, const char* name, const Value*);
Value* goslint_instance_invoke(const ComponentInstance*, const char* name,
                               const Value* const* args, size_t n);
int    goslint_instance_set_callback(const ComponentInstance*, const char* name,
                                     Value* (*cb)(void* ud, const Value* const* args, size_t n),
                                     void* ud, void (*drop_ud)(void* ud));
/* + get/set_global_property, set_global_callback, invoke_global (same shapes) */
int    goslint_instance_show(const ComponentInstance*);
int    goslint_instance_hide(const ComponentInstance*);
int    goslint_instance_run(const ComponentInstance*);   /* show + run_event_loop + hide */
Window* goslint_instance_window(const ComponentInstance*);
void   goslint_instance_free(ComponentInstance*);

/* ---- Value (marshalling core) ---- */
Value* goslint_value_new_void(void);
Value* goslint_value_new_double(double);
Value* goslint_value_new_bool(bool);
Value* goslint_value_new_string(const char* utf8);
Value* goslint_value_new_enum(const char* enum_name, const char* value);
Value* goslint_value_new_struct(const GoStruct*);
Value* goslint_value_new_model(GoModel*);                /* takes ownership */
Value* goslint_value_new_brush_solid(uint8_t r,uint8_t g,uint8_t b,uint8_t a);
Value* goslint_value_new_image_from_path(const char* path);
int    goslint_value_type(const Value*);                 /* ValueType discriminant */
bool   goslint_value_as_double(const Value*, double* out);
bool   goslint_value_as_bool(const Value*, bool* out);
char*  goslint_value_as_string(const Value*);            /* NULL if not string */
GoStruct* goslint_value_as_struct(const Value*);
/* value_as_model -> read access (row_count/row_data); value_as_brush_rgba; value_as_enum */
Value* goslint_value_clone(const Value*);
bool   goslint_value_eq(const Value*, const Value*);
void   goslint_value_free(Value*);

/* ---- Struct ---- */
GoStruct* goslint_struct_new(void);
GoStruct* goslint_struct_clone(const GoStruct*);
void   goslint_struct_free(GoStruct*);
Value* goslint_struct_get_field(const GoStruct*, const char* name);   /* owned */
void   goslint_struct_set_field(GoStruct*, const char* name, const Value*);
/* iterator: make_iter / iter_next(out_key,out_value) / iter_free */

/* ---- Model: Go-backed (vtable bridge, mirrors ModelAdaptorVTable) ---- */
GoModel* goslint_model_new(size_t (*row_count)(void* ud),
                           Value* (*row_data)(void* ud, size_t row),   /* owned, NULL=None */
                           void (*set_row_data)(void* ud, size_t row, Value*),
                           void* ud, void (*drop_ud)(void* ud));
void   goslint_model_notify_row_changed(GoModel*, size_t row);
void   goslint_model_notify_row_added(GoModel*, size_t row, size_t count);
void   goslint_model_notify_row_removed(GoModel*, size_t row, size_t count);
void   goslint_model_notify_reset(GoModel*);

/* ---- Window (subset; grow later) ---- */
void   goslint_window_set_title(Window*, const char*);
void   goslint_window_set_size(Window*, float w, float h);   /* + scale, position, request_redraw */
/* test injection: goslint_window_dispatch_pointer / _key_sequence  (for the test driver) */

/* ---- Timer ---- */
Timer* goslint_timer_new(void);
void   goslint_timer_start(Timer*, int mode, uint64_t interval_ms,
                           void (*cb)(void* ud), void* ud, void (*drop_ud)(void* ud));
void   goslint_timer_stop(Timer*); void goslint_timer_free(Timer*);
```

(Brush/Color/Image accessors and richer Window/Timer come in M6; the rest is M1–M5.)

---

## 5. Layer 1 — low-level Go package `slintsys` (cgo)

- `#cgo CFLAGS: -I${SRCDIR}/../include`
  `#cgo linux LDFLAGS: -L${SRCDIR}/../lib/linux_amd64 -lgoslint -Wl,-rpath,$ORIGIN/lib`
  (+ per-OS blocks; see §7).
- **Callbacks (the cgo gotcha):** Go pointers may not be stored in C. Use
  `runtime/cgo.Handle` — store each Go closure, pass the `uintptr` handle as `void* ud`.
  A single `//export goslintTrampoline` (and `…ModelRowCount`, etc.) receives
  `(ud, args)`, recovers the closure via `cgo.Handle(ud).Value()`, calls it. `drop_ud`
  releases the handle. **No raw Go pointer ever crosses into C.**
- **Marshalling:** `cValue(any) *C.Value` / `goValue(*C.Value) any` covering every
  `ValueType`. Free every owned `*C.Value`/`char*` (helpers + `defer`).
- **Lifetimes:** wrapper structs hold the C handle; provide explicit `Close()`. Optionally
  `runtime.SetFinalizer` as a backstop, but explicit close is primary (finalizers can run
  on the wrong thread — and Slint is thread-affine).

## 6. Layer 2 — idiomatic Go package `slint`

Target ergonomics (subject to refinement during M7):

```go
package main

import _ "embed"
import "github.com/USER/go-slint/slint"

//go:embed app.slint
var ui string

func main() {
    app, err := slint.Compile(ui)            // or slint.CompileFile("app.slint")
    must(err)
    win, _ := app.Component("AppWindow").Create()

    win.SetProperty("counter", 0)
    win.OnCallback("increment", func(args []slint.Value) slint.Value {
        n, _ := win.Int("counter")
        win.SetProperty("counter", n+1)
        return slint.Void()
    })

    rows := slint.NewSliceModel([]string{"a", "b"})  // implements slint.Model
    win.SetProperty("items", rows)
    rows.Append("c")                                  // pushes notify to UI

    win.Run()   // locks OS thread, runs the event loop
}
```

- `Value` = `any` with typed getters, or a small sealed type; decide in M7 from the
  Node/Python ergonomics.
- `Model` interface `{ RowCount() int; RowData(int) Value; SetRowData(int, Value) }` +
  `SliceModel`/`VecModel` helpers backed by the Layer-0 vtable.
- `slint.InvokeFromEventLoop(func())` for cross-goroutine UI mutation; document the
  golden rule: **all UI access happens on the loop thread** (`Run` calls
  `runtime.LockOSThread`).
- `.slint` is embedded with `go:embed` and compiled at startup (no external file needed
  at runtime). Build-time typed compilation = Layer 3, optional.

---

## 7. Platforms & build/distribution

Adopt the maintainer's advice: **build the Rust lib once per target and ship it**;
`go build` just links it. cgo never rebuilds Rust.

```
lib/<goos>_<goarch>/libgoslint.{a,so,dylib}   # checked in or fetched by `make libs`
include/goslint.h
```

Selection via per-OS `#cgo` blocks (and build tags for static-vs-dynamic). A
`build.go`/`Makefile`/mage target orchestrates: `cargo build -p goslint-sys
--release [--target …]` → cbindgen header → copy into `lib/<os>_<arch>/`.

| Target | Backend / renderer | Lib | System deps / notes |
|---|---|---|---|
| linux/amd64,arm64 | winit + femtovg (+software) | `.so` (prefer dynamic) or `.a` | fontconfig, libGL/EGL, X11+Wayland (`backend-winit` pulls both). |
| windows/amd64 | winit + femtovg/skia | **`.dll`** (dynamic — cgo static link is too slow per maintainers) | MSVC vs mingw: pick one toolchain; document. |
| darwin/amd64,arm64 | winit + femtovg/skia | `.dylib` | Cocoa/Metal frameworks pulled by Rust; set `-rpath @loader_path/lib`. |
| android/arm64,arm,amd64 | **android-activity (native-activity) + skia** | `.so` per ABI | **Hard.** NDK clang as `CC` for cgo + `cargo --target aarch64-linux-android`; JDK (`javac`); APK with NativeActivity glue; entry `android_main` (§8). |
| ios/arm64 (later) | winit + skia | `.a` | link into Xcode app; separate milestone. |

Cross-compiling cgo requires the target C toolchain (set `CC`/`CXX`); pair with
`cargo --target`. CI matrix builds all desktop libs; Android/iOS on dedicated runners.

---

## 8. Android (separate, high-risk milestone — M9)

The single hardest part; do a spike before committing to a shape.

- Slint's Android model: the app is a **`cdylib` `.so`** loaded by an
  `android-activity` `NativeActivity`. Rust's `android_main(app)` calls
  `set_platform(AndroidPlatform::new(app))` then your `slint_main()`.
- The conflict: **two runtimes** (Go's scheduler + the Android/Rust event loop) in one
  `.so`. Options to evaluate in the spike:
  1. **Rust owns `android_main`**, then calls into an exported Go function (Go built as
     `-buildmode=c-archive`, linked into the `goslint-sys` cdylib). Go runs its init on
     first call; UI work marshalled to the loop thread.
  2. **gomobile-style**: Go owns the `.so` (`-buildmode=c-shared`), Rust linked in, and a
     thin Kotlin/Java `NativeActivity` subclass bootstraps. More Go-native, but you must
     reproduce android-activity's lifecycle plumbing.
- Deliverable for M9-MVP: "hello window" APK on an arm64 emulator, built reproducibly.

---

## 9. Optional Layer 3 — typed codegen (`slintc`)

Reaches the maintainers' "type safety + autocomplete" goal **without** route B's
core-ABI binding:
- Parse `.slint` (reuse `ComponentDefinition.properties()/callbacks()/globals()` metadata,
  already exposed) and emit a typed Go file: a struct per component with
  `Counter() int` / `SetCounter(int)` / `OnIncrement(func(...))` wrappers that call Layer 2
  by name under the hood. Pure codegen over the reflective core — cheap and decoupled.
- Ship as `go:generate`-friendly CLI. Strictly additive; never required.

---

## 10. Milestones (each shippable; the "tick-off" plan)

- **M0 — Toolchain spike.** `goslint-sys` builds to staticlib; `slintsys` cgo-links it;
  `slint.Version()` returns the Slint version from Go. Proves cargo→cbindgen→cgo on Linux.
- **M1 — Compile & show.** compiler build_from_source/path, diagnostics, definition,
  create, `Run`, show/hide, `goslint_testing_init_headless`. → "hello window" + first
  headless test.
- **M2 — Properties & scalar values.** get/set; `Value` Void/Number/Bool/String round-trips;
  diagnostics surfaced as Go errors. Stand up the **Go test-driver** over a `types/`+
  `properties/` subset of `tests/cases`.
- **M3 — Callbacks, invoke, globals.** `cgo.Handle` trampoline; set_callback/invoke;
  global get/set/callback/invoke. Run `callbacks/` + `globals/` cases.
- **M4 — Structs & enums.** struct get/set/iter; enum conversion. Run `types/` cases.
- **M5 — Models.** Go-backed model vtable + notifications; read Slint-returned models;
  `SliceModel`. Run `models/` cases.
- **M6 — Graphics & timers.** brush/color, images (path/SVG/buffer), `Timer`. Run
  `text/`, relevant `widgets/`.
- **M7 — Idiomatic layer + green corpus.** finalize Layer 2 API; examples (todo, gallery
  port); **all 619 `tests/cases` green** under the testing backend; port key Node/Python
  unit tests; screenshot tests via software renderer (`SLINT_CREATE_SCREENSHOTS`).
- **M8 — Desktop packaging.** prebuilt libs for linux/windows/macos; build tags; CI
  matrix; `go install`-able examples.
- **M9 — Android** (spike → hello-window APK; §8).
- **M10 — Optional:** iOS; `slintc` typed codegen (§9).

Definition of "feature ticked off": a `tests/cases` group passes in the Go driver **and**
a focused Go unit test covers the Go-facing API for it.

---

## 11. Risks & mitigations

- **Internal-ABI churn** — *Mitigated by design*: shim binds only the safe Rust API; pin
  Slint to a tag; bump deliberately; CI rebuilds shim against the pin.
- **Panics across FFI (UB)** — `catch_unwind` in every `extern "C"` body. Mandatory.
- **cgo callback rules** — `cgo.Handle` only; never store Go pointers in C; one trampoline
  per callback shape.
- **Thread affinity / event loop** — `LockOSThread` in `Run`; `InvokeFromEventLoop` for
  cross-thread; testing backend in CI; document the golden rule loudly.
- **Windows/Android build pain** — ship prebuilt dynamic libs (per maintainer advice);
  pin toolchains; isolate Android as its own milestone with a spike gate.
- **Memory ownership bugs** — single documented ownership convention (§4.1); test under
  Go race detector + ASAN/valgrind on the Rust side.
- **Licensing (surface to the user before shipping):** Slint is tri-licensed
  (GPL-3.0 / Royalty-free / paid commercial). Bindings + any linked app inherit those
  terms. Decide the license posture early — it affects whether/how this can be released
  and used. **Open question for the user.**

---

## 12. Proposed repo layout

```
go-slint/
  PLAN.md  CLAUDE.md  README.md  LICENSE
  slint/                      # upstream checkout, PINNED (build dep; has no .go files → ignored by go)
  rust/goslint-sys/           # Layer 0 shim crate
    Cargo.toml  cbindgen.toml  src/{lib,value,model,compiler,instance,timer,error}.rs
  include/goslint.h           # generated
  lib/<goos>_<goarch>/        # prebuilt libgoslint.{a,so,dylib}
  slintsys/                   # Layer 1 (cgo)
  slint.go … (package slint)  # Layer 2 idiomatic (module root)
  cmd/examples/{hello,todo,gallery}/
  internal/testdriver/        # Go driver over slint/tests/cases (mirrors test-driver-interpreter)
  cmd/slintc/                 # Layer 3 (optional, later)
  Makefile | build.go | magefile.go
```
Module path e.g. `github.com/USER/go-slint`; idiomatic package `slint` at root,
low-level `slintsys`. (Consider `git mv slint third_party/slint` purely for clarity;
not required — Go ignores dirs without `.go` files.)

---

## 13. First actions for the implementing session (start at M0)

1. `cargo build -p slint-interpreter` inside `slint/` to confirm the workspace builds
   here and prime the cache.
2. Scaffold `rust/goslint-sys` (Cargo.toml per §4, `crate-type=["staticlib","cdylib"]`,
   dep on the local `slint-interpreter` with default features). Implement
   `goslint_version` + `goslint_last_error` + `goslint_string_free` with `catch_unwind`.
3. cbindgen config → emit `include/goslint.h`. `cargo build --release`; copy artifacts to
   `lib/linux_amd64/`.
4. Scaffold Go module; `slintsys` with the `#cgo` directives; call `goslint_version` from a
   `slint.Version()`; a Go test asserting it equals `1.17.0`. **That green test is M0.**
5. Proceed to M1 (compiler→create→run + headless test) following §4.2 / §10.
