// Command displaycodemod guards the flipped default display. Browser-
// core ticket 68 wrote it as the pre-flip safety net (pin .Flex() on
// every default-reliant Div/Img/Canvas chain, applied across the repo
// by tickets 69 and 70); ticket 71 flipped the default display to
// Block (spec D6, the browser div default), and the tool's rule moved
// with it:
//
// A root Div/Img/Canvas chain with NO display selector and NO flex/grid
// container vocabulary now deliberately uses the block default, like a
// plain HTML div — the tool leaves it alone. A chain whose vocabulary
// is flex/grid CONTAINER-only (FlexCol, FlexWrap, Gap, ItemsCenter,
// JustifyBetween, ContentStart, grid templates, or the Tailwind
// equivalents flex-col, gap-2, items-center, justify-*, content-*,
// grid-cols-*) became silently broken at the flip: those properties
// are inert under Block, so the element that used to lay its children
// out with flexbox now stacks them as blocks. The tool inserts an
// explicit .Flex() after those roots, restoring the pre-71 layout.
// Flex ITEM vocabulary (Flex1, FlexGrow, FlexShrink, FlexBasis,
// Self*) does not count: item properties depend on the PARENT being a
// flex container, not on the element's own display.
//
// The rule, in full:
//
//   - A ROOT is a call to one of the gpui package's constructors whose
//     element style starts at Style::default() with an empty caller
//     refinement: Div, Img and Canvas (the three BindStyled element
//     constructors in gpui; gpui.Styles() builds a bare refinement, not
//     an element, and every other element — Anchored, TopLayer, Empty,
//     and since 71 Svg, Text, StyledText, List and UniformList — pins
//     its display in its constructor). Custom components are covered
//     transitively: their render builds on a Div/Img/Canvas somewhere,
//     and that construction site is analyzed.
//
//   - The CHAIN is the maximal run of selector method calls built
//     directly on the root's result (gpui.Div().W4().P2() has the chain
//     W4, P2). Chains rooted elsewhere — a helper's returned element, a
//     component constructor — are not touched; their own construction
//     site is analyzed.
//
//   - The chain EXPLICITLY sets display when any link is one of the
//     display selectors (Display, Block, Flex, Grid, Inline, InlineFlex,
//     InlineBlock, InlineGrid, Contents, FlowRoot, Hidden) or a Class
//     call whose literal string contains a Tailwind display class
//     (flex, block, grid, hidden, inline, inline-flex, inline-block,
//     inline-grid, contents, flow-root, without a state-variant
//     prefix). Explicit chains are never touched.
//
//   - ACTION: insert .Flex() immediately after the root constructor call
//     when the chain does not explicitly set display AND it uses
//     flex/grid container vocabulary. Inserting at the root is the
//     earliest possible point, so every later display choice (a method,
//     a Class, a refinement merge, a helper that styles the element)
//     still wins; when nothing else sets display, the element's
//     container vocabulary is live again.
//
// Usage:
//
//	go run ./cmd/displaycodemod [flags] [patterns...]
//
// The default pattern list is ./... (resolved with `go list`, so
// testdata, reference/ and other ignored directories are skipped like
// any go command). Flags:
//
//	-w      write the rewritten files in place (gofmt-formatted)
//	-check  exit 1 when anything would change; no writes (the gate the
//	        tree must stay green on: a pending insertion is a flex-
//	        vocabulary chain the flip silently broke)
//	-v      print per-file insertion counts, explicit chains and
//	        default-reliant roots
//
// Without -w or -check the tool prints a unified diff of what it would
// change plus a per-directory summary. The rewrite is idempotent: a
// second run inserts nothing (the chain now selects display).
//
// The analysis is syntactic (go/parser + go/ast) plus file import facts,
// which is enough to resolve the gpui package alias, dot-imports and
// the gpui package's own files. Files carrying a generated-code header
// ("Code generated ... DO NOT EDIT.") are skipped: generated output is
// never hand-edited — the codemod's -check gate would otherwise force
// an impossible hand edit on a generated file; a generator that emits a
// flex-vocabulary Div() without display is fixed and rerun instead.
// Edits are byte-offset insertions, so comments, line breaks and all
// other formatting are preserved; the output is run through go/format
// (a no-op on gofmt-clean inputs).
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
	fs := flag.NewFlagSet("displaycodemod", flag.ContinueOnError)
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
		fmt.Fprintf(os.Stderr, "displaycodemod: %v\n", err)
		return 1
	}

	var (
		results []*FileRewrite
		errs    int
	)
	for _, path := range files {
		res, err := RewriteSource(path, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "displaycodemod: %v\n", err)
			errs++
			continue
		}
		results = append(results, res)
		if *verbose {
			kind := "unchanged"
			if res.Changed {
				kind = fmt.Sprintf("%d insertion(s)", len(res.Edits))
			}
			fmt.Printf("%s: %s (%d roots, %d explicit, %d default-block)\n", path, kind, res.Roots, res.Explicit, res.Defaults)
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
				fmt.Fprintf(os.Stderr, "displaycodemod: write %s: %v\n", res.Path, err)
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
