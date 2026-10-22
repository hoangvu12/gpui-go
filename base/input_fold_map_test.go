package base

// Ported test of U:input/editor/display_map/fold_map.rs:378-446.

import (
	"fmt"
	"sort"
	"testing"
)

// denseProjection is the per-row tables the projection used to be stored
// as (U:input/editor/display_map/fold_map.rs:382-397).
func denseProjection(wrapRowCount int, hidden [][2]int) ([]int, []*int) {
	var visible []int
	displayRows := make([]*int, wrapRowCount)
	for wrapRow := range displayRows {
		isHidden := false
		for _, r := range hidden {
			if wrapRow >= r[0] && wrapRow < r[1] {
				isHidden = true
			}
		}
		if !isHidden {
			n := len(visible)
			displayRows[wrapRow] = &n
			visible = append(visible, wrapRow)
		}
	}
	return visible, displayRows
}

func optInt(v int, ok bool) string {
	if !ok {
		return "None"
	}
	return fmt.Sprint(v)
}

func optPtr(p *int) string {
	if p == nil {
		return "None"
	}
	return fmt.Sprint(*p)
}

// U:input/editor/display_map/fold_map.rs:399-445
func TestFoldMapHiddenRunsMatchTheDenseProjection(t *testing.T) {
	const wrapRowCount = 10
	cases := [][][2]int{
		{},
		{{0, 3}},
		{{2, 5}},
		{{7, 10}},
		{{2, 4}, {4, 6}},
		{{1, 5}, {3, 8}},
		{{6, 9}, {1, 3}},
		{{0, 2}, {3, 4}, {8, 20}},
		{{0, 10}},
	}
	for _, hidden := range cases {
		m := newFoldMap()
		m.folded = append(m.folded, NewFoldRange(0, 1))
		m.setHiddenRows(wrapRowCount, append([][2]int(nil), hidden...))
		visible, displayRows := denseProjection(wrapRowCount, hidden)

		if got := m.displayRowCount(); got != len(visible) {
			t.Fatalf("%v: display row count = %d, want %d", hidden, got, len(visible))
		}
		for wrapRow := 0; wrapRow < wrapRowCount+2; wrapRow++ {
			var want *int
			if wrapRow < len(displayRows) {
				want = displayRows[wrapRow]
			}
			if got := optInt(m.wrapRowToDisplayRow(wrapRow)); got != optPtr(want) {
				t.Errorf("%v, wrap row %d: display row = %s, want %s", hidden, wrapRow, got, optPtr(want))
			}
			nearest := sort.SearchInts(visible, wrapRow)
			if nearest == len(visible) || visible[nearest] != wrapRow {
				nearest = max(nearest-1, 0)
			}
			if got := m.nearestVisibleDisplayRow(wrapRow); got != nearest {
				t.Errorf("%v, wrap row %d: nearest visible = %d, want %d", hidden, wrapRow, got, nearest)
			}
		}
		for displayRow := 0; displayRow < wrapRowCount+2; displayRow++ {
			want := "None"
			if displayRow < len(visible) {
				want = fmt.Sprint(visible[displayRow])
			}
			if got := optInt(m.displayRowToWrapRow(displayRow)); got != want {
				t.Errorf("%v, display row %d: wrap row = %s, want %s", hidden, displayRow, got, want)
			}
		}
	}
}
