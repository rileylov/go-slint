package main

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rileylov/go-slint"
)

// TestUICompiles validates the embedded markup without needing a display.
func TestUICompiles(t *testing.T) {
	app, err := slint.Compile(ui, slint.WithStyle("fluent"))
	if err != nil {
		t.Fatalf("compile ui.slint: %v", err)
	}
	defer app.Close()
	if names := app.ComponentNames(); !slices.Contains(names, "AppWindow") {
		t.Fatalf("component AppWindow not found in %v", names)
	}
}

// TestTransform covers the value-returning callback's pure Go logic (slices +
// strings + the external humanize module).
func TestTransform(t *testing.T) {
	if got := transform(""); got != "" {
		t.Fatalf("empty input: %q", got)
	}
	got := transform("abcdé")
	for _, want := range []string{"reversed: édcba", "upper: ABCDÉ", "length: 5 rune(s)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("transform(%q) = %q, missing %q", "abcdé", got, want)
		}
	}
}

// TestConcurrentCounter hammers the mutex-guarded counter from 8 goroutines.
// Run with -race to prove the binding's Go concurrency is data-race clean.
func TestConcurrentCounter(t *testing.T) {
	l := &lab{stop: make(chan struct{})}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); l.hammerWorker(l.stop) }()
	}
	time.Sleep(80 * time.Millisecond)
	close(l.stop)
	wg.Wait()

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.counter == 0 {
		t.Fatal("counter never advanced")
	}
}

// TestJobChannel exercises the workers→channel→consumer path (no UI thread):
// many producers feed jobMsgs, a single consumer drains them — race-clean.
func TestJobChannel(t *testing.T) {
	updates := make(chan jobMsg, 16)
	var wg sync.WaitGroup
	for i := range 5 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			updates <- jobMsg{name: string(rune('a' + id)), progress: 1, status: "done"}
		}(i)
	}
	go func() { wg.Wait(); close(updates) }()

	var got int
	for range updates {
		got++
	}
	if got != 5 {
		t.Fatalf("drained %d updates, want 5", got)
	}
}
