//go:build goslint_android

package slintsys

/*
#include "goslint.h"
*/
import "C"

import "unsafe"

// AndroidJavaVM returns the process JavaVM pointer for JNI interop, or 0 before
// the Android entry has run (it is always set by the time app code executes).
// Hand it to a JNI binding such as git.wow.st/gmp/jni, or to cgo code taking a
// uintptr_t. Every OS thread that calls JNI must be attached to the VM first
// (AttachCurrentThread) — remember goroutines migrate between threads.
func AndroidJavaVM() uintptr {
	return uintptr(unsafe.Pointer(C.goslint_android_java_vm()))
}

// AndroidActivity returns the app's NativeActivity as a JNI jobject reference.
// It doubles as the android.content.Context most platform APIs want. The
// reference is owned by the Android framework: do NOT delete it or wrap it in
// anything that would. Use the activity's getClassLoader() to look up app/dex
// classes — FindClass on a native thread only sees system classes.
func AndroidActivity() uintptr {
	return uintptr(unsafe.Pointer(C.goslint_android_activity()))
}
