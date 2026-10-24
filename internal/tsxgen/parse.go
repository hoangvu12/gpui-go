package tsxgen

import (
	"fmt"
	"strings"
)

// ParseDTS reads a gpui-script.d.ts back into a Surface. The drift
// tests use it to prove the committed file declares exactly the
// surface the engine accepts: a planted gap (a removed class, style
// key, element, prop or global) parses to a smaller surface and the
// comparison fails.
//
// The parser reads the grammar the generator emits — unions of string
// literals, number/string/boolean, numeric literals, references to the
// declared aliases and template literal types — and errors on anything
// else, so a hand edit that breaks the shape is loud, not silent.
func ParseDTS(src string) (*Surface, error) {
	s := &Surface{}
	for _, chunk := range splitDecls(src) {
		head := firstCodeLine(chunk)
		if head == "" {
			continue
		}
		trimmed := strings.TrimSpace(head)
		var err error
		switch {
		case strings.HasPrefix(trimmed, "type Child ="):
			// The scaffold's children union carries no surface (the
			// engine takes whatever JSX children lower).
		case strings.HasPrefix(trimmed, "type "):
			err = parseTypeDecl(s, chunk)
		case strings.HasPrefix(trimmed, "declare namespace JSX"):
			err = parseIntrinsicElements(s, chunk)
		case strings.HasPrefix(trimmed, "interface IntrinsicElements {"):
			err = parseIntrinsicElements(s, chunk)
		case strings.HasPrefix(trimmed, "interface HostProps {"):
			err = parseMemberDecl(chunk, "HostProps", func(name, typ string) {
				s.Props = append(s.Props, name)
			})
		case strings.HasPrefix(trimmed, "interface MouseEvent {"):
			err = parseMemberDecl(chunk, "MouseEvent", func(name, typ string) {
				s.Event = append(s.Event, Field{Name: name, Type: mustParseTSType(typ, name)})
			})
		case strings.HasPrefix(trimmed, "interface CSSProperties {"):
			err = parseMemberDecl(chunk, "CSSProperties", func(name, typ string) {
				s.Styles = append(s.Styles, StyleProp{Name: name, Type: mustParseTSType(typ, name)})
			})
		case strings.HasPrefix(trimmed, "declare "):
			sig := declareSig(chunk)
			s.Globals = append(s.Globals, Global{Name: globalName(sig), Sig: sig})
		case strings.HasPrefix(trimmed, "interface "):
			// The value interfaces (HostElement, StateHandle,
			// RequestInit, Response, Console) carry no surface.
		default:
			err = fmt.Errorf("tsxgen: unexpected declaration %q", clip(trimmed))
		}
		if err != nil {
			return nil, err
		}
	}
	if len(s.Elements) == 0 || len(s.Styles) == 0 || s.Classes.Class.IsEmpty() {
		return nil, fmt.Errorf("tsxgen: the .d.ts declares no surface")
	}
	// The event handlers are the on* props, as in the engine's
	// extraction (jsxElement's prop switch).
	for _, p := range s.Props {
		if strings.HasPrefix(p, "on") {
			s.Events = append(s.Events, p)
		}
	}
	return s, nil
}

// mustParseTSType parses a member's type or fails loudly.
func mustParseTSType(typ, where string) TSType {
	p := &typeParser{src: typ}
	t, err := p.parseUnion()
	if err != nil {
		panic(fmt.Sprintf("tsxgen: parse %s: %v", where, err))
	}
	p.skipSpace()
	if p.pos != len(p.src) {
		panic(fmt.Sprintf("tsxgen: parse %s: trailing %q", where, p.src[p.pos:]))
	}
	return t
}

// splitDecls splits the source into logical declarations: a line
// starting a declaration begins a chunk that ends when its braces
// close or its semicolon lands (banner and doc comments ride along).
func splitDecls(src string) []string {
	var chunks []string
	var current []string
	depth := 0
	flush := func() {
		if len(current) > 0 {
			chunks = append(chunks, strings.Join(current, "\n"))
			current = nil
		}
		depth = 0
	}
	startsDecl := func(line string) bool {
		t := strings.TrimSpace(line)
		return strings.HasPrefix(t, "type ") || strings.HasPrefix(t, "interface ") || strings.HasPrefix(t, "declare ")
	}
	lines := strings.Split(src, "\n")
	for _, line := range lines {
		if len(current) == 0 {
			if !startsDecl(line) {
				current = append(current, line) // banner or comment lines
				if strings.TrimSpace(line) == "" {
					flush()
				}
				continue
			}
			current = append(current, line)
			depth = braceDepth(line)
			t := strings.TrimSpace(line)
			if depth == 0 && (strings.HasSuffix(t, ";") || strings.HasSuffix(t, "}")) {
				// A single-line declaration (interface HostElement {}).
				flush()
			}
			continue
		}
		current = append(current, line)
		depth += braceDepth(line)
		t := strings.TrimSpace(line)
		if depth <= 0 && (strings.HasSuffix(t, ";") || strings.HasSuffix(t, "}")) {
			// The declaration is complete (an arm line, a single-line
			// interface, or a closing brace at the top level).
			flush()
		}
	}
	flush()
	return chunks
}

// braceDepth returns the net brace depth of a line.
func braceDepth(line string) int {
	depth := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '{':
			depth++
		case '}':
			depth--
		}
	}
	return depth
}

// declareSig returns a declaration chunk's exact text: the lines from
// the first `declare` to the chunk's last non-blank line, verbatim.
func declareSig(chunk string) string {
	lines := strings.Split(chunk, "\n")
	start := 0
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "declare ") {
			start = i
			break
		}
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}

// firstCodeLine returns the chunk's first non-blank, non-comment line.
func firstCodeLine(chunk string) string {
	for _, line := range strings.Split(chunk, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") ||
			strings.HasPrefix(t, "*") {
			continue
		}
		return line
	}
	return ""
}

// codeLines returns the chunk's code lines (no blanks, no comments).
func codeLines(chunk string) []string {
	var out []string
	for _, line := range strings.Split(chunk, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") ||
			strings.HasPrefix(t, "*") {
			continue
		}
		out = append(out, t)
	}
	return out
}

// parseTypeDecl reads one `type Name = …;` chunk into the surface.
func parseTypeDecl(s *Surface, chunk string) error {
	lines := codeLines(chunk)
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "type ") {
		return fmt.Errorf("tsxgen: malformed type declaration")
	}
	head := strings.TrimSuffix(lines[0], "=")
	name := strings.TrimSpace(strings.TrimPrefix(head, "type "))
	if name == "" {
		return fmt.Errorf("tsxgen: type declaration without a name")
	}
	var arms []string
	for _, line := range lines[1:] {
		t := strings.TrimSpace(line)
		t = strings.TrimSuffix(t, ";")
		t = strings.TrimSpace(strings.TrimSuffix(t, "|"))
		if t != "" {
			arms = append(arms, t)
		}
	}
	body := strings.Join(arms, " | ")
	p := &typeParser{src: body}
	t, err := p.parseUnion()
	if err != nil {
		return fmt.Errorf("tsxgen: parse %s: %w", name, err)
	}
	p.skipSpace()
	if p.pos != len(p.src) {
		return fmt.Errorf("tsxgen: parse %s: trailing %q", name, clip(p.src[p.pos:]))
	}
	if name == "Class" {
		s.Classes.Class = t
		return nil
	}
	s.Classes.Aliases = append(s.Classes.Aliases, Alias{Name: name, Type: t})
	return nil
}

// parseIntrinsicElements reads the JSX namespace's IntrinsicElements
// (or a bare interface of that name) into the surface's elements.
func parseIntrinsicElements(s *Surface, chunk string) error {
	lines := codeLines(chunk)
	in := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "interface IntrinsicElements") {
			in = true
			continue
		}
		if !in {
			continue
		}
		if strings.HasPrefix(t, "}") {
			break
		}
		member, _, ok := strings.Cut(t, ":")
		if !ok {
			return fmt.Errorf("tsxgen: IntrinsicElements: unexpected member %q", clip(t))
		}
		s.Elements = append(s.Elements, strings.TrimSpace(member))
	}
	if len(s.Elements) == 0 {
		return fmt.Errorf("tsxgen: IntrinsicElements: no elements found")
	}
	return nil
}

// parseMemberDecl reads an interface's members, calling add for each.
func parseMemberDecl(chunk string, name string, add func(name, typ string)) error {
	lines := codeLines(chunk)
	// The first line opens the interface; the last closes it.
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "interface "+name) {
		return fmt.Errorf("tsxgen: malformed interface %s", name)
	}
	for _, line := range lines[1 : len(lines)-1] {
		member, typ, ok := strings.Cut(line, ":")
		if !ok {
			return fmt.Errorf("tsxgen: %s: unexpected member %q", name, clip(line))
		}
		typ = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(typ), ";"))
		member = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(member), "?"))
		add(member, typ)
	}
	return nil
}

// globalName extracts the declared name from a declaration's text.
func globalName(sig string) string {
	rest := strings.TrimPrefix(sig, "declare ")
	for _, kw := range []string{"function ", "const ", "class ", "namespace "} {
		if strings.HasPrefix(rest, kw) {
			rest = strings.TrimPrefix(rest, kw)
			break
		}
	}
	if i := strings.IndexAny(rest, "(<={: "); i >= 0 {
		return rest[:i]
	}
	return rest
}

// typeParser is the recursive-descent parser for the type grammar the
// generator emits: unions of string literals, number/string/boolean,
// numeric literals, references and template literal types.
type typeParser struct {
	src string
	pos int
}

func (t *typeParser) skipSpace() {
	for t.pos < len(t.src) && (t.src[t.pos] == ' ' || t.src[t.pos] == '\t') {
		t.pos++
	}
}

// parseUnion parses arm ("|" arm)*.
func (t *typeParser) parseUnion() (TSType, error) {
	var out TSType
	for {
		arm, err := t.parseArm()
		if err != nil {
			return none, err
		}
		out.Literals = append(out.Literals, arm.Literals...)
		out.NumLits = append(out.NumLits, arm.NumLits...)
		out.Number = out.Number || arm.Number
		out.Str = out.Str || arm.Str
		out.Bool = out.Bool || arm.Bool
		out.Templates = append(out.Templates, arm.Templates...)
		out.Refs = append(out.Refs, arm.Refs...)
		t.skipSpace()
		if t.pos < len(t.src) && t.src[t.pos] == '|' {
			t.pos++
			t.skipSpace()
			continue
		}
		return out, nil
	}
}

// parseArm parses one union arm.
func (t *typeParser) parseArm() (TSType, error) {
	if t.pos >= len(t.src) {
		return none, fmt.Errorf("expected a type")
	}
	switch c := t.src[t.pos]; {
	case c == '"':
		s, err := t.parseStringLit()
		if err != nil {
			return none, err
		}
		return TSType{Literals: []string{s}}, nil
	case c == '`':
		return t.parseTemplate()
	case c >= '0' && c <= '9':
		start := t.pos
		for t.pos < len(t.src) && (t.src[t.pos] >= '0' && t.src[t.pos] <= '9' || t.src[t.pos] == '.') {
			t.pos++
		}
		return TSType{NumLits: []string{t.src[start:t.pos]}}, nil
	default:
		if isIdentChar(c, true) {
			start := t.pos
			for t.pos < len(t.src) && isIdentChar(t.src[t.pos], false) {
				t.pos++
			}
			name := t.src[start:t.pos]
			switch name {
			case "number":
				return TSType{Number: true}, nil
			case "string":
				return TSType{Str: true}, nil
			case "boolean":
				return TSType{Bool: true}, nil
			}
			return TSType{Refs: []string{name}}, nil
		}
		return none, fmt.Errorf("unexpected character %q", string(c))
	}
}

// parseStringLit parses a double-quoted string literal.
func (t *typeParser) parseStringLit() (string, error) {
	t.pos++ // the opening quote
	var b strings.Builder
	for t.pos < len(t.src) {
		c := t.src[t.pos]
		if c == '\\' && t.pos+1 < len(t.src) {
			b.WriteByte(c)
			b.WriteByte(t.src[t.pos+1])
			t.pos += 2
			continue
		}
		if c == '"' {
			t.pos++
			s := b.String()
			if strings.Contains(s, "\\") {
				return "", fmt.Errorf("escapes in string literals are not generated")
			}
			return s, nil
		}
		b.WriteByte(c)
		t.pos++
	}
	return "", fmt.Errorf("unterminated string literal")
}

// parseTemplate parses a backtick template with ${hole} types.
func (t *typeParser) parseTemplate() (TSType, error) {
	t.pos++ // the opening backtick
	var parts []string
	var holes []TSType
	var current strings.Builder
	for t.pos < len(t.src) {
		c := t.src[t.pos]
		if c == '\\' && t.pos+1 < len(t.src) {
			// An escaped backtick or ${ in a static part.
			if t.src[t.pos+1] == '`' || (t.src[t.pos+1] == '$' && t.pos+2 < len(t.src) && t.src[t.pos+2] == '{') {
				current.WriteByte(t.src[t.pos+1])
				t.pos += 2
				continue
			}
			current.WriteByte(c)
			t.pos++
			continue
		}
		if c == '$' && t.pos+1 < len(t.src) && t.src[t.pos+1] == '{' {
			parts = append(parts, current.String())
			current.Reset()
			t.pos += 2
			hole, err := t.parseUnion()
			if err != nil {
				return none, err
			}
			t.skipSpace()
			if t.pos >= len(t.src) || t.src[t.pos] != '}' {
				return none, fmt.Errorf("unterminated ${ hole")
			}
			t.pos++
			holes = append(holes, hole)
			continue
		}
		if c == '`' {
			parts = append(parts, current.String())
			t.pos++
			if len(parts) != len(holes)+1 {
				return none, fmt.Errorf("template with %d parts and %d holes", len(parts), len(holes))
			}
			return TSType{Templates: []TSTemplate{{Parts: parts, Holes: holes}}}, nil
		}
		current.WriteByte(c)
		t.pos++
	}
	return none, fmt.Errorf("unterminated template")
}

func isIdentChar(c byte, first bool) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_', c == '$':
		return true
	case c >= '0' && c <= '9':
		return !first
	}
	return false
}

// clip shortens a string for an error message.
func clip(s string) string {
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}
