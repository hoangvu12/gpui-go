//go:build windows

package winhostspec

// The consumer-gap 07 gates (.scratch/gpui-consumer-gaps/issues/
// 07-update-marshal-foreground.md): App.Update from a non-foreground
// goroutine serializes onto the host's foreground thread instead of
// racing the scheduler's wake drain. Before the marshal, a background
// update overlapping a foreground drive tripped the scheduler's update
// guard ("gpui: cannot run the scheduler while an application update
// is active"), which the host recorded as a window-trampoline panic
// and returned as the app's terminal error — mordbard's screenshot
// harness hit it on every close. These tests hold a background update
// open across a scheduler drive (the race, made deterministic), prove
// the marshaled update runs on the foreground thread with its panics
// propagating to the caller, and pin the inline fallbacks (AsyncApp
// task bodies, a stopped host).

import (
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// bootAppHost boots one real app on a fresh host with the run loop
// draining; the cleanup stops the host and reports the loop's terminal
// error through the returned channel (already buffered).
func bootAppHost(t *testing.T) (*gpui.App, *gpui.Host, chan error) {
	t.Helper()
	app := gpui.NewApp()
	host := gpui.NewHost()
	if err := app.Attach(host); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := host.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(host) }()
	t.Cleanup(func() {
		if err := host.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
	})
	return app, host, runDone
}

// TestAppUpdateFromBackgroundSurvivesSchedulerDrive is the ticket's
// panic, made deterministic: a background goroutine holds one
// application update open while the foreground wakes into a scheduler
// drive (a spawned task). Before the marshal, the drive's
// guardNotUpdating panicked against the open update, the host recorded
// it in the window trampoline, and the app's terminal error (and exit
// status) was wrong even though every frame had been drawn.
func TestAppUpdateFromBackgroundSurvivesSchedulerDrive(t *testing.T) {
	app, host, runDone := bootAppHost(t)

	started := make(chan struct{})
	release := make(chan struct{})
	updateReturned := make(chan struct{})
	go func() {
		// The mordbard harness pattern: an entity update driven from a
		// background goroutine (a.view.Update(a.gp, ...)).
		app.Update(func(*gpui.App) {
			close(started)
			<-release
		})
		close(updateReturned)
	}()
	<-started

	// Make the scheduler runnable: the wake drain on the host thread
	// drives it (the exact overlap that panicked).
	ran := make(chan struct{})
	app.Spawn(func(cx *gpui.AsyncApp) int64 {
		close(ran)
		return 1
	})

	// Hold the update open long enough for the wake to land and the
	// drive to run (the wake posts immediately; the generous window is
	// for a loaded machine). With the marshal, the drive runs only
	// after the update completes; without it, this window contains the
	// panic.
	time.Sleep(600 * time.Millisecond)
	close(release)
	<-updateReturned

	select {
	case <-ran:
	case <-time.After(10 * time.Second):
		t.Fatal("the spawned task never ran (its wake drain never executed)")
	}

	// The app must exit clean: no recorded trampoline panic.
	if err := host.Stop(); err != nil {
		t.Fatalf("host stop: %v", err)
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("the app's terminal error = %v, want nil (the background update no longer panics the scheduler drive)", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run loop never exited")
	}
}

// TestAppUpdateMarshalsOntoTheForegroundThread: an update driven from
// a background goroutine runs on the host thread (the marshal), and a
// panic inside it propagates to the calling goroutine.
func TestAppUpdateMarshalsOntoTheForegroundThread(t *testing.T) {
	app, host, _ := bootAppHost(t)

	onForeground := make(chan bool, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		app.Update(func(*gpui.App) {
			onForeground <- host.IsForegroundThread()
		})
	}()
	select {
	case fg := <-onForeground:
		if !fg {
			t.Fatal("the marshaled update ran off the foreground (host) thread")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the marshaled update never ran")
	}
	<-done

	// A panicking marshaled update re-raises on the caller.
	panicked := make(chan string, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				if msg, ok := r.(string); ok {
					panicked <- msg
				} else {
					panicked <- "not-a-string"
				}
			}
		}()
		app.Update(func(*gpui.App) { panic("update-boom") })
		panicked <- "no-panic"
	}()
	select {
	case msg := <-panicked:
		if msg != "update-boom" {
			t.Fatalf("the marshaled update's panic propagated as %q, want \"update-boom\"", msg)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the panicking marshaled update never returned")
	}

	// The host stays healthy after a contained update panic: posted
	// work still runs.
	posted := make(chan bool, 1)
	if !host.Post(func() { posted <- true }) {
		t.Fatal("the post was dropped after the update panic")
	}
	select {
	case <-posted:
	case <-time.After(10 * time.Second):
		t.Fatal("the host stopped serving posts after the update panic")
	}
}

// TestAsyncAppUpdateInsideTaskBodiesStillRunsInline: task bodies keep
// their sanctioned path — AsyncApp.Update runs the update inside the
// scheduler's drive (no marshal, no deadlock).
func TestAsyncAppUpdateInsideTaskBodiesStillRunsInline(t *testing.T) {
	app, _, _ := bootAppHost(t)

	updated := make(chan bool, 1)
	app.Spawn(func(cx *gpui.AsyncApp) int64 {
		cx.Update(func(a *gpui.App) {
			updated <- a != nil
		})
		return 1
	})
	select {
	case ok := <-updated:
		if !ok {
			t.Fatal("the AsyncApp update did not run")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the task body's AsyncApp.Update never ran (the drive no longer executes it inline)")
	}
}

// TestAppUpdateAfterLoopExitDoesNotBlock: after the host stopped, a
// background App.Update falls back to inline instead of hanging on a
// foreground that will never run it.
func TestAppUpdateAfterLoopExitDoesNotBlock(t *testing.T) {
	app, host, runDone := bootAppHost(t)
	if err := host.Stop(); err != nil {
		t.Fatalf("host stop: %v", err)
	}
	select {
	case <-runDone:
	case <-time.After(10 * time.Second):
		t.Fatal("the run loop never exited")
	}

	returned := make(chan struct{})
	go func() {
		app.Update(func(*gpui.App) {})
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("App.Update after the loop exited blocked forever (it must fall back inline)")
	}
}

// TestAppUpdateMarshalOrderWithConcurrentBackgrounds: concurrent
// background updates serialize (one update at a time on the
// foreground; the nesting guard stays intact).
func TestAppUpdateMarshalOrderWithConcurrentBackgrounds(t *testing.T) {
	app, _, _ := bootAppHost(t)

	const n = 8
	var mu sync.Mutex
	active := 0
	maxActive := 0
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			app.Update(func(*gpui.App) {
				mu.Lock()
				active++
				if active > maxActive {
					maxActive = active
				}
				mu.Unlock()
				time.Sleep(5 * time.Millisecond)
				mu.Lock()
				active--
				mu.Unlock()
			})
			done <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("a concurrent background update never completed")
		}
	}
	if maxActive != 1 {
		t.Fatalf("max concurrent updates = %d, want 1 (background updates serialize on the foreground)", maxActive)
	}
}
