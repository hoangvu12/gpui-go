// Command positioncodemod guards the flipped default position.
// Browser-core ticket 33 introduced position: static opt-in (spec D3)
// and ticket 76 flipped the default from Relative to Static (the CSS
// default; the pin's default is Relative — the recorded divergence).
// Like displaycodemod for the display flip (tickets 68-71), this tool
// finds the code the flip silently changes and pins .Relative() on it,
// restoring the pre-76 layout.
//
// A plain chain with no position selector now deliberately uses the
// static default, exactly like a plain HTML div: the tool leaves it
// alone. Two patterns are NOT that:
//
//   - INSET VOCABULARY (inert insets): a chain that sets insets
//     (Top/Left/Right/Bottom/Inset, or the Tailwind top-*, left-*,
//     right-*, bottom-*, inset-* classes) without stating position.
//     Insets are live only on positioned boxes (css-position-3: static
//     boxes ignore them; the record translation zeroes them), so the
//     offsets the chain used to get from the Relative default went
//     inert at the flip. The tool inserts .Relative() after the root,
//     restoring the offsets. This rule is a permanent gate: any new
//     inset-vocabulary chain without a position is dead styling.
//
//   - PARENT OF AN ABSOLUTE CHILD: a Div chain that attaches an
//     absolute-styled child (Child/Children/TryChildren with an
//     argument whose own method chain carries .Absolute() or a Class
//     literal containing the "absolute" class) without stating its own
//     position. Under the pin the default made the parent the
//     containing block (Taffy places every absolute child against its
//     direct parent); under the static default the child escapes to
//     the nearest positioned ancestor, the CSS rule. The tool inserts
//     .Relative() on the parent, restoring the pin's direct-parent
//     resolution. This rule is also a permanent gate — the guarded
//     escape: a parent that wants its absolute children kept writes
//     .Relative(); one that wants the CSS escape states .Static()
//     explicitly, which silences the finding.
//
// The rule, in full:
//
//   - A ROOT is a call to one of the gpui package's constructors whose
//     element style starts at Style::default() with an empty caller
//     refinement: Div, Img and Canvas (the three BindStyled element
//     constructors in gpui; the same root set displaycodemod uses).
//     gpui.Styles() builds a bare refinement, not an element, and every
//     other element (Anchored, TopLayer, Deferred, Empty, Svg, Text,
//     StyledText, List, UniformList) requests its own node style.
//     Custom components are covered transitively: their render builds
//     on a Div somewhere, and that construction site is analyzed.
//
//   - The CHAIN is the maximal run of selector method calls built
//     directly on the root's result (the receiver chain of the Child
//     call for the parent rule, the whole chain for the inset rule).
//     Chains rooted elsewhere — a helper's returned element, a
//     component constructor, a variable — are not touched; their own
//     construction site is analyzed (a documented limitation: a parent
//     built as a variable and handed an absolute child later is
//     reported only when its construction site chains Child directly).
//
//   - The chain EXPLICITLY sets position when any link is one of the
//     position selectors (Relative, Absolute, Fixed, Sticky, Static)
//     or a Class call whose literal string contains a Tailwind
//     position class (relative, absolute, fixed, sticky, static,
//     without a state-variant prefix). Explicit chains are never
//     touched.
//
//   - ACTION: insert .Relative() immediately after the root
//     constructor call when the chain does not explicitly set position
//     AND (it uses inset vocabulary, OR it attaches an absolute-styled
//     child). Inserting at the root is the earliest possible point, so
//     every later position choice (a method, a Class, a refinement
//     merge, a helper that styles the element) still wins.
//
// Files under internal/parity/fixtures are skipped for the
// parent-of-absolute rule: the parity fixture views mirror their
// checked-in Chrome pages, and an unpositioned parent of an absolute
// child is exactly what those pages contain — the static default is
// the point there, not a reliance on the pin. The inset rule still
// applies (a fixture view with inert insets is a bug in the view).
//
// Usage:
//
//	go run ./cmd/positioncodemod [flags] [patterns...]
//
// The default pattern list is ./... (resolved with `go list`, so
// testdata, reference/ and other ignored directories are skipped like
// any go command). Flags:
//
//	-w      write the rewritten files in place (gofmt-formatted)
//	-check  exit 1 when anything would change; no writes (the gate the
//	        tree must stay green on)
//	-v      print per-file insertion counts, explicit chains and
//	        default-static roots
//
// Without -w or -check the tool prints a unified diff of what it would
// change plus a per-directory summary. The rewrite is idempotent: a
// second run inserts nothing (the chain now selects position).
//
// The analysis is syntactic (go/parser + go/ast) plus file import
// facts, which is enough to resolve the gpui package alias, dot-imports
// and the gpui package's own files. Files carrying a generated-code
// header ("Code generated ... DO NOT EDIT.") are skipped. Edits are
// byte-offset insertions, so comments, line breaks and all other
// formatting are preserved; the output is run through go/format (a
// no-op on gofmt-clean inputs).
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run is the CLI body, separated for testing. It returns the process
// exit code: 0 when nothing needs a change (or -w applied them all),
// 1 when -check found pending changes or any file failed to analyze,
// 2 for usage errors.
func run(args []string) int {
	fs := flag.NewFlagSet("positioncodemod", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	write := fs.Bool("w", false, "write the rewritten files in place")
	check := fs.Bool("check", false, "exit non-zero when anything would change (no writes)")
	verbose := fs.Bool("v", false, "print per-file insertion counts and skipped chains")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	patterns := fs.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	files, err := listGoFiles(patterns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "positioncodemod: %v\n", err)
		return 1
	}

	var (
		results []*FileRewrite
		errs    int
	)
	for _, path := range files {
		res, err := RewriteSource(path, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "positioncodemod: %v\n", err)
			errs++
			continue
		}
		results = append(results, res)
		if *verbose {
			kind := "unchanged"
			if res.Changed {
				kind = fmt.Sprintf("%d insertion(s)", len(res.Edits))
			}
			fmt.Printf("%s: %s (%d roots, %d explicit, %d default-static)\n", path, kind, res.Roots, res.Explicit, res.Defaults)
		}
	}

	summary := summarize(results, *write)
	if !*check {
		for _, res := range results {
			if !res.Changed {
				continue
			}
			fmt.Print(unifiedDiff(res.Path, res.Final))
		}
	}
	if *write {
		for _, res := range results {
			if !res.Changed {
				continue
			}
			if err := os.WriteFile(res.Path, res.Final, 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "positioncodemod: write %s: %v\n", res.Path, err)
				errs++
			}
		}
	}
	fmt.Print(summary)

	switch {
	case errs > 0:
		return 1
	case *check && anyChanged(results):
		return 1
	default:
		return 0
	}
}

// anyChanged reports whether any analyzed file would be rewritten.
func anyChanged(results []*FileRewrite) bool {
	for _, res := range results {
		if res.Changed {
			return true
		}
	}
	return false
}
