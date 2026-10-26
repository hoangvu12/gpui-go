package gpui

//go:generate go run ../cmd/tailwindgen

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// This file is the Tailwind class parser behind Styled.Class (ADR 0001),
// a gpui-go addition with no counterpart at the pin. A class sets what
// the fluent method of the same name sets: the generated
// applyMethodClass (cmd/tailwindgen) covers every zero-argument Styled
// method, and the stages below add the classes whose value a method
// takes as an argument (bg-slate-800, w-[84px], p-13, rounded-[10px]),
// the state variants, and a reason for each Tailwind utility GPUI cannot
// express. docs/tailwind.md is the user-facing list.

// Class applies Tailwind CSS classes the way an HTML class attribute
// does:
//
//	gpui.Div().Class("flex items-center gap-2 px-4 py-2 rounded-lg bg-slate-800 hover:bg-slate-700")
//
// Each class sets what its method of the same name sets (items-center is
// ItemsCenter, px-4 is Px4, bg-slate-800 is Bg(tw.Slate800)). Within the
// string, classes apply left to right like a method chain, so a later
// class wins where two set the same property (Tailwind decides by
// stylesheet order instead; avoid conflicting classes). The string as a
// whole then refines the element: each property it sets replaces the
// element's, so a class cannot remove a value set before Class
// (content-normal, text-decoration-none), and grid placement classes
// replace the element's whole grid placement.
//
// The variants hover:, active:, focus:, focus-visible:, focus-within:,
// group-hover: and group-active: (one per class), and the classes group,
// group/name, overflow-auto and overflow-scroll, configure the element's
// interactivity, so they need a Div or a component embedding
// Interactive. They merge into state styles set before. Calling Hover
// after a Class with hover: panics like setting hover twice; the other
// state builders replace the style Class set. active: and scrolling
// need an ID; focus variants need TrackFocus.
//
// Each distinct string is parsed once and cached, so pass literals and
// use methods for computed values. A class Class cannot apply is skipped
// and reported to the class error handler (stderr by default, once per
// string; see SetClassErrorHandler). CheckClasses and cmd/twcheck find
// such classes before the program runs.
func (s *Styled[Self]) Class[_ lazy](classes string) Self {
	applyClasses(s.self, s.StyleMut(), classes)
	return s.self
}

// CheckClasses reports the classes of a Class string that Class cannot
// apply, joined with errors.Join (one error per class), or nil. It does
// not know the element, so it accepts state variants everywhere.
func CheckClasses(classes string) error { return parseClasses(classes).err }

// TailwindColor returns the Tailwind palette color with the given class
// color name ("slate-800", "white", "black" or "transparent"), for names
// known only at run time; package tw has the same colors as values.
func TailwindColor(name string) (Hsla, bool) {
	if name == "transparent" {
		return Rgb(0).Alpha(0), true
	}
	hex, ok := tailwindPalette[name]
	if !ok {
		return Hsla{}, false
	}
	return Rgb(hex), true
}

// SetClassErrorHandler sets the function Class reports unusable classes
// to, once per distinct string and problem, and returns the previous
// one. The default writes `gpui: Class("…"): …` to stderr; a handler
// that panics makes bad classes fatal (useful in tests). A nil handler
// restores the default. Setting a handler forgets which strings were
// already reported.
func SetClassErrorHandler(h func(classes string, err error)) (previous func(classes string, err error)) {
	previous = classErrorHandlerOrDefault()
	if h == nil {
		classErrorHandler.Store(nil)
	} else {
		classErrorHandler.Store(&h)
	}
	classErrorsSeen.Clear()
	return previous
}

// ---------------------------------------------------------------------------
// Applying a parsed class string
// ---------------------------------------------------------------------------

// applyClasses is Class's body, kept out of the generic method so that
// each call site compiles only a call.
func applyClasses(owner any, style *StyleRefinement, classes string) {
	set := parseClasses(classes)
	if set.err != nil && firstClassError(classes, false) {
		classErrorHandlerOrDefault()(classes, set.err)
	}
	style.Refine(&set.base)
	if !set.needsDiv() {
		return
	}
	d := interactiveDiv(owner)
	if d == nil {
		if firstClassError(classes, true) {
			classErrorHandlerOrDefault()(classes, fmt.Errorf(
				"%s need an interactive element (a Div, or a component embedding Interactive); %T has none",
				strings.Join(set.divClasses, " "), owner))
		}
		return
	}
	set.applyInteractivity(d)
}

// interactiveDiv returns the div carrying owner's interactivity: owner
// itself, or the inner div of a component embedding Interactive.
func interactiveDiv(owner any) *DivElement {
	switch o := owner.(type) {
	case *DivElement:
		return o
	case interface{ Interactivity() *DivElement }:
		return o.Interactivity()
	}
	return nil
}

// applyInteractivity sets the group, scroll and state styles on d,
// merging into state styles it already has.
func (set *classSet) applyInteractivity(d *DivElement) {
	if set.group != nil {
		d.Group(*set.group)
	}
	if set.scroll {
		d.scrollStateful = true
	}
	for _, st := range set.states {
		switch st.kind {
		case stateHover:
			mergeStateStyle(&d.hoverStyle, &st.style)
		case stateActive:
			mergeStateStyle(&d.activeStyle, &st.style)
		case stateFocus:
			mergeStateStyle(&d.focusStyle, &st.style)
		case stateFocusVisible:
			mergeStateStyle(&d.focusVisibleStyle, &st.style)
		case stateInFocus:
			mergeStateStyle(&d.inFocusStyle, &st.style)
		case stateGroupHover:
			mergeGroupStyle(&d.groupHoverStyle, st.group, &st.style)
		case stateGroupActive:
			mergeGroupStyle(&d.groupActiveStyle, st.group, &st.style)
		}
	}
}

// mergeStateStyle refines an existing state style, or sets a copy (the
// cached set is shared and must not be aliased).
func mergeStateStyle(dst **StyleRefinement, src *StyleRefinement) {
	if *dst != nil {
		(*dst).Refine(src)
		return
	}
	style := src.Clone()
	*dst = &style
}

// mergeGroupStyle refines an existing group style of the same group, or
// replaces it like GroupHover / GroupActive do.
func mergeGroupStyle(dst **GroupStyle, group string, src *StyleRefinement) {
	if *dst != nil && (*dst).Group == group {
		(*dst).Style.Refine(src)
		return
	}
	*dst = &GroupStyle{Group: group, Style: src.Clone()}
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	classErrorHandler atomic.Pointer[func(classes string, err error)]
	// classErrorsSeen holds the classErrorKeys already reported.
	classErrorsSeen sync.Map
)

// classErrorKey identifies one reported problem: a string's parse
// errors, or its need for an interactive element.
type classErrorKey struct {
	classes string
	noDiv   bool
}

// firstClassError reports whether this problem has not been reported
// since the handler was last set, and marks it reported.
func firstClassError(classes string, noDiv bool) bool {
	_, seen := classErrorsSeen.LoadOrStore(classErrorKey{classes, noDiv}, struct{}{})
	return !seen
}

func classErrorHandlerOrDefault() func(classes string, err error) {
	if h := classErrorHandler.Load(); h != nil {
		return *h
	}
	return defaultClassErrorHandler
}

func defaultClassErrorHandler(classes string, err error) {
	fmt.Fprintf(os.Stderr, "gpui: Class(%q): %s\n", classes, strings.ReplaceAll(err.Error(), "\n", "; "))
}

// classError is one class Class cannot apply. An empty reason means the
// class is unknown.
type classError struct {
	class  string
	reason string
	// err is the sentinel the reason carries (the named gradient
	// limits), so CheckClasses users can errors.Is it; nil otherwise.
	err error
}

func (e *classError) Error() string {
	if e.reason == "" {
		return fmt.Sprintf("unknown class %q", e.class)
	}
	return fmt.Sprintf("%q: %s", e.class, e.reason)
}

// Unwrap returns the sentinel the rejection carries (errors.Is), if
// any.
func (e *classError) Unwrap() error { return e.err }

// classErrorFor builds a Class rejection. A reason that is a named
// gradient limit's message carries that sentinel (single-sourced: the
// stages return the sentinels' Error() strings).
func classErrorFor(class, reason string) error {
	switch reason {
	case ErrGradientViaUnsupported.Error():
		return &classError{class: class, reason: reason, err: ErrGradientViaUnsupported}
	case ErrGradientInterpolationUnsupported.Error():
		return &classError{class: class, reason: reason, err: ErrGradientInterpolationUnsupported}
	}
	return &classError{class: class, reason: reason}
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

// classSet is a parsed class string. It is immutable once parsed and
// shared by every element using the string.
type classSet struct {
	// base is the refinement of the classes without a variant.
	base StyleRefinement
	// states holds one merged refinement per state (and group).
	states []*classState
	// group is the group the element joins (group, group/name).
	group *string
	// scroll records overflow-auto / overflow-scroll: the div becomes
	// stateful, like OverflowScroll.
	scroll bool
	// divClasses are the classes that need an interactive element.
	divClasses []string
	err        error
}

func (set *classSet) needsDiv() bool {
	return len(set.states) > 0 || set.group != nil || set.scroll
}

// classState is the merged refinement of one state variant.
type classState struct {
	kind  classStateKind
	group string
	style StyleRefinement
}

type classStateKind uint8

const (
	stateHover classStateKind = iota
	stateActive
	stateFocus
	stateFocusVisible
	stateInFocus
	stateGroupHover
	stateGroupActive
)

var (
	// classCache maps a class string to its *classSet.
	classCache     sync.Map
	classCacheSize atomic.Int64
)

// classCacheLimit bounds the cache for programs that format values into
// class strings; past it, strings are parsed on every call.
const classCacheLimit = 1 << 14

// parseClasses returns the parsed set for a class string, cached.
func parseClasses(classes string) *classSet {
	if v, ok := classCache.Load(classes); ok {
		return v.(*classSet)
	}
	set := buildClassSet(classes)
	if classCacheSize.Load() < classCacheLimit {
		if v, loaded := classCache.LoadOrStore(classes, set); loaded {
			return v.(*classSet)
		}
		classCacheSize.Add(1)
	}
	return set
}

func buildClassSet(classes string) *classSet {
	set := &classSet{}
	var errs []error
	for _, token := range strings.Fields(classes) {
		if err := set.add(token); err != nil {
			errs = append(errs, err)
		}
	}
	set.err = errors.Join(errs...)
	return set
}

// add parses one class with its optional variant into the set.
func (set *classSet) add(token string) error {
	parts := splitVariants(token)
	if len(parts) > 2 {
		return &classError{class: token, reason: "stacked variants are not supported; use one variant per class"}
	}
	// The important modifier (!p-4, p-4!) changes nothing here.
	utility := strings.TrimSuffix(strings.TrimPrefix(parts[len(parts)-1], "!"), "!")
	variant := len(parts) == 2
	var kind classStateKind
	var group string
	if variant {
		var reason string
		if kind, group, reason = parseVariant(parts[0]); reason != "" {
			return &classError{class: token, reason: reason}
		}
	}

	if utility == "group" || strings.HasPrefix(utility, "group/") {
		name, _ := strings.CutPrefix(strings.TrimPrefix(utility, "group"), "/")
		switch {
		case variant:
			return &classError{class: token, reason: "group cannot take a variant"}
		case utility != "group" && name == "":
			return &classError{class: token, reason: "group/ needs a name, like group/menu"}
		case set.group != nil && *set.group != name:
			return &classError{class: token, reason: "an element belongs to one group, and this one is already in another"}
		}
		set.group = &name
		set.divClasses = append(set.divClasses, token)
		return nil
	}
	if x, y, auto, ok := scrollClass(utility); ok {
		if variant {
			return &classError{class: token, reason: "scrolling cannot depend on a state"}
		}
		value := OverflowScroll
		if auto {
			value = OverflowAuto
		}
		if x {
			set.base.OverflowX = Some(value)
		}
		if y {
			set.base.OverflowY = Some(value)
		}
		set.scroll = true
		set.divClasses = append(set.divClasses, token)
		return nil
	}

	// The class applies to a copy of what the string built so far, like
	// the next method in a chain (col-span-2 row-span-2 keeps both), and
	// the copy replaces it only if the class applies.
	target := &set.base
	var state *classState
	if variant {
		var reason string
		if state, reason = set.state(kind, group); reason != "" {
			return &classError{class: token, reason: reason}
		}
		target = &StyleRefinement{}
		if state != nil {
			target = &state.style
		}
	}
	b := Styles()
	*b.StyleMut() = target.Clone()
	if ok, reason := applyUtility(b, utility); !ok {
		if reason == "" {
			reason = unknownClassReason(utility)
		}
		return classErrorFor(token, reason)
	}
	if !variant {
		set.base = *b.StyleMut()
		return nil
	}
	if state == nil {
		state = &classState{kind: kind, group: group}
		set.states = append(set.states, state)
	}
	state.style = *b.StyleMut()
	set.divClasses = append(set.divClasses, token)
	return nil
}

// state finds the merged state for a variant, or nil if the string has
// none yet. An element has one group-hover and one group-active style,
// each for one group.
func (set *classSet) state(kind classStateKind, group string) (*classState, string) {
	for _, st := range set.states {
		if st.kind != kind {
			continue
		}
		if st.group != group {
			return nil, "an element's group-hover: (or group-active:) styles can follow only one group"
		}
		return st, ""
	}
	return nil, ""
}

// splitVariants splits a class on the colons outside brackets:
// "hover:bg-[#fff]" is ["hover", "bg-[#fff]"].
func splitVariants(token string) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(token); i++ {
		switch token[i] {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				parts = append(parts, token[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, token[start:])
}

// parseVariant maps a variant to its state, or explains why it is not
// supported.
func parseVariant(v string) (kind classStateKind, group, reason string) {
	switch v {
	case "hover":
		return stateHover, "", ""
	case "active":
		return stateActive, "", ""
	case "focus":
		return stateFocus, "", ""
	case "focus-visible":
		return stateFocusVisible, "", ""
	case "focus-within":
		return stateInFocus, "", ""
	case "group-hover":
		return stateGroupHover, "", ""
	case "group-active":
		return stateGroupActive, "", ""
	}
	if name, ok := strings.CutPrefix(v, "group-hover/"); ok && name != "" {
		return stateGroupHover, name, ""
	}
	if name, ok := strings.CutPrefix(v, "group-active/"); ok && name != "" {
		return stateGroupActive, name, ""
	}
	switch {
	case v == "sm" || v == "md" || v == "lg" || v == "xl" || v == "2xl" ||
		strings.HasPrefix(v, "max-") || strings.HasPrefix(v, "min-") || v == "portrait" || v == "landscape":
		reason = "responsive variants are not supported (there are no media queries); choose classes in Render from the window size"
	case v == "dark" || v == "light":
		reason = v + ": is not supported; pick colors from your theme in Render"
	case v == "disabled" || v == "enabled" || v == "checked" || v == "invalid" || v == "valid" ||
		v == "required" || v == "optional" || v == "read-only" || v == "placeholder-shown" ||
		v == "indeterminate" || v == "default" || v == "autofill" || v == "open" || v == "visited" || v == "target":
		reason = v + ": is not supported; style that state in Render, e.g. .When(disabled, …)"
	case v == "first" || v == "last" || v == "odd" || v == "even" || v == "only" ||
		v == "first-of-type" || v == "last-of-type" || v == "only-of-type" || v == "empty" || v == "*":
		reason = "structural variants are not supported; style the item in the loop that builds it"
	case strings.HasPrefix(v, "group-"):
		reason = "only the group-hover: and group-active: group variants are supported"
	case strings.HasPrefix(v, "peer"):
		reason = "peer variants are not supported; put both elements in a group and use group-hover:"
	case strings.HasPrefix(v, "aria-") || strings.HasPrefix(v, "data-") ||
		strings.HasPrefix(v, "has-") || strings.HasPrefix(v, "supports-") || strings.HasPrefix(v, "not-"):
		reason = "attribute and selector variants are not supported; style from state in Render"
	case v == "before" || v == "after" || v == "placeholder" || v == "file" || v == "marker" ||
		v == "selection" || v == "first-line" || v == "first-letter" || v == "backdrop":
		reason = "pseudo-element variants are not supported"
	case v == "motion-safe" || v == "motion-reduce" || v == "print" || v == "contrast-more" ||
		v == "contrast-less" || v == "forced-colors" || v == "rtl" || v == "ltr":
		reason = "media variants are not supported"
	case strings.HasPrefix(v, "[") || strings.HasPrefix(v, "@"):
		reason = "arbitrary and container variants are not supported"
	default:
		reason = fmt.Sprintf("unknown variant %q", v+":")
	}
	return 0, "", reason
}

// scrollClass reports the scroll axes of the overflow-auto /
// overflow-scroll classes and their -x / -y forms (div.rs:1746-1763).
// Browser-core 36: overflow-auto now sets the real OverflowAuto
// (scrollbars and their gutter appear only when needed) — it previously
// pinned to OverflowScroll (always a scroller, the pin's approximation),
// a recorded divergence toward Chrome; the class still needs an
// interactive element for the scroll state.
func scrollClass(u string) (x, y, auto, ok bool) {
	switch u {
	case "overflow-auto":
		return true, true, true, true
	case "overflow-scroll":
		return true, true, false, true
	case "overflow-x-auto":
		return true, false, true, true
	case "overflow-x-scroll":
		return true, false, false, true
	case "overflow-y-auto":
		return false, true, true, true
	case "overflow-y-scroll":
		return false, true, false, true
	}
	return false, false, false, false
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

// classStage applies one family of utilities. handled reports that the
// class belongs to the family; a non-empty reason that it cannot apply.
type classStage func(b *StyleBuilder, u string) (handled bool, reason string)

// classStages run in order after the method classes. unsupportedClass
// runs before colorClass so that outline-none gets its reason rather
// than "unknown color"; gradientClass runs before unsupportedClass so
// from-/via-/to-/bg-linear-* get gradient reasons.
var classStages = []classStage{
	specialClass,
	lengthClass,
	typographyClass,
	borderClass,
	opacityClass,
	gridClass,
	gridAreaClass,
	aspectClass,
	blurClass,
	gradientClass,
	filterClass,
	transformClass,
	unsupportedClass,
	colorClass,
}

// applyUtility applies one class without a variant to b. It reports
// false with a reason when the class is unsupported, and false without
// one when it is unknown.
func applyUtility(b *StyleBuilder, u string) (ok bool, reason string) {
	if applyMethodClass(u, b) {
		return true, ""
	}
	for _, stage := range classStages {
		if handled, reason := stage(b, u); handled {
			return reason == "", reason
		}
	}
	return false, ""
}

// specialClass applies the bare classes whose GPUI method takes the
// value as an argument (border, rounded, shadow, ring), the Tailwind v3
// spellings of pinned methods, and the overflow values GPUI has no
// method for.
func specialClass(b *StyleBuilder, u string) (bool, string) {
	st := b.StyleMut()
	defaultRadius := RemsOf(0.25)
	switch u {
	case "border":
		b.Border1()
	case "border-t":
		b.BorderT1()
	case "border-r":
		b.BorderR1()
	case "border-b":
		b.BorderB1()
	case "border-l":
		b.BorderL1()
	case "border-x":
		b.BorderX1()
	case "border-y":
		b.BorderY1()
	case "border-solid":
		st.BorderStyle = Some(BorderStyleSolid)
	case "rounded":
		b.Rounded(defaultRadius)
	case "rounded-t":
		b.RoundedT(defaultRadius)
	case "rounded-r":
		b.RoundedR(defaultRadius)
	case "rounded-b":
		b.RoundedB(defaultRadius)
	case "rounded-l":
		b.RoundedL(defaultRadius)
	case "rounded-tl":
		b.RoundedTl(defaultRadius)
	case "rounded-tr":
		b.RoundedTr(defaultRadius)
	case "rounded-br":
		b.RoundedBr(defaultRadius)
	case "rounded-bl":
		b.RoundedBl(defaultRadius)
	case "shadow":
		// Tailwind v3's shadow is v4's (and the pin's) shadow-sm.
		b.ShadowSm()
	case "shadow-inner":
		b.Shadow([]BoxShadow{NewBoxShadow(0, 2, SolidBackground(Hsla{A: 0.05})).WithBlurRadius(4).WithInset()})
	case "ring":
		b.Ring(3)
	case "inset-ring":
		b.InsetRing(1)
	case "outline":
		// Tailwind v4's outline: outline: 1px solid.
		b.Outline(1)
	case "flex-grow":
		b.FlexGrow(1)
	case "flex-shrink":
		b.FlexShrink(1)
	case "aspect-video":
		b.AspectRatio(16.0 / 9.0)
	case "text-wrap":
		b.WhitespaceNormal()
	case "text-nowrap":
		b.WhitespaceNowrap()
	case "overflow-visible":
		st.OverflowX, st.OverflowY = Some(OverflowVisible), Some(OverflowVisible)
	case "overflow-clip":
		st.OverflowX, st.OverflowY = Some(OverflowClip), Some(OverflowClip)
	case "overflow-x-visible":
		st.OverflowX = Some(OverflowVisible)
	case "overflow-x-clip":
		st.OverflowX = Some(OverflowClip)
	case "overflow-y-visible":
		st.OverflowY = Some(OverflowVisible)
	case "overflow-y-clip":
		st.OverflowY = Some(OverflowClip)
	default:
		return false, ""
	}
	return true, ""
}

// lengthProp is a spacing or sizing class prefix.
type lengthProp struct {
	prefix string
	// definite marks DefiniteLength properties (padding, gap): never auto.
	definite bool
	// negative allows a leading minus (margins and insets).
	negative bool
	set      func(st *StyleRefinement, l Length)
}

// lengthProps lists the length prefixes, longer prefixes sharing a stem
// first (inset-x before inset).
var lengthProps = []lengthProp{
	{prefix: "inset-x", negative: true, set: func(st *StyleRefinement, l Length) { st.InsetLeft, st.InsetRight = Some(l), Some(l) }},
	{prefix: "inset-y", negative: true, set: func(st *StyleRefinement, l Length) { st.InsetTop, st.InsetBottom = Some(l), Some(l) }},
	{prefix: "inset", negative: true, set: func(st *StyleRefinement, l Length) {
		st.InsetTop, st.InsetRight, st.InsetBottom, st.InsetLeft = Some(l), Some(l), Some(l), Some(l)
	}},
	{prefix: "top", negative: true, set: func(st *StyleRefinement, l Length) { st.InsetTop = Some(l) }},
	{prefix: "right", negative: true, set: func(st *StyleRefinement, l Length) { st.InsetRight = Some(l) }},
	{prefix: "bottom", negative: true, set: func(st *StyleRefinement, l Length) { st.InsetBottom = Some(l) }},
	{prefix: "left", negative: true, set: func(st *StyleRefinement, l Length) { st.InsetLeft = Some(l) }},
	{prefix: "gap-x", definite: true, set: func(st *StyleRefinement, l Length) { st.GapWidth = Some(l.Definite) }},
	{prefix: "gap-y", definite: true, set: func(st *StyleRefinement, l Length) { st.GapHeight = Some(l.Definite) }},
	{prefix: "gap", definite: true, set: func(st *StyleRefinement, l Length) {
		st.GapWidth, st.GapHeight = Some(l.Definite), Some(l.Definite)
	}},
	{prefix: "p", definite: true, set: func(st *StyleRefinement, l Length) {
		d := Some(l.Definite)
		st.PaddingTop, st.PaddingRight, st.PaddingBottom, st.PaddingLeft = d, d, d, d
	}},
	{prefix: "px", definite: true, set: func(st *StyleRefinement, l Length) {
		st.PaddingLeft, st.PaddingRight = Some(l.Definite), Some(l.Definite)
	}},
	{prefix: "py", definite: true, set: func(st *StyleRefinement, l Length) {
		st.PaddingTop, st.PaddingBottom = Some(l.Definite), Some(l.Definite)
	}},
	{prefix: "pt", definite: true, set: func(st *StyleRefinement, l Length) { st.PaddingTop = Some(l.Definite) }},
	{prefix: "pr", definite: true, set: func(st *StyleRefinement, l Length) { st.PaddingRight = Some(l.Definite) }},
	{prefix: "pb", definite: true, set: func(st *StyleRefinement, l Length) { st.PaddingBottom = Some(l.Definite) }},
	{prefix: "pl", definite: true, set: func(st *StyleRefinement, l Length) { st.PaddingLeft = Some(l.Definite) }},
	{prefix: "m", negative: true, set: func(st *StyleRefinement, l Length) {
		st.MarginTop, st.MarginRight, st.MarginBottom, st.MarginLeft = Some(l), Some(l), Some(l), Some(l)
	}},
	{prefix: "mx", negative: true, set: func(st *StyleRefinement, l Length) { st.MarginLeft, st.MarginRight = Some(l), Some(l) }},
	{prefix: "my", negative: true, set: func(st *StyleRefinement, l Length) { st.MarginTop, st.MarginBottom = Some(l), Some(l) }},
	{prefix: "mt", negative: true, set: func(st *StyleRefinement, l Length) { st.MarginTop = Some(l) }},
	{prefix: "mr", negative: true, set: func(st *StyleRefinement, l Length) { st.MarginRight = Some(l) }},
	{prefix: "mb", negative: true, set: func(st *StyleRefinement, l Length) { st.MarginBottom = Some(l) }},
	{prefix: "ml", negative: true, set: func(st *StyleRefinement, l Length) { st.MarginLeft = Some(l) }},
	{prefix: "w", set: func(st *StyleRefinement, l Length) { st.SizeWidth = Some(l) }},
	{prefix: "h", set: func(st *StyleRefinement, l Length) { st.SizeHeight = Some(l) }},
	{prefix: "size", set: func(st *StyleRefinement, l Length) { st.SizeWidth, st.SizeHeight = Some(l), Some(l) }},
	{prefix: "min-w", set: func(st *StyleRefinement, l Length) { st.MinSizeWidth = Some(l) }},
	{prefix: "min-h", set: func(st *StyleRefinement, l Length) { st.MinSizeHeight = Some(l) }},
	{prefix: "max-w", set: func(st *StyleRefinement, l Length) { st.MaxSizeWidth = Some(l) }},
	{prefix: "max-h", set: func(st *StyleRefinement, l Length) { st.MaxSizeHeight = Some(l) }},
	{prefix: "basis", set: func(st *StyleRefinement, l Length) { st.FlexBasis = Some(l) }},
}

// maxWidthNamed is Tailwind v3's max-w scale in rems.
var maxWidthNamed = map[string]float32{
	"xs": 20, "sm": 24, "md": 28, "lg": 32, "xl": 36, "2xl": 42,
	"3xl": 48, "4xl": 56, "5xl": 64, "6xl": 72, "7xl": 80,
}

// lengthClass applies spacing and sizing classes with any value on the
// scale: multiples of 0.25 (p-13, w-0.75), fractions, px, full, auto,
// and arbitrary values (w-[84px], -mt-[4px]). For the values the
// generated methods cover it produces exactly what they do.
func lengthClass(b *StyleBuilder, u string) (bool, string) {
	body, neg := strings.CutPrefix(u, "-")
	if strings.HasPrefix(body, "inset-ring") || strings.HasPrefix(body, "inset-shadow") {
		return false, ""
	}
	for i := range lengthProps {
		p := &lengthProps[i]
		value, ok := strings.CutPrefix(body, p.prefix+"-")
		if !ok {
			continue
		}
		l, reason := p.parse(value, neg)
		if reason != "" {
			return true, reason
		}
		p.set(b.StyleMut(), l)
		return true, ""
	}
	return false, ""
}

// parse resolves a length class value.
func (p *lengthProp) parse(value string, neg bool) (Length, string) {
	if neg && !p.negative {
		return Length{}, "negative values are supported for margins and insets only"
	}
	switch value {
	case "auto":
		if p.definite || neg {
			return Length{}, "auto is not a valid value here"
		}
		return AutoLength(), ""
	case "screen", "dvh", "svh", "lvh", "dvw", "svw", "lvw":
		return Length{}, "viewport units are not supported; use size-full on a parent that fills the window (the root view is window-sized)"
	case "fit", "min", "max":
		return Length{}, "fit/min/max-content sizes are not supported; leave the size auto and use self-start or flex-none so the element hugs its content"
	case "none":
		if strings.HasPrefix(p.prefix, "max-") && !neg {
			return AutoLength(), ""
		}
	}
	if p.prefix == "max-w" && !neg {
		if rems, ok := maxWidthNamed[value]; ok {
			return RemsLength(rems), ""
		}
	}
	d, ok := definiteValue(value)
	if !ok {
		return Length{}, fmt.Sprintf("%q is not a length here (use 0–96 in steps of 0.25, px, full, a fraction like 1/2, or [12px], [1.5rem], [50%%])", value)
	}
	if d.Value < 0 && !p.negative {
		return Length{}, "negative values are supported for margins and insets only"
	}
	if neg {
		d.Value = -d.Value
	}
	return DefiniteLengthOf(d), ""
}

// definiteValue parses a non-auto length value: px, full, a fraction,
// a number on the 0.25rem spacing scale, or an arbitrary [..] value.
func definiteValue(v string) (DefiniteLength, bool) {
	switch v {
	case "px":
		return DefinitePx(1), true
	case "full":
		return Fraction(1), true
	}
	if inner, ok := arbitrary(v); ok {
		return arbitraryLength(inner)
	}
	if num, den, ok := strings.Cut(v, "/"); ok {
		a, okA := parseCount(num)
		b, okB := parseCount(den)
		if !okA || !okB || b == 0 {
			return DefiniteLength{}, false
		}
		// float32 division is correctly rounded, so this equals the
		// generated Fraction(1.0 / 3.0) constants.
		return Fraction(float32(a) / float32(b)), true
	}
	n, ok := spacingScale(v)
	if !ok {
		return DefiniteLength{}, false
	}
	if n == 0 {
		// The generated zero is px(0) (styles.rs suffix "0").
		return DefinitePx(0), true
	}
	return DefiniteRem(n / 4), true
}

// arbitraryLength parses a bracketed length: 12px, 1.5rem, 50% or 0.
func arbitraryLength(s string) (DefiniteLength, bool) {
	if s == "0" {
		return DefinitePx(0), true
	}
	if v, ok := strings.CutSuffix(s, "px"); ok {
		if n, ok := parseNumber(v); ok {
			return DefinitePx(n), true
		}
	}
	if v, ok := strings.CutSuffix(s, "rem"); ok {
		if n, ok := parseNumber(v); ok {
			return DefiniteRem(n), true
		}
	}
	if v, ok := strings.CutSuffix(s, "%"); ok {
		if n, ok := parseNumber(v); ok {
			return Fraction(n / 100), true
		}
	}
	return DefiniteLength{}, false
}

// absoluteLength parses a bracketed absolute length: 12px, 1.5rem or 0.
func absoluteLength(s string) (AbsoluteLength, bool) {
	d, ok := arbitraryLength(s)
	if !ok || d.Kind == DefiniteLengthFraction {
		return AbsoluteLength{}, false
	}
	return d.Absolute(), true
}

// typographyClass applies text classes with values: text-[13px],
// font-[550], leading-*, tracking-[…] and line-clamp-*.
func typographyClass(b *StyleBuilder, u string) (bool, string) {
	switch {
	case strings.HasPrefix(u, "text-["):
		inner, ok := arbitrary(u[len("text-"):])
		if !ok || strings.HasPrefix(inner, "#") {
			return false, "" // a color: colorClass
		}
		size, ok := absoluteLength(inner)
		if !ok {
			return true, "text-[…] takes a size like [13px] or [0.8rem], or a hex color like [#1e293b]"
		}
		b.TextSize(size)
	case strings.HasPrefix(u, "font-["):
		inner, ok := arbitrary(u[len("font-"):])
		w, okW := parseNumber(inner)
		if !ok || !okW || w < 1 || w > 1000 {
			return true, "font-[…] takes a weight from 1 to 1000, like [550]"
		}
		b.FontWeight(w)
	case strings.HasPrefix(u, "leading-"):
		v := u[len("leading-"):]
		if inner, ok := arbitrary(v); ok {
			if inner == "normal" {
				// CSS's own keyword (browser-parity D1): the font-metrics
				// line height, not Tailwind's named leading-normal (1.5).
				b.LineHeight(NormalLineHeight())
			} else if n, ok := parseNumber(inner); ok {
				b.LineHeight(Fraction(n))
			} else if d, ok := arbitraryLength(inner); ok {
				b.LineHeight(d)
			} else {
				return true, "leading-[…] takes a multiple of the font size like [1.4], a length like [20px], or [normal]"
			}
		} else if n, ok := spacingScale(v); ok {
			b.LineHeight(DefiniteRem(n / 4))
		} else {
			return true, "leading-… takes none, tight, snug, normal, relaxed, loose, a number on the 0.25rem scale, or [1.4] / [20px] / [normal]"
		}
	case strings.HasPrefix(u, "tracking-"):
		// Ticket 38: the named values are em-based and resolve against
		// the element's own font size (CSS letter-spacing's <length> em
		// conversion, style_builder_converter.cc ConvertSpacing); the
		// arbitrary form stays pixels.
		switch u {
		case "tracking-tighter":
			b.LetterSpacingEm(-0.05)
		case "tracking-tight":
			b.LetterSpacingEm(-0.025)
		case "tracking-normal":
			b.LetterSpacingEm(0)
		case "tracking-wide":
			b.LetterSpacingEm(0.025)
		case "tracking-wider":
			b.LetterSpacingEm(0.05)
		case "tracking-widest":
			b.LetterSpacingEm(0.1)
		default:
			inner, ok := arbitrary(u[len("tracking-"):])
			if !ok {
				return true, "tracking-… takes tighter, tight, normal, wide, wider, widest or [0.5px]"
			}
			px, ok := pixels(inner)
			if !ok {
				return true, "tracking-[…] takes pixels, like [0.5px] (the named values are em-based)"
			}
			b.LetterSpacing(px)
		}
	case strings.HasPrefix(u, "line-clamp-"):
		n, ok := parseCount(u[len("line-clamp-"):])
		if !ok || n == 0 || n > math.MaxInt32 {
			return true, "line-clamp-… takes a line count"
		}
		// Ticket 06: the clamp itself now ends the last line with the
		// CSS ellipsis (text_truncate.go's EvaluateTextOverflow
		// synthesis), so the class needs only the clamp.
		b.LineClamp(int(n))
	case u == "font-sans" || u == "font-serif" || u == "font-mono":
		// Ticket 06: the Tailwind default theme stacks, resolved the way
		// Chrome on Windows resolves them (see genericFontFamilyAliases'
		// doc and the font fixture): font-sans heads with ui-sans-serif,
		// system-ui (the system UI font, Segoe UI); font-serif's first
		// installed family is Georgia; font-mono's is Consolas.
		switch u {
		case "font-sans":
			b.FontFamily(".SystemUIFont")
		case "font-serif":
			b.FontFamily("Georgia")
		default:
			b.FontFamily("Consolas")
		}
	case isNumericVariantClass(u):
		// Ticket 06: the font-variant-numeric utilities map to their
		// OpenType features (Chromium's FromFontDescription, platform/
		// fonts/shaping/font_features.cc:157-196). Each class sets the
		// whole feature list, like Tailwind's single
		// font-feature-settings declaration.
		b.FontFeatures(numericVariantFeatures(u))
	default:
		return false, ""
	}
	return true, ""
}

// isNumericVariantClass reports whether u is one of Tailwind's
// font-variant-numeric utilities (including normal-nums, the reset).
func isNumericVariantClass(u string) bool {
	return numericVariantClass(u) != "" || u == "normal-nums"
}

// numericVariantClass returns the OpenType feature tag a Tailwind
// font-variant-numeric utility stands for, or "" when u is not one
// (the tags and their font-variant-numeric origins: Chromium's
// FromFontDescription, platform/fonts/shaping/font_features.cc:157-196;
// Tailwind's utilities are the same one-tag settings).
func numericVariantClass(u string) string {
	switch u {
	case "tabular-nums":
		return "tnum"
	case "proportional-nums":
		return "pnum"
	case "lining-nums":
		return "lnum"
	case "oldstyle-nums":
		return "onum"
	case "slashed-zero":
		return "zero"
	case "ordinal":
		return "ordn"
	case "diagonal-fractions":
		return "frac"
	case "stacked-fractions":
		return "afrc"
	default:
		return ""
	}
}

// numericVariantFeatures returns the feature list a numeric-variant
// class sets: normal-nums clears the features (Tailwind's
// font-feature-settings: normal); every other utility sets its single
// tag to 1.
func numericVariantFeatures(u string) []FontFeature {
	if u == "normal-nums" {
		return nil
	}
	return []FontFeature{{Tag: numericVariantClass(u), Value: 1}}
}

// borderClass applies border widths, radii and ring widths beyond the
// generated scale: border-13, border-[3px], border-t-[3px],
// rounded-[10px], rounded-tl-[10px], ring-3, inset-ring-[3px], plus
// the outline utilities' lengths: outline-3, outline-[3px],
// outline-offset-2 and outline-offset-[3px].
func borderClass(b *StyleBuilder, u string) (bool, string) {
	// outline-offset-<n> comes first: outline-offset-2 must not fall
	// into the outline-<n> width arm below.
	if rest, ok := strings.CutPrefix(u, "outline-offset-"); ok {
		width, ok := borderWidth(rest)
		if !ok {
			return false, "" // a color or another family
		}
		if width.Kind != AbsoluteLengthPx {
			return true, "outline offsets are pixels, like outline-offset-[3px]"
		}
		b.OutlineOffset(width.Value)
		return true, ""
	}
	if rest, ok := strings.CutPrefix(u, "outline-"); ok {
		width, ok := borderWidth(rest)
		if !ok {
			return false, "" // a color: colorClass
		}
		if width.Kind != AbsoluteLengthPx {
			return true, "outline widths are pixels, like outline-[3px]"
		}
		b.Outline(width.Value)
		return true, ""
	}
	for _, ring := range []struct {
		prefix string
		set    func(px float32)
	}{
		{"ring-", func(px float32) { b.Ring(px) }},
		{"inset-ring-", func(px float32) { b.InsetRing(px) }},
	} {
		rest, ok := strings.CutPrefix(u, ring.prefix)
		if !ok {
			continue
		}
		width, ok := borderWidth(rest)
		if !ok {
			return false, "" // a color: colorClass
		}
		if width.Kind != AbsoluteLengthPx {
			return true, "ring widths are pixels, like ring-[3px]"
		}
		ring.set(width.Value)
		return true, ""
	}
	if rest, ok := strings.CutPrefix(u, "border-"); ok {
		side, value := "", rest
		if len(rest) >= 2 && rest[1] == '-' && strings.IndexByte("trblxyse", rest[0]) >= 0 {
			side, value = rest[:1], rest[2:]
		}
		width, ok := borderWidth(value)
		if !ok {
			if side != "" {
				if _, isColor, _ := parseColor(value); isColor {
					return true, "per-side border colors are not supported: an element has one border color; use border-<color>"
				}
			}
			return false, ""
		}
		switch side {
		case "":
			b.Border(width)
		case "t":
			b.BorderT(width)
		case "r":
			b.BorderR(width)
		case "b":
			b.BorderB(width)
		case "l":
			b.BorderL(width)
		case "x":
			b.BorderX(width)
		case "y":
			b.BorderY(width)
		default:
			return true, "logical sides (border-s, border-e) are not supported; use border-l / border-r"
		}
		return true, ""
	}
	if rest, ok := strings.CutPrefix(u, "rounded-"); ok {
		corner, value := "", rest
		if i := strings.IndexByte(rest, '['); i > 0 && rest[i-1] == '-' {
			corner, value = rest[:i-1], rest[i:]
		}
		inner, ok := arbitrary(value)
		if !ok {
			return false, ""
		}
		r, ok := absoluteLength(inner)
		if !ok {
			return true, "rounded-[…] takes px or rem, like [10px]"
		}
		switch corner {
		case "":
			b.Rounded(r)
		case "t":
			b.RoundedT(r)
		case "r":
			b.RoundedR(r)
		case "b":
			b.RoundedB(r)
		case "l":
			b.RoundedL(r)
		case "tl":
			b.RoundedTl(r)
		case "tr":
			b.RoundedTr(r)
		case "br":
			b.RoundedBr(r)
		case "bl":
			b.RoundedBl(r)
		default:
			return false, ""
		}
		return true, ""
	}
	return false, ""
}

// borderWidth parses a border width value: a pixel count or [3px] /
// [0.1rem].
func borderWidth(v string) (AbsoluteLength, bool) {
	if inner, ok := arbitrary(v); ok {
		return absoluteLength(inner)
	}
	n, ok := parseCount(v)
	if !ok {
		return AbsoluteLength{}, false
	}
	return Px(float32(n)), true
}

// opacityClass applies opacity-<0..100> and opacity-[0.35].
func opacityClass(b *StyleBuilder, u string) (bool, string) {
	v, ok := strings.CutPrefix(u, "opacity-")
	if !ok {
		return false, ""
	}
	f, ok := percentValue(v)
	if !ok {
		return true, "opacity-… takes 0–100, or [0.35]"
	}
	b.Opacity(f)
	return true, ""
}

// gridClass applies grid templates and placements with a count, and
// the arbitrary track lists (browser-core 42): grid-cols-[240px_1fr]
// and grid-rows-[auto_auto] — the underscores unescape to spaces like
// every Tailwind arbitrary value, then parse as a CSS track list.
func gridClass(b *StyleBuilder, u string) (bool, string) {
	for _, g := range []struct {
		prefix string
		apply  func(n int16)
	}{
		{"grid-cols-", func(n int16) { b.GridCols(uint16(n)) }},
		{"grid-rows-", func(n int16) { b.GridRows(uint16(n)) }},
		{"col-span-", func(n int16) { b.ColSpan(uint16(n)) }},
		{"row-span-", func(n int16) { b.RowSpan(uint16(n)) }},
		{"col-start-", func(n int16) { b.ColStart(n) }},
		{"col-end-", func(n int16) { b.ColEnd(n) }},
		{"row-start-", func(n int16) { b.RowStart(n) }},
		{"row-end-", func(n int16) { b.RowEnd(n) }},
	} {
		v, ok := strings.CutPrefix(u, g.prefix)
		if !ok {
			continue
		}
		// The arbitrary track list: grid-cols-[240px_1fr] and
		// grid-rows-[auto_auto]. The placement prefixes keep their count
		// form only.
		if inner, ok := arbitrary(v); ok {
			if g.prefix != "grid-cols-" && g.prefix != "grid-rows-" {
				return true, g.prefix + "… takes a count, not a bracketed value"
			}
			tracks, err := parseTrackListText(tailwindUnescape(inner))
			if err != nil {
				return true, g.prefix + "[…] takes a track list, like [240px_1fr]: " + err.Error()
			}
			if len(tracks) > 256 {
				return true, g.prefix + "[…] takes at most 256 tracks"
			}
			if g.prefix == "grid-cols-" {
				b.GridColsList(tracks...)
			} else {
				b.GridRowsList(tracks...)
			}
			return true, ""
		}
		n, ok := parseCount(v)
		if !ok || n == 0 || n > math.MaxInt16 {
			return true, g.prefix + "… takes a count or a track list, like [240px_1fr] (other grid templates than repeat(n) need the brackets)"
		}
		g.apply(int16(n))
		return true, ""
	}
	return false, ""
}

// gridAreaClass applies the named grid area surface (browser-core 42):
// grid-areas-["head_head"_"side_main"] sets the container's template
// (the underscores unescape to spaces, restoring the CSS quoted rows)
// and grid-area-[head] places the item in the named area.
func gridAreaClass(b *StyleBuilder, u string) (bool, string) {
	if inner, ok := strings.CutPrefix(u, "grid-areas-"); ok {
		value, ok := arbitrary(inner)
		if !ok {
			// A bare word is not a template: the class needs the brackets.
			return true, "grid-areas-… takes a bracketed template, like [\"head_head\"_\"side_main\"]"
		}
		template, err := ParseGridTemplateAreas(tailwindUnescape(value))
		if err != nil {
			return true, "grid-areas-[…]: " + err.Error()
		}
		b.GridTemplateAreasValue(template)
		return true, ""
	}
	if inner, ok := strings.CutPrefix(u, "grid-area-"); ok {
		name, ok := arbitrary(inner)
		if !ok {
			return true, "grid-area-… takes a bracketed area name, like [head]"
		}
		if name == "" || !validGridAreaName(name) {
			return true, "grid-area-[…] takes an area name"
		}
		b.GridArea(name)
		return true, ""
	}
	return false, ""
}

// tailwindUnescape converts a Tailwind arbitrary value's underscores
// back to spaces (the documented unescaping; a leading underscore keeps
// a literal underscore, which grid values never need).
func tailwindUnescape(inner string) string {
	return strings.ReplaceAll(inner, "_", " ")
}

// aspectClass applies aspect-[4/3] and aspect-[1.5].
func aspectClass(b *StyleBuilder, u string) (bool, string) {
	v, ok := strings.CutPrefix(u, "aspect-")
	if !ok {
		return false, ""
	}
	inner, ok := arbitrary(v)
	if !ok {
		return false, ""
	}
	ratio, ok := parseNumber(inner)
	if num, den, fraction := strings.Cut(inner, "/"); fraction {
		w, okW := parseNumber(num)
		h, okH := parseNumber(den)
		ok = okW && okH && h != 0
		if ok {
			ratio = w / h
		}
	}
	if !ok || ratio <= 0 {
		return true, "aspect-[…] takes a ratio, like [4/3] or [1.5]"
	}
	b.AspectRatio(ratio)
	return true, ""
}

// blurScale is Tailwind v3's blur scale in px (blur-xs is v4's 4px).
var blurScale = map[string]float32{"xs": 4, "sm": 4, "": 8, "md": 12, "lg": 16, "xl": 24, "2xl": 40, "3xl": 64}

// blurClass applies blur-* and backdrop-blur-*. Filter classes
// compose in class order like the fluent methods (ticket 41: blur-sm
// grayscale is the CSS filter: blur(4px) grayscale(100%) — Tailwind
// composes its filter utilities through CSS variables in a fixed
// order, and the class order here is the CSS spelling, a superset for
// the single-function cases); blur-none resets the whole filter
// chain (Tailwind's variable reset).
func blurClass(b *StyleBuilder, u string) (bool, string) {
	rest, backdrop := strings.CutPrefix(u, "backdrop-")
	if rest != "blur" && !strings.HasPrefix(rest, "blur-") {
		if backdrop {
			return true, "only backdrop-blur-* backdrop filters are supported"
		}
		return false, ""
	}
	size := strings.TrimPrefix(strings.TrimPrefix(rest, "blur"), "-")
	if size == "none" {
		// Tailwind's reset: no blur clears the chain.
		if backdrop {
			b.StyleMut().BackdropFilter = Some([]Filter(nil))
		} else {
			b.StyleMut().Filter = Some([]Filter(nil))
		}
		return true, ""
	}
	var blur Filter
	if r, ok := blurScale[size]; ok {
		blur = Blur(r)
	} else if inner, ok := arbitrary(size); ok {
		px, ok := pixels(inner)
		if !ok || px < 0 {
			return true, "blur-[…] takes pixels, like [6px]"
		}
		blur = Blur(px)
	} else {
		return true, "blur-… takes none, sm, md, lg, xl, 2xl, 3xl, or [6px]"
	}
	if backdrop {
		b.BackdropBlur(blur.BlurRadius)
	} else {
		b.Blur(blur.BlurRadius)
	}
	return true, ""
}

// ---------------------------------------------------------------------------
// Gradients (browser-core ticket 40)
// ---------------------------------------------------------------------------

// bgLinearDirections maps Tailwind v4's bg-linear-to-* values to the
// CSS direction keywords (tailwindcss v4 utilities.ts,
// linearGradientDirections: 'to-t' -> "to top", …).
var bgLinearDirections = map[string]GradientDirection{
	"to-t":  GradientDirectionToTop,
	"to-tr": GradientDirectionToTopRight,
	"to-r":  GradientDirectionToRight,
	"to-br": GradientDirectionToBottomRight,
	"to-b":  GradientDirectionToBottom,
	"to-bl": GradientDirectionToBottomLeft,
	"to-l":  GradientDirectionToLeft,
	"to-tl": GradientDirectionToTopLeft,
}

// gradientClass applies the Tailwind v4 gradient utilities: bg-linear-*
// (the direction or angle), from-* and to-* (the two stops' colors and
// positions). Tailwind emits `in oklab` interpolation for the named and
// numeric forms (utilities.ts resolveInterpolationModifier); the record
// draws CSS's legacy sRGB interpolation, so those classes render like
// the equivalent CSS without an interpolation method, and the /srgb
// modifier opts into exactly that — a recorded class-layer divergence
// (docs/tailwind.md). via-* (a middle stop) is rejected with
// ErrGradientViaUnsupported: three stops need the n-stop record
// (ticket 52).
//
// The direction and the stops are separate classes, so they compose in
// chain order: bg-linear-* starts the gradient (keeping any stops an
// earlier bg-linear-* in the same string set, Tailwind's variables
// being independent) and from-*/to-* set one stop of the gradient the
// same string (or variant) already started.
func gradientClass(b *StyleBuilder, u string) (bool, string) {
	if u == "via-none" || strings.HasPrefix(u, "via-") {
		return true, ErrGradientViaUnsupported.Error()
	}
	body := strings.TrimPrefix(u, "-")
	if value, ok := strings.CutPrefix(body, "bg-linear-"); ok {
		return bgLinearClass(b, u, value, strings.HasPrefix(u, "-"))
	}
	if body == "bg-linear" {
		return true, "bg-linear takes a direction or an angle: bg-linear-to-r, bg-linear-45, bg-linear-[45deg]"
	}
	for _, root := range []string{"from-", "to-"} {
		if value, ok := strings.CutPrefix(u, root); ok {
			return gradientStopClass(b, u, root[:len(root)-1], value)
		}
	}
	return false, ""
}

// bgLinearClass applies one bg-linear-* utility (u for the error
// messages, value the class's value after bg-linear-, negative marking
// a leading minus).
func bgLinearClass(b *StyleBuilder, u, value string, negative bool) (bool, string) {
	value, modifier, hasModifier := cutModifier(value)
	keyword, isKeyword := bgLinearDirections[value]
	var direction GradientDirection
	var angle float32
	switch {
	case strings.HasPrefix(value, "["):
		inner, ok := arbitrary(value)
		if !ok {
			return true, "bg-linear-[…] takes an angle in brackets, like bg-linear-[45deg]"
		}
		if hasModifier {
			return true, "bg-linear-[…] takes no modifier (Tailwind v4 rejects it too)"
		}
		degrees, ok := parseAngle(inner)
		if !ok {
			return true, "bg-linear-[…] takes an angle, like bg-linear-[45deg] (deg, rad, grad or turn)"
		}
		// Tailwind v4's arbitrary form emits no interpolation method, so
		// it interpolates in CSS's legacy sRGB — exactly what the record
		// draws. The class matches a real Tailwind page bit for bit.
		angle = degrees
		direction = GradientDirectionAngle
	case isKeyword:
		if negative {
			return true, "bg-linear-to-* directions cannot be negative"
		}
		direction = keyword
	default:
		count, ok := parseCount(value)
		if !ok {
			return true, "bg-linear-<value> takes a direction or an angle: bg-linear-to-r, bg-linear-45, bg-linear-[45deg]"
		}
		if negative {
			angle = -float32(count)
		} else {
			angle = float32(count)
		}
		direction = GradientDirectionAngle
	}
	if hasModifier {
		// The modifier selects the interpolation space (Tailwind v4's
		// default is oklab); srgb is the one space the record draws.
		if modifier != "srgb" {
			return true, ErrGradientInterpolationUnsupported.Error()
		}
	}
	st := b.StyleMut()
	var gradient LinearGradient
	if background, ok := st.Background.Get(); ok && background.Kind == BackgroundKindLinearGradient {
		// Tailwind's direction and stops are independent variables, so a
		// later bg-linear-* keeps the stops an earlier one set.
		gradient = background.Gradient
	} else {
		gradient.From, gradient.To = gradientDefaultStops()
	}
	gradient.Direction = direction
	gradient.Angle = angle
	st.Background = Some(Background{Kind: BackgroundKindLinearGradient, Gradient: gradient})
	return true, ""
}

// gradientStopClass applies one from-*/to-* utility: a color
// (from-red-500, from-[#ff0000], from-transparent, from-red-500/50) or a
// position on the gradient line (from-10%, from-[10%]). root is "from"
// or "to".
func gradientStopClass(b *StyleBuilder, u, root, value string) (bool, string) {
	st := b.StyleMut()
	background, ok := st.Background.Get()
	if !ok || background.Kind != BackgroundKindLinearGradient {
		return true, u + " sets one stop of a gradient: put bg-linear-… earlier in the same class string (or the same variant), like bg-linear-to-r from-… to-…"
	}
	stop := &background.Gradient.From
	other := &background.Gradient.To
	if root == "to" {
		stop, other = &background.Gradient.To, &background.Gradient.From
	}
	if position, ok := gradientStopPosition(value); ok {
		stop.Position = position
	} else if c, isColor, reason := parseColor(value); isColor && reason == "" {
		stop.Color = c
	} else if inner, isLength := bracketedLength(value); isLength {
		return true, u + " takes a percentage position (from-10%, from-[10%]): a length like [" + inner + "] resolves against the gradient line's length, which needs the n-stop record (ticket 52)"
	} else if reason != "" {
		return true, reason
	} else {
		return true, u + " takes a color (from-red-500, from-[#ff0000], from-transparent) or a position (from-10%)"
	}
	if stop.Position == other.Position {
		return true, "from and to sit at the same position: a hard stop needs the n-stop record (ticket 52, the R16 gradient redesign)"
	}
	if background.Gradient.From.Position > background.Gradient.To.Position {
		return true, "from's position must come before to's: CSS clamps a descending pair into a hard stop, which needs the n-stop record (ticket 52)"
	}
	st.Background = Some(background)
	return true, ""
}

// gradientStopPosition parses a stop position: n% (a positive
// integer percent, Tailwind's gradient-color-stop-positions scale) or a
// bracketed percentage [10%] / [12.5%]. Returns the position as a
// fraction of the gradient line.
func gradientStopPosition(value string) (float32, bool) {
	inner, bracketed := arbitrary(value)
	if !bracketed {
		inner = value
	}
	if !strings.HasSuffix(inner, "%") {
		return 0, false
	}
	p := strings.TrimSuffix(inner, "%")
	if bracketed {
		f, err := strconv.ParseFloat(p, 32)
		if err != nil || f < 0 || f > 100 {
			return 0, false
		}
		return float32(f) / 100, true
	}
	n, err := strconv.ParseUint(p, 10, 32)
	if err != nil {
		return 0, false
	}
	return float32(n) / 100, true
}

// bracketedLength reports a bracketed value that is a length: a
// number followed by a unit, like [10px] (a trailing % is a position,
// a bare number is neither).
func bracketedLength(value string) (inner string, ok bool) {
	if !strings.HasPrefix(value, "[") {
		return "", false
	}
	inner, ok = arbitrary(value)
	if !ok || inner == "" || strings.HasSuffix(inner, "%") {
		return "", false
	}
	i := 0
	if inner[i] == '-' || inner[i] == '+' {
		i++
	}
	digits := 0
	for i < len(inner) && (inner[i] >= '0' && inner[i] <= '9' || inner[i] == '.') {
		i++
		digits++
	}
	if digits == 0 || i == len(inner) {
		return "", false // a bare number (no unit) is not a length here
	}
	return inner, true
}

// parseAngle parses a CSS angle with a unit: 45deg, 1.2rad, 100grad,
// 0.5turn (negative allowed), in degrees. The longer suffixes match
// first so "grad" never reads as "rad".
func parseAngle(s string) (float32, bool) {
	for _, unit := range []struct {
		suffix string
		factor float64
	}{
		{"grad", 0.9},
		{"turn", 360},
		{"deg", 1},
		{"rad", 180 / math.Pi},
	} {
		if v, ok := strings.CutSuffix(s, unit.suffix); ok {
			f, err := strconv.ParseFloat(v, 32)
			if err != nil {
				return 0, false
			}
			return float32(f * unit.factor), true
		}
	}
	return 0, false
}

// filterPercentScale is Tailwind v3's percentage scale for
// brightness, contrast and saturate as fractions (v3 has no
// brightness-105/110 for contrast; those are brightness's).
var filterPercentScale = map[string]float32{
	"0": 0, "50": 0.5, "75": 0.75, "90": 0.9, "95": 0.95, "100": 1,
	"105": 1.05, "110": 1.1, "125": 1.25, "150": 1.5, "200": 2,
}

// hueRotateScale is Tailwind v3's hue-rotate scale in degrees.
var hueRotateScale = map[string]float32{"0": 0, "15": 15, "30": 30, "60": 60, "90": 90, "180": 180}

// transformClass applies the CSS-transform families with values
// (browser-core ticket 56): translate-x-*, translate-y-*, scale-*,
// scale-x-*, scale-y-*, rotate-*, skew-x-* and skew-y-*. Consecutive
// transform classes APPEND to the transform list in class order - the
// CSS spelling of `transform: translate(16px) rotate(45deg)` - like the
// fluent methods (.TranslateX(Px(16)).Rotate(45)); a later
// .Transform(...) call replaces the whole list. The origin classes
// (origin-center, origin-top-left, ...) are method classes the
// generated switch applies.
func transformClass(b *StyleBuilder, u string) (bool, string) {
	body, neg := strings.CutPrefix(u, "-")
	// The 3D transform families (rotate-x-*, rotate-y-*, perspective)
	// get their own reason before the 2D families read them as the
	// rotate family with a bad value.
	for _, prefix := range []string{"rotate-x", "rotate-y", "perspective", "transform-3d", "backface"} {
		if body == prefix || strings.HasPrefix(body, prefix+"-") {
			return true, "3D transforms are not supported; translate-*, scale-*, rotate-*, skew-* and origin-* are 2D"
		}
	}
	var family string
	for _, candidate := range []string{"translate-x", "translate-y", "scale-x", "scale-y", "scale", "rotate", "skew-x", "skew-y"} {
		if rest, ok := strings.CutPrefix(body, candidate+"-"); ok {
			// The longer prefixes first so translate-x-4 does not read
			// as another family.
			_ = rest
			family = candidate
			break
		}
	}
	if family == "" {
		return false, ""
	}
	value, ok := strings.CutPrefix(body, family+"-")
	if !ok {
		return false, ""
	}
	switch family {
	case "translate-x", "translate-y":
		// A length like the spacing families: the 0.25rem scale, px,
		// full (100%), a fraction (translate-x-1/2) or an arbitrary
		// [12px]/[1.5rem]/[50%]. A negative value moves the other way.
		length, reason := transformLengthValue(value, neg)
		if reason != "" {
			return true, reason
		}
		if family == "translate-x" {
			b.TranslateX(length)
		} else {
			b.TranslateY(length)
		}
		return true, ""
	case "scale", "scale-x", "scale-y":
		// A plain number is a percentage (scale-95 = 0.95); an
		// arbitrary value is the raw factor (scale-[1.7]).
		factor, ok := transformScaleValue(value)
		if !ok {
			return true, family + "-… takes a percentage like " + family + "-95, or an arbitrary factor like [1.7]"
		}
		if neg {
			factor = -factor
		}
		switch family {
		case "scale":
			b.ScaleUniform(factor)
		case "scale-x":
			b.ScaleX(factor)
		default:
			b.ScaleY(factor)
		}
		return true, ""
	case "rotate", "skew-x", "skew-y":
		// A plain number or bracketed degrees: rotate-45, skew-x-3,
		// rotate-[45deg].
		degrees, ok := transformAngleValue(value)
		if !ok {
			return true, family + "-… takes degrees like " + family + "-45, or an arbitrary angle like [45deg]"
		}
		if neg {
			degrees = -degrees
		}
		switch family {
		case "rotate":
			b.Rotate(degrees)
		case "skew-x":
			b.SkewX(degrees)
		default:
			b.SkewY(degrees)
		}
		return true, ""
	}
	return false, ""
}

// transformLengthValue parses a translate class value: the spacing
// scale (a multiple of 0.25rem), px, full, a fraction or an arbitrary
// length. neg mirrors the leading minus onto the value.
func transformLengthValue(value string, neg bool) (DefiniteLength, string) {
	d, ok := definiteValue(value)
	if !ok {
		return DefiniteLength{}, "translate-… takes a length like translate-x-4 (the 0.25rem scale), px, full, a fraction like 1/2, or [12px], [1.5rem], [50%]"
	}
	if neg {
		d.Value = -d.Value
	}
	return d, ""
}

// transformScaleValue parses a scale class value: a plain number (the
// percentage, scale-95 = 0.95) or an arbitrary raw factor
// (scale-[1.7]).
func transformScaleValue(v string) (float32, bool) {
	if inner, ok := arbitrary(v); ok {
		n, ok := parseNumber(inner)
		if !ok {
			return 0, false
		}
		return n, true
	}
	n, ok := parseNumber(v)
	if !ok {
		return 0, false
	}
	return n / 100, true
}

// transformAngleValue parses an angle class value: a plain number
// (degrees) or an arbitrary [45deg].
func transformAngleValue(v string) (float32, bool) {
	if inner, ok := arbitrary(v); ok {
		if d, ok := strings.CutSuffix(inner, "deg"); ok {
			if n, ok := parseNumber(d); ok {
				return n, true
			}
		}
		return 0, false
	}
	n, ok := parseNumber(v)
	if !ok {
		return 0, false
	}
	return n, true
}

// filterClass applies the CSS colour filter utilities: grayscale,
// sepia and invert (bare = the full amount, -0 none), the scaled
// brightness-*, contrast-* and saturate-*, hue-rotate-*, and their
// arbitrary forms (grayscale-[.5], brightness-[1.75],
// hue-rotate-[90deg]). Each appends to the element's filter chain in
// class order — the CSS spelling of the chain, like the fluent methods
// (.Grayscale(1).Blur(4)); Tailwind itself composes its filter
// utilities through CSS variables in a fixed order, which the class
// order here is a superset of for the single-function cases. The blur
// family is blurClass's; the backdrop- prefix never reaches this stage
// (blurClass explains it). An unresolvable suffix reports why.
func filterClass(b *StyleBuilder, u string) (bool, string) {
	body := strings.TrimPrefix(u, "-")
	var name, suffix string
	for _, family := range []string{"grayscale", "sepia", "invert", "saturate", "brightness", "contrast", "hue-rotate"} {
		if body == family {
			name = family
			break
		}
		if rest, ok := strings.CutPrefix(body, family+"-"); ok {
			name, suffix = family, rest
			break
		}
	}
	if name == "" {
		return false, ""
	}
	negative := strings.HasPrefix(u, "-")
	switch name {
	case "grayscale", "sepia", "invert":
		// The bare class is the full amount (Tailwind's DEFAULT);
		// -0 turns it off; the arbitrary form takes a number or
		// percentage.
		amount := float32(1)
		switch {
		case suffix == "":
		case suffix == "0":
			amount = 0
		case suffix == "none":
			amount = 0
		default:
			value, ok := filterArbitraryAmount(suffix)
			if !ok {
				return true, name + "-… takes 0, none, or an arbitrary value, like [0.5] or [50%]"
			}
			amount = value
		}
		switch name {
		case "grayscale":
			b.Grayscale(amount)
		case "sepia":
			b.Sepia(amount)
		default:
			b.Invert(amount)
		}
		return true, ""
	case "saturate", "brightness", "contrast":
		if suffix == "" {
			return true, name + " needs a scale value, like " + name + "-150 (or an arbitrary [1.75])"
		}
		amount, ok := float32(1), false
		if value, scaled := filterPercentScale[suffix]; scaled {
			amount, ok = value, true
		} else {
			amount, ok = filterArbitraryAmount(suffix)
			if !ok {
				return true, name + "-… takes 0, 50, 75, 90, 95, 100, 105, 110, 125, 150, 200, or an arbitrary value, like [1.75]"
			}
		}
		switch name {
		case "saturate":
			b.Saturate(amount)
		case "brightness":
			b.Brightness(amount)
		default:
			b.Contrast(amount)
		}
		return true, ""
	default: // hue-rotate
		if suffix == "" {
			return true, "hue-rotate needs a scale value, like hue-rotate-90 (or an arbitrary [90deg])"
		}
		degrees, ok := float32(0), false
		if value, scaled := hueRotateScale[suffix]; scaled {
			degrees, ok = value, true
		} else if inner, arbitraryOK := arbitrary(suffix); arbitraryOK {
			if value, numberOK := filterDegrees(inner); numberOK {
				degrees, ok = value, true
			}
		}
		if !ok {
			return true, "hue-rotate-… takes 0, 15, 30, 60, 90, 180, or an arbitrary value, like [90deg]"
		}
		if negative {
			degrees = -degrees
		}
		b.HueRotate(degrees)
		return true, ""
	}
}

// filterArbitraryAmount parses an arbitrary filter amount: a bare
// number (grayscale-[.5], brightness-[1.75]) or a percentage
// (grayscale-[50%]).
func filterArbitraryAmount(suffix string) (float32, bool) {
	inner, ok := arbitrary(suffix)
	if !ok {
		return 0, false
	}
	percent := strings.HasSuffix(inner, "%")
	value, ok := parseNumber(strings.TrimSuffix(inner, "%"))
	if !ok {
		return 0, false
	}
	if percent {
		value /= 100
	}
	return float32(value), true
}

// filterDegrees parses an arbitrary hue-rotate value: a bare number
// (degrees) or one with a deg suffix ([90deg]).
func filterDegrees(inner string) (float32, bool) {
	value, ok := parseNumber(strings.TrimSuffix(inner, "deg"))
	if !ok {
		return 0, false
	}
	return float32(value), true
}

// unsupportedClasses are the Tailwind utilities GPUI cannot express. A
// prefix matches the class itself and the class followed by "-",
// ignoring a leading minus.
var unsupportedClasses = []struct {
	prefixes []string
	reason   string
}{
	{[]string{"z"}, "GPUI has no z-index: siblings paint in tree order; render overlays last, or wrap them in gpui.Deferred"},
	{[]string{"transition", "duration", "ease", "delay", "animate"}, "GPUI styles have no CSS transitions or animations: state styles switch instantly (animate with gpui.WithAnimation)"},
	{[]string{"transform"}, "the bare transform class is not needed; the transform utilities (translate-*, scale-*, rotate-*, skew-*) apply themselves, and .Transform(…) replaces the whole list"},
	{[]string{"perspective", "transform-3d", "backface"}, "3D transforms are not supported; translate-*, scale-*, rotate-*, skew-* and origin-* are 2D"},
	{[]string{"space-x", "space-y"}, "space-* is not supported; use gap-* on the flex parent"},
	{[]string{"divide"}, "divide-* is not supported; give each child after the first a border-t (or border-l), or put separator elements between them"},
	{[]string{"pointer-events"}, "pointer-events-* is not supported"},
	{[]string{"inline-table", "table", "list-item"}, "this display type is not supported; use flex, grid, block, inline, inline-flex, inline-block, inline-grid, contents or flow-root"},
	{[]string{"no-underline"}, "no-underline is not supported; leave underline out, or call .TextDecorationNone()"},
	{[]string{"outline-solid", "outline-dotted", "outline-double", "outline-hidden"}, "only outline, outline-<n>, outline-offset-<n>, outline-<color>, outline-none and outline-dashed are supported (the scene's border records are solid and dashed only; outline-hidden has no port spelling — outline-none is the plain opt-out)"},
	{[]string{"ring-offset"}, "ring offsets are not supported"},
	{[]string{"ring-inset"}, "use inset-ring-* for an inset ring"},
	{[]string{"bg-gradient"}, "Tailwind v4 renamed bg-gradient-* to bg-linear-*; write bg-linear-to-r from-… to-…"},
	{[]string{"bg-radial", "bg-conic"}, "radial and conic gradients are not supported yet (ticket 52, the R16 gradient redesign); only two-stop linear gradients are"},
	{[]string{"bg-cover", "bg-contain", "bg-center", "bg-no-repeat", "bg-repeat", "bg-fixed", "bg-clip", "bg-origin", "bg-blend", "bg-none"}, "background images are not supported; put an img element behind the content"},
	{[]string{"drop-shadow", "filter", "mix-blend"}, "only blur and the colour filters are supported (blur-*, grayscale, sepia, invert, brightness-*, contrast-*, saturate-*, hue-rotate-*); drop-shadow() and mix-blend-mode are not, and the bare filter class is not needed — each filter utility applies itself"},
	{[]string{"float", "clear"}, "floats are not supported; use flex or grid"},
	{[]string{"object"}, "known object-fit and object-position keywords are supported (object-contain, object-cover, object-fill, object-none, object-scale-down, object-left-top, …); arbitrary positions call .ObjectPosition(…) on the img"},
	{[]string{"order"}, "order-* is not supported; reorder the children"},
	{[]string{"columns", "break-after", "break-before", "break-inside"}, "multi-column layout is not supported; use grid"},
	{[]string{"place-content", "place-items", "place-self", "justify-items", "justify-self"}, "use items-*, justify-*, content-* and self-* instead"},
	{[]string{"box-border", "box-content", "box-decoration"}, "box sizing is always border-box; drop it"},
	{[]string{"whitespace-break-spaces"}, "only whitespace-normal, whitespace-nowrap, whitespace-pre, whitespace-pre-line and whitespace-pre-wrap are supported (browser-core 38); break-spaces is not"},
	{[]string{"break-normal", "hyphens", "text-pretty"}, "only break-all, break-keep, break-words/wrap-break-word, wrap-anywhere and text-balance are supported (browser-core 43); break-normal is the default, hyphens and text-wrap: pretty are not"},
	{[]string{"antialiased", "subpixel-antialiased"}, "GPUI chooses text antialiasing itself; drop it"},
	{[]string{"list"}, "list styles are not supported"},
	{[]string{"appearance", "resize", "accent", "caret"}, "form-control styling is not supported; GPUI has no native form controls"},
	{[]string{"scroll", "snap", "touch", "will-change", "isolation", "isolate"}, "this scrolling or compositing utility is not supported (overflow-auto/scroll, scrollbar-gutter-stable and the overscroll-* classes are)"},
	{[]string{"sr-only", "not-sr-only"}, "sr-only is not supported; describe the element through its accessibility properties"},
	{[]string{"container", "prose"}, "container and prose are not supported; set max-w-* and padding explicitly"},
	{[]string{"fill", "stroke"}, "an svg element is painted with its text color: use text-<color>"},
	{[]string{"placeholder"}, "placeholder styling belongs to the text input component"},
	{[]string{"decoration", "underline-offset"}, "decoration styles are not classes; use underline with .TextDecorationColor(c) / text-decoration-wavy"},
	{[]string{"cursor"}, "unknown cursor (cursor-default, cursor-pointer, cursor-text, cursor-move, cursor-grab, cursor-grabbing, cursor-not-allowed, cursor-crosshair, the *-resize cursors, …)"},
}

// unsupportedClass explains the Tailwind utilities GPUI cannot express.
func unsupportedClass(_ *StyleBuilder, u string) (bool, string) {
	body := strings.TrimPrefix(u, "-")
	for _, entry := range unsupportedClasses {
		for _, p := range entry.prefixes {
			if body == p || strings.HasPrefix(body, p+"-") {
				return true, entry.reason
			}
		}
	}
	return false, ""
}

// colorProps are the class prefixes that take a color.
var colorProps = []struct {
	prefix string
	set    func(st *StyleRefinement, c Hsla)
}{
	{"bg-", func(st *StyleRefinement, c Hsla) { st.Background = Some(SolidBackground(c)) }},
	{"text-", func(st *StyleRefinement, c Hsla) { st.Text.Color = Some(c) }},
	{"border-", func(st *StyleRefinement, c Hsla) { st.BorderColor = Some(SolidBackground(c)) }},
	{"ring-", func(st *StyleRefinement, c Hsla) { st.RingColor = Some(RingColorOf(SolidBackground(c))) }},
	{"inset-ring-", func(st *StyleRefinement, c Hsla) { st.InsetRingColor = Some(RingColorOf(SolidBackground(c))) }},
	{"outline-", func(st *StyleRefinement, c Hsla) { st.OutlineColor = Some(RingColorOf(SolidBackground(c))) }},
}

// colorClass applies bg-, text-, border-, ring-, inset-ring- and
// outline- colors.
func colorClass(b *StyleBuilder, u string) (bool, string) {
	for _, p := range colorProps {
		value, ok := strings.CutPrefix(u, p.prefix)
		if !ok {
			continue
		}
		c, isColor, reason := parseColor(value)
		if !isColor {
			continue
		}
		if reason != "" {
			return true, reason
		}
		p.set(b.StyleMut(), c)
		return true, ""
	}
	if value, ok := strings.CutPrefix(u, "shadow-"); ok {
		if _, isColor, _ := parseColor(value); isColor {
			return true, "shadow colors are not supported; call .Shadow with colored gpui.BoxShadow values"
		}
	}
	return false, ""
}

// unknownClassReason gives an unknown class with a color prefix a hint;
// other unknown classes get none.
func unknownClassReason(u string) string {
	for _, p := range colorProps {
		if strings.HasPrefix(u, p.prefix) {
			return "unknown class: not a utility, and not a palette color (slate-800, white, black, transparent, [#1e293b]); theme colors are not classes, pass them to the method (.Bg(c), .TextColor(c), …)"
		}
	}
	return ""
}

// parseColor parses a class color: a palette name (slate-800), white,
// black, transparent or [#hex], with an optional opacity modifier (/50,
// /[0.35]). isColor is false when value is not color-shaped; a reason
// explains a color-shaped value that is not usable.
func parseColor(value string) (c Hsla, isColor bool, reason string) {
	name, modifier, hasModifier := cutModifier(value)
	switch {
	case name == "transparent":
		c = Rgb(0).Alpha(0)
	case name == "current" || name == "inherit":
		return Hsla{}, true, "currentColor is not supported; repeat the color (text color already cascades to children)"
	case strings.HasPrefix(name, "["):
		inner, ok := arbitrary(name)
		if !ok {
			return Hsla{}, false, ""
		}
		if !strings.HasPrefix(inner, "#") {
			return Hsla{}, true, "arbitrary colors take hex, like [#1e293b]"
		}
		parsed, err := ParseHex(inner)
		if err != nil {
			return Hsla{}, true, "arbitrary colors take hex, like [#1e293b]"
		}
		c = parsed
	default:
		hex, ok := tailwindPalette[name]
		if !ok {
			if family, _, ok := strings.Cut(name, "-"); ok && tailwindPalette[family+"-500"] != 0 {
				return Hsla{}, true, "unknown shade; palette shades are 50, 100–900 and 950"
			}
			return Hsla{}, false, ""
		}
		c = Rgb(hex)
	}
	if hasModifier {
		f, ok := percentValue(modifier)
		if !ok {
			return Hsla{}, true, "the opacity modifier takes 0–100, or [0.35]"
		}
		c = c.Opacity(f)
	}
	return c, true, ""
}

// cutModifier splits value at its last slash outside brackets.
func cutModifier(value string) (name, modifier string, ok bool) {
	depth := 0
	for i := len(value) - 1; i >= 0; i-- {
		switch value[i] {
		case ']':
			depth++
		case '[':
			depth--
		case '/':
			if depth == 0 {
				return value[:i], value[i+1:], true
			}
		}
	}
	return value, "", false
}

// ---------------------------------------------------------------------------
// Value parsing
// ---------------------------------------------------------------------------

// arbitrary returns the inside of a bracketed value: "[12px]" is "12px".
func arbitrary(v string) (string, bool) {
	if len(v) >= 2 && v[0] == '[' && v[len(v)-1] == ']' {
		return v[1 : len(v)-1], true
	}
	return "", false
}

// parseNumber parses a plain decimal number ("1", "-0.5", ".25"); no
// exponents, signs other than a leading minus, or special values.
func parseNumber(s string) (float32, bool) {
	body := strings.TrimPrefix(s, "-")
	digits, dot := 0, false
	for i := 0; i < len(body); i++ {
		switch c := body[i]; {
		case c >= '0' && c <= '9':
			digits++
		case c == '.' && !dot:
			dot = true
		default:
			return 0, false
		}
	}
	if digits == 0 {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 32)
	if err != nil {
		return 0, false
	}
	return float32(f), true
}

// parseCount parses an unsigned decimal integer.
func parseCount(s string) (uint64, bool) {
	if s == "" || s[0] < '0' || s[0] > '9' {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 10, 32)
	return n, err == nil
}

// spacingScale parses a value on the spacing scale: a non-negative
// multiple of 0.25 written as a plain number (13, 0.5, 2.75). Its length
// is n/4 rem.
func spacingScale(v string) (float32, bool) {
	if v == "" || v[0] == '-' || v[0] == '.' || v[len(v)-1] == '.' {
		return 0, false
	}
	n, ok := parseNumber(v)
	if !ok || float64(n*4) != math.Trunc(float64(n*4)) {
		return 0, false
	}
	return n, true
}

// percentValue parses an opacity: 0–100 (percent), [0.35] or [35%].
func percentValue(v string) (float32, bool) {
	if inner, ok := arbitrary(v); ok {
		if p, ok := strings.CutSuffix(inner, "%"); ok {
			n, ok := parseNumber(p)
			if !ok || n < 0 || n > 100 {
				return 0, false
			}
			return n / 100, true
		}
		n, ok := parseNumber(inner)
		if !ok || n < 0 || n > 1 {
			return 0, false
		}
		return n, true
	}
	n, ok := parseNumber(v)
	if !ok || n < 0 || n > 100 {
		return 0, false
	}
	return n / 100, true
}

// pixels parses a bracketed pixel amount: 0.5px, -1px or 0.
func pixels(s string) (float32, bool) {
	if s == "0" {
		return 0, true
	}
	v, ok := strings.CutSuffix(s, "px")
	if !ok {
		return 0, false
	}
	return parseNumber(v)
}
