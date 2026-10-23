package script

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// EmbeddedBundle is a TSX UI compiled at build time and embedded in the
// binary — the release shape of scripting (ADR 0003). Development
// compiles .tsx on demand and reloads from disk; a release app embeds
// what the bundling tool compiled and reads nothing from disk, and it
// never links esbuild, which only the development half
// (gpui/script/compile, the tool's import) carries.
//
// The tool is cmd/tsxbundle, driven by a go:generate directive in the
// app, so the compile runs at build time (go generate ./...) and a
// compile error fails the step with the .tsx file and line. It writes
// the file //go:embed picks up beside a var of this type, plus the
// freshness test that guards it:
//
//	//go:generate go run github.com/hoangvu12/gpui-go/cmd/tsxbundle -var UI -entry ui/app.tsx
//	var UI script.EmbeddedBundle // generated: ui.tsxbundle.go
//
//	host := script.New(script.WithoutBanner())
//	if err := host.Load(UI.Bundle()); err != nil {
//		log.Fatal(err)
//	}
//
// Examples/tsx-hello is the sample app in this shape, including its
// headless -selftest mode (the release check).
type EmbeddedBundle struct {
	// Name is the embedded .js file's name (diagnostics that carry no
	// position, e.g. "the bundle exports no App function").
	Name string
	// JS is the compiled ES2017 bundle with its inline source map: goja
	// maps runtime errors back to the .tsx through it.
	JS string
	// Tool names the generator that produced the bundle, for
	// diagnostics.
	Tool string
	// Sources are the files the bundle was compiled from — the entry
	// and everything it imported — each with the SHA-256 of its bytes
	// at generate time. CheckFresh compares them with the disk, so a
	// release build made after an edit without regenerating is caught.
	Sources []EmbeddedSource
}

// EmbeddedSource is one input of an EmbeddedBundle.
type EmbeddedSource struct {
	// Path is the source's slash-separated path, relative to the
	// directory the bundle was generated in (the app's package
	// directory under go:generate; tests run there too).
	Path string
	// Hash is the SHA-256 of the file's bytes at generate time, in
	// HashSource's form.
	Hash string
}

// Bundle returns the embedded script as a Load argument.
func (b EmbeddedBundle) Bundle() Bundle { return Bundle{Name: b.Name, JS: b.JS} }

// CheckFresh reports whether the sources under root still match the
// ones the bundle was compiled from: nil when fresh (or when the bundle
// records no sources), an error naming the first file that changed or
// went missing otherwise. go build does not rerun go generate, so an
// edited .tsx does not fail the build by itself — the test the
// generator emits (calling this) is where a stale bundle is caught,
// before a release ships the old UI.
func (b EmbeddedBundle) CheckFresh(root string) error {
	for _, s := range b.Sources {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(s.Path)))
		if err != nil {
			return fmt.Errorf("tsxbundle: %s is gone, but the embedded bundle was built from it (go generate ./...): %w", s.Path, err)
		}
		if HashSource(data) != s.Hash {
			return fmt.Errorf("tsxbundle: %s changed since the embedded bundle was built from it (go generate ./...)", s.Path)
		}
	}
	return nil
}

// HashSource returns the SHA-256 of a source's bytes in the form
// EmbeddedSource.Hash records: the generator's half of CheckFresh, so
// the two cannot drift apart.
func HashSource(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
