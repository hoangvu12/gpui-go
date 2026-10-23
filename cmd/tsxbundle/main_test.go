package main

// tsxbundle's behaviour (ticket 65): the generate step writes the
// embedded bundle files (the .js //go:embed carries, the Go var, the
// freshness test) from a .tsx entry; a planted compile error fails the
// step with the .tsx file and line and writes nothing; regeneration is
// idempotent; and the real command (`go run .`, what go generate
// drives) exits non-zero on stderr with the same message.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui/script"
)

const labelTsx = `
export function label(n: number) {
	return <text id={"row-" + n}>{` + "`row ${n}`" + `}</text>;
}
`

const viewTsx = `
import { label } from "./label";
export function App() {
	return <div id="list">{[0, 1].map((i) => label(i))}</div>;
}
`

// writeSources lays out a two-file UI (the entry imports a module)
// under ui/ of root — the real generate layout: go:generate runs in
// the app's package directory, the entry is under it, and the
// generated files land in it, so the recorded paths read like
// "ui/view.tsx".
func writeSources(t *testing.T, root string) string {
	t.Helper()
	ui := filepath.Join(root, "ui")
	if err := os.MkdirAll(ui, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{"label.tsx": labelTsx, "view.tsx": viewTsx} {
		if err := os.WriteFile(filepath.Join(ui, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(ui, "view.tsx")
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestGenerateWritesTheEmbeddedBundleFiles(t *testing.T) {
	root := t.TempDir()
	entry := writeSources(t, root)

	if err := run([]string{
		"-entry", entry,
		"-out", root,
		"-var", "UI",
		"-package", "app",
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	// The bundle JS: esbuild's IIFE on the host global, with the
	// inline source map — one self-contained file, no imports.
	js := read(t, filepath.Join(root, "ui.tsxbundle.js"))
	if !strings.Contains(js, "var "+script.GlobalName) || strings.Contains(js, "import ") {
		t.Fatalf("the generated JS is not a self-contained bundle:\n%s", js)
	}
	if !strings.Contains(js, "sourceMappingURL=data:application/json;base64") {
		t.Fatalf("the generated JS has no inline source map:\n%s", js)
	}

	// The Go file: the embed directive, the EmbeddedBundle var with the
	// hashed sources, the package clause — and it parses.
	goSrc := read(t, filepath.Join(root, "ui.tsxbundle.go"))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "ui.tsxbundle.go", goSrc, parser.ParseComments)
	if err != nil {
		t.Fatalf("the generated Go file does not parse: %v\n%s", err, goSrc)
	}
	if file.Name.Name != "app" {
		t.Fatalf("package clause = %q, want the -package flag", file.Name.Name)
	}
	var hasEmbed, hasVar bool
	ast.Inspect(file, func(n ast.Node) bool {
		decl, ok := n.(*ast.GenDecl)
		if !ok || len(decl.Specs) != 1 {
			return true
		}
		vs, ok := decl.Specs[0].(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 {
			return true
		}
		// The //go:embed directive sits on the JS var's declaration doc.
		doc := decl.Doc
		if vs.Doc != nil {
			doc = vs.Doc
		}
		if doc != nil {
			for _, c := range doc.List {
				if strings.TrimSpace(c.Text) == "//go:embed ui.tsxbundle.js" {
					hasEmbed = true
				}
			}
		}
		if vs.Names[0].Name == "UI" && len(vs.Values) == 1 {
			if _, ok := vs.Values[0].(*ast.CompositeLit); ok {
				hasVar = true
			}
		}
		return true
	})
	if !hasEmbed || !hasVar {
		t.Fatalf("the generated Go file lacks the embed var:\n%s", goSrc)
	}
	if !strings.Contains(goSrc, "script.EmbeddedBundle") {
		t.Fatalf("the generated Go file has no EmbeddedBundle:\n%s", goSrc)
	}
	// Both sources are recorded with their paths relative to the
	// generated file's directory (where the embed and the test live).
	if !strings.Contains(goSrc, `Path: "ui/view.tsx"`) || !strings.Contains(goSrc, `Path: "ui/label.tsx"`) {
		t.Fatalf("the generated Go file does not record the sources:\n%s", goSrc)
	}
	if !strings.Contains(goSrc, `Hash: "sha256:`) {
		t.Fatalf("the generated Go file records no hashes:\n%s", goSrc)
	}

	// The freshness test: a _test.go file that calls CheckFresh on the
	// var, in the same package.
	testSrc := read(t, filepath.Join(root, "ui.tsxbundle_test.go"))
	if !strings.Contains(testSrc, "UI.CheckFresh(\".\")") || !strings.Contains(testSrc, "package app") {
		t.Fatalf("the generated test does not guard freshness:\n%s", testSrc)
	}
	if _, err := parser.ParseFile(fset, "ui.tsxbundle_test.go", testSrc, parser.ParseComments); err != nil {
		t.Fatalf("the generated test does not parse: %v\n%s", err, testSrc)
	}
}

func TestTheGeneratedSourceHashMatchesCheckFresh(t *testing.T) {
	root := t.TempDir()
	entry := writeSources(t, root)
	if err := run([]string{"-entry", entry, "-out", root}); err != nil {
		t.Fatalf("run: %v", err)
	}
	goSrc := read(t, filepath.Join(root, "view.tsxbundle.go"))

	// Rebuild the recorded hash by hand and find it in the file: the
	// generator's hashing must be CheckFresh's.
	data, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	want := script.HashSource(data)
	if !strings.Contains(goSrc, want) {
		t.Fatalf("the generated hash %q is not in:\n%s", want, goSrc)
	}
}

func TestGenerateIsIdempotent(t *testing.T) {
	root := t.TempDir()
	entry := writeSources(t, root)
	args := []string{"-entry", entry, "-out", root, "-var", "UI"}
	if err := run(args); err != nil {
		t.Fatalf("first run: %v", err)
	}
	js := read(t, filepath.Join(root, "ui.tsxbundle.js"))
	goSrc := read(t, filepath.Join(root, "ui.tsxbundle.go"))

	// Regenerating with unchanged sources rewrites the same bytes, so
	// committing generated files shows no churn.
	if err := run(args); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := read(t, filepath.Join(root, "ui.tsxbundle.js")); got != js {
		t.Fatal("the JS changed on regeneration")
	}
	if got := read(t, filepath.Join(root, "ui.tsxbundle.go")); got != goSrc {
		t.Fatal("the Go file changed on regeneration")
	}
}

func TestAPlantedCompileErrorFailsTheStepWithTheTsxFileAndLine(t *testing.T) {
	root := t.TempDir()
	ui := filepath.Join(root, "ui")
	if err := os.MkdirAll(ui, 0o755); err != nil {
		t.Fatal(err)
	}
	broken := "export function App() {\n\treturn <div id=\"box\">\n}\n" // unclosed JSX, line 3
	if err := os.WriteFile(filepath.Join(ui, "view.tsx"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	err := run([]string{"-entry", filepath.Join(ui, "view.tsx"), "-out", root})
	if err == nil {
		t.Fatal("a syntax error generated a bundle")
	}
	// The message carries the .tsx file and the line, so the generate
	// step (and any build running it) fails with a source position.
	if !strings.Contains(err.Error(), "view.tsx:") || !strings.Contains(err.Error(), ":3:") {
		t.Fatalf("the error does not carry the .tsx file:line: %v", err)
	}
	// And nothing was written: a failed generate leaves no stale files.
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 { // the ui/ directory with the broken source
		t.Fatalf("the failed run wrote files: %v", entries)
	}
}

func TestAMissingEntryFails(t *testing.T) {
	root := t.TempDir()
	err := run([]string{"-entry", filepath.Join(root, "nope.tsx"), "-out", root})
	if err == nil {
		t.Fatal("a missing entry generated a bundle")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatalf("the failed run wrote files: %v", entries)
	}
}

func TestBadFlagsFail(t *testing.T) {
	if err := run(nil); err == nil {
		t.Fatal("no -entry generated a bundle")
	}
	if err := run([]string{"-entry", "view.tsx", "-var", "1bad"}); err == nil {
		t.Fatal("a non-identifier -var generated a bundle")
	}
}

func TestTheCommandItselfFailsOnACompileError(t *testing.T) {
	// The real command go generate drives (go run .), not run in a
	// process: a planted error must fail the step — non-zero exit, the
	// .tsx file:line on stderr.
	root := t.TempDir()
	ui := filepath.Join(root, "ui")
	if err := os.MkdirAll(ui, 0o755); err != nil {
		t.Fatal(err)
	}
	broken := "export function App() {\n\treturn <div id=\"box\">\n}\n" // unclosed JSX, line 3
	if err := os.WriteFile(filepath.Join(ui, "view.tsx"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if testing.Short() {
		t.Skip("builds the tool")
	}
	cmd := exec.Command("go", "run", ".", "-entry", filepath.Join(ui, "view.tsx"), "-out", root)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the command succeeded on a broken .tsx\n%s", out)
	}
	if exit := cmd.ProcessState.ExitCode(); exit != 1 { // a build failure, not a panic
		t.Fatalf("exit code = %d, want 1\n%s", exit, out)
	}
	if !strings.Contains(string(out), "view.tsx:3:") {
		t.Fatalf("the command's message does not carry the .tsx file:line:\n%s", out)
	}
}
