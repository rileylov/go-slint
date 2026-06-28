// Shared Go ⇄ Slint interop logic, used by both the desktop entry (main.go) and
// the Android entry (android_main.go). See main.go for the feature checklist.
package main

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/rileylov/go-slint"
)

// jobMsg is one progress update produced by a worker goroutine and consumed by
// the single dispatcher goroutine.
type jobMsg struct {
	name     string
	progress float64
	status   string
}

type lab struct {
	win *slint.Instance

	mu        sync.Mutex // guards counter, hammering, stop, seq
	counter   int64
	prevCount int64
	hammering bool
	stop      chan struct{}
	seq       int

	jobs    *slint.SliceModel // the worker-pool model (slice of struct rows)
	updates chan jobMsg       // workers -> dispatcher
}

// runApp compiles the given markup, wires the Go ⇄ Slint boundary, starts the
// background goroutines and runs the event loop. Shared by all platforms; only
// the markup and widget style differ (fluent on desktop, material on Android).
func runApp(uiSource, style string) error {
	app, err := slint.Compile(uiSource, slint.WithStyle(style))
	if err != nil {
		return err
	}
	defer app.Close()

	win, err := app.Create("AppWindow")
	if err != nil {
		return err
	}
	defer win.Close()

	l := &lab{
		win:     win,
		jobs:    slint.NewSliceModel(),
		updates: make(chan jobMsg, 128),
	}
	defer l.jobs.Close()
	win.Set("jobs", l.jobs)

	// frontend -> backend
	win.OnCallback("toggle-hammer", func([]any) any { l.toggleHammer(); return nil })
	win.OnCallback("add-job", func([]any) any { l.addJob(); return nil })
	win.OnCallback("clear-done", func([]any) any { l.clearDone(); return nil })
	// a callback that RETURNS a value straight back into the .slint expression
	win.OnCallback("transform", func(a []any) any {
		s, _ := a[0].(string)
		return transform(s)
	})

	go l.dispatch()  // single consumer of the updates channel
	go l.statsLoop() // periodic backend -> frontend push

	return win.Run()
}

// ---- concurrency panel: many goroutines + mutex ----

func (l *lab) toggleHammer() {
	// runs on the Slint thread (invoked from a clicked callback)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hammering {
		close(l.stop)
		l.hammering = false
	} else {
		l.stop = make(chan struct{})
		l.hammering = true
		for range 8 {
			go l.hammerWorker(l.stop)
		}
	}
	l.win.Set("hammering", l.hammering)
}

func (l *lab) hammerWorker(stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
			l.mu.Lock()
			l.counter++ // the whole point: mutex-guarded shared write from 8 goroutines
			l.mu.Unlock()
			time.Sleep(50 * time.Microsecond) // keep it visible, not a CPU pin
		}
	}
}

// ---- worker pool: goroutines -> channel -> model ----

func (l *lab) addJob() {
	l.mu.Lock()
	l.seq++
	name := fmt.Sprintf("job-%02d", l.seq)
	l.mu.Unlock()

	// we're on the Slint thread here, so mutate the model directly
	l.jobs.Append(map[string]any{"name": name, "progress": 0.0, "status": "queued"})
	go l.runJob(name)
}

func (l *lab) runJob(name string) {
	for p := 0; p <= 100; p += 4 {
		time.Sleep(90 * time.Millisecond)
		status := "running"
		if p >= 100 {
			status = "done"
		}
		l.updates <- jobMsg{name: name, progress: float64(p) / 100, status: status}
	}
}

// dispatch is the single consumer: it serialises all worker updates and applies
// each to the model on the Slint thread.
func (l *lab) dispatch() {
	for u := range l.updates {
		slint.InvokeFromEventLoop(func() { l.applyJob(u) })
	}
}

func (l *lab) applyJob(u jobMsg) {
	for r := 0; r < l.jobs.Len(); r++ {
		if m, ok := l.jobs.RowData(r).(map[string]any); ok && m["name"] == u.name {
			l.jobs.SetRowData(r, map[string]any{"name": u.name, "progress": u.progress, "status": u.status})
			return
		}
	}
}

func (l *lab) clearDone() {
	for r := l.jobs.Len() - 1; r >= 0; r-- {
		if m, ok := l.jobs.RowData(r).(map[string]any); ok && m["status"] == "done" {
			l.jobs.RemoveAt(r)
		}
	}
}

// ---- periodic backend -> frontend stats ----

func (l *lab) statsLoop() {
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		l.mu.Lock()
		c := l.counter
		rate := (c - l.prevCount) * 5 // per second (200ms tick)
		l.prevCount = c
		hammering := l.hammering
		l.mu.Unlock()
		g := runtime.NumGoroutine()

		rateText := "idle"
		if hammering {
			rateText = humanize.Comma(rate) + " incr/s"
		}
		slint.InvokeFromEventLoop(func() {
			l.win.Set("counter-text", humanize.Comma(c))
			l.win.Set("rate-text", rateText)
			l.win.Set("goroutines", float64(g))
		})
	}
}

// transform is pure Go logic called synchronously from the UI; it returns a value
// the .slint expression consumes directly. Exercises slices ([]rune) + strings +
// the external humanize module.
func transform(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return fmt.Sprintf("reversed: %s\nupper: %s\nlength: %s rune(s)",
		string(r), strings.ToUpper(s), humanize.Comma(int64(len(r))))
}
