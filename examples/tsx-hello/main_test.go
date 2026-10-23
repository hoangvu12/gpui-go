//go:build windows

package main

// Ticket 65's release checks, on the sample app in release shape:
//
//   - the binary built to a temp directory runs from the embedded
//     bundle with the .tsx sources renamed away, headless (the
//     -selftest mode draws one frame in an offscreen test window);
//   - the import graph of the app has no esbuild (and no
//     gpui/script/compile) while the script host (goja) is present;
//   - the staleness guard catches a source edited after the bundle was
//     generated.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui/script"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds the app")
	}
	bin := filepath.Join(t.TempDir(), "tsx-hello.exe")
	cmd := exec.Command("go", "build", "-trimpath", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func TestTheReleaseBinaryRunsFromTheEmbeddedBundleWithTheSourcesGone(t *testing.T) {
	bin := buildBinary(t)

	// The .tsx sources go away: the app must not read them from disk.
	renamed := "ui.hidden"
	if err := os.Rename("ui", renamed); err != nil {
		t.Fatalf("rename ui: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Rename(renamed, "ui"); err != nil {
			t.Fatalf("restore ui: %v", err)
		}
	})

	// Headless, from an empty directory with no .tsx in reach: the
	// self-test loads the embedded bundle, renders one frame in an
	// offscreen test window (no visible window, no platform host) and
	// exercises one click.
	run := t.TempDir()
	cmd := exec.Command(bin, "-selftest")
	cmd.Dir = run
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("-selftest failed with the sources removed: %v\n%s", err, out)
	}
	t.Logf("%s ran from the embedded bundle (exit 0, %d bytes of output)", filepath.Base(bin), len(out))
}

func TestTheReleaseImportGraphLinksNoEsbuild(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	hasHost := false
	for _, dep := range strings.Split(string(out), "\n") {
		if strings.Contains(dep, "esbuild") {
			t.Fatalf("the release app links esbuild through %q", dep)
		}
		if dep == "github.com/hoangvu12/gpui-go/gpui/script/compile" {
			t.Fatal("the release app links the development-only compile package")
		}
		if dep == "github.com/dop251/goja" {
			hasHost = true // the script host itself ships
		}
	}
	if !hasHost {
		t.Fatal("the release app lost the script host (goja)")
	}
}

func TestTheStalenessGuardCatchesAnEditedSource(t *testing.T) {
	path := filepath.Join("ui", "app.tsx")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.WriteFile(path, original, 0o644); err != nil {
			t.Fatal(err)
		}
	})

	// The committed bundle is fresh (the generated TestUIIsFresh runs
	// the same check; this also proves the guard's other direction).
	if err := UI.CheckFresh("."); err != nil {
		t.Fatalf("the committed bundle is stale: %v", err)
	}

	// A late edit without regenerating: the guard must flag it before
	// a release builds from the old bundle.
	edited := append([]byte("\n// a late edit\n"), original...)
	if err := os.WriteFile(path, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	err = UI.CheckFresh(".")
	if err == nil {
		t.Fatal("an edited .tsx did not flag the embedded bundle stale")
	}
	if !strings.Contains(err.Error(), "ui/app.tsx") || !strings.Contains(err.Error(), "go generate") {
		t.Fatalf("the staleness error does not name the file and the fix: %v", err)
	}
}

func TestTheEmbeddedBundleCarriesTheGeneratedSource(t *testing.T) {
	// The committed generated file describes what it embeds: the tool
	// name and at least the entry source, hashed.
	if UI.Tool != "tsxbundle" {
		t.Fatalf("UI.Tool = %q", UI.Tool)
	}
	if len(UI.Sources) == 0 {
		t.Fatal("UI records no sources")
	}
	for _, s := range UI.Sources {
		if !strings.HasPrefix(s.Hash, "sha256:") {
			t.Fatalf("source %s has no recorded hash", s.Path)
		}
	}
	// Bundle() is the Load argument: it carries the name and JS.
	if UI.Bundle().Name != UI.Name || UI.Bundle().JS != UI.JS {
		t.Fatal("Bundle() does not carry the embedded name and JS")
	}
	if !strings.Contains(script.GlobalName, "gpui") { // trivial sanity of the compiled global
		t.Fatalf("script.GlobalName = %q", script.GlobalName)
	}
}
