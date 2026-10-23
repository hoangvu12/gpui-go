package compile

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hoangvu12/gpui-go/gpui/script"
)

// Dir is a development Source: a directory of .tsx/.ts/.js files on
// disk with one entry, recompiled by esbuild on demand. Changed
// reports whether the tree's files changed since the last check
// (Watch polls it); the entry's whole directory is watched, so a save
// to an imported module reloads too (the dependency graph), like
// zgui's require watch.
type Dir struct {
	entry string
	root  string

	mu    sync.Mutex
	files []fileStamp
}

// fileStamp is one watched file's identity: path, size and a content
// hash. The hash is needed because Windows updates a rewritten file's
// modification time lazily, so a same-size save would otherwise be
// invisible.
type fileStamp struct {
	path string
	size int64
	hash uint64
}

// Open returns the dev source rooted at the entry .tsx's directory.
func Open(entry string) (*Dir, error) {
	entry = filepath.Clean(entry)
	if _, err := os.Stat(entry); err != nil {
		return nil, err
	}
	d := &Dir{entry: entry, root: filepath.Dir(entry)}
	d.mu.Lock()
	d.files = d.scan()
	d.mu.Unlock()
	return d, nil
}

// Compile implements script.Source.
func (d *Dir) Compile() (script.Bundle, error) { return Files(d.entry) }

// Changed reports and consumes a change: any .tsx/.ts/.js file under
// the entry's directory (added, removed, resized or rewritten) since
// the last call.
func (d *Dir) Changed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.scan()
	if slices.Equal(now, d.files) {
		return false
	}
	d.files = now
	return true
}

// Watch polls the source every interval and calls notify (from the
// watcher's goroutine) once per change set — the development save
// notification. Cancel ctx to stop. The receiver of notify must call
// Host.Reload on the app's UI thread (see script's doc.go).
func (d *Dir) Watch(ctx context.Context, interval time.Duration, notify func()) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if d.Changed() {
					notify()
				}
			}
		}
	}()
}

// hashOf is a cheap content fingerprint (fnv-1a).
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

// scan collects the watched files' stamps (path, size and content
// hash, sorted by path).
func (d *Dir) scan() []fileStamp {
	var files []fileStamp
	_ = filepath.WalkDir(d.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil // a disappearing file is a change, not a failure
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != d.root && strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(name)) {
		case ".tsx", ".ts", ".js":
		default:
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		files = append(files, fileStamp{path: path, size: info.Size(), hash: hashOf(data)})
		return nil
	})
	slices.SortFunc(files, func(a, b fileStamp) int { return strings.Compare(a.path, b.path) })
	return files
}
