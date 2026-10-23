package base

// Port of U:input/editor/decorations.rs: text decorations (HighlightStyle
// over a byte range, Monaco's decorations) and geometric range
// decorations (fills and frames), their owner collections and the
// interval index culling them to the visible spans.
//
// Go adaptations: the TrackedDecoration trait is the trackedDecoration[T]
// constraint (value methods returning the re-ranged copy); the
// BTreeMap<id, entries> is a slice kept in id (creation) order; the
// collection handles hold a WeakEntity[EditorState] like upstream.
// create_*_collection exist on EditorState only upstream; here they panic
// on another mode.

import (
	"slices"
	"sort"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/base/sumtree"
	"github.com/hoangvu12/gpui-go/gpui"
)

// RangeDecorationStyle is the geometric presentation of a range
// decoration (U:input/editor/decorations.rs:10-19).
type RangeDecorationStyle uint8

const (
	// RangeDecorationFrame draws a continuous one-pixel frame; the default.
	RangeDecorationFrame RangeDecorationStyle = iota
	// RangeDecorationFill fills the continuous visual range.
	RangeDecorationFill
)

// RangeDecoration is a geometric decoration over a UTF-8 byte range
// (U:input/editor/decorations.rs:21-65).
type RangeDecoration struct {
	rng   InputSelection
	style RangeDecorationStyle
	color *gpui.Hsla
}

// NewRangeDecoration is a frame in the editor foreground
// (U:input/editor/decorations.rs:30-37).
func NewRangeDecoration(rng InputSelection) RangeDecoration { return RangeDecoration{rng: rng} }

// Range is the half-open byte range (U:input/editor/decorations.rs:39-42).
func (d RangeDecoration) Range() InputSelection { return d.rng }

// Style is the paint style (U:input/editor/decorations.rs:44-47).
func (d RangeDecoration) Style() RangeDecorationStyle { return d.style }

// Color is the application color override; false uses the editor
// fallback (U:input/editor/decorations.rs:49-52).
func (d RangeDecoration) Color() (gpui.Hsla, bool) {
	if d.color == nil {
		return gpui.Hsla{}, false
	}
	return *d.color, true
}

// WithStyle chooses a fill or frame (U:input/editor/decorations.rs:54-58).
func (d RangeDecoration) WithStyle(style RangeDecorationStyle) RangeDecoration {
	d.style = style
	return d
}

// WithColor overrides the editor foreground fallback
// (U:input/editor/decorations.rs:60-64).
func (d RangeDecoration) WithColor(color gpui.Hsla) RangeDecoration {
	d.color = &color
	return d
}

// TextDecoration is a presentation style over a UTF-8 byte range
// (U:input/editor/decorations.rs:67-82).
type TextDecoration struct {
	Range InputSelection
	Style gpui.HighlightStyle
}

// NewTextDecoration is TextDecoration::new (U:input/editor/decorations.rs:77-81).
func NewTextDecoration(rng InputSelection, style gpui.HighlightStyle) TextDecoration {
	return TextDecoration{Range: rng, Style: style}
}

// trackedDecoration is TrackedDecoration: text and geometric decorations
// share normalization and edit affinity (U:input/editor/decorations.rs:
// 221-243).
type trackedDecoration[T any] interface {
	trackedRange() InputSelection
	withTrackedRange(InputSelection) T
}

func (d TextDecoration) trackedRange() InputSelection { return d.Range }
func (d TextDecoration) withTrackedRange(r InputSelection) TextDecoration {
	d.Range = r
	return d
}
func (d RangeDecoration) trackedRange() InputSelection { return d.rng }
func (d RangeDecoration) withTrackedRange(r InputSelection) RangeDecoration {
	d.rng = r
	return d
}

// decorationIndex is a balanced interval index over insertion-order
// entries; each midpoint keeps its subtree's maximum end
// (U:input/editor/decorations.rs:245-305).
type decorationIndex struct {
	indices []int
	maxEnds []int
}

func newDecorationIndex[T trackedDecoration[T]](decorations []T) decorationIndex {
	indices := make([]int, len(decorations))
	for i := range indices {
		indices[i] = i
	}
	sort.Slice(indices, func(a, b int) bool {
		ra, rb := decorations[indices[a]].trackedRange().Start, decorations[indices[b]].trackedRange().Start
		if ra != rb {
			return ra < rb
		}
		return indices[a] < indices[b]
	})
	idx := decorationIndex{indices: indices, maxEnds: make([]int, len(indices))}
	decorationIndexBuild(&idx, decorations, 0, len(decorations))
	return idx
}

func decorationIndexBuild[T trackedDecoration[T]](idx *decorationIndex, decorations []T, start, end int) int {
	if start >= end {
		return 0
	}
	mid := start + (end-start)/2
	maxEnd := max(decorations[idx.indices[mid]].trackedRange().End,
		decorationIndexBuild(idx, decorations, start, mid),
		decorationIndexBuild(idx, decorations, mid+1, end))
	idx.maxEnds[mid] = maxEnd
	return maxEnd
}

// decorationIndexQuery appends the entries intersecting rng and returns
// the visited node count (U:input/editor/decorations.rs:279-304).
func decorationIndexQuery[T trackedDecoration[T]](idx *decorationIndex, decorations []T, start, end int, rng InputSelection, matches *[]int) int {
	if start >= end || rng.IsEmpty() {
		return 0
	}
	mid := start + (end-start)/2
	if idx.maxEnds[mid] <= rng.Start {
		return 1
	}
	visited := 1 + decorationIndexQuery(idx, decorations, start, mid, rng, matches)
	ix := idx.indices[mid]
	candidate := decorations[ix].trackedRange()
	if candidate.Start < rng.End {
		if candidate.End > rng.Start {
			*matches = append(*matches, ix)
		}
		visited += decorationIndexQuery(idx, decorations, mid+1, end, rng, matches)
	}
	return visited
}

// decorationCollectionID is DecorationCollectionId.
type decorationCollectionID int

// decorationEntries is one collection with its index
// (U:input/editor/decorations.rs:307-321).
type decorationEntries[T trackedDecoration[T]] struct {
	id          decorationCollectionID
	decorations []T
	index       decorationIndex
}

func newDecorationEntries[T trackedDecoration[T]](id decorationCollectionID, decorations []T) *decorationEntries[T] {
	return &decorationEntries[T]{id: id, decorations: decorations, index: newDecorationIndex(decorations)}
}

// decorationCollections is DecorationCollections
// (U:input/editor/decorations.rs:323-335).
type decorationCollections[T trackedDecoration[T]] struct {
	entries []*decorationEntries[T]
	nextID  int
}

func (c *decorationCollections[T]) entry(id decorationCollectionID) *decorationEntries[T] {
	for _, e := range c.entries {
		if e.id == id {
			return e
		}
	}
	return nil
}

// create is U:input/editor/decorations.rs:338-343.
func (c *decorationCollections[T]) create(decorations []T) decorationCollectionID {
	id := decorationCollectionID(c.nextID)
	c.nextID++
	c.entries = append(c.entries, newDecorationEntries(id, decorations))
	return id
}

// set is U:input/editor/decorations.rs:345-351.
func (c *decorationCollections[T]) set(id decorationCollectionID, decorations []T) bool {
	e := c.entry(id)
	if e == nil {
		return false
	}
	*e = *newDecorationEntries(id, decorations)
	return true
}

// append is U:input/editor/decorations.rs:353-360.
func (c *decorationCollections[T]) append(id decorationCollectionID, decorations []T) bool {
	e := c.entry(id)
	if e == nil {
		return false
	}
	e.decorations = append(e.decorations, decorations...)
	e.index = newDecorationIndex(e.decorations)
	return true
}

// get is U:input/editor/decorations.rs:362-366.
func (c *decorationCollections[T]) get(id decorationCollectionID) ([]T, bool) {
	e := c.entry(id)
	if e == nil {
		return nil, false
	}
	return e.decorations, true
}

// remove is entries.remove(&id) (U:input/editor/decorations.rs:191-201).
func (c *decorationCollections[T]) remove(id decorationCollectionID) bool {
	for i, e := range c.entries {
		if e.id == id {
			c.entries = slices.Delete(c.entries, i, i+1)
			return true
		}
	}
	return false
}

// adjustForEdit slides every range along with an edit and drops emptied
// ones; anchor transforms are monotone, so the start order is kept
// (U:input/editor/decorations.rs:368-393).
func (c *decorationCollections[T]) adjustForEdit(edited InputSelection, insertedLen int) {
	for _, e := range c.entries {
		n := len(e.decorations)
		if n == 0 || e.index.maxEnds[n/2] <= edited.Start {
			continue
		}
		remap := make([]int, 0, n)
		retained := 0
		kept := e.decorations[:0]
		for _, d := range e.decorations {
			d = d.withTrackedRange(adjustRangeForEdit(d.trackedRange(), edited, insertedLen))
			if d.trackedRange().IsEmpty() {
				remap = append(remap, -1)
				continue
			}
			remap = append(remap, retained)
			retained++
			kept = append(kept, d)
		}
		e.decorations = kept
		indices := e.index.indices[:0]
		for _, ix := range e.index.indices {
			if remap[ix] >= 0 {
				indices = append(indices, remap[ix])
			}
		}
		e.index.indices = indices
		e.index.maxEnds = make([]int, retained)
		decorationIndexBuild(&e.index, e.decorations, 0, retained)
	}
}

// clear empties every collection (U:input/editor/decorations.rs:395-399).
func (c *decorationCollections[T]) clear() {
	for _, e := range c.entries {
		*e = *newDecorationEntries[T](e.id, nil)
	}
}

// layers is iter(): each collection's decorations in creation order
// (U:input/editor/decorations.rs:401-405).
func (c *decorationCollections[T]) layers() [][]T {
	out := make([][]T, 0, len(c.entries))
	for _, e := range c.entries {
		out = append(out, e.decorations)
	}
	return out
}

// intersecting queries the visible buffer spans, keeping owner and item
// order (U:input/editor/decorations.rs:407-427).
func (c *decorationCollections[T]) intersecting(ranges []InputSelection) []T {
	var result []T
	for _, e := range c.entries {
		var matches []int
		for _, r := range ranges {
			decorationIndexQuery(&e.index, e.decorations, 0, len(e.decorations), r, &matches)
		}
		sort.Ints(matches)
		matches = slices.Compact(matches)
		for _, ix := range matches {
			result = append(result, e.decorations[ix])
		}
	}
	return result
}

// adjustRangeForEdit is U:input/editor/decorations.rs:430-474: insertion
// at an edge does not grow the range, inside it does; a replacement clips
// overlapping anchors.
func adjustRangeForEdit(rng, edited InputSelection, insertedLen int) InputSelection {
	removedLen := max(edited.End-edited.Start, 0)
	shift := func(offset int) int { return max(offset+insertedLen-removedLen, 0) }
	if edited.IsEmpty() {
		start, end := rng.Start, rng.End
		if rng.Start >= edited.Start {
			start = shift(rng.Start)
		}
		if rng.End > edited.Start {
			end = shift(rng.End)
		}
		return InputSelection{Start: start, End: end}
	}
	insertedEnd := edited.Start + insertedLen
	var start, end int
	switch {
	case rng.Start <= edited.Start:
		start = rng.Start
	case rng.Start >= edited.End:
		start = shift(rng.Start)
	default:
		start = edited.Start
	}
	switch {
	case rng.End <= edited.Start:
		end = rng.End
	case rng.End >= edited.End:
		end = shift(rng.End)
	default:
		end = insertedEnd
	}
	return InputSelection{Start: start, End: end}
}

// normalizeDecorations drops reversed or empty ranges and clips to UTF-8
// boundaries (U:input/editor/decorations.rs:476-494).
func normalizeDecorations[T trackedDecoration[T]](text *rope.Rope, decorations []T) []T {
	ext := RopeExtOf(text)
	out := make([]T, 0, len(decorations))
	for _, d := range decorations {
		r := d.trackedRange()
		// Reject reversed ranges before clipping.
		if r.Start >= r.End {
			continue
		}
		clipped := InputSelection{Start: ext.ClipOffset(r.Start, sumtree.Left), End: ext.ClipOffset(r.End, sumtree.Right)}
		if clipped.Start >= clipped.End {
			continue
		}
		out = append(out, d.withTrackedRange(clipped))
	}
	return out
}

// TextDecorationCollection is an independently managed collection of text
// decorations; clones address the same collection
// (U:input/editor/decorations.rs:87-150).
type TextDecorationCollection struct {
	state gpui.WeakEntity[EditorState]
	id    decorationCollectionID
}

// Set replaces every decoration of the collection
// (U:input/editor/decorations.rs:98-109).
func (c TextDecorationCollection) Set(decorations []TextDecoration, app *gpui.App) {
	_ = c.state.TryUpdate(app, func(s *EditorState, cx *gpui.Context[EditorState]) {
		if s.editorExtras().decorations.set(c.id, normalizeDecorations(&s.text, decorations)) {
			cx.Notify()
		}
	})
}

// Append adds decorations to the collection
// (U:input/editor/decorations.rs:111-122).
func (c TextDecorationCollection) Append(decorations []TextDecoration, app *gpui.App) {
	_ = c.state.TryUpdate(app, func(s *EditorState, cx *gpui.Context[EditorState]) {
		if s.editorExtras().decorations.append(c.id, normalizeDecorations(&s.text, decorations)) {
			cx.Notify()
		}
	})
}

// Clear removes every decoration (U:input/editor/decorations.rs:124-130).
func (c TextDecorationCollection) Clear(app *gpui.App) { c.Set(nil, app) }

// GetRanges is the collection's byte ranges
// (U:input/editor/decorations.rs:132-149).
func (c TextDecorationCollection) GetRanges(app *gpui.App) []InputSelection {
	out, _ := c.state.TryRead(app, func(s *EditorState, _ *gpui.App) []InputSelection {
		ds, _ := s.editorExtras().decorations.get(c.id)
		ranges := make([]InputSelection, 0, len(ds))
		for _, d := range ds {
			ranges = append(ranges, d.Range)
		}
		return ranges
	})
	return out
}

// RangeDecorationCollection is an independently managed collection of
// geometric decorations; dropping a handle does not clear it, operations
// on a disposed collection or a dropped editor do nothing
// (U:input/editor/decorations.rs:152-219).
type RangeDecorationCollection struct {
	state gpui.WeakEntity[EditorState]
	id    decorationCollectionID
}

// Set replaces only this owner's decorations
// (U:input/editor/decorations.rs:164-172).
func (c RangeDecorationCollection) Set(decorations []RangeDecoration, app *gpui.App) {
	_ = c.state.TryUpdate(app, func(s *EditorState, cx *gpui.Context[EditorState]) {
		if s.editorExtras().rangeDecos.set(c.id, normalizeDecorations(&s.text, decorations)) {
			cx.Notify()
		}
	})
}

// Append adds decorations, keeping their paint order
// (U:input/editor/decorations.rs:174-182).
func (c RangeDecorationCollection) Append(decorations []RangeDecoration, app *gpui.App) {
	_ = c.state.TryUpdate(app, func(s *EditorState, cx *gpui.Context[EditorState]) {
		if s.editorExtras().rangeDecos.append(c.id, normalizeDecorations(&s.text, decorations)) {
			cx.Notify()
		}
	})
}

// Clear empties the collection without invalidating its handles
// (U:input/editor/decorations.rs:184-187).
func (c RangeDecorationCollection) Clear(app *gpui.App) { c.Set(nil, app) }

// Dispose releases the collection, invalidating every handle
// (U:input/editor/decorations.rs:189-202).
func (c RangeDecorationCollection) Dispose(app *gpui.App) {
	_ = c.state.TryUpdate(app, func(s *EditorState, cx *gpui.Context[EditorState]) {
		if s.editorExtras().rangeDecos.remove(c.id) {
			cx.Notify()
		}
	})
}

// GetRanges is the tracked byte ranges in insertion order
// (U:input/editor/decorations.rs:204-218).
func (c RangeDecorationCollection) GetRanges(app *gpui.App) []InputSelection {
	out, _ := c.state.TryRead(app, func(s *EditorState, _ *gpui.App) []InputSelection {
		ds, _ := s.editorExtras().rangeDecos.get(c.id)
		ranges := make([]InputSelection, 0, len(ds))
		for _, d := range ds {
			ranges = append(ranges, d.rng)
		}
		return ranges
	})
	return out
}

// CreateRangeDecorationsCollection creates an independently owned
// collection of geometric decorations; ranges track edits like text
// decorations, folding changes only their projection
// (U:input/editor/decorations.rs:496-524). EditorState only.
func (s *InputBaseState[M]) CreateRangeDecorationsCollection(decorations []RangeDecoration, cx *gpui.Context[InputBaseState[M]]) RangeDecorationCollection {
	s.requireEditorMode("CreateRangeDecorationsCollection")
	id := s.editorExtras().rangeDecos.create(normalizeDecorations(&s.text, decorations))
	cx.Notify()
	return RangeDecorationCollection{state: editorWeakSelf(cx), id: id}
}

// CreateDecorationsCollection creates an independently managed collection
// of text decorations; the first collection wins when overlapping
// decorations set the same property (U:input/editor/decorations.rs:
// 526-554). EditorState only.
func (s *InputBaseState[M]) CreateDecorationsCollection(decorations []TextDecoration, cx *gpui.Context[InputBaseState[M]]) TextDecorationCollection {
	s.requireEditorMode("CreateDecorationsCollection")
	id := s.editorExtras().decorations.create(normalizeDecorations(&s.text, decorations))
	cx.Notify()
	return TextDecorationCollection{state: editorWeakSelf(cx), id: id}
}

// editorWeakSelf is cx.entity().downgrade() for an EditorState context.
func editorWeakSelf[M InputModeKind](cx *gpui.Context[InputBaseState[M]]) gpui.WeakEntity[EditorState] {
	return any(cx.Entity().Downgrade()).(gpui.WeakEntity[EditorState])
}
