package tsxgen

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// TSType is a TypeScript type the generated .d.ts declares, in the
// restricted grammar the generator emits and ParseDTS reads back:
// a union of string literals, number, string, boolean, numeric
// literals, template literal types and references to the aliases the
// .d.ts declares (PosPrefix, LengthValue, …).
//
//	render → the .d.ts text      parse ← the .d.ts text
//	match  → does a class string satisfy the type (refs resolved)
type TSType struct {
	// Literals are the union's string literals, sorted.
	Literals []string
	// NumLits are numeric literals (the style flex: 1).
	NumLits []string
	// Number, Str and Bool are the number, string and boolean keywords.
	Number bool
	Str    bool
	Bool   bool
	// Templates are the template literal types.
	Templates []TSTemplate
	// Refs are names of aliases the type references.
	Refs []string
}

// TSTemplate is one template literal type: static parts with a hole
// type between each pair (len(Parts) == len(Holes)+1).
type TSTemplate struct {
	Parts []string
	Holes []TSType
}

// numType is the `number` keyword.
var numType = TSType{Number: true}

// strType is the `string` keyword.
var strType = TSType{Str: true}

// boolType is the `boolean` keyword.
var boolType = TSType{Bool: true}

// none is the empty type (a union with no arms).
var none TSType

// Union builds the union of the given types.
func Union(types ...TSType) TSType {
	var out TSType
	for _, t := range types {
		out.Literals = append(out.Literals, t.Literals...)
		out.NumLits = append(out.NumLits, t.NumLits...)
		out.Number = out.Number || t.Number
		out.Str = out.Str || t.Str
		out.Bool = out.Bool || t.Bool
		out.Templates = append(out.Templates, t.Templates...)
		out.Refs = append(out.Refs, t.Refs...)
	}
	out.normalize()
	return out
}

// Lit builds a type of one string literal.
func Lit(s string) TSType { return TSType{Literals: []string{s}} }

// Lits builds a type of string literals (order kept: enums render in
// the engine's source order).
func Lits(ss ...string) TSType { return TSType{Literals: ss} }

// NumLit builds a type of one numeric literal (flex: 1).
func NumLit(s string) TSType { return TSType{NumLits: []string{s}} }

// Template builds `p0${h0}p1${h1}...` with len(holes) == len(parts)-1.
func Template(parts []string, holes ...TSType) TSType {
	if len(parts) != len(holes)+1 {
		panic(fmt.Sprintf("tsxgen: template with %d parts and %d holes", len(parts), len(holes)))
	}
	return TSType{Templates: []TSTemplate{{Parts: parts, Holes: holes}}}
}

// ref builds a reference to an alias the .d.ts declares.
func ref(name string) TSType { return TSType{Refs: []string{name}} }

// normalize sorts and dedupes the union arms, keeping the canonical
// order the .d.ts renders and the drift tests compare.
func (t *TSType) normalize() {
	t.Literals = dedupe(t.Literals)
	sort.Strings(t.Literals)
	t.NumLits = dedupe(t.NumLits)
	sort.Strings(t.NumLits)
	t.Refs = dedupe(t.Refs)
	sort.Strings(t.Refs)
	// Templates keep their order (families read in the engine's order).
	seen := map[string]bool{}
	var keep []TSTemplate
	for _, tmpl := range t.Templates {
		key := tmpl.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		keep = append(keep, tmpl)
	}
	t.Templates = keep
}

func dedupe(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// IsEmpty reports whether the type has no arms.
func (t TSType) IsEmpty() bool {
	return len(t.Literals) == 0 && len(t.NumLits) == 0 && !t.Number && !t.Str && !t.Bool &&
		len(t.Templates) == 0 && len(t.Refs) == 0
}

// Render writes the type as canonical .d.ts text. String literals are
// quoted, numeric literals are not (flex: 1), and template literal
// types render with their holes' types inline.
func (t TSType) Render() string {
	var arms []string
	for _, lit := range t.Literals {
		arms = append(arms, strconv.Quote(lit))
	}
	arms = append(arms, t.NumLits...)
	arms = append(arms, t.Refs...)
	if t.Number {
		arms = append(arms, "number")
	}
	if t.Str {
		arms = append(arms, "string")
	}
	if t.Bool {
		arms = append(arms, "boolean")
	}
	for _, tmpl := range t.Templates {
		arms = append(arms, tmpl.String())
	}
	if len(arms) == 0 {
		return "never"
	}
	return strings.Join(arms, " | ")
}

// String renders the template as backtick text: `p0${h0}p1${h1}`.
func (tmpl TSTemplate) String() string {
	var b strings.Builder
	b.WriteString("`")
	for i, hole := range tmpl.Holes {
		b.WriteString(escapeTemplate(tmpl.Parts[i]))
		b.WriteString("${")
		b.WriteString(hole.Render())
		b.WriteString("}")
	}
	b.WriteString(escapeTemplate(tmpl.Parts[len(tmpl.Parts)-1]))
	b.WriteString("`")
	return b.String()
}

// escapeTemplate escapes the backticks and ${ a static part would
// otherwise close.
func escapeTemplate(s string) string {
	s = strings.ReplaceAll(s, "`", "\\`")
	return strings.ReplaceAll(s, "${", "\\${")
}

// Resolve returns the type with its references replaced by the aliases
// they name (matching and sampling work on the resolved type).
func (t TSType) Resolve(aliases map[string]TSType) TSType {
	out := t
	out.Literals = append([]string(nil), t.Literals...)
	out.NumLits = append([]string(nil), t.NumLits...)
	out.Refs = nil
	for _, name := range t.Refs {
		alias, ok := aliases[name]
		if !ok {
			// An unknown reference is a generator bug; matching treats it
			// as never, and the tests fail on the missing alias.
			continue
		}
		resolved := alias.Resolve(aliases)
		out.Literals = append(out.Literals, resolved.Literals...)
		out.NumLits = append(out.NumLits, resolved.NumLits...)
		out.Number = out.Number || resolved.Number
		out.Str = out.Str || resolved.Str
		out.Bool = out.Bool || resolved.Bool
		out.Templates = append(out.Templates, resolved.Templates...)
		out.Refs = append(out.Refs, resolved.Refs...)
	}
	out.Templates = resolveTemplates(out.Templates, aliases)
	out.normalize()
	return out
}

func resolveTemplates(templates []TSTemplate, aliases map[string]TSType) []TSTemplate {
	var out []TSTemplate
	for _, tmpl := range templates {
		var holes []TSType
		for _, hole := range tmpl.Holes {
			// Every hole resolves fully, recursively: an alias may
			// reference aliases (LengthValue is three of them), and
			// matching works on concrete literals, keywords and
			// templates only.
			holes = append(holes, hole.Resolve(aliases))
		}
		out = append(out, TSTemplate{Parts: tmpl.Parts, Holes: holes})
	}
	return out
}

// Matches reports whether the string s satisfies the type, with the
// aliases' bodies substituted for references (the drift tests'
// .d.ts-side class check: does the declared type accept this class?).
func (t TSType) Matches(s string, aliases map[string]TSType) bool {
	r := t.Resolve(aliases)
	for _, lit := range r.Literals {
		if s == lit {
			return true
		}
	}
	if r.Number && isNumericString(s) {
		return true
	}
	if r.Str {
		return true
	}
	if r.Bool {
		return s == "true" || s == "false"
	}
	for _, tmpl := range r.Templates {
		if matchTemplate(s, tmpl.Parts, tmpl.Holes) {
			return true
		}
	}
	return false
}

// matchTemplate tries to match s against parts[i:] with holes[i:].
func matchTemplate(s string, parts []string, holes []TSType) bool {
	if len(holes) == 0 {
		return s == parts[0]
	}
	prefix := parts[0]
	if prefix != "" && !strings.HasPrefix(s, prefix) {
		return false
	}
	rest := s[len(prefix):]
	// Try every end position of the first hole's text (the hole may
	// match a variable-length run, so this backtracks).
	for end := 0; end <= len(rest); end++ {
		if !matchHole(holes[0], rest[:end]) {
			continue
		}
		if matchTemplate(rest[end:], parts[1:], holes[1:]) {
			return true
		}
	}
	return false
}

// matchHole reports whether one hole (already resolved to literals,
// number, string or templates) accepts text.
func matchHole(hole TSType, text string) bool {
	for _, lit := range hole.Literals {
		if text == lit {
			return true
		}
	}
	if hole.Number && isNumericString(text) {
		return true
	}
	if hole.Str {
		return true
	}
	for _, tmpl := range hole.Templates {
		if matchTemplate(text, tmpl.Parts, tmpl.Holes) {
			return true
		}
	}
	return false
}

// isNumericString reports whether s is a plain decimal number string
// (the form `${number}` in a template hole matches).
func isNumericString(s string) bool {
	if s == "" {
		return false
	}
	digits, dots := 0, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			digits++
		case c == '.' && i > 0:
			dots++
			if dots > 1 {
				return false
			}
		default:
			return false
		}
	}
	return digits > 0 && dots <= 1
}

// Equals reports whether two types render the same (the drift tests'
// comparison: the canonical text is the surface).
func (t TSType) Equals(other TSType) bool {
	return t.Render() == other.Render()
}

// Samples returns representative strings that satisfy the type (the
// drift tests probe the engine with them: everything the types accept,
// the engine must too). Each hole contributes up to cap samples so the
// cartesian product stays small.
func (t TSType) Samples(aliases map[string]TSType) []string {
	r := t.Resolve(aliases)
	var out []string
	out = append(out, r.Literals...)
	for _, tmpl := range r.Templates {
		out = append(out, sampleTemplate(tmpl)...)
	}
	if r.Number {
		out = append(out, "8")
	}
	// string, boolean and numeric literals contribute no class samples.
	return dedupe(out)
}

const sampleCap = 4

// sampleTemplate returns representative strings matching the template.
func sampleTemplate(tmpl TSTemplate) []string {
	out := []string{tmpl.Parts[0]}
	for i, hole := range tmpl.Holes {
		holeSamples := sampleHole(hole)
		var next []string
		for _, prefix := range out {
			for _, sample := range holeSamples {
				next = append(next, prefix+sample+tmpl.Parts[i+1])
			}
		}
		out = next
		if len(out) > 64 {
			out = out[:64]
		}
		if len(out) == 0 {
			return nil
		}
	}
	return out
}

// sampleHole returns a few strings one hole accepts.
func sampleHole(hole TSType) []string {
	var out []string
	out = append(out, hole.Literals[:min(sampleCap, len(hole.Literals))]...)
	for _, tmpl := range hole.Templates {
		for _, s := range sampleTemplate(tmpl) {
			if len(out) >= sampleCap+2 {
				break
			}
			out = append(out, s)
		}
	}
	if hole.Number {
		out = append(out, "1")
	}
	if hole.Str {
		out = append(out, "0f172a")
	}
	return dedupe(out)
}

// GoValues returns sample Go values for the type (the drift tests feed
// parseStyle the declared style values: everything the types accept,
// the engine must accept).
func (t TSType) GoValues(aliases map[string]TSType) []any {
	var out []any
	for _, s := range t.Samples(aliases) {
		if isNumericString(s) {
			out = append(out, mustFloat(s))
			continue
		}
		out = append(out, s)
	}
	for _, n := range t.NumLits {
		out = append(out, mustFloat(n))
	}
	if t.Number {
		out = append(out, 8)
	}
	if t.Bool {
		out = append(out, true)
	}
	return out
}

func mustFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(fmt.Sprintf("tsxgen: %q is not numeric: %v", s, err))
	}
	return f
}
