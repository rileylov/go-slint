//go:build !goslint_android

package main

import (
	"errors"
	"log"
	"runtime"
)

// Slint is thread-affine: pin the main goroutine.
func init() { runtime.LockOSThread() }

func main() {
	if err := run("fluent"); err != nil {
		log.Fatal(err)
	}
}

// Desktop stubs: the UI runs everywhere, but the notification itself is the
// Android demo — see android_main.go for the real JNI implementations.
func ensureNotificationPermission() (bool, error) { return true, nil }

func sendNotification(string, string, int) error {
	return errors.New("this demo posts Android notifications — run it with: goslint android dev -permissions POST_NOTIFICATIONS .")
}
