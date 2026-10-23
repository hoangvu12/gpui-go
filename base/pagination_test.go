package base

import (
	"reflect"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of U:pagination.rs tests (4/4).

func paginationWantPage(t *testing.T, name string, page int, ok bool, want int, wantOK bool) {
	t.Helper()
	if ok != wantOK || (ok && page != want) {
		t.Fatalf("%s = (%d, %v), want (%d, %v)", name, page, ok, want, wantOK)
	}
}

// U:pagination.rs:210-223
func TestPaginationClampsControlledValuesAndNavigationBoundaries(t *testing.T) {
	first := NewPaginationState(0, 0)
	if first.CurrentPage() != 1 || first.TotalPages() != 1 {
		t.Fatalf("first = %d/%d, want 1/1", first.CurrentPage(), first.TotalPages())
	}
	p, ok := first.PreviousPage()
	paginationWantPage(t, "first.previous", p, ok, 0, false)
	p, ok = first.NextPage()
	paginationWantPage(t, "first.next", p, ok, 0, false)

	last := NewPaginationState(20, 10)
	if last.CurrentPage() != 10 {
		t.Fatalf("last.current = %d, want 10", last.CurrentPage())
	}
	p, ok = last.PreviousPage()
	paginationWantPage(t, "last.previous", p, ok, 9, true)
	p, ok = last.NextPage()
	paginationWantPage(t, "last.next", p, ok, 0, false)
	p, ok = last.Disabled(true).PreviousPage()
	paginationWantPage(t, "disabled.previous", p, ok, 0, false)
}

// U:pagination.rs:225-241
func TestPaginationCreatesPagesAndNavigableEllipsisRanges(t *testing.T) {
	got := NewPaginationState(5, 10).VisiblePages(7).Items()
	want := []PaginationItem{
		PaginationItemPage(1),
		PaginationItemEllipsis(2, 3),
		PaginationItemPage(3),
		PaginationItemPage(4),
		PaginationItemPage(5),
		PaginationItemPage(6),
		PaginationItemPage(7),
		PaginationItemEllipsis(8, 10),
		PaginationItemPage(10),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %+v\nwant %+v", got, want)
	}
}

// U:pagination.rs:243-264
func TestPaginationValidatesEveryPageChangeRequest(t *testing.T) {
	cx := gpui.NewTestApp().AddEmptyWindow()
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		requested := 0
		state := NewPaginationState(3, 5).OnChange(func(page int, _ *gpui.Window, _ *gpui.App) { requested = page })

		state.RequestPage(3, w, app)
		state.RequestPage(0, w, app)
		state.RequestPage(6, w, app)
		if requested != 0 {
			t.Fatalf("requested = %d, want none", requested)
		}

		state.RequestPage(4, w, app)
		if requested != 4 {
			t.Fatalf("requested = %d, want 4", requested)
		}
		requested = 0
		state.Disabled(true).RequestPage(2, w, app)
		if requested != 0 {
			t.Fatalf("disabled requested = %d, want none", requested)
		}
	})
}

// U:pagination.rs:266-280
func TestPaginationExposesANamedNavigationLandmark(t *testing.T) {
	p := progressA11y(t, func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
		return NewPagination("pagination", NewPaginationState(1, 5)).
			AccessibilityLabel("Search results pages").render(w, app)
	})
	if p.role != gpui.RoleNavigation || p.spec.Label != "Search results pages" {
		t.Fatalf("node = %+v", p)
	}
}
