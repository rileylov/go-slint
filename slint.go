// The package overview lives in doc.go.
package slint

import (
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	"io/fs"
	pathpkg "path"
	"regexp"
	"strings"
	"unsafe"

	"github.com/rileylov/go-slint/slintsys"
)

// Version returns the Slint version these bindings were built against.
func Version() string { return slintsys.Version() }

// InitHeadless installs the headless testing backend (mock time, no real
// windows). Intended for tests; call once per process on the UI thread.
func InitHeadless() error { return slintsys.InitHeadless() }

// As converts a dynamic property/callback value to the typed T, returning a clear
// error instead of panicking when the value isn't that type. The generated typed
// getters build on it, so an unexpected value surfaces as an error — matching the
// dynamic API (Instance.Int, etc.) rather than a panic mid-getter.
func As[T any](v any) (T, error) {
	if t, ok := v.(T); ok {
		return t, nil
	}
	var zero T
	return zero, fmt.Errorf("goslint: value has type %T, want %T", v, zero)
}

// MockElapsedTime advances the headless backend's mock clock by ms milliseconds.
func MockElapsedTime(ms uint64) { slintsys.MockElapsedTime(ms) }

// Run enters the Slint event loop until the last window closes or [Quit] is
// called. It blocks and must run on the UI thread.
func Run() error { return slintsys.RunEventLoop() }

// RunUntilQuit is like [Run] but does not exit when the last window closes — it
// runs until [Quit]. Use it for multi-window apps that open and close windows
// dynamically. Show at least one window before calling it.
func RunUntilQuit() error { return slintsys.RunEventLoopUntilQuit() }

// Quit asks the running event loop to exit, so the blocking Run call returns. It
// releases nothing on its own: windows stay alive until [Instance.Close], and
// timers stay registered — they are not cancelled, and resume firing if a loop
// runs again (Stop/Close the ones that shouldn't). Work already posted with
// [InvokeFromEventLoop] is normally drained before the loop exits, but anything
// still queued when it stops is discarded without running (GOSLINT_DEV warns), so
// do shutdown work after Run returns rather than posting it during teardown.
func Quit() error { return slintsys.QuitEventLoop() }

// InvokeFromEventLoop posts fn to run once on the event-loop (UI) thread. Safe to
// call from any goroutine; it is the only safe way to touch UI state (properties,
// models, callbacks) from a background goroutine.
func InvokeFromEventLoop(fn func()) error { return slintsys.InvokeFromEventLoop(fn) }

// PanicInfo describes a problem the Go/Slint boundary had to contain: what kind
// ([PanicInfo.Kind]), the sort of code involved ([PanicInfo.Site]), which callback
// where one applies ([PanicInfo.Name]), the panic value or error, and a stack.
type PanicInfo = slintsys.PanicInfo

// ProblemKind distinguishes the two things the boundary contains: user code that
// panicked, and an argument it refused to pass on.
type ProblemKind = slintsys.ProblemKind

const (
	// PanicRecovered: your code panicked and the call was abandoned.
	PanicRecovered = slintsys.PanicRecovered
	// InvalidArgument: a value couldn't cross the C ABI without changing meaning
	// (e.g. a negative model row count, which would become ~1.8e19 rows), so the
	// call was skipped.
	InvalidArgument = slintsys.InvalidArgument
)

// SetPanicHandler installs fn to receive every problem the callback boundary
// contains — panics in handlers, timers, models, the rendering notifier, close
// handlers, InvokeFromEventLoop work, file loaders and translators, plus
// arguments rejected on their way to C.
//
// A Go panic must never unwind through C into Rust, so panics are always
// recovered and the offending call abandoned (a callback returns void, a model
// row count reads 0). Likewise a value that can't cross the ABI intact is
// dropped rather than corrupted. By default these are reported with a stack to
// stderr; install a handler to route them elsewhere — a log, telemetry, an
// in-app error dialog — or pass nil to restore the default. Either way the call
// did not do what it was asked: treat reports as bugs to fix.
//
// fn runs on the thread where the problem happened (usually the UI thread), so
// keep it quick. A panic inside fn is itself contained.
func SetPanicHandler(fn func(PanicInfo)) { slintsys.SetPanicHandler(fn) }

// ClipboardText returns the system clipboard's text ("" if empty or unavailable).
// The clipboard is provided by the backend, so call it once a window exists.
func ClipboardText() string { return slintsys.ClipboardText() }

// SetClipboardText sets the system clipboard text.
func SetClipboardText(s string) error { return slintsys.SetClipboardText(s) }

// SetTranslator installs a function that translates `@tr("…")` source strings at
// runtime, and re-renders existing translations. Call it again to switch languages
// (e.g. with a different lookup table); the handler returns the original string when
// it has no translation. Call on the UI thread, after a window/backend exists.
func SetTranslator(fn func(msgid string) string) error { return slintsys.SetTranslator(fn) }

// ClearTranslator removes the translator so `@tr` shows its source strings again.
func ClearTranslator() { slintsys.ClearTranslator() }

// Diagnostic is a single compiler message (error, warning, or note).
type Diagnostic = slintsys.Diagnostic

// DiagnosticError reports one or more compiler errors.
type DiagnosticError struct {
	Diagnostics []Diagnostic
}

// Error implements the error interface, listing the compiler diagnostics.
func (e *DiagnosticError) Error() string {
	var b strings.Builder
	b.WriteString("slint: compilation failed")
	for _, d := range e.Diagnostics {
		if d.Level == 0 { // error
			fmt.Fprintf(&b, "\n  %s:%d:%d: %s", d.File, d.Line, d.Col, d.Message)
		}
	}
	return b.String()
}

// Compilation is a successfully compiled set of components.
type Compilation struct {
	result *slintsys.Result
}

// Option configures a compilation.
type Option func(*slintsys.Compiler)

// WithStyle selects the widget style (e.g. "fluent", "material", "cupertino").
// Required for `.slint` files that import "std-widgets.slint".
func WithStyle(style string) Option {
	return func(c *slintsys.Compiler) { c.SetStyle(style) }
}

// WithIncludePaths sets the paths used to resolve `.slint` imports.
func WithIncludePaths(paths ...string) Option {
	return func(c *slintsys.Compiler) { c.SetIncludePaths(paths) }
}

// WithLibraryPaths maps `@library` import names to their paths, e.g.
// WithLibraryPaths(map[string]string{"mylib": "./libs/mylib"}) resolves
// `import { Foo } from "@mylib/foo.slint"`.
func WithLibraryPaths(libs map[string]string) Option {
	return func(c *slintsys.Compiler) { c.SetLibraryPaths(libs) }
}

// FileLoader resolves a `.slint` import path to source; ok=false means "not found"
// (normal include-path/disk resolution then proceeds). Builtins like std-widgets
// are handled internally and never passed to it.
type FileLoader = slintsys.FileLoader

// WithFileLoader installs a fallback resolver for `.slint` imports, letting a
// multi-file component compile entirely from in-memory source (no files on disk).
// Generated typed code uses this to embed every imported file.
func WithFileLoader(fn FileLoader) Option {
	return func(c *slintsys.Compiler) { c.SetFileLoader(fn) }
}

// Compile compiles `.slint` source. It returns a [*DiagnosticError] if the
// source has errors.
func Compile(source string, opts ...Option) (*Compilation, error) {
	return finish(build(opts, func(c *slintsys.Compiler) *slintsys.Result {
		return c.BuildFromSource(source, "")
	}))
}

// CompileFile compiles a `.slint` file from disk.
func CompileFile(path string, opts ...Option) (*Compilation, error) {
	return finish(build(opts, func(c *slintsys.Compiler) *slintsys.Result {
		return c.BuildFromPath(path)
	}))
}

// CompileSource compiles markup from a string while treating it as if it lived at
// `path`, so relative imports (and @image-url) resolve from path's directory on
// disk. Generated typed code uses this so multi-file components work; for a single
// embedded file with no relative imports, plain [Compile] is enough.
func CompileSource(path, source string, opts ...Option) (*Compilation, error) {
	return finish(build(opts, func(c *slintsys.Compiler) *slintsys.Result {
		return c.BuildFromSource(source, path)
	}))
}

// CompileFS compiles the `.slint` file `entry` from `fsys` (typically an embed.FS),
// resolving every relative import through the same filesystem — so a multi-file
// component compiles entirely from embedded bytes, with nothing on disk and no temp
// files. Import paths are resolved relative to the FS root (i.e. as embedded), which
// matches `//go:embed` when the embedding .go sits alongside the markup. This is the
// self-contained compile path for generated code and for hand-rolled stateful reload.
//
// Relative `@image-url` references resolve through fsys too: embed the image files
// alongside the markup and they ship inside the binary (the interpreter otherwise
// loads images from disk at render time, which breaks once the binary leaves the
// source tree). References that don't resolve in fsys — absolute paths, data: URLs,
// files that aren't embedded — keep the interpreter's normal disk resolution.
//
//	//go:embed app.slint components/card.slint icons/logo.png
//	var ui embed.FS
//	comp, err := slint.CompileFS(ui, "app.slint")
func CompileFS(fsys fs.FS, entry string, opts ...Option) (*Compilation, error) {
	src, err := fs.ReadFile(fsys, entry)
	if err != nil {
		return nil, fmt.Errorf("slint: read entry %q: %w", entry, err)
	}
	loader := func(path string) (string, bool) {
		p := pathpkg.Clean(path)
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return "", false
		}
		return embedImages(fsys, p, string(b)), true
	}
	// Our FS loader resolves embedded imports; user-supplied opts still apply (and a
	// user WithFileLoader, coming later, would override ours).
	return CompileSource(entry, embedImages(fsys, pathpkg.Clean(entry), string(src)),
		append([]Option{WithFileLoader(loader)}, opts...)...)
}

// imageURLRe matches the path argument of `@image-url("…")`. Only the quoted
// string is captured; extra arguments (e.g. nine-slice) sit outside the match
// and survive a rewrite untouched.
var imageURLRe = regexp.MustCompile(`@image-url\s*\(\s*"([^"]+)"`)

// embedImages rewrites the relative @image-url references in one .slint source to
// data: URLs backed by fsys, so images render from embedded bytes exactly like the
// markup compiles from them. file is the source's path within fsys — its directory
// anchors relative references, matching the interpreter's own resolution rule.
// References that can't be served from fsys (data: URLs, absolute paths, ../
// escapes, files that aren't embedded) are left as written, keeping the
// interpreter's normal disk resolution for them.
func embedImages(fsys fs.FS, file, src string) string {
	if !strings.Contains(src, "@image-url") {
		return src
	}
	return imageURLRe.ReplaceAllStringFunc(src, func(m string) string {
		path := imageURLRe.FindStringSubmatch(m)[1]
		// A scheme or drive prefix (data:, http:, C:/…) or a rooted path can never
		// be an fsys key; leave those to the interpreter.
		if strings.HasPrefix(path, "/") || strings.Contains(strings.SplitN(path, "/", 2)[0], ":") {
			return m
		}
		key := pathpkg.Clean(pathpkg.Join(pathpkg.Dir(file), path))
		b, err := fs.ReadFile(fsys, key) // rejects ../ escapes by construction
		if err != nil {
			return m
		}
		return `@image-url("data:` + imageMIME(path) + `;base64,` + base64.StdEncoding.EncodeToString(b) + `"`
	})
}

// imageMIME maps an image reference's extension to the MIME type used in the
// data: URL (the decoder selects by it).
func imageMIME(path string) string {
	switch strings.ToLower(pathpkg.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg", ".svgz":
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
}

func build(opts []Option, f func(*slintsys.Compiler) *slintsys.Result) *slintsys.Result {
	c := slintsys.NewCompiler()
	defer c.Free()
	for _, o := range opts {
		o(c)
	}
	return f(c)
}

func finish(r *slintsys.Result) (*Compilation, error) {
	if !r.Valid() {
		return nil, fmt.Errorf("slint: %s", slintsys.LastError())
	}
	if r.HasErrors() {
		diags := r.Diagnostics()
		r.Free()
		return nil, &DiagnosticError{Diagnostics: diags}
	}
	return &Compilation{result: r}, nil
}

// ComponentNames lists the exported components.
func (c *Compilation) ComponentNames() []string { return c.result.ComponentNames() }

// Create instantiates the named component.
func (c *Compilation) Create(name string) (*Instance, error) {
	def := c.result.Component(name)
	if def == nil {
		return nil, fmt.Errorf("slint: no component named %q", name)
	}
	defer def.Free()
	inner, err := def.Create()
	if err != nil {
		return nil, err
	}
	return &Instance{inner: inner}, nil
}

// CreateWithWindow instantiates the named component reusing winOwner's window, so the
// new content renders in the same on-screen window. Used by live reload to swap the
// UI in place instead of opening a new window.
func (c *Compilation) CreateWithWindow(name string, winOwner *Instance) (*Instance, error) {
	def := c.result.Component(name)
	if def == nil {
		return nil, fmt.Errorf("slint: no component named %q", name)
	}
	defer def.Free()
	inner, err := def.CreateWithWindow(winOwner.inner)
	if err != nil {
		return nil, err
	}
	return &Instance{inner: inner}, nil
}

// Close releases the compilation's resources.
func (c *Compilation) Close() { c.result.Free() }

// Instance is a live component instance.
type Instance struct {
	inner *slintsys.Instance
}

// Get reads a property as a Go value (float64, bool, string, or nil).
func (i *Instance) Get(name string) (any, error) { return i.inner.GetProperty(name) }

// Set writes a property from a Go value.
func (i *Instance) Set(name string, v any) error { return i.inner.SetProperty(name, toSys(v)) }

// Int reads a numeric property as an int.
func (i *Instance) Int(name string) (int, error) {
	v, err := i.inner.GetProperty(name)
	if err != nil {
		return 0, err
	}
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("slint: property %q is not a number (got %T)", name, v)
	}
	return int(f), nil
}

// Float reads a numeric property.
func (i *Instance) Float(name string) (float64, error) {
	v, err := i.inner.GetProperty(name)
	if err != nil {
		return 0, err
	}
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("slint: property %q is not a number (got %T)", name, v)
	}
	return f, nil
}

// Bool reads a boolean property.
func (i *Instance) Bool(name string) (bool, error) {
	v, err := i.inner.GetProperty(name)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("slint: property %q is not a bool (got %T)", name, v)
	}
	return b, nil
}

// Str reads a string property.
func (i *Instance) Str(name string) (string, error) {
	v, err := i.inner.GetProperty(name)
	if err != nil {
		return "", err
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("slint: property %q is not a string (got %T)", name, v)
	}
	return s, nil
}

// Enum is the Go representation of a Slint enumeration value (e.g.
// Enum{Type: "TextHorizontalAlignment", Value: "center"}). Structs are
// represented as map[string]any.
type Enum = slintsys.Enum

// Callback is a Go handler invoked by Slint. Its args and return use the same
// Go value representation as properties (float64, bool, string, map[string]any,
// Enum, nil, ...).
type Callback = slintsys.CallbackFunc

// Model is a data source backing a Slint model property / `for` loop.
type Model = slintsys.Model

// ModelHandle binds a Model for use as a property value; report data changes via
// its Notify* methods. Obtain one with NewModel or SliceModel.Handle.
type ModelHandle = slintsys.ModelHandle

// NewModel binds a custom Model implementation so it can be assigned to a property.
func NewModel(m Model) *ModelHandle { return slintsys.NewModelHandle(m) }

// LiveModel is a model bindable to a `[T]` property so its rows update in place — a
// *SliceModel, or the *ModelHandle returned by NewModel. The generated typed
// Set<Name>Model setters (emitted only for array properties) accept it; unlike the
// snapshot Set<Name>([]T), the binding stays live (Append/SetRowData/RemoveAt flow
// to the UI per row).
type LiveModel interface{ Handle() *ModelHandle }

// SliceModel is a built-in slice-backed Model whose mutators auto-notify Slint.
type SliceModel struct {
	items  []any
	handle *slintsys.ModelHandle
}

// NewSliceModel creates a slice-backed model from the given items.
func NewSliceModel(items ...any) *SliceModel {
	s := &SliceModel{items: append([]any(nil), items...)}
	s.handle = slintsys.NewModelHandle(s)
	return s
}

// RowCount returns the number of rows. Part of the [Model] interface.
func (s *SliceModel) RowCount() int { return len(s.items) }

// RowData returns the value at row, or nil if out of range. Part of the [Model] interface.
func (s *SliceModel) RowData(row int) any {
	if row < 0 || row >= len(s.items) {
		return nil
	}
	return s.items[row]
}

// SetRowData replaces the value at row (when in range) and notifies Slint. Must be
// called on the UI thread. Part of the [Model] interface.
func (s *SliceModel) SetRowData(row int, v any) {
	slintsys.CheckUIThread("model SetRowData", "")
	if row >= 0 && row < len(s.items) {
		s.items[row] = v
		s.handle.NotifyRowChanged(row)
	}
}

// Append adds an item and notifies Slint.
func (s *SliceModel) Append(v any) {
	slintsys.CheckUIThread("model Append", "")
	s.items = append(s.items, v)
	s.handle.NotifyRowAdded(len(s.items)-1, 1)
}

// RemoveAt removes the item at row and notifies Slint.
func (s *SliceModel) RemoveAt(row int) {
	slintsys.CheckUIThread("model RemoveAt", "")
	if row < 0 || row >= len(s.items) {
		return
	}
	s.items = append(s.items[:row], s.items[row+1:]...)
	s.handle.NotifyRowRemoved(row, 1)
}

// Len reports the number of rows.
func (s *SliceModel) Len() int { return len(s.items) }

// Handle returns the binding handle (also accepted directly by property setters).
func (s *SliceModel) Handle() *ModelHandle { return s.handle }

// Close releases the model's binding handle.
func (s *SliceModel) Close() { s.handle.Close() }

// toSys maps Go-facing model wrappers to values the cgo layer understands.
func toSys(v any) any {
	if s, ok := v.(*SliceModel); ok {
		return s.handle
	}
	return v
}

func mapToSys(args []any) []any {
	out := make([]any, len(args))
	for k, a := range args {
		out[k] = toSys(a)
	}
	return out
}

// Color is an RGBA color (a `color` property, or a solid `brush`).
type Color = slintsys.Color

// Gradient is a gradient `brush` value (linear by default, or Radial); set it on
// a brush/color property, or read one back from Get. GradientStop is one stop.
type Gradient = slintsys.Gradient

// GradientStop is one stop of a [Gradient]: Pos in 0..=1 with a Color.
type GradientStop = slintsys.GradientStop

// Image is a loaded image; assign it to an `image` property and Close it when done.
//
// TODO(v1.0): make Image (and Timer) a real struct rather than a slintsys alias, so the
// public API doesn't leak Layer 1 into the v1 contract. This requires wrapping/unwrapping
// at every Value boundary (Set, Get, callback args, model rows, nested structs), so it's
// a v1.0 task — see "Toward v1.0" in CLAUDE.md — not a 0.x patch.
type Image = slintsys.Image

// DataTransfer is a drag-and-drop payload — Slint 1.17's `data-transfer` type carried
// by a DragArea. go-slint bridges its plain-text content: return one from a callback
// wired to produce a `data-transfer` (e.g. `DragArea.data: Api.makeData(...)`), and
// read it from a DropArea's DropEvent (`event.data`). A non-empty payload is required
// for a drag to start.
type DataTransfer = slintsys.DataTransfer

// NewDataTransfer builds a drag payload carrying text (e.g. an item id or JSON).
func NewDataTransfer(text string) DataTransfer { return DataTransfer{Text: text} }

// LoadImage loads an image (PNG/JPEG) from a file path.
func LoadImage(path string) (*Image, error) { return slintsys.LoadImage(path) }

// NewImage builds an image from any Go image.Image — decoded (image/png, image/jpeg),
// drawn (image/draw, plotting libs), or generated. Use this to display dynamic
// content the way Slint's SharedPixelBuffer does in Rust. The pixels are copied, so
// the source can be reused or freed afterward.
//
// It converts to non-premultiplied RGBA (Slint's expected format); note Go's
// image.RGBA is *premultiplied*, which this handles correctly.
func NewImage(src image.Image) (*Image, error) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h)) // tightly packed, non-premultiplied
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return slintsys.ImageFromRGBA(dst.Pix, w, h)
}

// NewImageRGBA builds an image from a tightly-packed RGBA8 buffer (w*h*4 bytes,
// row-major, non-premultiplied alpha). The bytes are copied.
func NewImageRGBA(pix []byte, w, h int) (*Image, error) { return slintsys.ImageFromRGBA(pix, w, h) }

// NewImageRGB builds an image from a tightly-packed RGB8 buffer (w*h*3 bytes).
func NewImageRGB(pix []byte, w, h int) (*Image, error) { return slintsys.ImageFromRGB(pix, w, h) }

// NewImageFromSVG builds an image from in-memory SVG bytes (e.g. a go:embed'd .svg).
// Slint rasterizes the SVG at render size, so it stays crisp at any scale and needs
// no file on disk — ideal for self-contained binaries and APKs, where @image-url
// can't resolve a path. Assign it to an `image` property; Close it when done.
func NewImageFromSVG(data []byte) (*Image, error) { return slintsys.ImageFromSVG(data) }

// NewImageFromData builds a raster image from in-memory encoded bytes (PNG/JPEG/…),
// decoded by Slint. format is an optional lowercase hint ("png", "jpeg", …); "" lets
// Slint auto-detect. For a Go image.Image (decoded, drawn, or generated), use
// NewImage instead.
func NewImageFromData(data []byte, format string) (*Image, error) {
	return slintsys.ImageFromData(data, format)
}

// RenderingState identifies the phase a rendering notifier is called at.
type RenderingState int

const (
	// RenderingSetup: the window's graphics context was created — initialize your
	// GL state here (e.g. go-gl's gl.InitWithProcAddrFunc(slint.GLProcAddress)).
	RenderingSetup RenderingState = slintsys.RenderingSetup
	// BeforeRendering: about to render the scene — draw underlays and upload
	// textures here (they appear beneath the UI; give the Window a transparent
	// background so they show through).
	BeforeRendering RenderingState = slintsys.BeforeRendering
	// AfterRendering: scene rendered, not yet presented — draw overlays here.
	AfterRendering RenderingState = slintsys.AfterRendering
	// RenderingTeardown: the context is going away — release GL resources.
	RenderingTeardown RenderingState = slintsys.RenderingTeardown
)

// SetRenderingNotifier registers fn to run at each rendering phase, on the UI
// thread with the window's OpenGL context current — the hook for custom GPU
// drawing under/over the UI and for zero-copy texture updates (see
// NewImageFromGLTexture). GL renderer only (femtovg, the desktop default): with
// SLINT_BACKEND=software this returns an error. OpenGL calls are valid only
// inside fn; resolve them through GLProcAddress. See examples/glunderlay and
// examples/glvideo.
func (i *Instance) SetRenderingNotifier(fn func(RenderingState)) error {
	return i.inner.SetRenderingNotifier(func(state int) { fn(RenderingState(state)) })
}

// GLProcAddress resolves an OpenGL function by name — Slint's own context loader.
// Only valid inside a rendering-notifier callback (nil elsewhere). Designed to
// plug straight into a GL binding, e.g. gl.InitWithProcAddrFunc(slint.GLProcAddress).
func GLProcAddress(name string) unsafe.Pointer { return slintsys.GLProcAddress(name) }

// NewImageFromGLTexture wraps an OpenGL 2D RGBA texture you own as an Image —
// zero-copy: Slint samples the live texture every frame, so updating the texture
// (in a rendering-notifier callback) updates what's displayed without re-sending
// pixels. This is the high-throughput path for video/game/visualization frames;
// compare NewImageRGBA, which copies the pixels on every call.
//
// Rules: create/update the texture inside the notifier callback (same GL
// context), keep it alive as long as any property shows the Image, and Close the
// Image before deleting the texture. bottomLeftOrigin flips sampling for
// FBO-rendered (bottom-up) textures.
func NewImageFromGLTexture(textureID uint32, width, height int, bottomLeftOrigin bool) (*Image, error) {
	return slintsys.ImageFromGLTexture(textureID, width, height, bottomLeftOrigin)
}

// Timer fires a Go callback after an interval. Timers fire only while the event
// loop runs (Run); create and start them after Create.
//
// TODO(v1.0): wrap in a real struct (see the Image alias note above).
type Timer = slintsys.Timer

// Timer modes.
const (
	TimerSingleShot = slintsys.TimerSingleShot
	TimerRepeated   = slintsys.TimerRepeated
)

// NewTimer creates a stopped timer.
func NewTimer() *Timer { return slintsys.NewTimer() }

// SingleShot fires fn once after the given number of milliseconds.
func SingleShot(intervalMs uint64, fn func()) { slintsys.SingleShot(intervalMs, fn) }

// OnCallback installs a handler for the named callback.
func (i *Instance) OnCallback(name string, fn Callback) error {
	return i.inner.SetCallback(name, fn)
}

// OnGlobalCallback installs a handler for a callback on an exported global.
func (i *Instance) OnGlobalCallback(global, name string, fn Callback) error {
	return i.inner.SetGlobalCallback(global, name, fn)
}

// Invoke calls a callback or function, returning its result (nil for void).
func (i *Instance) Invoke(name string, args ...any) (any, error) {
	return i.inner.Invoke(name, mapToSys(args))
}

// InvokeGlobal calls a callback or function on an exported global.
func (i *Instance) InvokeGlobal(global, name string, args ...any) (any, error) {
	return i.inner.InvokeGlobal(global, name, mapToSys(args))
}

// GetGlobal reads a property of an exported global singleton.
func (i *Instance) GetGlobal(global, name string) (any, error) {
	return i.inner.GetGlobalProperty(global, name)
}

// SetGlobal writes a property of an exported global singleton.
func (i *Instance) SetGlobal(global, name string, v any) error {
	return i.inner.SetGlobalProperty(global, name, toSys(v))
}

// Show makes the window visible without blocking. A hidden window (closed by the
// user, or hidden with Hide) can be shown again — closing never destroys it.
func (i *Instance) Show() error { return i.inner.Show() }

// Hide hides the window without running the OnCloseRequested handler. The
// instance stays alive; release it with [Instance.Close].
func (i *Instance) Hide() error { return i.inner.Hide() }

// Run shows the window and runs the event loop, blocking until the window closes
// or [Quit] is called; it then hides the window. The instance is still alive when
// Run returns — do cleanup here, and release it with [Instance.Close].
func (i *Instance) Run() error { return i.inner.Run() }

// WindowSize returns the window's size in physical pixels (divide by [Instance.ScaleFactor]
// for logical .slint pixels). Most reliable once the window is shown.
func (i *Instance) WindowSize() (w, h int) { return i.inner.WindowSize() }

// SetWindowSize sets the window's size in physical pixels.
func (i *Instance) SetWindowSize(w, h int) { i.inner.SetWindowSize(w, h) }

// WindowPosition returns the window's top-left in physical pixels. It is a no-op on
// Wayland (the compositor controls placement); it works on X11, Windows, and macOS.
func (i *Instance) WindowPosition() (x, y int) { return i.inner.WindowPosition() }

// SetWindowPosition moves the window (physical pixels). No-op on Wayland; see
// [Instance.WindowPosition].
func (i *Instance) SetWindowPosition(x, y int) { i.inner.SetWindowPosition(x, y) }

// ScaleFactor returns the window's device-pixel ratio (physical ÷ logical pixels).
func (i *Instance) ScaleFactor() float32 { return i.inner.WindowScaleFactor() }

// SetFullscreen toggles fullscreen for the window.
func (i *Instance) SetFullscreen(on bool) { i.inner.SetWindowFullscreen(on) }

// SetMaximized toggles the maximized state of the window.
func (i *Instance) SetMaximized(on bool) { i.inner.SetWindowMaximized(on) }

// SetMinimized toggles the minimized (iconified) state of the window.
func (i *Instance) SetMinimized(on bool) { i.inner.SetWindowMinimized(on) }

// RequestRedraw asks the window to repaint on the next frame.
func (i *Instance) RequestRedraw() { i.inner.RequestRedraw() }

// ResizeEdge identifies the window edge or corner an interactive resize grabs
// (see [Instance.StartSystemResize]).
type ResizeEdge int

// Resize edges/corners, matching winit's ResizeDirection.
const (
	ResizeEast ResizeEdge = iota
	ResizeNorth
	ResizeNorthEast
	ResizeNorthWest
	ResizeSouth
	ResizeSouthEast
	ResizeSouthWest
	ResizeWest
)

// StartSystemMove hands the window to the OS for an interactive move — the
// building block for dragging a frameless window (`Window { no-frame: true; }`)
// by a custom title bar. Call it from a callback fired by a TouchArea
// pointer-event while the button is down; the OS then tracks the pointer until
// release, so one call moves the window for the whole gesture. Desktop winit
// backends only (Windows, macOS, X11, Wayland); elsewhere — headless tests,
// Android — it returns an error. This is winit's drag_window escape hatch; see
// examples/frameless for the wiring.
func (i *Instance) StartSystemMove() error { return i.inner.WindowDragMove() }

// StartSystemResize is [Instance.StartSystemMove] for resizing: the OS grabs
// the pointer and resizes the window from the given edge or corner until the
// button is released (winit's drag_resize_window). Pair the grab areas with
// the matching `mouse-cursor` (ew-resize, ns-resize, nwse-resize, …) so the
// pointer advertises the resize.
func (i *Instance) StartSystemResize(edge ResizeEdge) error {
	return i.inner.WindowDragResize(int(edge))
}

// Close releases the instance. It is safe to call from inside one of this
// instance's own callbacks (or a binding-evaluated pure callback): the native
// component stays alive until the call that dispatched the callback — including
// a running [Instance.Run] loop — finishes, and is torn down there. After
// Close, other methods return errors or zero values.
func (i *Instance) Close() { i.inner.Free() }

// OnCloseRequested registers a handler invoked when the window's close is requested
// (the user clicking the close button, or RequestClose). Return true to allow the
// window to close, false to keep it open — e.g. to show a confirm dialog or save
// first. Runs on the event loop.
//
// Allowing the close HIDES the window; it does not release the instance (Show
// brings it back). Call [Instance.Close] when you're done with it.
func (i *Instance) OnCloseRequested(handler func() (allowClose bool)) {
	i.inner.OnCloseRequested(handler)
}

// RequestClose asks the window to close, running the OnCloseRequested handler — as
// if the user clicked the close button.
func (i *Instance) RequestClose() { i.inner.RequestClose() }

// RegisterFontFromPath registers a TrueType/OpenType font file so `.slint`
// `font-family` can use it. It applies to all windows (registered into the shared
// context); call it before the text using the font is shown.
func (i *Instance) RegisterFontFromPath(path string) error { return i.inner.RegisterFontFromPath(path) }

// RegisterFontFromMemory registers a font from bytes (e.g. a go:embed'd .ttf). The
// data is copied and kept for the process. (You can also just `import "font.ttf"`
// in your .slint, which the interpreter registers for you.)
func (i *Instance) RegisterFontFromMemory(data []byte) error {
	return i.inner.RegisterFontFromMemory(data)
}

// Snapshot renders the window's current contents to an image.NRGBA — handy for
// screenshots, export (encode it with image/png), or visual tests. It may re-render
// and can be slow; the window should be created (and usually shown) first.
func (i *Instance) Snapshot() (*image.NRGBA, error) {
	pix, w, h, err := i.inner.TakeSnapshot()
	if err != nil {
		return nil, err
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h)) // straight RGBA8 == NRGBA.Pix
	copy(img.Pix, pix)
	return img, nil
}

// SnapshotRGBA is like Snapshot but returns the raw straight-RGBA8 bytes (w*h*4)
// without allocating an image, for callers that handle pixels directly.
func (i *Instance) SnapshotRGBA() (pix []byte, w, h int, err error) {
	return i.inner.TakeSnapshot()
}
