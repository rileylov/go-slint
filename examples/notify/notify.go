// Command notify demonstrates calling a native Android API from Go: a button in
// a Slint UI posts a real system notification through JNI (NotificationManager).
// It shows the whole platform-interop pattern with zero dependencies beyond the
// NDK's jni.h: slint.AndroidJavaVM()/AndroidActivity() hand over the two pointers
// JNI needs, and android_main.go does the calls in a small cgo C helper.
//
// The platform split: android_main.go (build tag goslint_android — what `goslint
// android build/dev` sets) implements sendNotification/ensureNotificationPermission
// for real; main.go stubs them so the same UI runs (and compile-tests) on desktop.
package main

import (
	_ "embed"
	"fmt"

	"github.com/rileylov/go-slint"
)

//go:embed app.slint
var ui string

// run opens the window and wires the button. Shared by desktop and Android.
func run(style string) error {
	app, err := slint.Compile(ui, slint.WithStyle(style))
	if err != nil {
		return err
	}
	defer app.Close()

	win, err := app.Create("App")
	if err != nil {
		return err
	}
	defer win.Close()

	count := 0
	win.OnCallback("send", func([]any) any {
		granted, err := ensureNotificationPermission()
		if err != nil {
			win.Set("status", "permission check failed: "+err.Error())
			return nil
		}
		if !granted {
			// Android 13+: the first tap pops the system permission dialog.
			win.Set("status", "allow notifications, then tap again")
			return nil
		}
		count++
		err = sendNotification("Hello from Go + Slint", fmt.Sprintf("Notification #%d, posted over JNI", count), count)
		if err != nil {
			win.Set("status", "send failed: "+err.Error())
			return nil
		}
		win.Set("status", fmt.Sprintf("sent #%d — check the shade", count))
		return nil
	})

	return win.Run()
}
