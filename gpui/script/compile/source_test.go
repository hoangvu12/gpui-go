package compile_test

// The dev source (Dir): compiling from a directory on disk, and change
// detection for the watcher. Change detection is testable without
// timing: the poll (Changed) is called directly, and Watch is observed
// through a channel with a generous deadline.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui/script/compile"
)

// writeView writes (or rewrites) the directory's view.tsx.
func writeView(t testing.TB, dir, width string) {
	t.Helper()
	src := "export function App() {\n" +
		"\treturn <div id=\"box\" style={{ width: " + width + ", height: 40 }} />;\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(dir, "view.tsx"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDirCompilesTheEntry(t *testing.T) {
	dir := t.TempDir()
	writeView(t, dir, "200")
	d, err := compile.Open(filepath.Join(dir, "view.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.Compile()
	if err != nil {
		t.Fatalf("Dir.Compile: %v", err)
	}
	if !strings.Contains(b.JS, "function App()") {
		t.Fatalf("the bundle has no App function")
	}
}

func TestDirChangedDetectsSavesUntilConsumed(t *testing.T) {
	dir := t.TempDir()
	writeView(t, dir, "200")
	d, err := compile.Open(filepath.Join(dir, "view.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Changed() {
		t.Fatal("a fresh source already reports a change")
	}
	writeView(t, dir, "240")
	if !d.Changed() {
		t.Fatal("saving the entry is not detected")
	}
	if d.Changed() {
		t.Fatal("the same state reports a change twice")
	}
	// An imported module change is detected too (the dependency graph).
	if err := os.WriteFile(filepath.Join(dir, "extra.tsx"), []byte("export const k = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !d.Changed() {
		t.Fatal("a new .tsx file in the tree is not detected")
	}
}

func TestDirWatchNotifiesOnSave(t *testing.T) {
	dir := t.TempDir()
	writeView(t, dir, "200")
	d, err := compile.Open(filepath.Join(dir, "view.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes := make(chan struct{}, 4)
	d.Watch(ctx, 5*time.Millisecond, func() { changes <- struct{}{} })

	// A save is delivered; the deadline is generous because the poll
	// interval is tiny (the change itself is deterministic).
	writeView(t, dir, "240")
	select {
	case <-changes:
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not report a save within 2s")
	}
	// And no spurious reports follow.
	select {
	case <-changes:
		t.Fatal("Watch reported a change without one")
	case <-time.After(60 * time.Millisecond):
	}
	// Cancelling stops it (give the watcher a moment to observe the
	// cancellation before the next save).
	cancel()
	time.Sleep(60 * time.Millisecond)
	writeView(t, dir, "280")
	select {
	case <-changes:
		t.Fatal("Watch reported a change after cancellation")
	case <-time.After(60 * time.Millisecond):
	}
}
