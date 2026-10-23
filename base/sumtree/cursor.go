package sumtree

type stackEntry[T Item[S], S Summary[S], D Dimension[S, D]] struct {
	tree     *node[T, S]
	index    int
	position D
}

// Cursor walks a SumTree while accumulating dimension D
// (zed-sum-tree/src/cursor.rs:28-36). Like zed's, a fresh cursor must be
// positioned with Seek, Next or Prev before Item and friends are called.
type Cursor[T Item[S], S Summary[S], D Dimension[S, D]] struct {
	tree     *node[T, S]
	stack    []stackEntry[T, S, D]
	position D
	didSeek  bool
	atEnd    bool
}

// NewCursor is `tree.cursor::<D>()` (sum_tree.rs:374-382, cursor.rs:63-72).
func NewCursor[D Dimension[S, D], T Item[S], S Summary[S]](t SumTree[T, S]) *Cursor[T, S, D] {
	n := t.node()
	return &Cursor[T, S, D]{tree: n, atEnd: t.IsEmpty()}
}

func (c *Cursor[T, S, D]) reset() {
	var zero D
	c.didSeek = false
	c.atEnd = c.tree.isLeaf() && len(c.tree.items) == 0
	c.stack = c.stack[:0]
	c.position = zero
}

// Start is the dimension accumulated before the current item (cursor.rs:81-83).
func (c *Cursor[T, S, D]) Start() D { return c.position }

// End is Start plus the current item's summary (cursor.rs:85-94).
func (c *Cursor[T, S, D]) End() D {
	if s, ok := c.ItemSummary(); ok {
		return c.position.AddSummary(s)
	}
	return c.position
}

func (c *Cursor[T, S, D]) assertDidSeek() {
	if !c.didSeek {
		panic("Must call `seek`, `next` or `prev` before calling this method")
	}
}

// Item is the current item; ok is false when the tree is empty or the cursor
// is at the end (cursor.rs:96-114).
func (c *Cursor[T, S, D]) Item() (T, bool) {
	c.assertDidSeek()
	var zero T
	if len(c.stack) == 0 {
		return zero, false
	}
	e := c.stack[len(c.stack)-1]
	if e.index == len(e.tree.items) {
		return zero, false
	}
	return e.tree.items[e.index], true
}

// ItemSummary is the current item's summary (cursor.rs:116-135).
func (c *Cursor[T, S, D]) ItemSummary() (S, bool) {
	c.assertDidSeek()
	var zero S
	if len(c.stack) == 0 {
		return zero, false
	}
	e := c.stack[len(c.stack)-1]
	if e.index == len(e.tree.childSummaries) {
		return zero, false
	}
	return e.tree.childSummaries[e.index], true
}

// NextItem is the item after the current one (cursor.rs:137-158).
func (c *Cursor[T, S, D]) NextItem() (T, bool) {
	c.assertDidSeek()
	var zero T
	if len(c.stack) > 0 {
		e := c.stack[len(c.stack)-1]
		if e.index == len(e.tree.items)-1 {
			if leaf := c.nextLeaf(); leaf != nil {
				return leaf.items[0], true
			}
			return zero, false
		}
		return e.tree.items[e.index+1], true
	}
	if c.atEnd {
		return zero, false
	}
	return SumTree[T, S]{c.tree}.First()
}

func (c *Cursor[T, S, D]) nextLeaf() *node[T, S] {
	for i := len(c.stack) - 2; i >= 0; i-- {
		e := c.stack[i]
		if e.index < len(e.tree.childTrees)-1 {
			return leftmostLeaf(e.tree.childTrees[e.index+1])
		}
	}
	return nil
}

// PrevItem is the item before the current one (cursor.rs:175-196).
func (c *Cursor[T, S, D]) PrevItem() (T, bool) {
	c.assertDidSeek()
	var zero T
	if len(c.stack) > 0 {
		e := c.stack[len(c.stack)-1]
		if e.index == 0 {
			if leaf := c.prevLeaf(); leaf != nil {
				return leaf.items[len(leaf.items)-1], true
			}
			return zero, false
		}
		return e.tree.items[e.index-1], true
	}
	if c.atEnd {
		return SumTree[T, S]{c.tree}.Last()
	}
	return zero, false
}

func (c *Cursor[T, S, D]) prevLeaf() *node[T, S] {
	for i := len(c.stack) - 2; i >= 0; i-- {
		e := c.stack[i]
		if e.index != 0 {
			return rightmostLeaf(e.tree.childTrees[e.index-1])
		}
	}
	return nil
}

// Prev moves to the previous item (cursor.rs:213-282).
func (c *Cursor[T, S, D]) Prev() {
	var zero D
	if !c.didSeek {
		c.didSeek = true
		c.atEnd = true
	}
	empty := c.tree.isLeaf() && len(c.tree.items) == 0
	if c.atEnd {
		c.position = zero
		c.atEnd = empty
		if !empty {
			c.stack = append(c.stack, stackEntry[T, S, D]{
				tree:     c.tree,
				index:    len(c.tree.childSummaries),
				position: zero.AddSummary(c.tree.summary),
			})
		}
	}
	descending := false
	for len(c.stack) > 0 {
		if len(c.stack) >= 2 {
			c.position = c.stack[len(c.stack)-2].position
		} else {
			c.position = zero
		}
		e := &c.stack[len(c.stack)-1]
		if !descending {
			if e.index == 0 {
				c.stack = c.stack[:len(c.stack)-1]
				continue
			}
			e.index--
		}
		for _, s := range e.tree.childSummaries[:e.index] {
			c.position = c.position.AddSummary(s)
		}
		e.position = c.position
		descending = true // filter_node is `|_| true` for prev()
		if e.tree.isLeaf() {
			break
		}
		child := e.tree.childTrees[e.index]
		c.stack = append(c.stack, stackEntry[T, S, D]{tree: child, index: len(child.childSummaries) - 1})
	}
}

// Next moves to the next item (cursor.rs:284-375).
func (c *Cursor[T, S, D]) Next() {
	descend := false
	if len(c.stack) == 0 {
		if !c.atEnd {
			var zero D
			c.stack = append(c.stack, stackEntry[T, S, D]{tree: c.tree, position: zero})
			descend = true
		}
		c.didSeek = true
	}
	for len(c.stack) > 0 {
		e := &c.stack[len(c.stack)-1]
		var sub *node[T, S]
		if !e.tree.isLeaf() {
			if !descend {
				e.index++
				e.position = c.position
			}
			if e.index < len(e.tree.childTrees) {
				sub = e.tree.childTrees[e.index]
			}
		} else {
			if !descend {
				s := e.tree.childSummaries[e.index]
				e.index++
				e.position = e.position.AddSummary(s)
				c.position = c.position.AddSummary(s)
			}
			if e.index < len(e.tree.childSummaries) {
				return
			}
		}
		if sub != nil {
			descend = true
			c.stack = append(c.stack, stackEntry[T, S, D]{tree: sub, position: c.position})
		} else {
			descend = false
			c.stack = c.stack[:len(c.stack)-1]
		}
	}
	c.atEnd = len(c.stack) == 0
}

// Seek resets the cursor and seeks to target with bias; it reports whether
// the target was found exactly (cursor.rs:391-398).
func (c *Cursor[T, S, D]) Seek(target SeekTarget[D], bias Bias) bool {
	c.reset()
	return c.seekInternal(target, bias, nil)
}

// SeekForward seeks from the current position (cursor.rs:400-406).
func (c *Cursor[T, S, D]) SeekForward(target SeekTarget[D], bias Bias) bool {
	return c.seekInternal(target, bias, nil)
}

// Slice advances to target and returns the traversed items as a tree
// (cursor.rs:408-422).
func (c *Cursor[T, S, D]) Slice(end SeekTarget[D], bias Bias) SumTree[T, S] {
	agg := &sliceAggregate[T, S]{}
	c.seekInternal(end, bias, agg)
	return agg.tree
}

// Suffix returns every item from the cursor to the end (cursor.rs:424-427).
func (c *Cursor[T, S, D]) Suffix() SumTree[T, S] {
	return c.Slice(endTarget[D]{}, Right)
}

// CursorSummary advances to end and returns the traversed items measured in
// dimension Out (`cursor.summary::<_, Out>`, cursor.rs:429-438).
func CursorSummary[Out Dimension[S, Out], T Item[S], S Summary[S], D Dimension[S, D]](c *Cursor[T, S, D], end SeekTarget[D], bias Bias) Out {
	agg := &summaryAggregate[T, S, Out]{}
	c.seekInternal(end, bias, agg)
	return agg.out
}

// seekInternal is seek_internal (cursor.rs:440-547).
func (c *Cursor[T, S, D]) seekInternal(target SeekTarget[D], bias Bias, agg aggregate[T, S]) bool {
	if target.Cmp(c.position) < 0 {
		panic("cannot seek backward")
	}
	if !c.didSeek {
		var zero D
		c.didSeek = true
		c.stack = append(c.stack, stackEntry[T, S, D]{tree: c.tree, position: zero})
	}
	ascending := false
outer:
	for len(c.stack) > 0 {
		e := &c.stack[len(c.stack)-1]
		if !e.tree.isLeaf() {
			if ascending {
				e.index++
				e.position = c.position
			}
			for e.index < len(e.tree.childTrees) {
				childTree, childSummary := e.tree.childTrees[e.index], e.tree.childSummaries[e.index]
				childEnd := c.position.AddSummary(childSummary)
				cmp := target.Cmp(childEnd)
				if cmp > 0 || (cmp == 0 && bias == Right) {
					c.position = childEnd
					if agg != nil {
						agg.pushTree(childTree)
					}
					e.index++
					e.position = c.position
				} else {
					c.stack = append(c.stack, stackEntry[T, S, D]{tree: childTree, position: c.position})
					ascending = false
					continue outer
				}
			}
		} else {
			if agg != nil {
				agg.beginLeaf()
			}
			for e.index < len(e.tree.items) {
				item, itemSummary := e.tree.items[e.index], e.tree.childSummaries[e.index]
				childEnd := c.position.AddSummary(itemSummary)
				cmp := target.Cmp(childEnd)
				if cmp > 0 || (cmp == 0 && bias == Right) {
					c.position = childEnd
					if agg != nil {
						agg.pushItem(item, itemSummary)
					}
					e.index++
				} else {
					if agg != nil {
						agg.endLeaf()
					}
					break outer
				}
			}
			if agg != nil {
				agg.endLeaf()
			}
		}
		c.stack = c.stack[:len(c.stack)-1]
		ascending = true
	}
	c.atEnd = len(c.stack) == 0
	end := c.position
	if bias == Left {
		if s, ok := c.ItemSummary(); ok {
			end = end.AddSummary(s)
		}
	}
	return target.Cmp(end) == 0
}

// aggregate is SeekAggregate (cursor.rs:704-719); nil stands for `()`.
type aggregate[T Item[S], S Summary[S]] interface {
	beginLeaf()
	endLeaf()
	pushItem(item T, summary S)
	pushTree(tree *node[T, S])
}

// sliceAggregate is SliceSeekAggregate (cursor.rs:721-773).
type sliceAggregate[T Item[S], S Summary[S]] struct {
	tree        SumTree[T, S]
	leafItems   []T
	leafSums    []S
	leafSummary S
}

func (a *sliceAggregate[T, S]) beginLeaf() {}

func (a *sliceAggregate[T, S]) endLeaf() {
	var zero S
	a.tree.Append(SumTree[T, S]{&node[T, S]{summary: a.leafSummary, items: a.leafItems, childSummaries: a.leafSums}})
	a.leafItems, a.leafSums, a.leafSummary = nil, nil, zero
}

func (a *sliceAggregate[T, S]) pushItem(item T, summary S) {
	a.leafItems = append(a.leafItems, item)
	a.leafSums = append(a.leafSums, summary)
	a.leafSummary = a.leafSummary.AddSummary(summary)
}

func (a *sliceAggregate[T, S]) pushTree(tree *node[T, S]) { a.tree.Append(SumTree[T, S]{tree}) }

// summaryAggregate is SummarySeekAggregate (cursor.rs:728, 775-797).
type summaryAggregate[T Item[S], S Summary[S], Out Dimension[S, Out]] struct{ out Out }

func (a *summaryAggregate[T, S, Out]) beginLeaf() {}
func (a *summaryAggregate[T, S, Out]) endLeaf()   {}

func (a *summaryAggregate[T, S, Out]) pushItem(_ T, summary S) {
	a.out = a.out.AddSummary(summary)
}

func (a *summaryAggregate[T, S, Out]) pushTree(tree *node[T, S]) {
	a.out = a.out.AddSummary(tree.summary)
}

// endTarget always compares Greater (cursor.rs:799-811).
type endTarget[D any] struct{}

func (endTarget[D]) Cmp(D) int { return 1 }
