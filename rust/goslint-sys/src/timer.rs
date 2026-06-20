// C ABI for Slint timers. The callback is a C function pointer + host handle
// (cgo.Handle), with a drop invoked when the timer's closure is released. Timers
// fire on the event-loop thread, so a loop must be running.

use crate::guard;
use i_slint_core::timers::{Timer, TimerMode};

/// Holds a foreign tick callback; calls `drop` when released.
struct TimerCallback {
    handle: usize,
    cb: extern "C" fn(usize),
    drop: Option<extern "C" fn(usize)>,
}

impl Drop for TimerCallback {
    fn drop(&mut self) {
        if let Some(d) = self.drop {
            d(self.handle);
        }
    }
}

impl TimerCallback {
    // A method (not field access) so closures capture the whole struct — otherwise
    // Rust 2021 disjoint capture would move only the Copy fields and drop the
    // Drop-guard `TimerCallback` immediately, releasing the host handle early.
    fn call(&self) {
        (self.cb)(self.handle)
    }
}

fn timer_mode(mode: i32) -> TimerMode {
    if mode == 0 {
        TimerMode::SingleShot
    } else {
        TimerMode::Repeated
    }
}

#[no_mangle]
pub extern "C" fn goslint_timer_new() -> *mut Timer {
    guard(std::ptr::null_mut(), || Box::into_raw(Box::new(Timer::default())))
}

/// # Safety
/// `t` must be NULL or a pointer from goslint_timer_new.
#[no_mangle]
pub unsafe extern "C" fn goslint_timer_free(t: *mut Timer) {
    if !t.is_null() {
        drop(Box::from_raw(t));
    }
}

/// Start (or restart) the timer. `drop` is invoked with `handle` when the
/// callback is released (timer stop / restart / free).
///
/// # Safety
/// `t` must be a valid timer pointer; `cb` a valid function pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_timer_start(
    t: *const Timer,
    mode: i32,
    interval_ms: u64,
    cb: extern "C" fn(usize),
    handle: usize,
    drop: Option<extern "C" fn(usize)>,
) {
    guard((), || {
        let t = match t.as_ref() {
            Some(t) => t,
            None => return,
        };
        let data = TimerCallback { handle, cb, drop };
        t.start(timer_mode(mode), std::time::Duration::from_millis(interval_ms), move || {
            data.call()
        });
    })
}

/// Fire a one-shot callback after `interval_ms`.
///
/// # Safety
/// `cb` must be a valid function pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_timer_single_shot(
    interval_ms: u64,
    cb: extern "C" fn(usize),
    handle: usize,
    drop: Option<extern "C" fn(usize)>,
) {
    guard((), || {
        let data = TimerCallback { handle, cb, drop };
        Timer::single_shot(std::time::Duration::from_millis(interval_ms), move || data.call());
    })
}

/// # Safety
/// `t` must be NULL or a timer pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_timer_stop(t: *const Timer) {
    guard((), || {
        if let Some(t) = t.as_ref() {
            t.stop();
        }
    })
}

/// # Safety
/// `t` must be NULL or a timer pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_timer_restart(t: *const Timer) {
    guard((), || {
        if let Some(t) = t.as_ref() {
            t.restart();
        }
    })
}

/// # Safety
/// `t` must be NULL or a timer pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_timer_running(t: *const Timer) -> bool {
    guard(false, || match t.as_ref() {
        Some(t) => t.running(),
        None => false,
    })
}
