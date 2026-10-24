//go:build windows

package main

// The watcher (ticket 66): one scan of a real temp tree classifies
// what changed and into which half of the dev loop, and catches the
// edit Windows hides from size+mtime — a same-size rewrite. The
// debounce collapses a burst of detections into one action with the
// kinds merged and the first detection carried.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherClassifiesAndDetectsEdits(t *testing.T) {
	dir := t.TempDir()
	appTSX := filepath.Join(dir, "ui", "app.tsx")
	mainGo := filepath.Join(dir, "main.go")
	for path, content := range map[string]string{
		appTSX:                                "const TAG = \"v1\";\n",
		mainGo:                                "package main\n",
		filepath.Join(dir, "ui", "data.json"): "{}\n",
		filepath.Join(dir, "notes.md"):        "not a source\n",
		filepath.Join(dir, "node_modules", "x.js"): "dep\n",
		filepath.Join(dir, "testdata", "z.go"):     "ignored by the go tool\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	w := newWatcher(dir)
	// The first scan from an empty baseline sees everything that is
	// watched: both halves.
	if change := w.scan(); !change.reload || !change.restart {
		t.Fatalf("the first scan = %+v, want both kinds", change)
	}
	// A quiet tree reports nothing.
	if change := w.scan(); change.any() {
		t.Fatalf("a quiet tree reported %+v", change)
	}

	// A .tsx edit (same size, one character: the Windows mtime trap)
	// is a reload change.
	if err := os.WriteFile(appTSX, []byte("const TAG = \"v2\";\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if change := w.scan(); !change.reload || change.restart {
		t.Fatalf("a .tsx edit = %+v, want reload only", change)
	}

	// A .go edit is a restart change.
	if err := os.WriteFile(mainGo, []byte("package main\n\nvar _ = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if change := w.scan(); change.reload || !change.restart {
		t.Fatalf("a .go edit = %+v, want restart only", change)
	}

	// go.mod changes restart (the module graph changed).
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if change := w.scan(); change.reload || !change.restart {
		t.Fatalf("a go.mod edit = %+v, want restart only", change)
	}

	// A new .json (an esbuild-bundled input) reloads.
	if err := os.WriteFile(filepath.Join(dir, "ui", "more.json"), []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if change := w.scan(); !change.reload || change.restart {
		t.Fatalf("a new .json = %+v, want reload only", change)
	}

	// A removed .tsx is a reload change, not silence.
	if err := os.Remove(appTSX); err != nil {
		t.Fatal(err)
	}
	if change := w.scan(); !change.reload || change.restart {
		t.Fatalf("a removed .tsx = %+v, want reload only", change)
	}

	// Not sources: .md, node_modules, testdata.
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "x.js"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "testdata", "z.go"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if change := w.scan(); change.any() {
		t.Fatalf("not-source edits reported %+v", change)
	}
}

func TestDebounceCollapsesABurstIntoOneFire(t *testing.T) {
	d := newDebounce(30 * time.Millisecond)
	first := time.Now()
	d.ping(changeSet{reload: true, detected: first})
	time.Sleep(5 * time.Millisecond)
	d.ping(changeSet{reload: true})
	time.Sleep(5 * time.Millisecond)
	// A burst that spans both halves of the loop merges; the restart
	// wins the act, so the fire carries both kinds.
	d.ping(changeSet{restart: true})

	select {
	case change := <-d.fired:
		if !change.reload || !change.restart {
			t.Fatalf("the merged fire = %+v, want both kinds", change)
		}
		if change.detected.Before(first) {
			t.Fatalf("the fire's detection time %v predates the burst", change.detected)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the burst never fired")
	}

	// The burst is one fire, not one per ping.
	select {
	case change := <-d.fired:
		t.Fatalf("a second fire for one burst: %+v", change)
	case <-time.After(120 * time.Millisecond):
	}

	// A later change fires again, carrying its own detection time.
	later := time.Now()
	d.ping(changeSet{reload: true, detected: later})
	select {
	case change := <-d.fired:
		if !change.reload || change.restart {
			t.Fatalf("the later fire = %+v, want reload only", change)
		}
		if change.detected.Before(later) {
			t.Fatalf("the later fire carried the first burst's time: %v", change.detected)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the later change never fired")
	}
}

func TestFileKindClassification(t *testing.T) {
	for name, want := range map[string]struct {
		kind    changeKind
		watched bool
	}{
		"app.tsx":    {changeReloadSet, true},
		"app.ts":     {changeReloadSet, true},
		"app.js":     {changeReloadSet, true},
		"app.mjs":    {changeReloadSet, true},
		"app.cjs":    {changeReloadSet, true},
		"data.json":  {changeReloadSet, true},
		"main.go":    {changeRestartSet, true},
		"GO.MOD":     {changeRestartSet, true},
		"go.sum":     {changeRestartSet, true},
		"go.work":    {changeRestartSet, true},
		"readme.md":  {changeReloadSet, false},
		"bundle.png": {changeReloadSet, false},
		"go.mod.txt": {changeReloadSet, false},
	} {
		kind, watched := fileKind(name)
		if kind != want.kind || watched != want.watched {
			t.Errorf("fileKind(%q) = (%v, %v), want (%v, %v)", name, kind, watched, want.kind, want.watched)
		}
	}
}
