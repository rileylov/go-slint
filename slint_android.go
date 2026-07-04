//go:build goslint_android

package slint

import "github.com/rileylov/go-slint/slintsys"

// AndroidJavaVM returns the process JavaVM pointer, the entry ticket for calling
// Android platform APIs (Bluetooth, notifications, sensors, …) over JNI. The JVM
// already runs in every Android app process — goslint just hands you the pointer.
// Pair it with a JNI binding (e.g. git.wow.st/gmp/jni) or your own cgo. Threads
// calling JNI must be attached to the VM; see examples/notify for a worked,
// dependency-free pattern.
func AndroidJavaVM() uintptr { return slintsys.AndroidJavaVM() }

// AndroidActivity returns the app's NativeActivity as a JNI jobject — also the
// android.content.Context most platform APIs require. The reference is owned by
// the framework: never delete it. For classes from your own dex, resolve them
// through this activity's getClassLoader(), not FindClass (native threads only
// see system classes).
func AndroidActivity() uintptr { return slintsys.AndroidActivity() }
