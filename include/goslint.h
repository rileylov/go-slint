/* goslint.h — C ABI for the go-slint shim (Layer 0).
 *
 * Hand-written through M1; cbindgen takes over as the surface grows (PLAN.md §4).
 *
 * Ownership: every char* and every handle (GoValue*, GoCompiler*, ...) returned
 * by this library is heap-owned by the library and must be released with the
 * matching *_free (strings: goslint_string_free). A NULL handle / NULL char*
 * return means failure; call goslint_last_error() for detail. Inbound strings
 * are borrowed (copied internally). All component/instance/value calls are
 * affine to a single OS thread (the UI thread). */
#ifndef GOSLINT_H
#define GOSLINT_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Opaque handles. */
typedef struct GoValue GoValue;
typedef struct GoStruct GoStruct;
typedef struct GoModel GoModel;
typedef struct GoImage GoImage;
typedef struct GoTimer GoTimer;
typedef struct GoCompiler GoCompiler;
typedef struct GoCompilationResult GoCompilationResult;
typedef struct GoComponentDefinition GoComponentDefinition;
typedef struct GoComponentInstance GoComponentInstance;

/* ---- library / diagnostics-as-strings ---- */
char *goslint_version(void);
char *goslint_last_error(void);
void  goslint_string_free(char *s);
char *goslint_smoke_compile(void);

/* ---- event loop & platform ---- */
int  goslint_testing_init_headless(void);          /* 0 = ok */
int  goslint_testing_init_integration(void);       /* simple loop, system time */
void goslint_testing_mock_elapsed_time(uint64_t ms);
void goslint_testing_configure_fonts(void);
void goslint_testing_set_os_windows(void);
int  goslint_run_event_loop(void);                 /* blocks; UI thread only */
int  goslint_run_event_loop_until_quit(void);      /* blocks; does not quit on last window close */
int  goslint_quit_event_loop(void);
int  goslint_invoke_from_event_loop(void (*cb)(uintptr_t), uintptr_t handle, void (*drop)(uintptr_t));

/* system clipboard (needs a backend; works after the first window / init_headless) */
char *goslint_clipboard_get_text(void);             /* owned (goslint_string_free) or NULL if empty */
int   goslint_clipboard_set_text(const char *text); /* 0 ok, 1 on failure */

/* ---- compiler ---- */
GoCompiler *goslint_compiler_new(void);
void        goslint_compiler_free(GoCompiler *c);
void        goslint_compiler_set_style(GoCompiler *c, const char *style);
void        goslint_compiler_set_include_paths(GoCompiler *c, const char *const *paths, size_t n);
void        goslint_compiler_set_library_paths(GoCompiler *c, const char *const *names, const char *const *paths, size_t n);
/* fallback import loader: returns malloc'd source (freed by the library) or NULL */
typedef char *(*GoFileLoaderLoad)(uintptr_t handle, const char *path);
void        goslint_compiler_set_file_loader(GoCompiler *c, uintptr_t handle, GoFileLoaderLoad load, void (*drop)(uintptr_t));
GoCompilationResult *goslint_compiler_build_from_source(GoCompiler *c, const char *src, const char *path);
GoCompilationResult *goslint_compiler_build_from_path(GoCompiler *c, const char *path);

/* ---- compilation result ---- */
bool   goslint_result_has_errors(const GoCompilationResult *r);
size_t goslint_result_diagnostic_count(const GoCompilationResult *r);
/* level: 0=error 1=warning 2=note. message/file are owned out-strings (free
 * each; file may be set to NULL). Any out-pointer may be NULL to skip it. */
void   goslint_result_diagnostic(const GoCompilationResult *r, size_t i,
                                 int32_t *level, char **message, char **file,
                                 uint32_t *line, uint32_t *col);
size_t goslint_result_component_count(const GoCompilationResult *r);
char  *goslint_result_component_name(const GoCompilationResult *r, size_t i);
GoComponentDefinition *goslint_result_component(const GoCompilationResult *r, const char *name);
/* JSON of a component's typed interface (for `goslint generate`); owned string */
char *goslint_definition_type_info(const GoComponentDefinition *d);
void   goslint_result_free(GoCompilationResult *r);

/* ---- component definition ---- */
char *goslint_definition_name(const GoComponentDefinition *d);
GoComponentInstance *goslint_definition_create(const GoComponentDefinition *d);
void  goslint_definition_free(GoComponentDefinition *d);

/* ---- component instance ---- */
GoValue *goslint_instance_get_property(const GoComponentInstance *i, const char *name);
int      goslint_instance_set_property(const GoComponentInstance *i, const char *name, const GoValue *v);
int      goslint_instance_show(const GoComponentInstance *i);
int      goslint_instance_hide(const GoComponentInstance *i);
int      goslint_instance_run(const GoComponentInstance *i);
void     goslint_instance_free(GoComponentInstance *i);

/* ---- window control (physical pixels) ---- */
int      goslint_instance_window_size(const GoComponentInstance *i, uint32_t *w, uint32_t *h);
void     goslint_instance_window_set_size(const GoComponentInstance *i, uint32_t w, uint32_t h);
int      goslint_instance_window_position(const GoComponentInstance *i, int32_t *x, int32_t *y);
void     goslint_instance_window_set_position(const GoComponentInstance *i, int32_t x, int32_t y);
float    goslint_instance_window_scale_factor(const GoComponentInstance *i);
void     goslint_instance_window_set_fullscreen(const GoComponentInstance *i, bool on);
void     goslint_instance_window_set_maximized(const GoComponentInstance *i, bool on);
void     goslint_instance_window_set_minimized(const GoComponentInstance *i, bool on);
void     goslint_instance_window_request_redraw(const GoComponentInstance *i);
/* close handling: cb returns true to allow close (window hides), false to keep open */
typedef bool (*GoCloseRequested)(uintptr_t handle);
void     goslint_instance_on_close_requested(const GoComponentInstance *i, uintptr_t handle, GoCloseRequested cb, void (*drop)(uintptr_t));
void     goslint_instance_request_close(const GoComponentInstance *i);
/* custom fonts (for `font-family`); 0 on success, 1 on failure (see goslint_last_error) */
int      goslint_instance_register_font_from_path(const GoComponentInstance *i, const char *path);
int      goslint_instance_register_font_from_memory(const GoComponentInstance *i, const uint8_t *data, size_t n);
/* render the window to an RGBA8 buffer (w*h*4 bytes); NULL on failure. Free with goslint_pixels_free. */
uint8_t *goslint_instance_take_snapshot(const GoComponentInstance *i, uint32_t *w, uint32_t *h);
void     goslint_pixels_free(uint8_t *ptr, size_t n);

/* ---- callbacks, invoke, globals ---- */
/* A callback receives a host handle (user_data) + borrowed args, and returns an
 * owned GoValue (NULL == Void) that the library takes ownership of. `drop` is
 * called with user_data when the handler is released. */
typedef GoValue *(*GoCallback)(uintptr_t user_data, GoValue **args, size_t n);

GoValue *goslint_instance_invoke(const GoComponentInstance *i, const char *name, GoValue **args, size_t n);
int      goslint_instance_set_callback(const GoComponentInstance *i, const char *name,
                                       GoCallback cb, uintptr_t user_data, void (*drop)(uintptr_t));
GoValue *goslint_instance_get_global_property(const GoComponentInstance *i, const char *global, const char *name);
int      goslint_instance_set_global_property(const GoComponentInstance *i, const char *global, const char *name, const GoValue *v);
int      goslint_instance_set_global_callback(const GoComponentInstance *i, const char *global, const char *name,
                                              GoCallback cb, uintptr_t user_data, void (*drop)(uintptr_t));
GoValue *goslint_instance_invoke_global(const GoComponentInstance *i, const char *global, const char *name, GoValue **args, size_t n);

/* ---- value (M1: scalars) ---- */
/* type codes: 0 void, 1 number, 2 string, 3 bool, 4 model, 5 struct, 6 brush,
 * 7 image, -1 other, -2 null pointer. */
GoValue *goslint_value_new_void(void);
GoValue *goslint_value_new_double(double d);
GoValue *goslint_value_new_bool(bool b);
GoValue *goslint_value_new_string(const char *s);
int32_t  goslint_value_type(const GoValue *v);
bool     goslint_value_as_double(const GoValue *v, double *out);
bool     goslint_value_as_bool(const GoValue *v, bool *out);
char    *goslint_value_as_string(const GoValue *v);
GoValue *goslint_value_clone(const GoValue *v);
bool     goslint_value_eq(const GoValue *a, const GoValue *b);
void     goslint_value_free(GoValue *v);

/* ---- struct & enum values (M4) ---- */
GoValue *goslint_value_new_struct(const GoStruct *s);
GoStruct *goslint_value_as_struct(const GoValue *v);   /* owned; NULL if not a struct */
GoValue *goslint_value_new_enum(const char *enum_name, const char *value);
bool     goslint_value_as_enum(const GoValue *v, char **out_name, char **out_value);

GoStruct *goslint_struct_new(void);
void      goslint_struct_free(GoStruct *s);
void      goslint_struct_set_field(GoStruct *s, const char *name, const GoValue *v);
GoValue  *goslint_struct_get_field(const GoStruct *s, const char *name);
size_t    goslint_struct_field_count(const GoStruct *s);
char     *goslint_struct_field_name(const GoStruct *s, size_t i);

/* ---- models (M5) ---- */
typedef size_t (*GoModelRowCount)(uintptr_t handle);
typedef GoValue *(*GoModelRowData)(uintptr_t handle, size_t row); /* owned; NULL == no row */
typedef void (*GoModelSetRowData)(uintptr_t handle, size_t row, GoValue *value); /* takes ownership */

GoModel *goslint_model_new(uintptr_t handle, GoModelRowCount rc, GoModelRowData rd,
                           GoModelSetRowData srd, void (*drop)(uintptr_t));
void     goslint_model_free(GoModel *m);
GoValue *goslint_value_new_model(const GoModel *m);
/* snapshot model from a list of values (a VecModel); items are cloned, caller frees them */
GoValue *goslint_value_new_array(const GoValue *const *items, size_t n);
void     goslint_model_notify_row_changed(const GoModel *m, size_t row);
void     goslint_model_notify_row_added(const GoModel *m, size_t row, size_t count);
void     goslint_model_notify_row_removed(const GoModel *m, size_t row, size_t count);
void     goslint_model_notify_reset(const GoModel *m);

/* read a Slint-returned model out of a Value */
size_t   goslint_value_model_row_count(const GoValue *v);
GoValue *goslint_value_model_row_data(const GoValue *v, size_t row);

/* ---- color, image, timer (M6) ---- */
GoValue *goslint_value_new_color(uint8_t r, uint8_t g, uint8_t b, uint8_t a);
bool     goslint_value_as_color(const GoValue *v, uint8_t *r, uint8_t *g, uint8_t *b, uint8_t *a);

/* gradient brushes */
typedef struct { float pos; uint8_t r, g, b, a; } GoGradientStop;
GoValue *goslint_value_new_linear_gradient(float angle, const GoGradientStop *stops, size_t n);
GoValue *goslint_value_new_radial_gradient(const GoGradientStop *stops, size_t n);
int      goslint_value_brush_kind(const GoValue *v); /* -1 none, 0 solid, 1 linear, 2 radial, 3 other */
float    goslint_value_linear_gradient_angle(const GoValue *v);
size_t   goslint_value_gradient_stop_count(const GoValue *v);
bool     goslint_value_gradient_stop(const GoValue *v, size_t i, GoGradientStop *out);

GoImage *goslint_image_load_from_path(const char *path);
/* build from raw pixels (copied): rgba8 = w*h*4 bytes, rgb8 = w*h*3 bytes, row-major */
GoImage *goslint_image_from_rgba8(const uint8_t *data, uint32_t w, uint32_t h);
GoImage *goslint_image_from_rgb8(const uint8_t *data, uint32_t w, uint32_t h);
void     goslint_image_free(GoImage *img);
void     goslint_image_size(const GoImage *img, uint32_t *w, uint32_t *h);
GoValue *goslint_value_new_image(const GoImage *img);
GoImage *goslint_value_as_image(const GoValue *v);

GoTimer *goslint_timer_new(void);
void     goslint_timer_free(GoTimer *t);
void     goslint_timer_start(const GoTimer *t, int32_t mode, uint64_t interval_ms,
                             void (*cb)(uintptr_t), uintptr_t handle, void (*drop)(uintptr_t));
void     goslint_timer_single_shot(uint64_t interval_ms, void (*cb)(uintptr_t),
                                   uintptr_t handle, void (*drop)(uintptr_t));
void     goslint_timer_stop(const GoTimer *t);
void     goslint_timer_restart(const GoTimer *t);
bool     goslint_timer_running(const GoTimer *t);

#ifdef __cplusplus
}
#endif

#endif /* GOSLINT_H */
