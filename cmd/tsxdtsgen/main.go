// Command tsxdtsgen regenerates the TSX surface's .d.ts (ticket 63,
// ADR 0003's "generated .d.ts"): it extracts the elements, style
// properties, events, globals and Tailwind classes the engine accepts
// from the engine's own source (internal/tsxgen: the host's switches,
// parseStyle's cases and the class layer's generated tables) and
// writes gpui/script/types/gpui-script.d.ts, the ambient declarations
// .tsx views and cmd/tsxcheck type-check against.
//
// It is a maintainer tool in the style of cmd/stylegen and
// cmd/tailwindgen: the output is committed, regeneration is
// byte-idempotent, and consumers never run it. The go:generate
// directive lives in gpui/script/types; regenerate with:
//
//	go generate ./gpui/script/types
//
// (or go run ./cmd/tsxdtsgen from the module root). An engine change
// the extractor does not recognize fails the command with the name of
// what to teach it, and the drift tests (internal/tsxgen) fail until
// the file is regenerated — the types cannot silently drift.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hoangvu12/gpui-go/internal/tsxgen"
)

// dtsPath is where the generated file lands (relative to the module
// root): beside the package that embeds it.
const dtsPath = "gpui/script/types/gpui-script.d.ts"

func main() {
	root := flag.String("root", "", "the module root (default: the nearest go.mod above the working directory)")
	check := flag.Bool("check", false, "fail when the committed file differs instead of writing it")
	flag.Parse()
	dir := *root
	if dir == "" {
		found, err := moduleRoot()
		if err != nil {
			fail(err)
		}
		dir = found
	}
	s, err := tsxgen.Extract(dir)
	if err != nil {
		fail(err)
	}
	want := []byte(s.DTS())
	path := filepath.Join(dir, filepath.FromSlash(dtsPath))
	if *check {
		got, err := os.ReadFile(path)
		if err != nil {
			fail(err)
		}
		if !bytes.Equal(got, want) {
			fail(fmt.Errorf("%s is stale: regenerate with go generate ./gpui/script/types", dtsPath))
		}
		fmt.Printf("tsxdtsgen: %s is current\n", dtsPath)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fail(err)
	}
	changed, err := writeFileIfChanged(path, want)
	if err != nil {
		fail(err)
	}
	if changed {
		fmt.Printf("tsxdtsgen: wrote %s\n", dtsPath)
	} else {
		fmt.Printf("tsxdtsgen: %s unchanged\n", dtsPath)
	}
	fmt.Printf("tsxdtsgen: %d elements, %d props (%d events), %d style properties, %d globals, %d class aliases\n",
		len(s.Elements), len(s.Props), len(s.Events), len(s.Styles), len(s.Globals), len(s.Classes.Aliases))
}

// moduleRoot walks up from the working directory to the nearest go.mod.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("tsxdtsgen: no go.mod above %s; pass -root", dir)
		}
		dir = parent
	}
}

// writeFileIfChanged writes only when the bytes differ, so regeneration
// shows no churn in commits (the same contract as cmd/tsxbundle).
func writeFileIfChanged(path string, want []byte) (bool, error) {
	if got, err := os.ReadFile(path); err == nil && bytes.Equal(got, want) {
		return false, nil
	}
	if err := os.WriteFile(path, want, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "tsxdtsgen:", err)
	os.Exit(1)
}
