package gpui

import (
	"fmt"
)

// The position lint of browser-core ticket 33 (spec D3): report every
// absolutely positioned div whose direct parent is position: static.
// Under the CSS containing-block rule (css-position-3 §2.1; Chromium
// decides containership from position != static,
// LayoutObject::ComputeIsAbsoluteContainer,
// core/layout/layout_object.cc:2069-2081), such a child resolves
// against the nearest positioned ancestor instead of the parent —
// which is exactly what the runtime does — but it is rarely what the
// author meant: the usual intent is "pin to my direct parent", and the
// fix for that is .Relative() on the parent. The lint is advisory: it
// walks the authored element tree BEFORE any window or frame exists
// (it never renders), so it can run from tests, generators or a dev
// command.
//
// Walk scope: DIVS (the authoring surface that carries Position
// styles), unwrapped through their accessibility companions. Views
// build their trees at render time, and Anchored/Deferred position
// children outside the CSS flow, so the lint does not look inside
// them; an absolute div under a static parent nested there is not
// reported (documented limitation — those containers adopt
// RequestContainerLayout with tickets 25/76).

// PositionLint is one finding of LintAbsoluteUnderStatic.
type PositionLint struct {
	// Element is the absolute element's path in the walked tree:
	// element ids where the divs have them, child indices otherwise,
	// joined with "/" (for example "list/child[2]/badge").
	Element string
	// Parent is the static parent's path (same form as Element).
	Parent string
	// ContainingBlock is the nearest positioned ancestor's path, or
	// empty when the walked tree has none (the runtime then resolves
	// against the window root — the port's initial containing block —
	// or falls back to the direct parent when no participating
	// ancestor exists; see containingblock.go).
	ContainingBlock string
	// Message is the human-readable finding.
	Message string
}

// LintAbsoluteUnderStatic walks the given element trees and reports
// every absolutely positioned div whose direct parent is a static div.
// It never renders and needs no window: it reads the authored styles
// (the base style each DivElement would compute). The corrected form —
// the parent positioned (relative or absolute), or the child no longer
// absolute — produces no findings.
func LintAbsoluteUnderStatic(roots ...AnyElement) []PositionLint {
	var findings []PositionLint
	for _, root := range roots {
		lintPositionWalk(root, -1, nil, &findings)
	}
	return findings
}

// lintPositionNode is one walked div: its builder, tree path and the
// enclosing div node (for ancestor walks).
type lintPositionNode struct {
	div    *DivElement
	path   string
	parent *lintPositionNode
}

// lintPositionWalk descends one element: divs (through their a11y
// companions) carry the position styles; everything else is skipped.
// parent is the enclosing div node, nil at the walked roots; index is
// the element's position in the parent's child list (-1 for a root).
func lintPositionWalk(el AnyElement, index int, parent *lintPositionNode, findings *[]PositionLint) {
	div := lintDivOf(el)
	if div == nil {
		return
	}
	parentPath := ""
	if parent != nil {
		parentPath = parent.path
	}
	node := &lintPositionNode{
		div:    div,
		path:   lintPathOf(div, parentPath, index),
		parent: parent,
	}
	if parent != nil {
		parentPosition := parent.div.computeStyle().Position
		childPosition := div.computeStyle().Position
		if childPosition == PositionAbsolute && parentPosition == PositionStatic {
			finding := PositionLint{Element: node.path, Parent: parent.path}
			if cb := lintNearestPositionedAncestor(parent); cb != "" {
				finding.ContainingBlock = cb
				finding.Message = fmt.Sprintf(
					"element %q is position: absolute but its parent %q is position: static, so it resolves against the nearest positioned ancestor %q, not the parent; call .Relative() on the parent if it should be the containing block",
					node.path, parent.path, cb)
			} else {
				finding.Message = fmt.Sprintf(
					"element %q is position: absolute but its parent %q is position: static, so it escapes the parent toward the window root (no positioned ancestor in the walked tree); call .Relative() on the parent if it should be the containing block",
					node.path, parent.path)
			}
			*findings = append(*findings, finding)
		}
	}
	for i := range div.children {
		lintPositionWalk(div.children[i], i, node, findings)
	}
}

// lintNearestPositionedAncestor returns the nearest positioned
// (non-static) div's path at or above node, or "" when the whole
// walked chain is static.
func lintNearestPositionedAncestor(node *lintPositionNode) string {
	for node != nil {
		if node.div.computeStyle().Position != PositionStatic {
			return node.path
		}
		node = node.parent
	}
	return ""
}

// lintPathOf builds one div's path: its name identity when it has one,
// otherwise "child[i]" under the parent's path (a walked root without
// a name is "root").
func lintPathOf(div *DivElement, parentPath string, index int) string {
	if div.elementID != nil && div.elementID.Kind == ElementIDName && div.elementID.Name != "" {
		return div.elementID.Name
	}
	if parentPath == "" {
		return "root"
	}
	return fmt.Sprintf("%s/child[%d]", parentPath, index)
}

// lintDivOf unwraps a div builder from an AnyElement: the plain div
// element, or a div wrapped by the accessibility companion
// (div_a11y.go divIntoA11yElement). Both convert through CustomElement,
// whose drawable stores the Element interface. Non-div elements
// report nil.
func lintDivOf(el AnyElement) *DivElement {
	if el.obj == nil {
		return nil
	}
	d, ok := el.obj.(*drawable[divFrameState, divPrepaintState, Element[divFrameState, divPrepaintState]])
	if !ok {
		return nil
	}
	if div, ok := d.element.(*divElement); ok {
		return div.builder
	}
	if a, ok := d.element.(*a11yElement[divFrameState, divPrepaintState]); ok {
		if div, ok := a.child.(*divElement); ok {
			return div.builder
		}
	}
	return nil
}
