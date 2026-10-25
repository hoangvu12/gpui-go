package gpui

import (
	"strings"
	"testing"
)

// The position lint's own tests (browser-core ticket 33): the lint
// flags the sample — an absolute div under a static parent — and
// passes on the corrected one (the parent positioned, or the child not
// absolute). The lint needs no window: it walks the authored builders.

// lintSample builds the sample tree: a positioned root, a static
// wrapper (id when named), and an absolute badge child.
func lintSample(rootStatic, wrapperStatic bool) AnyElement {
	root := Div().Flex().ID("root").Size(Px(200), Px(160))
	if rootStatic {
		root = root.Static()
	} else {
		root = root.Relative()
	}
	wrapper := Div().Flex().ID("wrapper").Size(Px(100), Px(80))
	if wrapperStatic {
		wrapper = wrapper.Static()
	} else {
		wrapper = wrapper.Relative()
	}
	badge := Div().Flex().ID("badge").Absolute().Size(Px(40), Px(30))
	return root.Child(wrapper.Child(badge)).IntoElement()
}

// TestLintFlagsAbsoluteUnderStaticParent pins the flag: one finding
// naming the absolute element, the static parent, the containing block
// the runtime will actually use, and the fix.
func TestLintFlagsAbsoluteUnderStaticParent(t *testing.T) {
	findings := LintAbsoluteUnderStatic(lintSample(false, true))
	if len(findings) != 1 {
		t.Fatalf("findings = %d (%v), want 1", len(findings), findings)
	}
	f := findings[0]
	if f.Element != "badge" || f.Parent != "wrapper" || f.ContainingBlock != "root" {
		t.Fatalf("finding paths = element %q parent %q containing block %q, want badge/wrapper/root", f.Element, f.Parent, f.ContainingBlock)
	}
	for _, want := range []string{"badge", "wrapper", "position: static", "root", ".Relative()"} {
		if !strings.Contains(f.Message, want) {
			t.Fatalf("message %q does not mention %q", f.Message, want)
		}
	}
}

// TestLintPassesOnTheCorrectedTree pins the corrected forms: a
// positioned parent (relative or absolute) with an absolute child
// produces no findings, and neither does a static parent whose child
// is not absolute.
func TestLintPassesOnTheCorrectedTree(t *testing.T) {
	if findings := LintAbsoluteUnderStatic(lintSample(false, false)); len(findings) != 0 {
		t.Fatalf("relative parent findings = %v, want none", findings)
	}
	absoluteParent := Div().Flex().ID("root").Relative().Child(
		Div().Flex().ID("wrapper").Absolute().Child(Div().Flex().ID("badge").Absolute()))
	if findings := LintAbsoluteUnderStatic(absoluteParent.IntoElement()); len(findings) != 0 {
		t.Fatalf("absolute parent findings = %v, want none (absolute is positioned)", findings)
	}
	staticParentInFlowChild := Div().Flex().ID("root").Relative().Child(
		Div().Flex().ID("wrapper").Static().Child(Div().Flex().ID("badge").Size(Px(40), Px(30))))
	if findings := LintAbsoluteUnderStatic(staticParentInFlowChild.IntoElement()); len(findings) != 0 {
		t.Fatalf("static parent with an in-flow child findings = %v, want none", findings)
	}
}

// TestLintReportsNestedStaticChainsAndRootlessChains pins the deeper
// walks: a nested static chain reports the absolute leaf with the
// positioned ancestor as the containing block, and a fully static
// chain (no positioned ancestor in the tree) reports the escape
// toward the window root. Anonymous paths use child indices.
func TestLintReportsNestedStaticChainsAndRootlessChains(t *testing.T) {
	nested := Div().Flex().ID("root").Relative().Child(
		Div().Flex().Static().Child(
			Div().Flex().Static().Child(
				Div().Flex().ID("leaf").Absolute())))
	findings := LintAbsoluteUnderStatic(nested.IntoElement())
	if len(findings) != 1 || findings[0].Element != "leaf" || findings[0].ContainingBlock != "root" {
		t.Fatalf("nested chain findings = %+v, want leaf resolving against root", findings)
	}

	rootless := Div().Flex().Static().Child(
		Div().Flex().Static().Child(
			Div().Flex().ID("leaf").Absolute()))
	findings = LintAbsoluteUnderStatic(rootless.IntoElement())
	if len(findings) != 1 || findings[0].ContainingBlock != "" {
		t.Fatalf("rootless chain findings = %+v, want no containing block in the tree", findings)
	}
	if !strings.Contains(findings[0].Message, "window root") {
		t.Fatalf("message %q does not explain the escape toward the window root", findings[0].Message)
	}

	// Anonymous elements take child-index paths.
	anon := Div().Flex().Relative().Child(
		Div().Flex().Static().Child(
			Div().Flex().Absolute()))
	findings = LintAbsoluteUnderStatic(anon.IntoElement())
	if len(findings) != 1 || findings[0].Parent != "root/child[0]" || findings[0].Element != "root/child[0]/child[0]" {
		t.Fatalf("anonymous findings = %+v, want index paths under root", findings)
	}
}

// TestLintWalksAccessibilityWrappedDivs pins the a11y companion unwrap:
// a div converted through divIntoA11yElement is still walked.
func TestLintWalksAccessibilityWrappedDivs(t *testing.T) {
	wrapper := Div().Flex().ID("wrapper").Static().Child(Div().Flex().ID("badge").Absolute())
	root := Div().Flex().ID("root").Relative().Child(divIntoA11yElement(wrapper))
	findings := LintAbsoluteUnderStatic(root.IntoElement())
	if len(findings) != 1 || findings[0].Element != "badge" || findings[0].Parent != "wrapper" {
		t.Fatalf("a11y-wrapped findings = %+v, want badge under wrapper", findings)
	}
}

// TestLintSkipsNonDivChildrenWithoutPanicking pins the walk scope: text
// and empty children are skipped (nothing to check) and never panic.
func TestLintSkipsNonDivChildrenWithoutPanicking(t *testing.T) {
	tree := Div().Flex().ID("root").Static().Children(
		Text("hello"),
		Empty(),
		Div().Flex().ID("badge").Absolute())
	findings := LintAbsoluteUnderStatic(tree.IntoElement())
	if len(findings) != 1 || findings[0].Element != "badge" {
		t.Fatalf("findings = %+v, want only the badge finding", findings)
	}
	if findings[0].ContainingBlock != "" {
		t.Fatalf("static root finding names containing block %q, want none in the tree", findings[0].ContainingBlock)
	}
}
