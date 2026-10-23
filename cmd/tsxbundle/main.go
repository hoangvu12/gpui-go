// Command tsxbundle is the build-time half of TSX release bundling
// (ADR 0003, ticket 65): it compiles a .tsx UI with esbuild — through
// gpui/script/compile, the development-only package — and writes, into
// the app's package directory, three files (named after the var):
//
//	ui.tsxbundle.js       the compiled ES2017 bundle (inline source map)
//	ui.tsxbundle.go       the script.EmbeddedBundle var //go:embed picks up
//	ui.tsxbundle_test.go  the freshness guard (EmbeddedBundle.CheckFresh)
//
// The app drives it with a go:generate directive, so the compile runs
// at build time and `go run` carries the tool with the module (nothing
// to install, its version cannot drift from the app):
//
//	//go:generate go run github.com/hoangvu12/gpui-go/cmd/tsxbundle -var UI -entry ui/app.tsx
//
// go generate runs in the directive's directory, so -entry is relative
// to the app's package and the files land beside it, where //go:embed
// can reach them (embed never crosses the package directory). A
// compile error fails the command — and so the generate step, and any
// build that runs it — with the .tsx file and line.
//
// The app imports neither this tool nor gpui/script/compile: esbuild
// stays out of the release import graph (go list -deps proves it), and
// the shipped binary reads no scripts from disk (the bundle is
// embedded). Regeneration is idempotent — unchanged sources rewrite
// the same bytes — so committing the generated files shows no churn.
//
// Why go:generate and not a build hook: Go has no build.rs; go build
// never runs code, so the idiomatic pre-build step is a go:generate
// directive the developer (or CI) runs before building. The flip side
// is staleness — go build does not rerun the generator either — which
// is what the generated test guards: it fails when a .tsx changed
// since the bundle was compiled (see EmbeddedBundle.CheckFresh).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/hoangvu12/gpui-go/gpui/script"
	"github.com/hoangvu12/gpui-go/gpui/script/compile"
)

// toolName names the generator in diagnostics and the generated code.
const toolName = "tsxbundle"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, toolName+":", err)
		os.Exit(1)
	}
}

// run is the command, testable without a process: it parses the flags,
// compiles the entry and writes the generated files.
func run(args []string) error {
	flags := flag.NewFlagSet(toolName, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	entry := flags.String("entry", "", "the .tsx entry, relative to the working directory (go:generate runs in the app's package)")
	out := flags.String("out", ".", "directory the generated files are written to (//go:embed reads them there)")
	pkg := flags.String("package", "main", "package clause of the generated Go file")
	name := flags.String("var", "", "name of the generated script.EmbeddedBundle var (default: the entry's name, Go-cased)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *entry == "" {
		flags.Usage()
		return fmt.Errorf("-entry is required")
	}
	if *name == "" {
		*name = goCased(stem(*entry))
	}
	if !isIdentifier(*name) {
		return fmt.Errorf("-var %q is not a Go identifier", *name)
	}
	if !isIdentifier(*pkg) {
		return fmt.Errorf("-package %q is not a Go package name", *pkg)
	}

	// The compile: esbuild's error (file:line:column) fails the step.
	bundle, inputs, err := compile.Build(*entry)
	if err != nil {
		return err
	}

	// The recorded source paths are relative to the output directory —
	// where the generated Go file, the embed and the freshness test
	// live, and where go:generate and go test both run — so the guard
	// can re-read them from ".". Both sides are made absolute first, so
	// a relative -entry with a relative -out and a manual absolute
	// invocation both work.
	absOut, err := filepath.Abs(*out)
	if err != nil {
		return err
	}
	entryDir := filepath.Dir(filepath.Clean(*entry))
	sources := make([]script.EmbeddedSource, 0, len(inputs))
	for _, in := range inputs {
		path := filepath.Join(entryDir, in)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", in, err)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(absOut, abs)
		if err != nil {
			return err
		}
		sources = append(sources, script.EmbeddedSource{
			Path: filepath.ToSlash(rel),
			Hash: script.HashSource(data),
		})
	}

	// File names come from the var: ui.tsxbundle.js for UI. The three
	// files are one family — regenerate replaces all of them.
	base := strings.ToLower(*name) + ".tsxbundle"
	jsPath := filepath.Join(*out, base+".js")
	goPath := filepath.Join(*out, base+".go")
	testPath := filepath.Join(*out, base+"_test.go")

	embedded := script.EmbeddedBundle{
		Name:    filepath.Base(jsPath),
		JS:      "", // read back through //go:embed, not duplicated here
		Tool:    toolName,
		Sources: sources,
	}

	goSrc, err := format.Source([]byte(goFileSource(*pkg, *name, embedded, jsPath)))
	if err != nil {
		return fmt.Errorf("generated Go does not gofmt: %w", err)
	}
	testSrc, err := format.Source([]byte(testFileSource(*pkg, *name)))
	if err != nil {
		return fmt.Errorf("generated test does not gofmt: %w", err)
	}

	writes := []struct{ path, content string }{
		{jsPath, bundle.JS},
		{goPath, string(goSrc)},
		{testPath, string(testSrc)},
	}
	for _, w := range writes {
		if err := writeIfChanged(w.path, []byte(w.content)); err != nil {
			return err
		}
	}
	fmt.Printf("%s: %s (%d source%s, %d bytes)\n",
		toolName, filepath.Base(jsPath), len(sources), plural(len(sources)), len(bundle.JS))
	return nil
}

// goFileSource is the generated Go file: the embed var the compiler
// fills from the .js beside it, and the EmbeddedBundle value the app
// loads. The var and the .js file are fixed at generate time; the JS
// itself stays in the .js file so the diff of an edit shows the
// compiled change, not a re-quoted Go string.
func goFileSource(pkg, name string, embedded script.EmbeddedBundle, jsPath string) string {
	jsFile := filepath.ToSlash(filepath.Base(jsPath))
	jsVar := name + "JS"
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by %s (go run github.com/hoangvu12/gpui-go/cmd/%s); DO NOT EDIT.\n", toolName, toolName)
	b.WriteString("// Regenerate after editing the .tsx sources: go generate ./...\n\n")
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	b.WriteString("import (\n")
	b.WriteString("\t_ \"embed\" // go:embed the compiled bundle below\n\n")
	b.WriteString("\t\"github.com/hoangvu12/gpui-go/gpui/script\"\n")
	b.WriteString(")\n\n")
	fmt.Fprintf(&b, "// The compiled UI, embedded at build time: esbuild ran when this file\n")
	fmt.Fprintf(&b, "// was generated, so the binary reads no scripts from disk and links no\n")
	fmt.Fprintf(&b, "// esbuild (gpui/script/compile stays out of the app's import graph).\n")
	fmt.Fprintf(&b, "// The JS is the %s file this package embeds; the sources\n", jsFile)
	fmt.Fprintf(&b, "// below are the .tsx files it was compiled from, hashed for the\n")
	fmt.Fprintf(&b, "// freshness guard.\n\n")
	fmt.Fprintf(&b, "//go:embed %s\n", jsFile)
	fmt.Fprintf(&b, "var %s string\n\n", jsVar)
	fmt.Fprintf(&b, "// %s is the app's embedded TSX bundle.\n", name)
	fmt.Fprintf(&b, "//\n")
	fmt.Fprintf(&b, "//\thost := script.New(script.WithoutBanner())\n")
	fmt.Fprintf(&b, "//\tif err := host.Load(%s.Bundle()); err != nil {\n", name)
	fmt.Fprintf(&b, "//\t\tlog.Fatal(err)\n")
	fmt.Fprintf(&b, "//\t}\n")
	fmt.Fprintf(&b, "var %s = script.EmbeddedBundle{\n", name)
	fmt.Fprintf(&b, "\tName: %q,\n", embedded.Name)
	fmt.Fprintf(&b, "\tJS:   %s,\n", jsVar)
	fmt.Fprintf(&b, "\tTool: %q,\n", embedded.Tool)
	fmt.Fprintf(&b, "\tSources: []script.EmbeddedSource{\n")
	for _, s := range embedded.Sources {
		fmt.Fprintf(&b, "\t\t{Path: %q, Hash: %q},\n", s.Path, s.Hash)
	}
	b.WriteString("\t},\n}\n")
	return b.String()
}

// testFileSource is the generated staleness guard: go build does not
// rerun go generate, so the test is where an out-of-date bundle is
// caught before a release ships the old UI.
func testFileSource(pkg, name string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by %s (go run github.com/hoangvu12/gpui-go/cmd/%s); DO NOT EDIT.\n\n", toolName, toolName)
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	b.WriteString("import \"testing\"\n\n")
	fmt.Fprintf(&b, "// Test%sIsFresh fails when a .tsx source changed after the embedded\n", name)
	b.WriteString("// bundle was compiled from it: regenerate with go generate ./...\n")
	b.WriteString("// before building a release.\n")
	fmt.Fprintf(&b, "func Test%sIsFresh(t *testing.T) {\n", name)
	fmt.Fprintf(&b, "\tif err := %s.CheckFresh(\".\"); err != nil {\n", name)
	b.WriteString("\t\tt.Fatal(err)\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n")
	return b.String()
}

// writeIfChanged writes the file only when its bytes differ, so a
// regeneration over unchanged sources does not touch it (a committed
// generated file shows no churn and nothing rebuilds).
func writeIfChanged(path string, content []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, content) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

// stem is the entry's file name without its extension.
func stem(entry string) string {
	base := filepath.Base(entry)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// goCased turns a file name into a Go identifier name: hello_world →
// HelloWorld, app → App.
func goCased(s string) string {
	var b strings.Builder
	upper := true
	for _, r := range s {
		switch {
		case r == '_' || r == '-' || r == '.' || r == ' ':
			upper = true
		case upper:
			b.WriteRune(unicode.ToUpper(r))
			upper = false
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isIdentifier reports whether s is a Go identifier.
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || unicode.IsLetter(r) {
			continue
		}
		if i > 0 && unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return true
}

// plural is the plural s for n, for the run summary.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
