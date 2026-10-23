// Package compile is the development-only half of the TSX script host
// (ADR 0003): esbuild lowers .tsx sources to the ES2017 floor, bundles
// imports into one IIFE (goja has no ES modules) and keeps an inline
// source map so errors carry the .tsx file and line. The host package
// (gpui/script) runs the result; it never imports this one, so a
// release build that embeds a prebuilt bundle never links esbuild —
// only development builds and build-time tooling do.
//
// A release app compiles its .tsx ahead of time (a build step or
// go:generate invoking this package), embeds the output and loads it
// with script.New().Load; a compile error then fails the build with
// the .tsx file and line, exactly as a failed dev reload reports it.
package compile

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/hoangvu12/gpui-go/gpui/script"
)

// commonOptions are the shared esbuild settings: the ADR's compile
// contract (ES2017 floor, JSX classic factory, bundled, source maps
// kept), minus the entry source.
func commonOptions() api.BuildOptions {
	return api.BuildOptions{
		Bundle:      true,
		Format:      api.FormatIIFE,
		GlobalName:  script.GlobalName,
		Target:      api.ES2017,
		JSX:         api.JSXTransform,
		JSXFactory:  "h",
		JSXFragment: "Fragment",
		Sourcemap:   api.SourceMapInline,
		Write:       false,
	}
}

// String compiles one .tsx source held in memory: sourcefile is its
// name for diagnostics (and for the source map). Imports cannot
// resolve here — use Files or a Dir for multi-file sources.
func String(sourcefile, src string) (script.Bundle, error) {
	opts := commonOptions()
	opts.Stdin = &api.StdinOptions{
		Contents:   src,
		Sourcefile: sourcefile,
		Loader:     api.LoaderTSX,
		ResolveDir: ".",
	}
	return bundle(api.Build(opts), bundleName(sourcefile))
}

// bundleName names the compiled bundle after its entry, with the .js
// extension the output actually has (diagnostics only: positions map
// back to the .tsx sources through the inline source map).
func bundleName(entry string) string {
	return strings.TrimSuffix(filepath.Base(entry), filepath.Ext(entry)) + ".js"
}

// Files compiles the entry .tsx and everything it imports from disk
// into one bundle (the development and build-time path). Diagnostics
// name files relative to the entry's directory.
func Files(entry string) (script.Bundle, error) {
	dir, base := filepath.Split(filepath.Clean(entry))
	opts := commonOptions()
	opts.AbsWorkingDir = dir
	opts.EntryPoints = []string{base}
	return bundle(api.Build(opts), bundleName(entry))
}

// bundle reads esbuild's result: the code, or the errors joined with
// each one's .tsx file, line and column.
func bundle(result api.BuildResult, name string) (script.Bundle, error) {
	if len(result.Errors) > 0 {
		return script.Bundle{}, buildError(result.Errors)
	}
	var js strings.Builder
	for _, f := range result.OutputFiles {
		js.Write(f.Contents)
	}
	if js.Len() == 0 {
		return script.Bundle{}, errors.New("compile: esbuild produced no output for " + name)
	}
	return script.Bundle{Name: name, JS: js.String()}, nil
}

// buildError formats esbuild's messages as file:line:column: text.
func buildError(errs []api.Message) error {
	var msgs []string
	for _, e := range errs {
		if e.Location != nil {
			msgs = append(msgs, fmt.Sprintf("%s:%d:%d: %s",
				e.Location.File, e.Location.Line, e.Location.Column+1, e.Text))
			continue
		}
		msgs = append(msgs, e.Text)
	}
	return errors.New(strings.Join(msgs, "\n"))
}
