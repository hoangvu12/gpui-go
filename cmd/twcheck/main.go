// Command twcheck checks Tailwind class strings without running the app.
// It finds every .Class("…") call whose argument is a constant string and
// checks that string against the classes gpui's Class parser supports
// (gpui.CheckClasses), so agents and humans catch unknown or unsupported
// Tailwind classes (typos, z-10, sm:p-4, space-x-2, …) before the app
// runs rather than from its stderr:
//
//	go run github.com/hoangvu12/gpui-go/cmd/twcheck [-v] [packages]
//
// Each argument is a directory, dir/... for a directory and everything
// under it, or a .go file; with no arguments it checks ./..., the current
// directory and everything under it. The recursion skips vendor, testdata
// and directories whose names start with "." or "_". Test files are
// checked too.
//
// A constant string is a string literal, the name of a package-level
// string constant declared in the same directory, or a parenthesized
// expression or concatenation (+) of those. Other arguments (variables,
// function calls, fmt.Sprintf, …) are skipped; -v lists them on stderr
// with a summary. twcheck reads syntax only and does not know the
// receiver's type, so any method named Class called with one constant
// string is checked.
//
// Each problem is one line on stdout, path:line:col: error, at the
// argument's position. The exit status is 1 if there is a problem or a
// parse error, 2 on a usage error (a bad flag or an unreadable path) and
// 0 otherwise. Importing gpui does not load the native DLL: gpui's init
// only arms lazy loaders.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hoangvu12/gpui-go/gpui"
)

func main() {
	verbose := flag.Bool("v", false, "list skipped Class calls and print a summary on stderr")
	flag.Usage = usage
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		args = []string{"./..."}
	}
	targets, err := expand(args)
	if err != nil {
		fail(err)
	}
	c := &checker{fset: token.NewFileSet(), verbose: *verbose}
	for _, t := range targets {
		c.checkDir(t)
	}
	if c.verbose {
		fmt.Fprintf(os.Stderr, "twcheck: %d Class strings checked, %d skipped\n", c.checked, c.skipped)
	}
	if c.failed {
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: twcheck [-v] [packages]")
	fmt.Fprintln(os.Stderr, "Each package is a directory, dir/... or a .go file (default ./...).")
	flag.PrintDefaults()
}

// A target is a directory to check: every .go file in it, or only the
// files named on the command line.
type target struct {
	dir   string
	files map[string]bool // base names; nil means every .go file
}

// expand turns the command-line arguments into targets, in argument
// order, with each directory once.
func expand(args []string) ([]*target, error) {
	var targets []*target
	byDir := map[string]*target{}
	add := func(dir, file string) {
		dir = filepath.Clean(dir)
		t := byDir[dir]
		if t == nil {
			t = &target{dir: dir, files: map[string]bool{}}
			byDir[dir] = t
			targets = append(targets, t)
		}
		switch {
		case file == "":
			t.files = nil
		case t.files != nil:
			t.files[file] = true
		}
	}
	for _, arg := range args {
		if root, ok := strings.CutSuffix(filepath.ToSlash(arg), "/..."); ok {
			err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					return nil
				}
				if path != root && skipDir(d.Name()) {
					return filepath.SkipDir
				}
				add(path, "")
				return nil
			})
			if err != nil {
				return nil, err
			}
			continue
		}
		info, err := os.Stat(arg)
		switch {
		case err != nil:
			return nil, err
		case info.IsDir():
			add(arg, "")
		case strings.HasSuffix(arg, ".go"):
			add(filepath.Dir(arg), filepath.Base(arg))
		default:
			return nil, fmt.Errorf("%s: not a directory or .go file", arg)
		}
	}
	return targets, nil
}

// skipDir reports whether a recursive walk skips the directory, as the go
// tool does.
func skipDir(name string) bool {
	return name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// checker checks Class calls and counts what it saw.
type checker struct {
	fset    *token.FileSet
	verbose bool
	checked int  // constant Class strings checked
	skipped int  // Class calls whose argument is not a constant string
	failed  bool // a problem or error was reported
}

// checkDir parses every .go file in the target's directory, so a Class
// argument may name a constant from a file that is not itself checked,
// and checks the target's files.
func (c *checker) checkDir(t *target) {
	entries, err := os.ReadDir(t.dir)
	if err != nil {
		c.report(err)
		return
	}
	var all, check []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		mine := t.files == nil || t.files[name]
		file, err := parser.ParseFile(c.fset, filepath.Join(t.dir, name), nil, parser.SkipObjectResolution)
		if file != nil {
			all = append(all, file)
		}
		if !mine {
			continue
		}
		if err != nil {
			c.report(err) // go/parser errors start with the file's path
			continue
		}
		check = append(check, file)
	}
	consts := packageConsts(all)
	for _, file := range check {
		c.checkFile(file, consts[file.Name.Name])
	}
}

// checkFile checks every one-argument Class call in file, reporting each
// unsupported class at the argument's position.
func (c *checker) checkFile(file *ast.File, consts *constScope) {
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); !ok || sel.Sel.Name != "Class" {
			return true
		}
		arg := call.Args[0]
		pos := c.fset.Position(arg.Pos())
		classes, ok := consts.eval(arg)
		if !ok {
			c.skipped++
			if c.verbose {
				fmt.Fprintf(os.Stderr, "%s:%d: skipped (not a constant string)\n", pos.Filename, pos.Line)
			}
			return true
		}
		c.checked++
		for _, err := range classErrors(classes) {
			c.report(fmt.Errorf("%s: %w", pos, err))
		}
		return true
	})
}

// report prints a problem on stdout and marks the run failed.
func (c *checker) report(err error) {
	fmt.Println(err)
	c.failed = true
}

// classErrors returns one error per unknown or unsupported class in
// classes, unwrapping CheckClasses' errors.Join.
func classErrors(classes string) []error {
	err := gpui.CheckClasses(classes)
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return joined.Unwrap()
	}
	return []error{err}
}

// packageConsts collects the package-level constants of files by package
// name, so an external _test package does not see the package's names.
func packageConsts(files []*ast.File) map[string]*constScope {
	scopes := map[string]*constScope{}
	for _, file := range files {
		scope := scopes[file.Name.Name]
		if scope == nil {
			scope = &constScope{exprs: map[string]ast.Expr{}, memo: map[string]*constant{}}
			scopes[file.Name.Name] = scope
		}
		scope.declare(file)
	}
	return scopes
}

// constScope is one package's package-level constants, evaluated on
// demand. A name declared more than once (in files for different build
// tags) has no single value and never evaluates.
type constScope struct {
	exprs map[string]ast.Expr
	memo  map[string]*constant // a nil entry is being evaluated
}

type constant struct {
	value string
	ok    bool
}

// declare records the const declarations in file.
func (c *constScope) declare(file *ast.File) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		var values []ast.Expr
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			if len(vs.Values) > 0 {
				values = vs.Values // an empty list repeats the previous one
			}
			for i, name := range vs.Names {
				if name.Name == "_" {
					continue
				}
				var expr ast.Expr
				if i < len(values) {
					expr = values[i]
				}
				if _, dup := c.exprs[name.Name]; dup {
					expr = nil
				}
				c.exprs[name.Name] = expr
			}
		}
	}
}

// eval evaluates expr as a constant string: a string literal, a
// parenthesized or + concatenated constant string, or a constant's name.
func (c *constScope) eval(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		return s, err == nil
	case *ast.ParenExpr:
		return c.eval(e.X)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		x, ok := c.eval(e.X)
		if !ok {
			return "", false
		}
		y, ok := c.eval(e.Y)
		return x + y, ok
	case *ast.Ident:
		return c.lookup(e.Name)
	}
	return "", false
}

// lookup evaluates the constant name once and remembers the result. A
// reference cycle (invalid Go, but possible mid-edit) does not evaluate.
func (c *constScope) lookup(name string) (string, bool) {
	if k, seen := c.memo[name]; seen {
		if k == nil {
			return "", false
		}
		return k.value, k.ok
	}
	c.memo[name] = nil
	value, ok := c.eval(c.exprs[name])
	c.memo[name] = &constant{value, ok}
	return value, ok
}

// fail reports a usage error and exits 2.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "twcheck:", err)
	os.Exit(2)
}
