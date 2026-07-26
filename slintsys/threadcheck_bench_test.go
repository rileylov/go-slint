package slintsys

import (
	"runtime"
	"testing"
)

// Prod path: guard disabled (GOSLINT_DEV unset). Should be ~free, zero allocs.
func BenchmarkCheckDisabled(b *testing.B) {
	prev := threadCheck
	threadCheck = false
	b.Cleanup(func() { threadCheck = prev })
	b.ReportAllocs()
	name := "status"
	for i := 0; i < b.N; i++ {
		CheckUIThread("Set", name)
	}
}

// Dev path: guard enabled, called on the UI thread (the normal case — no panic).
// Cost = atomic load + one cgo thread-id call.
func BenchmarkCheckEnabledOnThread(b *testing.B) {
	prev := threadCheck
	threadCheck = true
	runtime.LockOSThread()
	MarkUIThread("test")
	b.Cleanup(func() { threadCheck = prev; uiThreadID.Store(0) })
	b.ReportAllocs()
	name := "status"
	for i := 0; i < b.N; i++ {
		CheckUIThread("Set", name)
	}
}

// For reference: the cgo thread-id call alone.
func BenchmarkOSThreadID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = osThreadID()
	}
}
