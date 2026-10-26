package tsxgen

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"
)

// classSurface extracts the Tailwind class surface from the class
// layer's own tables and builds the Class type the .d.ts declares.
//
// Parsed from the engine's source (so they cannot drift):
//   - the method classes of gpui/class_generated.go's applyMethodClass
//     (the table cmd/tailwindgen generates from the pin), cross-checked
//     against its methodClassCount;
//   - specialClass's and scrollClass's literal cases and the accepted
//     variants of parseVariant (gpui/class.go);
//   - the length prefixes (lengthProps, with their definite/negative
//     flags), the color prefixes (colorProps), the grid prefixes, the
//     named scales (maxWidthNamed, blurScale, filterPercentScale,
//     hueRotateScale) and the palette
//     (gpui/tailwind_palette_generated.go).
//
// The value shapes of the prefix families (the spacing scale, bracket
// lengths, opacity, typography, border widths, gradients, filters) are
// the shapes the class parser's stages accept; the drift tests verify
// every declared arm against gpui.CheckClasses — the same check
// Styled.Class runs — and probe the engine with the whole Tailwind
// utility namespace, so a wrong or missing shape fails the tests.
func (e *extractor) classSurface(s *Surface) error {
	// The method classes: applyMethodClass's switch, cross-checked
	// against the count const beside it (both generated together).
	methods, err := e.switchCases("gpui/class_generated.go", "applyMethodClass", "class")
	if err != nil {
		return err
	}
	count, err := e.intConst("gpui/class_generated.go", "methodClassCount")
	if err != nil {
		return err
	}
	if count != len(methods) {
		return fmt.Errorf("tsxgen: applyMethodClass: %d classes, methodClassCount says %d", len(methods), count)
	}

	// The literal classes of the class layer's own stages.
	special, err := e.switchCases("gpui/class.go", "specialClass", "u")
	if err != nil {
		return err
	}
	scroll, err := e.switchCases("gpui/class.go", "scrollClass", "u")
	if err != nil {
		return err
	}

	// The variants the parser accepts (parseVariant's cases that
	// return without a reason) and the named group variants.
	variants, err := e.variants()
	if err != nil {
		return err
	}

	// The length prefixes: lengthProps' table (definite marks the
	// padding/gap prefixes that take no auto; negative the ones that
	// take a leading minus).
	lengths, err := e.lengthPrefixes()
	if err != nil {
		return err
	}

	// The color prefixes and the palette.
	colorPrefixes, err := e.colorPrefixes()
	if err != nil {
		return err
	}
	palette, err := e.palette()
	if err != nil {
		return err
	}

	// The named scales and the grid prefixes.
	maxW, err := e.mapKeyVar("gpui/class.go", "maxWidthNamed")
	if err != nil {
		return err
	}
	blur, err := e.mapKeyVar("gpui/class.go", "blurScale")
	if err != nil {
		return err
	}
	percents, err := e.mapKeyVar("gpui/class.go", "filterPercentScale")
	if err != nil {
		return err
	}
	hues, err := e.mapKeyVar("gpui/class.go", "hueRotateScale")
	if err != nil {
		return err
	}
	grid, err := e.gridPrefixes()
	if err != nil {
		return err
	}

	c := ClassSurface{}
	alias := func(name string, t TSType) {
		c.Aliases = append(c.Aliases, Alias{Name: name, Type: t})
	}

	// ---------------------------------------------------------------------------
	// The value scales (shared aliases)
	// ---------------------------------------------------------------------------

	// Spacing is the spacing scale: any non-negative multiple of 0.25
	// written as a number, plus the px and full keywords. The engine
	// accepts the unbounded scale; `${number}` types it without a cap
	// (it also matches exponent forms the engine rejects — the engine
	// rejects those at load; the type is the value space, not a gate).
	alias("Spacing", Union(
		Template([]string{"", ""}, numType),
		Lit("px"), Lit("full"),
	))
	// Fraction is a fraction value (1/2, 3/4, …): the engine accepts
	// any numerator/denominator pair.
	alias("Fraction", Template([]string{"", "/", ""}, numType, numType))
	// BracketLength is an arbitrary length in brackets.
	alias("BracketLength", Union(
		Template([]string{"[", "px]"}, numType),
		Template([]string{"[", "rem]"}, numType),
		Template([]string{"[", "%]"}, numType),
		Lit("[0]"),
	))
	// LengthValue is everything a length family accepts as its value.
	alias("LengthValue", Union(
		ref("Spacing"), ref("Fraction"), ref("BracketLength"),
	))
	// Opacity is an opacity value: a percentage (0–100) or an
	// arbitrary decimal in brackets.
	alias("Opacity", Union(
		Template([]string{"", ""}, numType),
		Template([]string{"[", "]"}, numType),
		Template([]string{"[", "%]"}, numType),
	))
	// ColorValue is a class color: a palette name, transparent, or an
	// arbitrary hex in brackets (the engine validates the hex shape).
	colorValue := Lits(append(append([]string(nil), palette...), "transparent")...)
	alias("PaletteColor", colorValue)
	alias("ColorValue", Union(ref("PaletteColor"), Template([]string{"[#", "]"}, strType)))

	// ---------------------------------------------------------------------------
	// The prefix families
	// ---------------------------------------------------------------------------

	var pos, neg, auto []string
	for _, p := range lengths {
		pos = append(pos, p.prefix)
		if p.negative {
			neg = append(neg, p.prefix)
		}
		if !p.definite {
			auto = append(auto, p.prefix)
		}
	}
	sort.Strings(pos)
	sort.Strings(neg)
	sort.Strings(auto)
	alias("PosPrefix", Lits(pos...))
	alias("NegPrefix", Lits(neg...))
	alias("AutoPrefix", Lits(auto...))

	sort.Strings(colorPrefixes)
	alias("ColorPrefix", Lits(colorPrefixes...))

	var arms []TSType
	// The literal classes the engine's tables name.
	arms = append(arms, Lits(append(append(methods, special...), scroll...)...))
	// group and group/name.
	arms = append(arms, Lit("group"), Template([]string{"group/", ""}, strType))
	// Length families: positive, negative, auto; max-w/max-h none; the
	// named max-width scale.
	arms = append(arms,
		Template([]string{"", "-", ""}, ref("PosPrefix"), ref("LengthValue")),
		Template([]string{"-", "-", ""}, ref("NegPrefix"), ref("LengthValue")),
		Template([]string{"", "-auto"}, ref("AutoPrefix")),
		Lit("max-w-none"), Lit("max-h-none"),
		prefixLits("max-w-", maxW),
	)
	// Colors: the palette, transparent and arbitrary hex, with the
	// opacity modifier.
	arms = append(arms,
		Template([]string{"", "", ""}, ref("ColorPrefix"), ref("ColorValue")),
		Template([]string{"", "", "/", ""}, ref("ColorPrefix"), ref("ColorValue"), numType),
	)
	// Opacity, grid counts, aspect ratios.
	arms = append(arms, Template([]string{"opacity-", ""}, ref("Opacity")))
	for _, g := range grid {
		arms = append(arms, Template([]string{g, ""}, numType))
	}
	// The arbitrary grid track lists (browser-core 42):
	// grid-cols-[240px 1fr] and its common spellings, typed with numeric
	// holes so the drift tests' samples stay engine-valid (the sampler
	// fills ${string} holes with hex-like strings, which a track list
	// rejects). The full track-list grammar — auto, the content
	// keywords, minmax(...) — plus the named-area classes
	// (grid-areas-["head_head"_"side_main"], grid-area-[head]) stay
	// load-checked only, the composed forms' rule: the engine accepts
	// them at class time, the d.ts does not advertise them.
	arms = append(arms,
		Template([]string{"grid-cols-[", "px]"}, numType),
		Template([]string{"grid-cols-[", "fr]"}, numType),
		Template([]string{"grid-cols-[", "px_", "fr]"}, numType, numType),
		Template([]string{"grid-rows-[", "px]"}, numType),
		Template([]string{"grid-rows-[", "fr]"}, numType),
		Template([]string{"grid-rows-[", "px_", "fr]"}, numType, numType),
	)
	arms = append(arms,
		Template([]string{"aspect-[", "]"}, numType),
		Template([]string{"aspect-[", "/", "]"}, numType, numType),
	)
	// The class parser's stage families: typography, border widths,
	// radii and rings, filters, gradients and blur.
	arms = append(arms, stageArms(blur, percents, hues)...)

	// The variants wrap every non-variant class (CheckClasses accepts
	// them everywhere; the interactive ones need a div at run time).
	// The important modifier (! before or after) is accepted too.
	alias("Variant", Union(
		Lits(variants...),
		Template([]string{"group-hover/", ""}, strType),
		Template([]string{"group-active/", ""}, strType),
	))
	core := Union(arms...)
	alias("CoreClass", core)
	// The variant and important compositions stay lazy (${string}
	// tails): distributing CoreClass under every variant is a union
	// tsc cannot check in useful time (measured: minutes), and the
	// composed forms are load-checked anyway — the engine strips the
	// variant and the ! and validates the class left, exactly as it
	// validates a space-separated className. The drift tests probe the
	// compositions against CheckClasses directly.
	c.Class = Union(
		core,
		Template([]string{"", ":", ""}, ref("Variant"), strType),
		Template([]string{"!", ""}, strType),
		Template([]string{"", "!"}, strType),
	)
	s.Classes = c
	return nil
}

// prefixLits builds the `max-w-`-style literal classes of a named
// scale.
func prefixLits(prefix string, names []string) TSType {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = prefix + n
	}
	return Lits(out...)
}

// stageArms declares the class parser's stage families whose shapes
// are code rather than a table: typography sizes and weights, leading,
// tracking, line clamps, border widths and radii, ring widths, the
// filter families, gradients and blur. Every arm here is verified
// against gpui.CheckClasses by the drift tests (and the candidates
// probe catches one the engine accepts but these lack).
func stageArms(blur, percents, hues []string) []TSType {
	var arms []TSType
	// Typography: sizes, weights and the named font stacks. A bare
	// bracketed zero is a length too (absoluteLength's "0").
	arms = append(arms,
		Template([]string{"text-[", "px]"}, numType),
		Template([]string{"text-[", "rem]"}, numType),
		Lit("text-[0]"),
		Template([]string{"font-[", "]"}, numType),
		Lit("font-sans"), Lit("font-serif"), Lit("font-mono"),
	)
	// The font-variant-numeric utilities (one OpenType feature each).
	arms = append(arms, Lits("normal-nums", "tabular-nums", "proportional-nums",
		"lining-nums", "oldstyle-nums", "slashed-zero", "ordinal",
		"diagonal-fractions", "stacked-fractions"))
	// Leading and tracking with arbitrary values; line clamps. The
	// leading scale is the plain number scale (leading-13): the px and
	// full keywords and the fractions are not leading values. A
	// bracketed `normal` is CSS's own keyword (browser-parity D1 — the
	// font-metrics line height), distinct from Tailwind's named
	// leading-normal (1.5).
	arms = append(arms,
		Template([]string{"leading-", ""}, numType),
		Template([]string{"leading-[", "]"}, numType),
		Template([]string{"leading-[", "px]"}, numType),
		Template([]string{"leading-[", "rem]"}, numType),
		Template([]string{"leading-[", "%]"}, numType),
		Lit("leading-[0]"),
		Lit("leading-[normal]"),
		Template([]string{"tracking-[", "px]"}, numType),
		Lit("tracking-[0]"),
		// The named tracking scale is em-based (browser-core 38) and
		// resolves against the element's own font size.
		Lits("tracking-tighter", "tracking-tight", "tracking-normal",
			"tracking-wide", "tracking-wider", "tracking-widest"),
		Template([]string{"line-clamp-", ""}, numType),
	)
	// Border widths: any side, any count or bracket length (a bare
	// bracketed zero is one).
	sides := []string{"t", "r", "b", "l", "x", "y"}
	arms = append(arms, Template([]string{"border-", ""}, numType),
		Template([]string{"border-[", "px]"}, numType),
		Template([]string{"border-[", "rem]"}, numType),
		Lit("border-[0]"))
	for _, side := range sides {
		arms = append(arms,
			Template([]string{"border-" + side + "-", ""}, numType),
			Template([]string{"border-" + side + "-[", "px]"}, numType),
			Template([]string{"border-" + side + "-[", "rem]"}, numType),
			Lit("border-"+side+"-[0]"),
		)
	}
	// Radii: any corner with a bracket length.
	corners := []string{"", "t", "r", "b", "l", "tl", "tr", "br", "bl"}
	for _, corner := range corners {
		sep := "-"
		if corner == "" {
			sep = ""
		}
		arms = append(arms,
			Template([]string{"rounded" + sep + corner + "-[", "px]"}, numType),
			Template([]string{"rounded" + sep + corner + "-[", "rem]"}, numType),
			Lit("rounded"+sep+corner+"-[0]"),
		)
	}
	// Ring widths are pixels (the engine rejects rem rings).
	arms = append(arms,
		Template([]string{"ring-", ""}, numType),
		Template([]string{"ring-[", "px]"}, numType),
		Lit("ring-[0]"),
		Template([]string{"inset-ring-", ""}, numType),
		Template([]string{"inset-ring-[", "px]"}, numType),
		Lit("inset-ring-[0]"),
	)
	// Outline widths and offsets are pixels (like ring widths; the
	// engine rejects rem outlines). The bare outline, outline-none and
	// outline-dashed literals come from the method/special tables.
	arms = append(arms,
		Template([]string{"outline-", ""}, numType),
		Template([]string{"outline-[", "px]"}, numType),
		Lit("outline-[0]"),
		Template([]string{"outline-offset-", ""}, numType),
		Template([]string{"outline-offset-[", "px]"}, numType),
		Lit("outline-offset-[0]"),
	)
	// The monochrome filters: bare, off, or an arbitrary amount, each
	// with a negative form.
	mono := Lits("grayscale", "sepia", "invert")
	tail := Union(
		Lit(""), Lit("-0"), Lit("-none"),
		Template([]string{"-[", "]"}, numType),
		Template([]string{"-[", "%]"}, numType),
	)
	arms = append(arms,
		Template([]string{"", "", ""}, mono, tail),
		Template([]string{"-", "", ""}, mono, tail),
	)
	// The scaled colour filters and hue-rotate (negative allowed).
	scaled := Lits("brightness", "contrast", "saturate")
	arms = append(arms,
		Template([]string{"", "-", ""}, scaled, Lits(percents...)),
		Template([]string{"-", "-", ""}, scaled, Lits(percents...)),
		Template([]string{"", "-[", "]"}, scaled, numType),
		Template([]string{"", "-[", "%]"}, scaled, numType),
		Template([]string{"-", "-[", "]"}, scaled, numType),
		Template([]string{"-", "-[", "%]"}, scaled, numType),
	)
	arms = append(arms,
		Template([]string{"hue-rotate-", ""}, Lits(hues...)),
		Template([]string{"-hue-rotate-", ""}, Lits(hues...)),
		Template([]string{"hue-rotate-[", "]"}, numType),
		Template([]string{"hue-rotate-[", "deg]"}, numType),
		Template([]string{"-hue-rotate-[", "]"}, numType),
		Template([]string{"-hue-rotate-[", "deg]"}, numType),
	)
	// Gradients: linear directions, angles and their interpolation
	// modifier. The from-/to- stops are not single classes (the engine
	// requires a bg-linear-… earlier in the same class string), so they
	// are not Class arms; the engine checks them inside a class list.
	arms = append(arms,
		Lits("bg-linear-to-t", "bg-linear-to-tr", "bg-linear-to-r", "bg-linear-to-br",
			"bg-linear-to-b", "bg-linear-to-bl", "bg-linear-to-l", "bg-linear-to-tl"),
		Template([]string{"bg-linear-", ""}, numType),
		Template([]string{"-bg-linear-", ""}, numType),
		Template([]string{"bg-linear-", "/srgb"}, numType),
		Template([]string{"bg-linear-[", "deg]"}, numType),
		Template([]string{"bg-linear-[", "rad]"}, numType),
		Template([]string{"bg-linear-[", "grad]"}, numType),
		Template([]string{"bg-linear-[", "turn]"}, numType),
		Template([]string{"-bg-linear-[", "deg]"}, numType),
		Template([]string{"-bg-linear-[", "rad]"}, numType),
		Template([]string{"-bg-linear-[", "grad]"}, numType),
		Template([]string{"-bg-linear-[", "turn]"}, numType),
	)
	// Blur and backdrop blur: the scale, none and arbitrary pixels.
	blurArms := blurScaleArms(blur, "blur")
	arms = append(arms, blurArms...)
	arms = append(arms, blurScaleArms(blur, "backdrop-blur")...)
	return arms
}

// blurScaleArms renders one blur family's arms (the parsed scale, none
// and arbitrary pixels, with the bare bracketed zero).
func blurScaleArms(scale []string, prefix string) []TSType {
	var lits []string
	for _, s := range scale {
		if s == "" {
			lits = append(lits, prefix)
			continue
		}
		lits = append(lits, prefix+"-"+s)
	}
	return []TSType{
		Lits(lits...),
		Lit(prefix + "-none"),
		Template([]string{prefix + "-[", "px]"}, numType),
		Lit(prefix + "-[0]"),
	}
}

// lengthPrefixEntry is one lengthProps prefix.
type lengthPrefixEntry struct {
	prefix   string
	definite bool
	negative bool
}

// switchCases returns the string cases of one function's switch.
func (e *extractor) switchCases(path, fn, tag string) ([]string, error) {
	decl, err := e.funcDecl(path, fn)
	if err != nil {
		return nil, err
	}
	switches := findSwitch(decl, tag, false)
	if len(switches) != 1 {
		return nil, fmt.Errorf("tsxgen: %s: expected one switch tagged %q, found %d", fn, tag, len(switches))
	}
	return stringCases(switches[0], fn)
}

// variants returns the state variants the class parser accepts.
func (e *extractor) variants() (variants []string, err error) {
	decl, err := e.funcDecl("gpui/class.go", "parseVariant")
	if err != nil {
		return nil, err
	}
	switches := findSwitch(decl, "v", false)
	if len(switches) != 1 {
		return nil, fmt.Errorf("tsxgen: parseVariant: expected one switch tagged v, found %d", len(switches))
	}
	for _, stmt := range switches[0].Body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok || cc.List == nil {
			continue
		}
		if len(cc.Body) == 1 && isEmptyReturn3(cc.Body[0]) {
			for _, expr := range cc.List {
				if s, ok := stringLit(expr); ok {
					variants = append(variants, s)
				}
			}
		}
	}
	for _, g := range prefixLiterals(decl) {
		if !strings.HasPrefix(g, "group-hover/") && !strings.HasPrefix(g, "group-active/") {
			return nil, fmt.Errorf("tsxgen: parseVariant: unexpected CutPrefix %q", g)
		}
	}
	if len(variants) == 0 {
		return nil, fmt.Errorf("tsxgen: parseVariant: no accepted variants found")
	}
	return variants, nil
}

// lengthPrefixes returns lengthProps' entries.
func (e *extractor) lengthPrefixes() ([]lengthPrefixEntry, error) {
	spec, err := e.varDecl("gpui/class.go", "lengthProps")
	if err != nil {
		return nil, err
	}
	structs, err := structLits(spec.Values[0], "lengthProps")
	if err != nil {
		return nil, err
	}
	var out []lengthPrefixEntry
	for _, sc := range structs {
		prefix, _, err := fieldValue(sc, "prefix")
		if err != nil {
			return nil, err
		}
		if prefix == "" {
			return nil, fmt.Errorf("tsxgen: lengthProps: an entry without a prefix")
		}
		_, definite, err := fieldValue(sc, "definite")
		if err != nil {
			return nil, err
		}
		_, negative, err := fieldValue(sc, "negative")
		if err != nil {
			return nil, err
		}
		out = append(out, lengthPrefixEntry{prefix, definite, negative})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("tsxgen: lengthProps: no prefixes found")
	}
	return out, nil
}

// colorPrefixes returns colorProps' prefixes.
func (e *extractor) colorPrefixes() ([]string, error) {
	spec, err := e.varDecl("gpui/class.go", "colorProps")
	if err != nil {
		return nil, err
	}
	structs, err := structLits(spec.Values[0], "colorProps")
	if err != nil {
		return nil, err
	}
	// The entries are positional: {prefix, setter}.
	var out []string
	for _, sc := range structs {
		if len(sc.Elts) == 0 {
			return nil, fmt.Errorf("tsxgen: colorProps: an empty entry")
		}
		prefix, ok := stringLit(sc.Elts[0])
		if !ok || prefix == "" {
			return nil, fmt.Errorf("tsxgen: colorProps: an entry without a prefix")
		}
		out = append(out, prefix)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("tsxgen: colorProps: no prefixes found")
	}
	return out, nil
}

// palette returns the palette's color names.
func (e *extractor) palette() ([]string, error) {
	spec, err := e.varDecl("gpui/tailwind_palette_generated.go", "tailwindPalette")
	if err != nil {
		return nil, err
	}
	return mapKeys(spec.Values[0], "tailwindPalette")
}

// mapKeyVar returns a string-keyed map variable's keys.
func (e *extractor) mapKeyVar(path, name string) ([]string, error) {
	spec, err := e.varDecl(path, name)
	if err != nil {
		return nil, err
	}
	return mapKeys(spec.Values[0], name)
}

// gridPrefixes returns gridClass's prefix table (positional strings).
func (e *extractor) gridPrefixes() ([]string, error) {
	decl, err := e.funcDecl("gpui/class.go", "gridClass")
	if err != nil {
		return nil, err
	}
	var prefixes []string
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || len(lit.Elts) == 0 {
			return true
		}
		// The prefix table's entries are [string, func] composites.
		if s, ok := stringLit(lit.Elts[0]); ok {
			if _, isFunc := lit.Elts[1].(*ast.FuncLit); isFunc {
				prefixes = append(prefixes, s)
			}
		}
		return true
	})
	if len(prefixes) == 0 {
		return nil, fmt.Errorf("tsxgen: gridClass: no prefixes found")
	}
	return prefixes, nil
}
