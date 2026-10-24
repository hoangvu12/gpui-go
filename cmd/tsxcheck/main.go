// Command tsxcheck type-checks .tsx views against the engine's TSX
// surface (ticket 63, ADR 0003's "check command runs tsc"): it runs
// tsc — with the generated gpui-script.d.ts the module carries — over
// the given sources, so a misspelled style key, a bad value or an
// unknown prop is caught before the app runs, the way twcheck catches
// bad Class strings on the Go side.
//
//	go run github.com/hoangvu12/gpui-go/cmd/tsxcheck [-typescript path] [-strict] [-init] <dir-or-view.tsx>...
//
// Each argument is a .tsx entry or a directory (every .tsx and .ts
// under it, node_modules and hidden directories skipped); tsc follows
// the entry's imports. The flags:
//
//	-typescript path   pin the tsc to run (a tsc.js or an executable);
//	                   the GPUI_TYPESCRIPT environment variable does
//	                   the same; without it the checker looks for a
//	                   typescript package in node_modules above the
//	                   sources, the npm global install, and tsc on PATH.
//	                   Nothing is installed and no network is touched:
//	                   without a tsc the command explains that and
//	                   exits 3.
//	-strict            run tsc in strict mode (the default checks the
//	                   surface, not style: types are advisory to esbuild).
//	-init              write gpui-script.d.ts beside the sources (for
//	                   editors, which read it from disk) and exit.
//
// The exit status is 0 when the sources type-check, 1 when tsc found
// problems (one line each, file:line:col), 2 on a usage error and 3
// when no tsc was found. The debug host does the same check after
// every reload: script.WithTypeCheck(types.NewChecker(dir)).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/hoangvu12/gpui-go/gpui/script/types"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the command body, with the output streams and the exit status
// returned so the tests drive it without a process.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tsxcheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	program := fs.String("typescript", "", "the tsc to run (a tsc.js or an executable; default GPUI_TYPESCRIPT, then discovery)")
	strict := fs.Bool("strict", false, "run tsc in strict mode")
	init := fs.Bool("init", false, "write gpui-script.d.ts beside the sources and exit")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: tsxcheck [-typescript path] [-strict] [-init] <dir-or-view.tsx>...")
		fmt.Fprintln(stderr, "Type-checks .tsx views against the engine's TSX surface (tsc + the generated .d.ts).")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return 2
	}
	target := fs.Arg(0)
	info, err := os.Stat(target)
	if err != nil {
		fmt.Fprintf(stderr, "tsxcheck: %v\n", err)
		return 2
	}
	dir := filepath.Dir(target)
	if info.IsDir() {
		dir = target
	}
	if *init {
		path := filepath.Join(dir, types.DTSFilename)
		if err := os.WriteFile(path, []byte(types.DTS()), 0o644); err != nil {
			fmt.Fprintf(stderr, "tsxcheck: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "tsxcheck: wrote %s (editors read it from disk; the check itself always uses the module's current one)\n", path)
		return 0
	}

	var opts []types.Option
	if *program != "" {
		opts = append(opts, types.WithProgram(*program))
	}
	if *strict {
		opts = append(opts, types.WithStrict())
	}
	c := types.NewChecker(dir, opts...)
	diags, err := c.Check()
	if err != nil {
		fmt.Fprintf(stderr, "tsxcheck: %v\n", err)
		return 3
	}
	status := 0
	for _, d := range diags {
		fmt.Fprintln(stdout, d)
		if d.Kind == "error" {
			status = 1
		}
	}
	if status == 0 {
		fmt.Fprintf(stdout, "tsxcheck: %s type-checks clean\n", dir)
	}
	return status
}
