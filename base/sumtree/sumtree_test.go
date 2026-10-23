package sumtree

import (
	"cmp"
	"math/rand"
	"os"
	"slices"
	"testing"
)

// TestMain uses zed's cfg(test) TREE_BASE of 2 (sum_tree.rs:12-13).
func TestMain(m *testing.M) {
	treeBase = 2
	os.Exit(m.Run())
}

// Test fixtures from zed-sum-tree/src/sum_tree.rs:1361-1443.

type u8 uint8

type integersSummary struct {
	count, sum   int
	containsEven bool
	max          u8
}

func (s integersSummary) AddSummary(o integersSummary) integersSummary {
	return integersSummary{s.count + o.count, s.sum + o.sum, s.containsEven || o.containsEven, max(s.max, o.max)}
}

func (v u8) Summary() integersSummary {
	return integersSummary{count: 1, sum: int(v), containsEven: v&1 == 0, max: v}
}

type count int

func (c count) AddSummary(s integersSummary) count { return c + count(s.count) }
func (c count) Cmp(loc count) int                  { return cmp.Compare(c, loc) }

type sumDim int

func (d sumDim) AddSummary(s integersSummary) sumDim { return d + sumDim(s.sum) }

// countOf seeks a cursor whose dimension is the summary itself
// (`impl SeekTarget<IntegersSummary, IntegersSummary> for Count`,
// sum_tree.rs:1429-1433). Go allows one Cmp per type, so it is a separate
// type from the `count` dimension.
type countOf int

func (c countOf) Cmp(loc integersSummary) int { return cmp.Compare(int(c), loc.count) }

type tree = SumTree[u8, integersSummary]

func items(t tree) []u8 {
	if it := t.Items(); it != nil {
		return it
	}
	return []u8{}
}

func eqItems(t *testing.T, got, want []u8) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("items %v want %v", got, want)
	}
}

func rangeU8(lo, hi int) []u8 {
	var out []u8
	for i := lo; i < hi; i++ {
		out = append(out, u8(i))
	}
	return out
}

// sum_tree.rs:945-955 test_extend_and_push_tree
func TestExtendAndPushTree(t *testing.T) {
	var t1, t2 tree
	t1.Extend(rangeU8(0, 20))
	t2.Extend(rangeU8(50, 100))
	t1.Append(t2)
	eqItems(t, items(t1), append(rangeU8(0, 20), rangeU8(50, 100)...))
}

// sum_tree.rs:1344-1359 test_from_iter
func TestFromIter(t *testing.T) {
	eqItems(t, items(FromIter[u8](rangeU8(0, 100))), rangeU8(0, 100))
	eqItems(t, items(FromIter[u8]([]u8{1})), []u8{1})
}

type cursorState struct {
	item, prev, next *u8
	start            int
}

func opt(v u8, ok bool) *u8 {
	if !ok {
		return nil
	}
	return &v
}

func checkCursor(t *testing.T, c *Cursor[u8, integersSummary, integersSummary], want cursorState) {
	t.Helper()
	got := cursorState{opt(c.Item()), opt(c.PrevItem()), opt(c.NextItem()), c.Start().sum}
	ptr := func(p *u8) any {
		if p == nil {
			return nil
		}
		return *p
	}
	if ptr(got.item) != ptr(want.item) || ptr(got.prev) != ptr(want.prev) || ptr(got.next) != ptr(want.next) || got.start != want.start {
		t.Fatalf("cursor item=%v prev=%v next=%v start=%d; want item=%v prev=%v next=%v start=%d",
			ptr(got.item), ptr(got.prev), ptr(got.next), got.start, ptr(want.item), ptr(want.prev), ptr(want.next), want.start)
	}
}

func p(v u8) *u8 { return &v }

// sum_tree.rs:1134-1321 test_cursor
func TestCursor(t *testing.T) {
	// Empty tree
	var empty tree
	c := NewCursor[integersSummary](empty)
	eqItems(t, items(c.Slice(countOf(0), Right)), []u8{})
	checkCursor(t, c, cursorState{nil, nil, nil, 0})
	c.Prev()
	checkCursor(t, c, cursorState{nil, nil, nil, 0})
	c.Next()
	checkCursor(t, c, cursorState{nil, nil, nil, 0})

	// Single-element tree
	var one tree
	one.Extend([]u8{1})
	c = NewCursor[integersSummary](one)
	eqItems(t, items(c.Slice(countOf(0), Right)), []u8{})
	checkCursor(t, c, cursorState{p(1), nil, nil, 0})
	c.Next()
	checkCursor(t, c, cursorState{nil, p(1), nil, 1})
	c.Prev()
	checkCursor(t, c, cursorState{p(1), nil, nil, 0})

	c = NewCursor[integersSummary](one)
	eqItems(t, items(c.Slice(countOf(1), Right)), []u8{1})
	checkCursor(t, c, cursorState{nil, p(1), nil, 1})
	c.Seek(countOf(0), Right)
	eqItems(t, items(c.Slice(countOf(Extent[count](one)), Right)), []u8{1})
	checkCursor(t, c, cursorState{nil, p(1), nil, 1})

	// Multiple-element tree
	var many tree
	many.Extend([]u8{1, 2, 3, 4, 5, 6})
	c = NewCursor[integersSummary](many)
	eqItems(t, items(c.Slice(countOf(2), Right)), []u8{1, 2})
	checkCursor(t, c, cursorState{p(3), p(2), p(4), 3})
	c.Next()
	checkCursor(t, c, cursorState{p(4), p(3), p(5), 6})
	c.Next()
	checkCursor(t, c, cursorState{p(5), p(4), p(6), 10})
	c.Next()
	checkCursor(t, c, cursorState{p(6), p(5), nil, 15})
	c.Next()
	c.Next()
	checkCursor(t, c, cursorState{nil, p(6), nil, 21})
	c.Prev()
	checkCursor(t, c, cursorState{p(6), p(5), nil, 15})
	c.Prev()
	checkCursor(t, c, cursorState{p(5), p(4), p(6), 10})
	c.Prev()
	checkCursor(t, c, cursorState{p(4), p(3), p(5), 6})
	c.Prev()
	checkCursor(t, c, cursorState{p(3), p(2), p(4), 3})
	c.Prev()
	checkCursor(t, c, cursorState{p(2), p(1), p(3), 1})
	c.Prev()
	checkCursor(t, c, cursorState{p(1), nil, p(2), 0})
	c.Prev()
	checkCursor(t, c, cursorState{nil, nil, p(1), 0})
	c.Next()
	checkCursor(t, c, cursorState{p(1), nil, p(2), 0})

	c = NewCursor[integersSummary](many)
	eqItems(t, items(c.Slice(countOf(Extent[count](many)), Right)), items(many))
	checkCursor(t, c, cursorState{nil, p(6), nil, 21})

	c.Seek(countOf(3), Right)
	eqItems(t, items(c.Slice(countOf(Extent[count](many)), Right)), []u8{4, 5, 6})
	checkCursor(t, c, cursorState{nil, p(6), nil, 21})

	// Seeking can bias left or right
	c.Seek(countOf(1), Left)
	if it, _ := c.Item(); it != 1 {
		t.Fatalf("seek(1, Left) item %d", it)
	}
	c.Seek(countOf(1), Right)
	if it, _ := c.Item(); it != 2 {
		t.Fatalf("seek(1, Right) item %d", it)
	}

	// Slicing without resetting starts from where the cursor is parked at.
	c.Seek(countOf(1), Right)
	eqItems(t, items(c.Slice(countOf(3), Right)), []u8{2, 3})
	eqItems(t, items(c.Slice(countOf(6), Left)), []u8{4, 5})
	eqItems(t, items(c.Slice(countOf(6), Right)), []u8{6})
}

// sum_tree.rs:957-1132 test_random, without the FilterCursor section (not
// ported, unused by Base) and par_extend (rayon; Extend is used instead).
func TestRandom(t *testing.T) {
	for seed := int64(0); seed < 100; seed++ {
		rng := rand.New(rand.NewSource(seed))
		randItems := func(n int) []u8 {
			out := make([]u8, n)
			for i := range out {
				out[i] = u8(rng.Intn(256))
			}
			return out
		}
		var tr tree
		tr.Extend(randItems(rng.Intn(10)))

		for op := 0; op < 5; op++ {
			spliceEnd := rng.Intn(int(Extent[count](tr)) + 1)
			spliceStart := rng.Intn(spliceEnd + 1)
			newItems := randItems(rng.Intn(10))
			treeEnd := Extent[count](tr)

			reference := items(tr)
			reference = append(append(append([]u8{}, reference[:spliceStart]...), newItems...), reference[spliceEnd:]...)

			c := NewCursor[count](tr)
			nt := c.Slice(count(spliceStart), Right)
			nt.Extend(newItems)
			c.Seek(count(spliceEnd), Right)
			nt.Append(c.Slice(treeEnd, Right))
			tr = nt

			eqItems(t, items(tr), reference)

			// Cursor iteration agrees with Iter (`tree.cursor::<()>().collect()`).
			var viaCursor []u8
			ic := NewCursor[count](tr)
			ic.Next()
			for it, ok := ic.Item(); ok; it, ok = ic.Item() {
				viaCursor = append(viaCursor, it)
				ic.Next()
			}
			if viaCursor == nil {
				viaCursor = []u8{}
			}
			eqItems(t, viaCursor, reference)

			beforeStart := false
			c = NewCursor[count](tr)
			startPos := rng.Intn(len(reference) + 1)
			c.Seek(count(startPos), Right)
			pos := startPos + rng.Intn(len(reference)-startPos+1)
			c.SeekForward(count(pos), Right)

			for i := 0; i < 10; i++ {
				if int(c.Start()) != pos {
					t.Fatalf("seed %d: start %d want %d", seed, c.Start(), pos)
				}
				if prev, ok := c.PrevItem(); pos > 0 {
					if !ok || prev != reference[pos-1] {
						t.Fatalf("seed %d: prev_item", seed)
					}
				} else if ok {
					t.Fatalf("seed %d: prev_item should be None", seed)
				}
				if item, ok := c.Item(); pos < len(reference) && !beforeStart {
					if !ok || item != reference[pos] {
						t.Fatalf("seed %d: item", seed)
					}
				} else if ok {
					t.Fatalf("seed %d: item should be None", seed)
				}
				next, ok := c.NextItem()
				switch {
				case beforeStart:
					if len(reference) == 0 && ok || len(reference) > 0 && (!ok || next != reference[0]) {
						t.Fatalf("seed %d: next_item before start", seed)
					}
				case pos+1 < len(reference):
					if !ok || next != reference[pos+1] {
						t.Fatalf("seed %d: next_item", seed)
					}
				default:
					if ok {
						t.Fatalf("seed %d: next_item should be None", seed)
					}
				}
				if i < 5 {
					c.Next()
					if pos < len(reference) {
						pos++
						beforeStart = false
					}
				} else {
					c.Prev()
					if pos == 0 {
						beforeStart = true
					}
					pos = max(pos-1, 0)
				}
			}
		}

		for i := 0; i < 10; i++ {
			end := rng.Intn(int(Extent[count](tr)) + 1)
			start := rng.Intn(end + 1)
			startBias, endBias := Bias(rng.Intn(2)), Bias(rng.Intn(2))
			c := NewCursor[count](tr)
			c.Seek(count(start), startBias)
			slice := c.Slice(count(end), endBias)
			c.Seek(count(start), startBias)
			s := CursorSummary[sumDim](c, count(end), endBias)
			if int(s) != slice.Summary().sum {
				t.Fatalf("seed %d: summary %d vs slice %d", seed, s, slice.Summary().sum)
			}
		}
	}
}

func TestPersistentAppend(t *testing.T) {
	var a tree
	a.Extend(rangeU8(0, 30))
	b := a // O(1) clone
	b.Push(99)
	b.Extend(rangeU8(40, 45))
	eqItems(t, items(a), rangeU8(0, 30))
	eqItems(t, items(b), append(append(rangeU8(0, 30), 99), rangeU8(40, 45)...))
	if f, _ := b.First(); f != 0 {
		t.Fatal("first")
	}
	if l, _ := b.Last(); l != 44 {
		t.Fatal("last")
	}
	if _, ok := (tree{}).First(); ok {
		t.Fatal("first of empty")
	}
	if FromItem[u8](7).Summary().sum != 7 || Left.Invert() != Right || Right.Invert() != Left {
		t.Fatal("from_item / invert")
	}
}
