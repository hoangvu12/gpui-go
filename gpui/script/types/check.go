package types

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/hoangvu12/gpui-go/gpui/script"
)

// Checker type-checks a directory of .tsx sources against the
// generated .d.ts with tsc (the check the ticket's "check command"
// runs and a debug host runs after each reload). Constructed with the
// sources' directory; Check runs tsc and reports its diagnostics.
//
//	tsc := types.NewChecker("ui", types.WithProgram(pathToTsc))
//	script.New(script.WithSource(src), script.WithTypeCheck(tsc))
//
// tsc is found once, on first use: an explicit program (WithProgram or
// the GPUI_TYPESCRIPT environment variable, a tsc.js or an executable),
// a typescript package in node_modules above the sources, the npm
// global one, or tsc on PATH. Nothing is installed and no network is
// touched; when tsc is absent Check returns an error that names every
// place it looked and how to get tsc (a local `npm i -D typescript`
// suffices — the checker runs it with node, no global install).
type Checker struct {
	dir     string
	program string // an explicit tsc program (flag or environment)
	strict  bool

	// resolved is the cached tsc invocation, found on first Check.
	resolved *tscCommand
}

// Option configures a checker.
type Option func(*Checker)

// WithProgram sets the tsc to run explicitly: the path of a tsc.js
// (run with node) or a tsc executable. CI machines pin one; the
// GPUI_TYPESCRIPT environment variable does the same.
func WithProgram(path string) Option {
	return func(c *Checker) { c.program = path }
}

// WithStrict enables tsc's strict mode for the check (the default
// matches esbuild's stance: types are advisory, so the check reports
// surface misuse, not style).
func WithStrict() Option {
	return func(c *Checker) { c.strict = true }
}

// NewChecker returns a checker for the sources in dir.
func NewChecker(dir string, opts ...Option) *Checker {
	c := &Checker{dir: dir, program: os.Getenv(EnvTypeScript)}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// EnvTypeScript names the environment variable that pins the tsc to
// run (a tsc.js path or an executable), for CI and tests.
const EnvTypeScript = "GPUI_TYPESCRIPT"

// Check type-checks the .tsx and .ts sources under the checker's
// directory against the generated .d.ts, reporting tsc's diagnostics
// (every one carries the .tsx file, line and column). Sources are
// walked once per check (node_modules and hidden directories are
// skipped); the .d.ts itself runs from a temp directory, so nothing is
// written into the app's sources.
func (c *Checker) Check() ([]script.TypeDiagnostic, error) {
	files, err := sourceFiles(c.dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("script/types: no .tsx or .ts sources under %s", c.dir)
	}
	cmd, err := c.tsc()
	if err != nil {
		return nil, err
	}
	// The declarations run from a temp directory (an app's own
	// gpui-script.d.ts copy, written with tsxcheck -init for its
	// editor, is not a tsc command-line input and so is not double
	// declared).
	tmp, err := os.MkdirTemp("", "gpui-script-types")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	dtsPath := filepath.Join(tmp, DTSFilename)
	if err := os.WriteFile(dtsPath, []byte(dts), 0o644); err != nil {
		return nil, err
	}
	args := append([]string{
		"--noEmit",
		"--jsx", "react",
		"--jsxFactory", "h",
		"--jsxFragmentFactory", "Fragment",
		"--target", "es2017",
		"--lib", "es2017",
		"--skipLibCheck",
		"--pretty", "false",
	}, cmd.tscArgs...)
	if c.strict {
		args = append(args, "--strict")
	}
	for _, f := range files {
		args = append(args, f)
	}
	args = append(args, dtsPath)

	var out bytes.Buffer
	run := cmd.build(c.dir, args, &out)
	runErr := run()
	diags := parseDiagnostics(out.String(), c.dir)
	if runErr != nil && len(diags) == 0 {
		// tsc itself failed (a config or crash): no diagnostics, and
		// its exit status names the problem.
		return nil, fmt.Errorf("script/types: tsc: %w: %s", runErr, clipOutput(out.String()))
	}
	return diags, nil
}

// Available reports whether a tsc was found (it resolves on first
// use, so this probes without side effects beyond the search).
func (c *Checker) Available() bool {
	_, err := c.tsc()
	return err == nil
}

// tscCommand is one resolved tsc: either node running a tsc.js, or an
// executable run directly.
type tscCommand struct {
	// node is the node executable (empty when prog runs standalone).
	node string
	// prog is the tsc to run: a tsc.js when node is set, else an
	// executable.
	prog string
	// tscArgs are the extra arguments the program itself needs.
	tscArgs []string
}

// build returns a runnable command for one check.
func (t *tscCommand) build(dir string, args []string, out *bytes.Buffer) func() error {
	return func() error {
		var cmd *exec.Cmd
		if t.node != "" {
			cmd = exec.Command(t.node, append([]string{t.prog}, args...)...)
		} else {
			cmd = exec.Command(t.prog, args...)
		}
		cmd.Dir = dir
		cmd.Stdout = out
		cmd.Stderr = out
		return cmd.Run()
	}
}

// tsc resolves the tsc to run once.
func (c *Checker) tsc() (*tscCommand, error) {
	if c.resolved != nil {
		return c.resolved, nil
	}
	cmd, err := findTypeScript(c.dir, c.program)
	if err != nil {
		return nil, err
	}
	c.resolved = cmd
	return cmd, nil
}

// findTypeScript locates a tsc: the explicit program first (a .js run
// with node, anything else run directly), then a typescript package
// in node_modules above dir, then the npm global one, then tsc on
// PATH.
func findTypeScript(dir, program string) (*tscCommand, error) {
	if program != "" {
		return commandFor(program)
	}
	// A typescript package above the sources (npm i -D typescript).
	for d := dir; ; d = filepath.Dir(d) {
		if tsc := filepath.Join(d, "node_modules", "typescript", "lib", "tsc.js"); fileExists(tsc) {
			if node, err := exec.LookPath("node"); err == nil {
				return &tscCommand{node: node, prog: tsc}, nil
			}
			return nil, fmt.Errorf("%w: %s is a tsc.js but node was not found on PATH", ErrTypeScriptNotFound, tsc)
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
	}
	// The npm global install (npm root -g without spawning npm: the
	// default prefixes).
	var globals []string
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		globals = append(globals, filepath.Join(appdata, "npm", "node_modules", "typescript", "lib", "tsc.js"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		globals = append(globals, filepath.Join(home, ".npm-global", "lib", "node_modules", "typescript", "lib", "tsc.js"))
	}
	for _, tsc := range globals {
		if fileExists(tsc) {
			if node, err := exec.LookPath("node"); err == nil {
				return &tscCommand{node: node, prog: tsc}, nil
			}
		}
	}
	// A tsc on PATH (an executable, not npm's shell shim).
	if tsc, err := exec.LookPath("tsc"); err == nil {
		return &tscCommand{prog: tsc}, nil
	}
	return nil, fmt.Errorf("%w (looked for a typescript package in node_modules above %s, the npm global install, and tsc on PATH; install one locally with `npm i -D typescript` — nothing global is needed — or point at one with %s)", ErrTypeScriptNotFound, dir, EnvTypeScript)
}

// commandFor turns an explicit program into a command: a .js (or a
// directory holding one) runs with node, anything else runs directly.
func commandFor(program string) (*tscCommand, error) {
	info, err := os.Stat(program)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v (point at a tsc.js or a tsc executable; a local `npm i -D typescript` provides one — nothing global is needed — and %s pins it)", ErrTypeScriptNotFound, program, err, EnvTypeScript)
	}
	if info.IsDir() {
		// A typescript package directory.
		program = filepath.Join(program, "lib", "tsc.js")
		if !fileExists(program) {
			return nil, fmt.Errorf("%w: %s has no lib/tsc.js", ErrTypeScriptNotFound, info.Name())
		}
	}
	if strings.HasSuffix(program, ".js") {
		node, err := exec.LookPath("node")
		if err != nil {
			return nil, fmt.Errorf("%w: %s needs node, which was not found on PATH", ErrTypeScriptNotFound, program)
		}
		return &tscCommand{node: node, prog: program}, nil
	}
	return &tscCommand{prog: program}, nil
}

// ErrTypeScriptNotFound reports that no tsc is available to check
// types with.
var ErrTypeScriptNotFound = errors.New("script/types: tsc (TypeScript) was not found")

// sourceFiles returns the .tsx and .ts sources under dir, sorted
// (node_modules and hidden or _-prefixed directories are skipped, like
// Go tooling skips them).
func sourceFiles(dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("script/types: %w", err)
	}
	if !info.IsDir() {
		return []string{dir}, nil
	}
	var out []string
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == dir {
				return nil
			}
			name := d.Name()
			if name == "node_modules" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			return nil
		}
		if ext := filepath.Ext(name); ext == ".tsx" || ext == ".ts" {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// diagLine matches tsc's --pretty false output: path(line,col): error
// TS2322: message.
var diagLine = regexp.MustCompile(`^(.*)\((\d+),(\d+)\): (error|warning) (TS\d+): (.*)$`)

// parseDiagnostics reads tsc's output into diagnostics, with paths
// relative to dir.
func parseDiagnostics(out, dir string) []script.TypeDiagnostic {
	var diags []script.TypeDiagnostic
	var current *script.TypeDiagnostic
	for _, line := range strings.Split(out, "\n") {
		if m := diagLine.FindStringSubmatch(line); m != nil {
			lineNo, _ := strconv.Atoi(m[2])
			col, _ := strconv.Atoi(m[3])
			current = &script.TypeDiagnostic{
				Kind:    m[4],
				Code:    m[5],
				File:    relPath(dir, m[1]),
				Line:    lineNo,
				Column:  col,
				Message: m[6],
			}
			diags = append(diags, *current)
			continue
		}
		if current != nil && strings.HasPrefix(line, "  ") {
			// A continuation of the previous message (the "Did you
			// mean …" hints).
			diags[len(diags)-1].Message += "\n" + strings.TrimSpace(line)
		}
		if strings.TrimSpace(line) == "" {
			current = nil
		}
	}
	return diags
}

// relPath makes an absolute diagnostic path relative to dir when it
// is under it.
func relPath(dir, path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(dir, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.Clean(path)
	}
	return rel
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// clipOutput bounds an error message's command output.
func clipOutput(out string) string {
	out = strings.TrimSpace(out)
	if len(out) > 500 {
		out = out[:500] + "…"
	}
	return out
}
