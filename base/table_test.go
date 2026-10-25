package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of U:table.rs tests (3/3).

func tableWantIndex(t *testing.T, name string, got gpui.Optional[int], want int) {
	t.Helper()
	if v, ok := got.Get(); !ok || v != want {
		t.Fatalf("%s = %v (set %v), want %d", name, v, ok, want)
	}
}

// U:table.rs:339-352
func TestTableProjectsItsAccessibleName(t *testing.T) {
	p := progressA11y(t, func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
		return NewTable("positions").AccessibilityLabel("Open positions").render(w, app)
	})
	if p.spec.Label != "Open positions" || p.role != gpui.RoleTable {
		t.Fatalf("node = %+v", p)
	}
}

// U:table.rs:354-379
func TestTableRowAndCellsProjectAccessibilityIndices(t *testing.T) {
	row := progressA11y(t, func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
		return NewTableRow("row", 3).render(w, app)
	})
	tableWantIndex(t, "row index", row.spec.RowIndex, 3)
	head := progressA11y(t, func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
		return NewTableHead("head", 2).render(w, app)
	})
	tableWantIndex(t, "head column index", head.spec.ColumnIndex, 2)
	cell := progressA11y(t, func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
		return NewTableCell("cell", 4).render(w, app)
	})
	tableWantIndex(t, "cell column index", cell.spec.ColumnIndex, 4)
}

// U:table.rs:404-423
func TestTableForwardsChildrenInstanceStyleAndPointerInteraction(t *testing.T) {
	clicks := 0
	cx := drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		table := NewTable("table").
			DebugSelector("base-table").
			W(gpui.Px(120)).
			H(gpui.Px(60)).
			OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) { clicks++ }).
			Child(NewTableBody("body").Child(
				NewTableRow("row", 1).Child(NewTableCell("cell", 1).Child(
					gpui.Div().Flex().DebugSelector("table-child").Size(gpui.Px(20))))))
		return elementOf(t, table)
	})

	table := debugBounds(t, cx, "base-table")
	if table.Size.Width != 120 || table.Size.Height != 60 {
		t.Fatalf("table = %v, want 120x60", table.Size)
	}
	child := debugBounds(t, cx, "table-child")
	if child.Size.Width != 20 || child.Size.Height != 20 {
		t.Fatalf("child = %v, want 20x20", child.Size)
	}

	clickAt(cx, 10, 10)
	clickAt(cx, 100, 50)
	if clicks != 2 {
		t.Fatalf("clicks = %d, want 2", clicks)
	}
}
