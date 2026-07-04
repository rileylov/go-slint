# notify — call a native Android API (JNI) from Go

A Slint button that posts a **real system notification** through Android's
`NotificationManager`, using nothing but `slint.AndroidJavaVM()` /
`slint.AndroidActivity()` and the NDK's `jni.h` — no Java build step, no dex, no
extra dependencies. This is the reference pattern for reaching any platform API
(Bluetooth, sensors, …) from a goslint app.

## Run it (phone plugged in, USB debugging on)

```sh
goslint android dev -permissions POST_NOTIFICATIONS ./examples/notify
```

or build an APK: `goslint android build -permissions POST_NOTIFICATIONS ./examples/notify`.

On Android 13+ the **first tap** shows the system permission dialog — allow it,
then tap again. Needs Android 8+ (API 26, notification channels).

## How it works

- `goslint android build` sets the `goslint_android` build tag; `android_main.go`
  (tagged) holds the entry point and a small cgo C helper that does the JNI calls.
  `main.go` (untagged side) stubs the same functions so the UI also runs on desktop.
- The JVM already lives in every Android app process — `slint.AndroidJavaVM()`
  just hands you the pointer. Framework classes resolve with `FindClass` from any
  attached thread; only classes from your own dex would need the activity's
  classloader.
- The mandatory notification small-icon comes from a runtime `Bitmap` wrapped in
  an `Icon` — the APK ships no resources (`hasCode="false"`), and it doesn't need any.
- `-permissions POST_NOTIFICATIONS` puts the entry in the generated manifest; the
  runtime prompt (Android 13+) is requested over JNI too (`requestPermissions`).

Debugging: `adb logcat -s goslint-notify goslintapp AndroidRuntime`.
