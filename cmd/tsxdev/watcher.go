package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// changeSet is one batch of source changes the watcher detected: which
// half of the dev loop it needs (reload for UI sources, restart for Go
// sources) and when the batch was first seen (the edit-to-action
// latencies are measured from there).
type changeSet struct {
	reload   bool
	restart  bool
	detected time.Time
}

// any reports whether the set carries a change.
func (c changeSet) any() bool { return c.reload || c.restart }

// watcher polls the app's package directory for source edits — path,
// size and a content hash per file. The hash matters because Windows
// updates a rewritten file's modification time lazily, so a same-size
// save (the common edit: same text, one character swapped) is
// invisible to size+mtime but not to the content (compile.Dir, the
// script host's dev source, hashes for the same reason).
//
// The poll reads the tree's source files (skipping the directories the
// Go tool and esbuild also skip) and sleeps between scans; there is no
// busy loop, and the cost is one stat plus one read of each watched
// file per poll — milliseconds for an app package.
type watcher struct {
	root string

	stamps map[string]stamp
}

// stamp is one watched file's identity: its size and content hash.
type stamp struct {
	size int64
	hash uint64
}

// newWatcher returns a watcher for the tree rooted at root.
func newWatcher(root string) *watcher {
	return &watcher{root: root, stamps: map[string]stamp{}}
}

// scan walks the tree once, reports what changed since the last scan
// (added, removed, resized or rewritten files, classified), and makes
// what it saw the new baseline. It is called from one goroutine at a
// time (the baseline scan, then the poll loop).
func (w *watcher) scan() changeSet {
	now := map[string]stamp{}
	change := changeSet{}
	_ = filepath.WalkDir(w.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil // a disappearing file is a change, not a failure
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != w.root && skipDir(name) {
				return fs.SkipDir
			}
			return nil
		}
		kind, watched := fileKind(name)
		if !watched {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		key := filepath.ToSlash(path)
		current := stamp{size: int64(len(data)), hash: hashOf(data)}
		now[key] = current
		if old, ok := w.stamps[key]; !ok || old != current {
			if kind == changeRestartSet {
				change.restart = true
			} else {
				change.reload = true
			}
		}
		return nil
	})
	// A removed file is a change too (the last import of a deleted
	// module should not go unnoticed forever).
	for key := range w.stamps {
		if _, ok := now[key]; !ok {
			kind, _ := fileKind(filepath.Base(filepath.FromSlash(key)))
			if kind == changeRestartSet {
				change.restart = true
			} else {
				change.reload = true
			}
		}
	}
	if change.any() && change.detected.IsZero() {
		change.detected = time.Now()
	}
	w.stamps = now
	return change
}

// changeKind classifies a file's changes: Go sources (and the module
// files) restart, UI sources reload.
type changeKind uint8

const (
	// changeReloadSet is the UI half: .tsx, .ts, .js, .mjs, .cjs, .json
	// — everything esbuild can pull into a bundle.
	changeReloadSet changeKind = iota
	// changeRestartSet is the Go half: .go, go.mod, go.sum, go.work.
	changeRestartSet
)

// fileKind classifies one file name. The bool reports whether the file
// is watched at all.
func fileKind(name string) (changeKind, bool) {
	switch strings.ToLower(name) {
	case "go.mod", "go.sum", "go.work":
		return changeRestartSet, true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".go":
		return changeRestartSet, true
	case ".tsx", ".ts", ".js", ".mjs", ".cjs", ".json":
		return changeReloadSet, true
	}
	return changeReloadSet, false
}

// skipDir reports whether a directory is not part of the build or the
// bundle: what the Go tool ignores (dot and underscore prefixes,
// testdata) plus node_modules (esbuild resolves through it, but the
// dependencies are not the app's sources; a change there is a
// dependency update, not a dev edit).
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
		name == "node_modules" || name == "testdata"
}

// hashOf is a cheap content fingerprint (fnv-1a, compile.Dir's).
func hashOf(data []byte) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for _, b := range data {
		h ^= uint64(b)
		h *= prime64
	}
	return h
}

// debounce waits for a quiet period after a change before acting: one
// save that lands as several events (an editor's write-then-rename, a
// multi-file save) collapses into one action, and the kinds of every
// pending change merge (a .tsx and a .go edit in one save restart once).
// The fire carries the first detection time, so the edit-to-action
// latency is measured from the change, not from the last event.
type debounce struct {
	delay time.Duration

	mu      sync.Mutex
	pending changeSet
	timer   *time.Timer

	fired chan changeSet
}

// newDebounce returns a debouncer that fires change batches after
// delay of quiet.
func newDebounce(delay time.Duration) *debounce {
	return &debounce{delay: delay, fired: make(chan changeSet, 16)}
}

// ping records a detected change (merging its kinds into any pending
// batch) and restarts the quiet timer.
func (d *debounce) ping(change changeSet) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pending.any() {
		d.pending.reload = d.pending.reload || change.reload
		d.pending.restart = d.pending.restart || change.restart
	} else {
		// detected stays the first detection of the batch.
		d.pending = change
		if d.pending.detected.IsZero() {
			d.pending.detected = time.Now()
		}
	}
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(d.delay, d.fire)
}

// fire delivers the pending batch (time.AfterFunc's goroutine).
func (d *debounce) fire() {
	d.mu.Lock()
	change := d.pending
	d.pending = changeSet{}
	d.timer = nil
	d.mu.Unlock()
	if change.any() {
		d.fired <- change
	}
}
