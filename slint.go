// The package overview lives in doc.go.
package slint

import (
	"fmt"
	"image"
	"image/draw"
	"io/fs"
	pathpkg "path"
	"strings"

	"github.com/rileylov/go-slint/slintsys"
)

// Version returns the Slint version these bindings were built against.
func Version() string { return slintsys.Version() }

// InitHeadless installs the headless testing backend (mock time, no real
// windows). Intended for tests; call once per process on the UI thread.
func InitHeadless() error { return slintsys.InitHeadless() }

// MockElapsedTime advances the headless backend's mock clock by ms milliseconds.
func MockElapsedTime(ms uint64) { slintsys.MockElapsedTime(ms) }

// Run enters the Slint event loop until the last window closes or [Quit] is
// called. It blocks and must run on the UI thread.
func Run() error { return slintsys.RunEventLoop() }

// RunUntilQuit is like [Run] but does not exit when the last window closes — it
// runs until [Quit]. Use it for multi-window apps that open and close windows
// dynamically. Show at least one window before calling it.
func RunUntilQuit() error { return slintsys.RunEventLoopUntilQuit() }

// Quit asks the running event loop to exit.
func Quit() error { return slintsys.QuitEventLoop() }

// InvokeFromEventLoop posts fn to run once on the event-loop (UI) thread. Safe to
// call from any goroutine; it is the only safe way to touch UI state (properties,
// models, callbacks) from a background goroutine.
func InvokeFromEventLoop(fn func()) error { return slintsys.InvokeFromEventLoop(fn) }

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

// DiagnosticError reports one or more compiler errors.
type DiagnosticError struct {
	Diagnostics []slintsys.Diagnostic
}

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
//	//go:embed app.slint components/card.slint
//	var ui embed.FS
//	comp, err := slint.CompileFS(ui, "app.slint")
func CompileFS(fsys fs.FS, entry string, opts ...Option) (*Compilation, error) {
	src, err := fs.ReadFile(fsys, entry)
	if err != nil {
		return nil, fmt.Errorf("slint: read entry %q: %w", entry, err)
	}
	loader := func(path string) (string, bool) {
		b, err := fs.ReadFile(fsys, pathpkg.Clean(path))
		if err != nil {
			return "", false
		}
		return string(b), true
	}
	// Our FS loader resolves embedded imports; user-supplied opts still apply (and a
	// user WithFileLoader, coming later, would override ours).
	return CompileSource(entry, string(src), append([]Option{WithFileLoader(loader)}, opts...)...)
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

func (s *SliceModel) RowCount() int { return len(s.items) }

func (s *SliceModel) RowData(row int) any {
	if row < 0 || row >= len(s.items) {
		return nil
	}
	return s.items[row]
}

func (s *SliceModel) SetRowData(row int, v any) {
	if row >= 0 && row < len(s.items) {
		s.items[row] = v
		s.handle.NotifyRowChanged(row)
	}
}

// Append adds an item and notifies Slint.
func (s *SliceModel) Append(v any) {
	s.items = append(s.items, v)
	s.handle.NotifyRowAdded(len(s.items)-1, 1)
}

// RemoveAt removes the item at row and notifies Slint.
func (s *SliceModel) RemoveAt(row int) {
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
func (s *SliceModel) Close() { s.handle.Free() }

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

// Image is a loaded image; assign it to an `image` property and Free it when done.
type Image = slintsys.Image

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

// Timer fires a Go callback after an interval. Timers fire only while the event
// loop runs (Run); create and start them after Create.
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

// Show makes the window visible. Hide hides it. Run shows then runs the loop.
func (i *Instance) Show() error { return i.inner.Show() }
func (i *Instance) Hide() error { return i.inner.Hide() }
func (i *Instance) Run() error  { return i.inner.Run() }

// Window control. Sizes and positions are in physical pixels; divide by
// ScaleFactor to get logical (.slint) pixels. These act on the instance's window
// and are most reliable once it's shown.
//
// Note: on Wayland the compositor controls window placement, so
// SetWindowPosition/WindowPosition are no-ops there (they work on X11, Windows,
// and macOS). Run on X11 — e.g. with WAYLAND_DISPLAY unset — to use positioning.
func (i *Instance) WindowSize() (w, h int)     { return i.inner.WindowSize() }
func (i *Instance) SetWindowSize(w, h int)     { i.inner.SetWindowSize(w, h) }
func (i *Instance) WindowPosition() (x, y int) { return i.inner.WindowPosition() }
func (i *Instance) SetWindowPosition(x, y int) { i.inner.SetWindowPosition(x, y) }
func (i *Instance) ScaleFactor() float32       { return i.inner.WindowScaleFactor() }
func (i *Instance) SetFullscreen(on bool)      { i.inner.SetWindowFullscreen(on) }
func (i *Instance) SetMaximized(on bool)       { i.inner.SetWindowMaximized(on) }
func (i *Instance) SetMinimized(on bool)       { i.inner.SetWindowMinimized(on) }
func (i *Instance) RequestRedraw()             { i.inner.RequestRedraw() }

// Close releases the instance.
func (i *Instance) Close() { i.inner.Free() }

// OnCloseRequested registers a handler invoked when the window's close is requested
// (the user clicking the close button, or RequestClose). Return true to allow the
// window to close, false to keep it open — e.g. to show a confirm dialog or save
// first. Runs on the event loop.
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
