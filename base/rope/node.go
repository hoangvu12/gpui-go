package rope

import "strings"

// Tree shape, from ropey/src/tree/mod.rs:8-14 (non-test constants). They are
// variables only so the tests can shrink them like ropey's test build does
// (ropey/src/tree/mod.rs:15-21) and exercise deep trees on small inputs.
var (
	maxChildren = 16
	minChildren = 7
	maxText     = 1024
	minText     = maxText/2 - maxText/32
)

// textInfo is the per-node summary (ropey/src/tree/text_info.rs:30-48) for the
// metrics Base enables: bytes, chars, utf16 code units and LF line breaks.
type textInfo struct {
	bytes, chars, utf16, lf int
}

func (a textInfo) add(b textInfo) textInfo {
	return textInfo{a.bytes + b.bytes, a.chars + b.chars, a.utf16 + b.utf16, a.lf + b.lf}
}

// infoOf mirrors TextInfo::from_str (ropey/src/tree/text_info.rs:77-99):
// chars counts non-continuation bytes, utf16 adds one surrogate per 4-byte
// char, lf counts U+000A.
func infoOf(s string) textInfo {
	ti := textInfo{bytes: len(s)}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b&0xC0 != 0x80 {
			ti.chars++
			ti.utf16++
			if b >= 0xF0 {
				ti.utf16++
			}
		}
		if b == '\n' {
			ti.lf++
		}
	}
	return ti
}

// node is an immutable B-tree node. A leaf holds a chunk of text that always
// starts and ends on char boundaries; an internal node holds children of
// equal height. Nodes are never mutated once built, which is what makes a
// Rope value an O(1) clone (ropey uses Arc copy-on-write for the same end).
type node struct {
	info     textInfo
	leaf     bool
	text     string
	children []*node
}

var emptyLeaf = &node{leaf: true}

func newLeaf(s string) *node { return &node{info: infoOf(s), leaf: true, text: s} }

func newInternal(children []*node) *node {
	n := &node{children: children}
	for _, c := range children {
		n.info = n.info.add(c.info)
	}
	return n
}

// isBoundary reports whether byte i of s starts a char (or is len(s));
// ropey/src/lib.rs:315-323.
func isBoundary(s string, i int) bool {
	return i >= len(s) || s[i]&0xC0 != 0x80
}

// splitFloor/splitCeil are find_appropriate_split_floor/ceil
// (ropey/src/lib.rs:372-405): the nearest char boundary that does not split a
// CRLF pair.
func splitFloor(s string, i int) int {
	if i >= len(s) {
		return len(s)
	}
	for i > 0 && !isBoundary(s, i) {
		i--
	}
	if i > 0 && i < len(s) && s[i] == '\n' && s[i-1] == '\r' {
		i--
	}
	return i
}

func splitCeil(s string, i int) int {
	if i >= len(s) {
		return len(s)
	}
	for i < len(s) && !isBoundary(s, i) {
		i++
	}
	if i > 0 && i < len(s) && s[i] == '\n' && s[i-1] == '\r' {
		i++
	}
	return i
}

// splitText cuts s into leaf-sized chunks of near-equal size. Like ropey's
// insert path (ropey/src/rope.rs:585-597) chunks stay at most maxText-4
// bytes so multi-byte chars and CRLF pairs never force an oversize leaf.
func splitText(s string) []string {
	if len(s) <= maxText {
		return []string{s}
	}
	limit := maxText - 4
	n := (len(s) + limit - 1) / limit
	per := (len(s) + n - 1) / n
	out := make([]string, 0, n+1)
	for start := 0; start < len(s); {
		if len(s)-start <= maxText {
			out = append(out, s[start:])
			break
		}
		cut := splitFloor(s, start+per)
		if cut <= start {
			cut = splitCeil(s, start+per)
		}
		out = append(out, s[start:cut])
		start = cut
	}
	return out
}

func leavesOf(s string) []*node {
	chunks := splitText(s)
	out := make([]*node, len(chunks))
	for i, c := range chunks {
		out[i] = newLeaf(c)
	}
	return out
}

// group packs same-height nodes into parents of at most maxChildren children,
// spreading them evenly so every parent keeps at least minChildren when there
// is more than one parent.
func group(nodes []*node) []*node {
	if len(nodes) <= maxChildren {
		return []*node{newInternal(nodes)}
	}
	k := (len(nodes) + maxChildren - 1) / maxChildren
	out := make([]*node, 0, k)
	for i := 0; i < k; i++ {
		lo, hi := i*len(nodes)/k, (i+1)*len(nodes)/k
		out = append(out, newInternal(append([]*node(nil), nodes[lo:hi]...)))
	}
	return out
}

// buildRoot stacks levels until one root remains.
func buildRoot(nodes []*node) *node {
	if len(nodes) == 0 {
		return emptyLeaf
	}
	for len(nodes) > 1 {
		nodes = group(nodes)
	}
	return nodes[0]
}

func fromString(s string) *node {
	if s == "" {
		return emptyLeaf
	}
	return buildRoot(leavesOf(s))
}

// insert returns the node(s) of n's height that replace n after inserting
// text at local byte idx (a char boundary).
func (n *node) insert(idx int, text string) []*node {
	if n.leaf {
		s := n.text[:idx] + text + n.text[idx:]
		if len(s) <= maxText {
			return []*node{newLeaf(s)}
		}
		return leavesOf(s)
	}
	acc, ci := 0, len(n.children)-1
	for i, c := range n.children {
		if idx < acc+c.info.bytes || i == len(n.children)-1 {
			ci = i
			break
		}
		acc += c.info.bytes
	}
	repl := n.children[ci].insert(idx-acc, text)
	children := make([]*node, 0, len(n.children)+len(repl)-1)
	children = append(children, n.children[:ci]...)
	children = append(children, repl...)
	children = append(children, n.children[ci+1:]...)
	return group(children)
}

// remove returns n with local bytes [start, end) removed, or nil when nothing
// is left. Underfull children produced by the removal are merged with a
// neighbour so the tree keeps equal leaf depth and bounded fan-out.
func (n *node) remove(start, end int) *node {
	if n.leaf {
		s := n.text[:start] + n.text[end:]
		if s == "" {
			return nil
		}
		return newLeaf(s)
	}
	children := make([]*node, 0, len(n.children))
	acc := 0
	for _, c := range n.children {
		cs, ce := acc, acc+c.info.bytes
		acc = ce
		if ce <= start || cs >= end {
			children = append(children, c)
			continue
		}
		if start <= cs && ce <= end {
			continue // fully removed
		}
		lo, hi := max(start, cs)-cs, min(end, ce)-cs
		if r := c.remove(lo, hi); r != nil {
			children = append(children, r)
		}
	}
	if len(children) == 0 {
		return nil
	}
	return newInternal(mergeUnderfull(children))
}

func underfull(n *node) bool {
	if n.leaf {
		return n.info.bytes < minText
	}
	return len(n.children) < minChildren
}

// mergeUnderfull joins each underfull child with its right (or left)
// neighbour, re-splitting when the merge overflows.
func mergeUnderfull(children []*node) []*node {
	for i := 0; i < len(children) && len(children) > 1; i++ {
		if !underfull(children[i]) {
			continue
		}
		j := i + 1
		if j == len(children) {
			i, j = i-1, i
		}
		a, b := children[i], children[j]
		var merged []*node
		if a.leaf {
			merged = leavesOf(a.text + b.text)
		} else {
			merged = group(append(append([]*node(nil), a.children...), b.children...))
		}
		rest := append(append([]*node(nil), children[:i]...), merged...)
		children = append(rest, children[j+1:]...)
	}
	return children
}

// pullUp replaces an internal root that has a single child by that child
// (Rope::pull_up_singular_nodes, ropey/src/rope.rs:401-411).
func pullUp(n *node) *node {
	for !n.leaf && len(n.children) == 1 {
		n = n.children[0]
	}
	return n
}

// --- queries on absolute positions ------------------------------------------

// chunkAt returns the leaf text containing byte i and its absolute start; a
// one-past-the-end i returns the last leaf (ropey/src/shared_impl.rs:270-289).
func (n *node) chunkAt(i int) (string, int) {
	base := 0
	for !n.leaf {
		ci := len(n.children) - 1
		for k, c := range n.children {
			if i < base+c.info.bytes {
				ci = k
				break
			}
			if k < len(n.children)-1 {
				base += c.info.bytes
			}
		}
		n = n.children[ci]
	}
	return n.text, base
}

// prefix returns the summed info of everything before byte i, where i is a
// char boundary or one byte inside a char (the partial char is excluded).
func (n *node) prefix(i int) textInfo {
	var acc textInfo
	for !n.leaf {
		ci := len(n.children) - 1
		for k, c := range n.children {
			if i < acc.bytes+c.info.bytes {
				ci = k
				break
			}
			if k < len(n.children)-1 {
				acc = acc.add(c.info)
			}
		}
		n = n.children[ci]
	}
	local := i - acc.bytes
	for local > 0 && !isBoundary(n.text, local) {
		local--
	}
	return acc.add(infoOf(n.text[:local]))
}

// lineStart is _line_to_byte_idx for LF: the byte after the line-th LF, 0
// for line 0, and the text end past the last break.
func (n *node) lineStart(line int) int {
	if line <= 0 {
		return 0
	}
	if line > n.info.lf {
		return n.info.bytes
	}
	base, seen := 0, 0
	for !n.leaf {
		for k, c := range n.children {
			if seen+c.info.lf >= line || k == len(n.children)-1 {
				n = c
				break
			}
			seen += c.info.lf
			base += c.info.bytes
		}
	}
	want := line - seen
	for k := 0; k < len(n.text); k++ {
		if n.text[k] == '\n' {
			want--
			if want == 0 {
				return base + k + 1
			}
		}
	}
	return base + len(n.text)
}

// charStart is _char_to_byte_idx: the byte offset of char c.
func (n *node) charStart(c int) int {
	return n.seekUnits(c, func(t textInfo) int { return t.chars }, func(b byte) int {
		if b&0xC0 != 0x80 {
			return 1
		}
		return 0
	})
}

// utf16Start is _utf16_to_byte_idx: the byte offset of the char that UTF-16
// unit u belongs to (ropey/src/shared_impl.rs:392-425).
func (n *node) utf16Start(u int) int {
	return n.seekUnits(u, func(t textInfo) int { return t.utf16 }, func(b byte) int {
		switch {
		case b&0xC0 == 0x80:
			return 0
		case b >= 0xF0:
			return 2
		}
		return 1
	})
}

// seekUnits descends by a char-aligned metric and returns the byte offset of
// the char in which unit u falls (u == total gives the text end).
func (n *node) seekUnits(u int, metric func(textInfo) int, unitsAt func(byte) int) int {
	if u >= metric(n.info) {
		return n.info.bytes
	}
	base, seen := 0, 0
	for !n.leaf {
		for k, c := range n.children {
			if u < seen+metric(c.info) || k == len(n.children)-1 {
				n = c
				break
			}
			seen += metric(c.info)
			base += c.info.bytes
		}
	}
	for k := 0; k < len(n.text); k++ {
		w := unitsAt(n.text[k])
		if w == 0 {
			continue
		}
		if u < seen+w {
			return base + k
		}
		seen += w
	}
	return base + len(n.text)
}

// leaves appends the leaf texts of n in order.
func (n *node) appendTo(b *strings.Builder) {
	if n.leaf {
		b.WriteString(n.text)
		return
	}
	for _, c := range n.children {
		c.appendTo(b)
	}
}
