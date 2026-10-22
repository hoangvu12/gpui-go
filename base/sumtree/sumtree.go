// Package sumtree is a Go port of zed-sum-tree 0.2.0 (Apache-2.0), the
// persistent B+ tree with monoid summaries that GPUI Base uses for soft-wrap
// rows (input/editor/display_map/text_wrapper.rs:59-236,500-512) and
// diagnostics (input/editor/diagnostics.rs:137-252), plus the Bias enum used
// by the rope and movement code. Sources are cited as
// zed-sum-tree/src/<file>:<line> (registry crate zed-sum-tree-0.2.0).
//
// Go adaptations:
//
//   - Every Base summary uses the unit context `()`, so contexts are dropped
//     (zed's ContextLessSummary shape, sum_tree.rs:43-58).
//   - Rust's `Summary::zero` / `Dimension::zero` is the Go zero value of the
//     type, which must therefore be the identity element. That holds for all
//     of Base's summaries and dimensions.
//   - A summary type is automatically a dimension of itself, as zed's blanket
//     impl (sum_tree.rs:93-101) provides, because Summary[S] and
//     Dimension[S, S] have the same method.
//   - Go methods cannot take type parameters, so `tree.cursor::<D>()` is
//     NewCursor[D](tree) and `cursor.summary::<_, Out>()` is
//     CursorSummary[Out](cursor, ...). The blanket "D1 seeks
//     Dimensions<D1, D2>" impl (sum_tree.rs:136-146) is FirstDimension.
//   - Option<&T> results are (T, bool).
//   - Not ported (unused by Base): KeyedItem/edit/get/insert_or_replace,
//     TreeMap/TreeSet, FilterCursor, par_* (rayon), update_last.
package sumtree

import "iter"

// treeBase is TREE_BASE (sum_tree.rs:12-15): nodes hold at most 2*treeBase
// children. Tests set it to 2 like zed's cfg(test) build.
var treeBase = 6

// Bias settles ambiguities when determining positions in an ordered sequence
// (sum_tree.rs:148-185). Left attaches to the element on the left, Right to
// the element on the right.
type Bias uint8

const (
	// Left attaches to the character on the left (the default).
	Left Bias = iota
	// Right attaches to the character on the right.
	Right
)

// Invert returns the opposite bias.
func (b Bias) Invert() Bias {
	if b == Left {
		return Right
	}
	return Left
}

// Summary is a monoid describing a subtree (sum_tree.rs:37-41): AddSummary
// returns the receiver followed by other. The zero value is the identity.
type Summary[S any] interface {
	AddSummary(other S) S
}

// Item is a value stored in a SumTree (sum_tree.rs:17-24).
type Item[S any] interface {
	Summary() S
}

// Dimension is a quantity accumulated from summaries while seeking
// (sum_tree.rs:74-91). The zero value is the identity.
type Dimension[S any, D any] interface {
	AddSummary(summary S) D
}

// SeekTarget compares a seek target with the cursor's current location
// (sum_tree.rs:103-111): negative, zero or positive as the target is before,
// at or after cursorLocation.
type SeekTarget[D any] interface {
	Cmp(cursorLocation D) int
}

// Dimensions measures two dimensions at once (sum_tree.rs:119-134, with the
// defaulted third dimension dropped). V0 and V1 are Rust's `.0` and `.1`.
type Dimensions[S any, D1 Dimension[S, D1], D2 Dimension[S, D2]] struct {
	V0 D1
	V1 D2
}

// AddSummary adds summary to both dimensions.
func (d Dimensions[S, D1, D2]) AddSummary(summary S) Dimensions[S, D1, D2] {
	return Dimensions[S, D1, D2]{d.V0.AddSummary(summary), d.V1.AddSummary(summary)}
}

type firstDimension[S any, D1 Dimension[S, D1], D2 Dimension[S, D2]] struct {
	target SeekTarget[D1]
}

func (f firstDimension[S, D1, D2]) Cmp(loc Dimensions[S, D1, D2]) int {
	return f.target.Cmp(loc.V0)
}

// FirstDimension lets a target of the first dimension seek a cursor over
// Dimensions[S, D1, D2] (sum_tree.rs:136-146).
func FirstDimension[S any, D1 Dimension[S, D1], D2 Dimension[S, D2]](target SeekTarget[D1]) SeekTarget[Dimensions[S, D1, D2]] {
	return firstDimension[S, D1, D2]{target}
}

// node is an immutable tree node (sum_tree.rs:809-822). Height 0 is a leaf,
// whose childSummaries are the item summaries.
type node[T Item[S], S Summary[S]] struct {
	height         int
	summary        S
	childSummaries []S
	childTrees     []*node[T, S]
	items          []T
}

func (n *node[T, S]) isLeaf() bool { return n.height == 0 }

func (n *node[T, S]) isUnderflowing() bool {
	if n.isLeaf() {
		return len(n.items) < treeBase
	}
	return len(n.childTrees) < treeBase
}

func (n *node[T, S]) clone() *node[T, S] {
	c := *n
	c.childSummaries = append([]S(nil), n.childSummaries...)
	if n.isLeaf() {
		c.items = append([]T(nil), n.items...)
	} else {
		c.childTrees = append([]*node[T, S](nil), n.childTrees...)
	}
	return &c
}

func sum[S Summary[S]](summaries []S) S {
	var s S
	for _, v := range summaries {
		s = s.AddSummary(v)
	}
	return s
}

// SumTree is a persistent B+ tree of items with cached summaries
// (sum_tree.rs:187-194). A SumTree is a value: assignment shares structure
// (Rust's O(1) `clone`) and later appends to either copy never affect the
// other. The zero value is an empty tree.
type SumTree[T Item[S], S Summary[S]] struct {
	root *node[T, S]
}

// New returns an empty tree (sum_tree.rs:207-213).
func New[T Item[S], S Summary[S]]() SumTree[T, S] { return SumTree[T, S]{} }

func (t SumTree[T, S]) node() *node[T, S] {
	if t.root == nil {
		return &node[T, S]{}
	}
	return t.root
}

// FromItem returns a tree holding one item (sum_tree.rs:224-228).
func FromItem[T Item[S], S Summary[S]](item T) SumTree[T, S] {
	var t SumTree[T, S]
	t.Push(item)
	return t
}

// FromIter builds a balanced tree from items in order (sum_tree.rs:230-294).
func FromIter[T Item[S], S Summary[S]](items []T) SumTree[T, S] {
	var nodes []*node[T, S]
	for len(items) > 0 {
		k := min(len(items), 2*treeBase)
		leaf := &node[T, S]{items: append([]T(nil), items[:k]...)}
		leaf.childSummaries = make([]S, k)
		for i, it := range leaf.items {
			leaf.childSummaries[i] = it.Summary()
		}
		leaf.summary = leaf.childSummaries[0]
		for _, s := range leaf.childSummaries[1:] {
			leaf.summary = leaf.summary.AddSummary(s)
		}
		nodes = append(nodes, leaf)
		items = items[k:]
	}
	height := 0
	for len(nodes) > 1 {
		height++
		var parents []*node[T, S]
		var cur *node[T, S]
		for _, child := range nodes {
			if cur == nil {
				cur = &node[T, S]{height: height}
			}
			cur.summary = cur.summary.AddSummary(child.summary)
			cur.childSummaries = append(cur.childSummaries, child.summary)
			cur.childTrees = append(cur.childTrees, child)
			if len(cur.childTrees) == 2*treeBase {
				parents = append(parents, cur)
				cur = nil
			}
		}
		if cur != nil {
			parents = append(parents, cur)
		}
		nodes = parents
	}
	if len(nodes) == 0 {
		return SumTree[T, S]{}
	}
	return SumTree[T, S]{nodes[0]}
}

// Items returns all items in order (sum_tree.rs:358-368).
func (t SumTree[T, S]) Items() []T {
	var out []T
	for it := range t.Iter() {
		out = append(out, it)
	}
	return out
}

// Iter iterates the items in order (sum_tree.rs:370-372, cursor.rs:550-613).
func (t SumTree[T, S]) Iter() iter.Seq[T] {
	return func(yield func(T) bool) {
		var walk func(n *node[T, S]) bool
		walk = func(n *node[T, S]) bool {
			if n.isLeaf() {
				for _, it := range n.items {
					if !yield(it) {
						return false
					}
				}
				return true
			}
			for _, c := range n.childTrees {
				if !walk(c) {
					return false
				}
			}
			return true
		}
		walk(t.node())
	}
}

// First returns the first item (sum_tree.rs:398-401).
func (t SumTree[T, S]) First() (T, bool) {
	n := t.node()
	for !n.isLeaf() {
		n = n.childTrees[0]
	}
	if len(n.items) == 0 {
		var zero T
		return zero, false
	}
	return n.items[0], true
}

// Last returns the last item (sum_tree.rs:403-405).
func (t SumTree[T, S]) Last() (T, bool) {
	n := t.node()
	for !n.isLeaf() {
		n = n.childTrees[len(n.childTrees)-1]
	}
	if len(n.items) == 0 {
		var zero T
		return zero, false
	}
	return n.items[len(n.items)-1], true
}

// Summary returns the summary of the whole tree (sum_tree.rs:464-469).
func (t SumTree[T, S]) Summary() S { return t.node().summary }

// Extent returns the whole tree measured in dimension D (sum_tree.rs:451-462).
func Extent[D Dimension[S, D], T Item[S], S Summary[S]](t SumTree[T, S]) D {
	var d D
	return d.AddSummary(t.node().summary)
}

// IsEmpty reports whether the tree has no items (sum_tree.rs:471-476).
func (t SumTree[T, S]) IsEmpty() bool {
	n := t.node()
	return n.isLeaf() && len(n.items) == 0
}

// Extend appends items in order (sum_tree.rs:478-483).
func (t *SumTree[T, S]) Extend(items []T) { t.Append(FromIter[T, S](items)) }

// Push appends one item (sum_tree.rs:496-506).
func (t *SumTree[T, S]) Push(item T) {
	s := item.Summary()
	t.Append(SumTree[T, S]{&node[T, S]{summary: s, items: []T{item}, childSummaries: []S{s}}})
}

// Append appends all items of other (sum_tree.rs:508-520).
func (t *SumTree[T, S]) Append(other SumTree[T, S]) {
	if t.IsEmpty() {
		*t = other
		return
	}
	o := other.node()
	if !o.isLeaf() || len(o.items) > 0 {
		self := t.node()
		if self.height < o.height {
			for _, c := range o.childTrees {
				t.Append(SumTree[T, S]{c})
			}
		} else if left, split := pushTreeRecursive(self, o); split != nil {
			t.root = fromChildTrees(left, split)
		} else {
			t.root = left
		}
	}
}

// pushTreeRecursive is push_tree_recursive (sum_tree.rs:522-640) on an
// immutable node: it returns the updated copy of self and, when it
// overflowed, the right half that must become self's new sibling.
func pushTreeRecursive[T Item[S], S Summary[S]](self, other *node[T, S]) (*node[T, S], *node[T, S]) {
	n := self.clone()
	if !n.isLeaf() {
		n.summary = n.summary.AddSummary(other.summary)
		delta := n.height - other.height
		var sumsToAppend []S
		var treesToAppend []*node[T, S]
		switch {
		case delta == 0:
			sumsToAppend = other.childSummaries
			treesToAppend = other.childTrees
		case delta == 1 && !other.isUnderflowing():
			sumsToAppend = []S{other.summary}
			treesToAppend = []*node[T, S]{other}
		default:
			last := len(n.childTrees) - 1
			child, split := pushTreeRecursive(n.childTrees[last], other)
			n.childTrees[last] = child
			n.childSummaries[last] = child.summary
			if split != nil {
				sumsToAppend = []S{split.summary}
				treesToAppend = []*node[T, S]{split}
			}
		}
		count := len(n.childTrees) + len(treesToAppend)
		if count > 2*treeBase {
			mid := (count + count%2) / 2
			allS := append(append([]S(nil), n.childSummaries...), sumsToAppend...)
			allT := append(append([]*node[T, S](nil), n.childTrees...), treesToAppend...)
			n.childSummaries, n.childTrees = allS[:mid:mid], allT[:mid:mid]
			n.summary = sum(n.childSummaries)
			right := &node[T, S]{
				height:         n.height,
				childSummaries: allS[mid:],
				childTrees:     allT[mid:],
				summary:        sum(allS[mid:]),
			}
			return n, right
		}
		n.childSummaries = append(n.childSummaries, sumsToAppend...)
		n.childTrees = append(n.childTrees, treesToAppend...)
		return n, nil
	}
	count := len(n.items) + len(other.items)
	if count > 2*treeBase {
		mid := (count + count%2) / 2
		allI := append(append([]T(nil), n.items...), other.items...)
		allS := append(append([]S(nil), n.childSummaries...), other.childSummaries...)
		n.items, n.childSummaries = allI[:mid:mid], allS[:mid:mid]
		n.summary = sum(n.childSummaries)
		return n, &node[T, S]{items: allI[mid:], childSummaries: allS[mid:], summary: sum(allS[mid:])}
	}
	n.summary = n.summary.AddSummary(other.summary)
	n.items = append(n.items, other.items...)
	n.childSummaries = append(n.childSummaries, other.childSummaries...)
	return n, nil
}

// fromChildTrees is from_child_trees (sum_tree.rs:642-660).
func fromChildTrees[T Item[S], S Summary[S]](left, right *node[T, S]) *node[T, S] {
	sums := []S{left.summary, right.summary}
	return &node[T, S]{
		height:         left.height + 1,
		childSummaries: sums,
		childTrees:     []*node[T, S]{left, right},
		summary:        sum(sums),
	}
}

func leftmostLeaf[T Item[S], S Summary[S]](n *node[T, S]) *node[T, S] {
	for !n.isLeaf() {
		n = n.childTrees[0]
	}
	return n
}

func rightmostLeaf[T Item[S], S Summary[S]](n *node[T, S]) *node[T, S] {
	for !n.isLeaf() {
		n = n.childTrees[len(n.childTrees)-1]
	}
	return n
}
