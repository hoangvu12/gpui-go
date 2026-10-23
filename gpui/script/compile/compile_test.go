package compile_test

// The compile package's behaviour (ticket 62): esbuild lowers .tsx to
// the ES2017 floor, bundles imports (goja has no ES modules), and keeps
// an inline source map so errors carry the .tsx file and line.
//
// The compile path is development-only (ADR 0003): a release build
// embeds a bundle compiled ahead of time and never imports this
// package, which is why esbuild lives here and not in gpui/script.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui/script"
	"github.com/hoangvu12/gpui-go/gpui/script/compile"
)

const counterTsx = `export function App() {
	return <div id="box">{bind(state("clicks", 0), (n) => ` + "`Clicked ${n}`" + `)}</div>;
}
`

// mustString compiles in-memory TSX.
func mustString(t testing.TB, name, src string) script.Bundle {
	t.Helper()
	b, err := compile.String(name, src)
	if err != nil {
		t.Fatalf("compile.String(%s): %v", name, err)
	}
	return b
}

func TestStringCompilesTSXToAnES2017Bundle(t *testing.T) {
	b := mustString(t, "view.tsx", counterTsx)
	if b.Name != "view.js" {
		t.Errorf("bundle name = %q, want the entry's name with the output extension", b.Name)
	}
	if !strings.Contains(b.JS, "function App()") {
		t.Errorf("the bundle has no App function:\n%s", b.JS)
	}
	// goja has no ES modules: the bundle must be a plain IIFE whose
	// exports land on the host's global.
	if !strings.Contains(b.JS, "var "+script.GlobalName) {
		t.Errorf("the bundle does not assign the %s global:\n%s", script.GlobalName, b.JS)
	}
	// JSX must be lowered: no raw JSX reaches goja.
	if strings.Contains(b.JS, "<div") {
		t.Errorf("the bundle still contains JSX:\n%s", b.JS)
	}
	// The inline source map (runtime errors map back to the .tsx).
	if !strings.Contains(b.JS, "sourceMappingURL=data:application/json;base64") {
		t.Errorf("the bundle has no inline source map:\n%s", b.JS)
	}
}

func TestASyntaxErrorReportsTheTsxFileAndLine(t *testing.T) {
	src := "export function App() {\n\treturn <div id=\"box\">\n}\n" // unclosed JSX
	_, err := compile.String("view.tsx", src)
	if err == nil {
		t.Fatal("a syntax error compiled")
	}
	// The message names the .tsx file, a line and a column.
	if !strings.Contains(err.Error(), "view.tsx:") {
		t.Fatalf("the error does not name the file: %v", err)
	}
	if !strings.Contains(err.Error(), ":3:") {
		t.Fatalf("the error does not name the line: %v", err)
	}
	t.Logf("syntax error: %v", err)
}

func TestFilesBundlesImportsFromDisk(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("label.tsx", `
export function label(n: number) {
	return <text id={"row-" + n}>{`+"`row ${n}`"+`}</text>;
}
`)
	write("view.tsx", `
import { label } from "./label";
export function App() {
	return <div id="list">{[0, 1].map((i) => label(i))}</div>;
}
`)
	b, err := compile.Files(filepath.Join(dir, "view.tsx"))
	if err != nil {
		t.Fatalf("compile.Files: %v", err)
	}
	if !strings.Contains(b.JS, "row ") {
		t.Errorf("the imported module was not bundled in:\n%s", b.JS)
	}
	// One self-contained output: no further files are read at run time.
	if strings.Contains(b.JS, "import ") {
		t.Errorf("the bundle still contains an import statement:\n%s", b.JS)
	}
}

func TestFilesReportsTheErroringModuleFileAndLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "label.tsx"), []byte("export function label() {\n\treturn <text>hi</text\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "view.tsx"), []byte("import { label } from \"./label\";\nexport function App() {\n\treturn <div>{label()}</div>;\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := compile.Files(filepath.Join(dir, "view.tsx"))
	if err == nil {
		t.Fatal("a syntax error in an imported module compiled")
	}
	if !strings.Contains(err.Error(), "label.tsx:") {
		t.Fatalf("the error does not name the imported module's file: %v", err)
	}
	t.Logf("imported-module error: %v", err)
}

func TestFilesRejectsAMissingEntry(t *testing.T) {
	_, err := compile.Files(filepath.Join(t.TempDir(), "nope.tsx"))
	if err == nil {
		t.Fatal("a missing entry compiled")
	}
}
