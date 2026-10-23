//go:build windows

package script_test

// The release loader API (ticket 65): an EmbeddedBundle — what
// cmd/tsxbundle generates and //go:embed carries — yields the Bundle
// the host loads, and CheckFresh compares the recorded source hashes
// with the disk so a release build made from an out-of-date bundle is
// caught (the generated freshness test calls CheckFresh; a test is the
// Go place where "the build" fails, since go build never reruns go
// generate).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui/script"
	"github.com/hoangvu12/gpui-go/gpui/script/compile"
)

const helloView = `export function App() {
	return <div id="card">{bind(state("clicks", 0), (n: number) => ` + "`Hello TSX! ${n}`" + `)}</div>;
}
`

// embed compiles the view and wraps it as the generator would: the
// bundle plus the hashed source recorded beside it.
func embed(t testing.TB, dir, name, src string) script.EmbeddedBundle {
	t.Helper()
	b, err := compile.String(name, src)
	if err != nil {
		t.Fatalf("compile.String: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return script.EmbeddedBundle{
		Name: b.Name,
		JS:   b.JS,
		Tool: "tsxbundle",
		Sources: []script.EmbeddedSource{{
			Path: name,
			Hash: script.HashSource(data),
		}},
	}
}

func TestAnEmbeddedBundleLoadsThroughThePublicAPI(t *testing.T) {
	dir := t.TempDir()
	e := embed(t, dir, "view.tsx", helloView)

	h := script.New()
	if err := h.Load(e.Bundle()); err != nil {
		t.Fatalf("Load(embedded): %v", err)
	}
	if got, _ := h.Text("card"); got != "Hello TSX! 0" {
		t.Fatalf("card text = %q, want %q", got, "Hello TSX! 0")
	}
	// Bundle() carries the diagnostics name and the compiled JS (with
	// its inline source map), nothing else needs translating.
	if e.Bundle().Name != e.Name || e.Bundle().JS != e.JS {
		t.Fatal("Bundle() does not carry the embedded name and JS")
	}
}

func TestCheckFreshPassesWhileTheSourcesMatch(t *testing.T) {
	dir := t.TempDir()
	e := embed(t, dir, "view.tsx", helloView)
	if err := e.CheckFresh(dir); err != nil {
		t.Fatalf("a matching source reported stale: %v", err)
	}
	// An unrelated file beside the sources is not a change: only the
	// files the bundle was built from are recorded.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.CheckFresh(dir); err != nil {
		t.Fatalf("an unrelated file flagged the bundle stale: %v", err)
	}
	// A bundle with no recorded sources has nothing to check (a
	// hand-built one).
	if err := (script.EmbeddedBundle{}).CheckFresh(dir); err != nil {
		t.Fatalf("no sources, yet not fresh: %v", err)
	}
}

func TestCheckFreshFlagsAnEditedSource(t *testing.T) {
	dir := t.TempDir()
	e := embed(t, dir, "view.tsx", helloView)
	if err := os.WriteFile(filepath.Join(dir, "view.tsx"), []byte("\n"+helloView), 0o644); err != nil {
		t.Fatal(err)
	}
	err := e.CheckFresh(dir)
	if err == nil {
		t.Fatal("an edited .tsx did not flag the bundle stale")
	}
	if !strings.Contains(err.Error(), "view.tsx") || !strings.Contains(err.Error(), "go generate") {
		t.Fatalf("the staleness error does not name the file and the fix: %v", err)
	}
}

func TestCheckFreshFlagsAMissingSource(t *testing.T) {
	dir := t.TempDir()
	e := embed(t, dir, "view.tsx", helloView)
	if err := os.Remove(filepath.Join(dir, "view.tsx")); err != nil {
		t.Fatal(err)
	}
	err := e.CheckFresh(dir)
	if err == nil {
		t.Fatal("a removed .tsx did not flag the bundle stale")
	}
	if !strings.Contains(err.Error(), "view.tsx") {
		t.Fatalf("the staleness error does not name the file: %v", err)
	}
}

func TestHashSourceIsTheRecordedForm(t *testing.T) {
	// The generator and CheckFresh must agree on the hash's shape.
	h := script.HashSource([]byte("hello"))
	if !strings.HasPrefix(h, "sha256:") || len(h) != len("sha256:")+64 {
		t.Fatalf("HashSource = %q, want sha256: + 64 hex digits", h)
	}
	if script.HashSource([]byte("hello")) != h {
		t.Fatal("HashSource is not stable")
	}
	if script.HashSource([]byte("hellp")) == h {
		t.Fatal("HashSource collides on a one-byte change")
	}
}
