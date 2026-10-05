package gpui

// This file ports the key-context layer of the pinned CE reference
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/keymap/context.rs — KeyContext and ContextEntry
//     (parse "Identifier key=value" sequences, first-entry-wins add/set,
//     extend, primary/secondary), and KeyBindingContextPredicate (the
//     `>`, `||`, `&&`, `==`, `!=` and `!` operator language with the
//     same precedence ladder: child(1) < or(2) < and(3) < eq(4) <
//     not(5), identifier characters and vim operator characters,
//     eval/depth_of/is_superset semantics).
//
// Go adaptations are recorded beside the behavior they alter: Option<
// SharedString> value entries become plain strings ("" is never a valid
// value because identifiers cannot be empty), and parse errors are
// typed errors instead of anyhow chains.

import (
	"fmt"
	"strings"
	"unicode"
)

// ---------------------------------------------------------------------------
// KeyContext (context.rs KeyContext)
// ---------------------------------------------------------------------------

// ContextEntry is one entry of a KeyContext: a plain identifier
// (Value == "", no value) or a key-value pair.
type ContextEntry struct {
	// Key is the key (or the identifier when Value is empty).
	Key string
	// Value is the value, or "" when this entry is an identifier.
	Value string
}

// KeyContext resolves whether an action should be dispatched at a point
// in the element tree: a set of identifiers and key-value pairs
// (context.rs KeyContext). The zero value is the empty context.
type KeyContext struct {
	entries []ContextEntry
}

// NewKeyContextWithDefaults returns a context containing an "os" key
// set to "windows" (context.rs new_with_defaults, the Windows branch of
// the pinned source; the port targets Windows).
func NewKeyContextWithDefaults() KeyContext {
	var c KeyContext
	c.Set("os", "windows")
	return c
}

// ParseKeyContext parses a key context from the reference's string
// format: identifiers and key=value pairs separated by whitespace
// ("StatusBar mode = visible"). Like the reference parser, duplicate
// keys keep their first entry.
func ParseKeyContext(source string) (KeyContext, error) {
	var c KeyContext
	rest := skipContextWhitespace(source)
	if err := c.parseExpr(rest); err != nil {
		return KeyContext{}, err
	}
	return c, nil
}

// MustParseKeyContext parses a key context, panicking on failure (the
// test spelling).
func MustParseKeyContext(source string) KeyContext {
	c, err := ParseKeyContext(source)
	if err != nil {
		panic(err)
	}
	return c
}

func (c *KeyContext) parseExpr(source string) error {
	if source == "" {
		return nil
	}
	key := takeContextIdentifier(source)
	source = skipContextWhitespace(source[len(key):])
	if strings.HasPrefix(source, "=") {
		source = skipContextWhitespace(source[1:])
		value := takeContextIdentifier(source)
		source = skipContextWhitespace(source[len(value):])
		if value == "" {
			return fmt.Errorf("gpui: key context: missing value after %q=", key)
		}
		c.Set(key, value)
	} else {
		if key == "" {
			return fmt.Errorf("gpui: key context: expected an identifier or key=value pair near %q", source)
		}
		c.Add(key)
	}
	return c.parseExpr(source)
}

func takeContextIdentifier(source string) string {
	for i, r := range source {
		if !isContextIdentifierRune(r) {
			return source[:i]
		}
	}
	return source
}

func isContextIdentifierRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

// Primary returns the primary context entry (usually the component
// name): the first identifier entry.
func (c KeyContext) Primary() (ContextEntry, bool) {
	for i := range c.entries {
		if c.entries[i].Value == "" {
			return c.entries[i], true
		}
	}
	return ContextEntry{}, false
}

// Secondary returns everything except the primary entry, in order.
func (c KeyContext) Secondary() []ContextEntry {
	primary, has := c.Primary()
	out := make([]ContextEntry, 0, len(c.entries))
	for _, entry := range c.entries {
		if has && entry == primary {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// IsEmpty reports whether this context has no entries.
func (c KeyContext) IsEmpty() bool { return len(c.entries) == 0 }

// Extend extends this context with another context's entries, keeping
// this context's entries on key conflicts (context.rs extend).
func (c *KeyContext) Extend(other KeyContext) {
	for _, entry := range other.entries {
		if !c.Contains(entry.Key) {
			c.entries = append(c.entries, entry)
		}
	}
}

// Add adds an identifier to this context unless it is already present
// (first-entry-wins).
func (c *KeyContext) Add(identifier string) {
	if !c.Contains(identifier) {
		c.entries = append(c.entries, ContextEntry{Key: identifier})
	}
}

// Set sets a key-value pair unless the key is already present
// (first-entry-wins).
func (c *KeyContext) Set(key, value string) {
	if !c.Contains(key) {
		c.entries = append(c.entries, ContextEntry{Key: key, Value: value})
	}
}

// Contains reports whether this context contains a given identifier or
// key.
func (c KeyContext) Contains(key string) bool {
	for i := range c.entries {
		if c.entries[i].Key == key {
			return true
		}
	}
	return false
}

// Get returns the value for a key, if set.
func (c KeyContext) Get(key string) (string, bool) {
	for i := range c.entries {
		if c.entries[i].Key == key {
			if c.entries[i].Value == "" {
				return "", false
			}
			return c.entries[i].Value, true
		}
	}
	return "", false
}

// Entries returns a copy of the entries in insertion order.
func (c KeyContext) Entries() []ContextEntry {
	return append([]ContextEntry{}, c.entries...)
}

// String renders the context in its parse-compatible spelling
// (context.rs Debug).
func (c KeyContext) String() string {
	var b strings.Builder
	for i, entry := range c.entries {
		if i > 0 {
			b.WriteByte(' ')
		}
		if entry.Value != "" {
			fmt.Fprintf(&b, "%s=%s", entry.Key, entry.Value)
		} else {
			b.WriteString(entry.Key)
		}
	}
	return b.String()
}

// Equal reports whether two contexts carry the same entries in the
// same order (the derived PartialEq).
func (c KeyContext) Equal(other KeyContext) bool {
	if len(c.entries) != len(other.entries) {
		return false
	}
	for i := range c.entries {
		if c.entries[i] != other.entries[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// KeyBindingContextPredicate (context.rs KeyBindingContextPredicate)
// ---------------------------------------------------------------------------

// PredicateKind discriminates KeyBindingContextPredicate variants.
type PredicateKind uint8

const (
	// PredicateIdentifier matches a context containing an identifier.
	PredicateIdentifier PredicateKind = iota
	// PredicateEqual matches a context with key == value.
	PredicateEqual
	// PredicateNotEqual matches a key present with value != given, or
	// the key absent.
	PredicateNotEqual
	// PredicateDescendant matches a predicate below another predicate
	// in the element tree ("parent > child").
	PredicateDescendant
	// PredicateNot inverts another predicate.
	PredicateNot
	// PredicateAnd matches when both children match.
	PredicateAnd
	// PredicateOr matches when either child matches.
	PredicateOr
)

// KeyBindingContextPredicate is the small language describing which
// contexts correspond to which actions (context.rs
// KeyBindingContextPredicate).
type KeyBindingContextPredicate struct {
	// Kind is the variant.
	Kind PredicateKind
	// Identifier/Key is the identifier (Identifier), the left side of a
	// comparison, or the operator character of a vim-operator
	// identifier.
	Identifier string
	// Value is the right side of a comparison.
	Value string
	// Children holds the sub-predicates (left, right) or the single
	// wrapped predicate (Not), in that order.
	Children []*KeyBindingContextPredicate
}

// predicate precedences (context.rs consts): child < or < and < eq < not.
const (
	predicatePrecedenceChild = 1
	predicatePrecedenceOr    = 2
	predicatePrecedenceAnd   = 3
	predicatePrecedenceEq    = 4
	predicatePrecedenceNot   = 5
)

// ParseKeyBindingContextPredicate parses a predicate in the keymap's
// context format:
//
//	StatusBar                     — an identifier
//	mode == visible               — a key-value pair
//	StatusBar && mode == visible  — conjunction
//	StatusBar > mode == visible   — descendant nesting
//	!mode, a || b                 — negation, disjunction
func ParseKeyBindingContextPredicate(source string) (*KeyBindingContextPredicate, error) {
	source = skipContextWhitespace(source)
	predicate, rest, err := parsePredicateExpr(source, 0)
	if err != nil {
		return nil, err
	}
	if rest != "" {
		return nil, fmt.Errorf("gpui: key binding context: unexpected character %q", string(rest[0]))
	}
	return predicate, nil
}

// MustParseKeyBindingContextPredicate parses a predicate, panicking on
// failure (the KeyBinding::new spelling of the reference).
func MustParseKeyBindingContextPredicate(source string) *KeyBindingContextPredicate {
	p, err := ParseKeyBindingContextPredicate(source)
	if err != nil {
		panic(err)
	}
	return p
}

func parsePredicateExpr(source string, minPrecedence uint32) (*KeyBindingContextPredicate, string, error) {
	predicate, rest, err := parsePredicatePrimary(source)
	if err != nil {
		return nil, "", err
	}
	source = rest

parse:
	for {
		operators := []struct {
			text       string
			precedence uint32
			kind       PredicateKind
		}{
			{">", predicatePrecedenceChild, PredicateDescendant},
			{"&&", predicatePrecedenceAnd, PredicateAnd},
			{"||", predicatePrecedenceOr, PredicateOr},
			{"==", predicatePrecedenceEq, PredicateEqual},
			{"!=", predicatePrecedenceEq, PredicateNotEqual},
		}
		for _, op := range operators {
			if strings.HasPrefix(source, op.text) && op.precedence >= minPrecedence {
				source = skipContextWhitespace(source[len(op.text):])
				right, rest, err := parsePredicateExpr(source, op.precedence+1)
				if err != nil {
					return nil, "", err
				}
				if op.kind == PredicateEqual || op.kind == PredicateNotEqual {
					if predicate.Kind != PredicateIdentifier || right.Kind != PredicateIdentifier {
						return nil, "", fmt.Errorf("gpui: key binding context: operands of ==/!= must be identifiers")
					}
					predicate = &KeyBindingContextPredicate{
						Kind:       op.kind,
						Identifier: predicate.Identifier,
						Value:      right.Identifier,
					}
				} else {
					predicate = &KeyBindingContextPredicate{
						Kind:     op.kind,
						Children: []*KeyBindingContextPredicate{predicate, right},
					}
				}
				source = rest
				continue parse
			}
		}
		break
	}
	return predicate, source, nil
}

func parsePredicatePrimary(source string) (*KeyBindingContextPredicate, string, error) {
	if source == "" {
		return nil, "", fmt.Errorf("gpui: key binding context: unexpected end")
	}
	next := rune(source[0])
	switch {
	case next == '(':
		source = skipContextWhitespace(source[1:])
		predicate, rest, err := parsePredicateExpr(source, 0)
		if err != nil {
			return nil, "", err
		}
		if !strings.HasPrefix(rest, ")") {
			return nil, "", fmt.Errorf("gpui: key binding context: expected a ')'")
		}
		return predicate, skipContextWhitespace(rest[1:]), nil
	case next == '!':
		source = skipContextWhitespace(source[1:])
		inner, rest, err := parsePredicateExpr(source, predicatePrecedenceNot)
		if err != nil {
			return nil, "", err
		}
		return &KeyBindingContextPredicate{Kind: PredicateNot, Children: []*KeyBindingContextPredicate{inner}}, rest, nil
	case isContextIdentifierRune(next):
		length := 0
		for i, r := range source {
			if !isContextIdentifierRune(r) && !isVimOperatorRune(r) {
				length = i
				break
			}
			length = i + utf8RuneLen(r)
		}
		identifier, rest := source[:length], source[length:]
		return &KeyBindingContextPredicate{Kind: PredicateIdentifier, Identifier: identifier}, skipContextWhitespace(rest), nil
	case isVimOperatorRune(next):
		// A vim operator character used as an identifier (">", "<",
		// "~", "\"", "?").
		return &KeyBindingContextPredicate{Kind: PredicateIdentifier, Identifier: source[:1]}, skipContextWhitespace(source[1:]), nil
	default:
		return nil, "", fmt.Errorf("gpui: key binding context: unexpected character %q", string(next))
	}
}

func utf8RuneLen(r rune) int {
	switch {
	case r < 0x80:
		return 1
	case r < 0x800:
		return 2
	case r < 0x10000:
		return 3
	default:
		return 4
	}
}

func isVimOperatorRune(r rune) bool {
	return r == '>' || r == '<' || r == '~' || r == '"' || r == '?'
}

// DepthOf finds the deepest depth at which the predicate matches the
// context stack (contexts ordered from lowest to highest). It returns
// -1 when the predicate does not match (the reference Option<usize>).
func (p *KeyBindingContextPredicate) DepthOf(contexts []KeyContext) int {
	for depth := len(contexts); depth >= 0; depth-- {
		if p.evalInner(contexts[:depth], contexts) {
			return depth
		}
	}
	return -1
}

// Eval evaluates the predicate against a stack of contexts ordered
// from lowest to highest.
func (p *KeyBindingContextPredicate) Eval(contexts []KeyContext) bool {
	return p.evalInner(contexts, contexts)
}

func (p *KeyBindingContextPredicate) evalInner(contexts, allContexts []KeyContext) bool {
	if len(contexts) == 0 {
		return false
	}
	context := contexts[len(contexts)-1]
	switch p.Kind {
	case PredicateIdentifier:
		return context.Contains(p.Identifier)
	case PredicateEqual:
		value, ok := context.Get(p.Identifier)
		return ok && value == p.Value
	case PredicateNotEqual:
		value, ok := context.Get(p.Identifier)
		return !ok || value != p.Value
	case PredicateNot:
		for i := range allContexts {
			if p.Children[0].evalInner(allContexts[:i+1], allContexts) {
				return false
			}
		}
		return true
	case PredicateDescendant:
		// Workspace > Pane > Editor: the parent must match a prefix of
		// the contexts and the child must match the remainder
		// (context.rs eval_inner, including its quirk that the child is
		// evaluated against the remainder as the full context stack).
		for i := 0; i+1 < len(contexts); i++ {
			if p.Children[0].evalInner(contexts[:i+1], allContexts) {
				if !p.Children[1].evalInner(contexts[i+1:], contexts[i+1:]) {
					return false
				}
				return true
			}
		}
		return false
	case PredicateAnd:
		return p.Children[0].evalInner(contexts, allContexts) && p.Children[1].evalInner(contexts, allContexts)
	case PredicateOr:
		return p.Children[0].evalInner(contexts, allContexts) || p.Children[1].evalInner(contexts, allContexts)
	}
	return false
}

// IsSuperset reports whether this predicate matches all possible
// contexts matched by the other predicate (context.rs is_superset,
// including its conservative false answers).
func (p *KeyBindingContextPredicate) IsSuperset(other *KeyBindingContextPredicate) bool {
	if p.equal(other) {
		return true
	}
	if p.Kind == PredicateOr {
		return p.Children[0].IsSuperset(other) || p.Children[1].IsSuperset(other)
	}
	switch other.Kind {
	case PredicateDescendant:
		return p.IsSuperset(other.Children[1])
	case PredicateAnd:
		return p.IsSuperset(other.Children[0]) || p.IsSuperset(other.Children[1])
	}
	return false
}

func (p *KeyBindingContextPredicate) equal(other *KeyBindingContextPredicate) bool {
	if p == other {
		return true
	}
	if p == nil || other == nil || p.Kind != other.Kind || p.Identifier != other.Identifier || p.Value != other.Value {
		return false
	}
	if len(p.Children) != len(other.Children) {
		return false
	}
	for i := range p.Children {
		if !p.Children[i].equal(other.Children[i]) {
			return false
		}
	}
	return true
}

// String renders the predicate in its parse-compatible spelling
// (context.rs Display: And joins with " && " parenthesizing Or
// children; Or joins with " || " parenthesizing And children; Not
// renders "!ident" or "!(pred)").
func (p *KeyBindingContextPredicate) String() string {
	var b strings.Builder
	p.write(&b)
	return b.String()
}

func (p *KeyBindingContextPredicate) write(b *strings.Builder) {
	switch p.Kind {
	case PredicateIdentifier:
		b.WriteString(p.Identifier)
	case PredicateEqual:
		fmt.Fprintf(b, "%s == %s", p.Identifier, p.Value)
	case PredicateNotEqual:
		fmt.Fprintf(b, "%s != %s", p.Identifier, p.Value)
	case PredicateDescendant:
		p.Children[0].write(b)
		b.WriteString(" > ")
		p.Children[1].write(b)
	case PredicateNot:
		if p.Children[0].Kind == PredicateIdentifier {
			b.WriteString("!")
			b.WriteString(p.Children[0].Identifier)
		} else {
			b.WriteString("!(")
			p.Children[0].write(b)
			b.WriteString(")")
		}
	case PredicateAnd:
		p.writeJoined(b, " && ", PredicateAnd, PredicateOr)
	case PredicateOr:
		p.writeJoined(b, " || ", PredicateOr, PredicateAnd)
	}
}

func (p *KeyBindingContextPredicate) writeJoined(b *strings.Builder, separator string, operator PredicateKind, parenthesize PredicateKind) {
	first := true
	p.writeJoinedInner(b, separator, operator, parenthesize, &first)
}

func (p *KeyBindingContextPredicate) writeJoinedInner(b *strings.Builder, separator string, operator, parenthesize PredicateKind, first *bool) {
	if p.Kind == operator {
		p.Children[0].writeJoinedInner(b, separator, operator, parenthesize, first)
		p.Children[1].writeJoinedInner(b, separator, operator, parenthesize, first)
		return
	}
	if !*first {
		b.WriteString(separator)
	}
	*first = false
	if p.Kind == parenthesize {
		b.WriteString("(")
		p.write(b)
		b.WriteString(")")
	} else {
		p.write(b)
	}
}

func skipContextWhitespace(source string) string {
	for i, r := range source {
		if !unicode.IsSpace(r) {
			return source[i:]
		}
	}
	return ""
}
