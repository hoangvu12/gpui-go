package base

// An unstyled pagination navigation landmark and its controlled state
// (U:pagination.rs).
//
// Go adaptation: PaginationItem's two variants (Page(usize),
// Ellipsis(Range<usize>)) are one struct with an Ellipsis flag; the
// ellipsis range is [Start, End) like Rust's Range. Page numbers are
// one-based ints. Optional page results are (page, ok) pairs.
//
// Display (map gap 2b): the Pagination root
// (U:pagination.rs:115) is a plain div upstream, display Block under
// crates.io gpui 0.2.2 (the pin defaults to Flex, R:style.rs:1024-1027),
// so each such base is built with Block() before the caller's style
// refines over it: the upstream default layout holds and a caller's
// Flex() still wins.

import "github.com/hoangvu12/gpui-go/gpui"

// PageChangeHandler handles a requested page change
// (U:pagination.rs:10).
type PageChangeHandler = func(page int, w *gpui.Window, app *gpui.App)

// PaginationItem is a visible destination in a pagination control
// (U:pagination.rs:12-17).
type PaginationItem struct {
	// Ellipsis selects the Ellipsis variant; otherwise the item is Page.
	Ellipsis bool
	// Page is the Page variant's page number.
	Page int
	// Start and End are the Ellipsis variant's skipped pages [Start, End).
	Start, End int
}

// PaginationItemPage is PaginationItem::Page.
func PaginationItemPage(page int) PaginationItem { return PaginationItem{Page: page} }

// PaginationItemEllipsis is PaginationItem::Ellipsis(start..end).
func PaginationItemEllipsis(start, end int) PaginationItem {
	return PaginationItem{Ellipsis: true, Start: start, End: end}
}

// PaginationState is the controlled behavior shared by every part of a
// pagination control (U:pagination.rs:19-27). It is a value: builders
// return a modified copy, like the Rust Clone + builder chain.
type PaginationState struct {
	currentPage  int
	totalPages   int
	visiblePages int
	disabled     bool
	onChange     PageChangeHandler
}

// NewPaginationState clamps the total to at least one page and the
// current page into 1..=total (U:pagination.rs:30-39).
func NewPaginationState(currentPage, totalPages int) PaginationState {
	totalPages = max(totalPages, 1)
	return PaginationState{
		currentPage:  min(max(currentPage, 1), totalPages),
		totalPages:   totalPages,
		visiblePages: 5,
	}
}

// VisiblePages sets the visible page count, at least 5
// (U:pagination.rs:41-44).
func (s PaginationState) VisiblePages(visiblePages int) PaginationState {
	s.visiblePages = max(visiblePages, 5)
	return s
}

// Disabled sets whether page changes are ignored (U:pagination.rs:46-49).
func (s PaginationState) Disabled(disabled bool) PaginationState {
	s.disabled = disabled
	return s
}

// OnChange handles a requested page change (U:pagination.rs:51-59). It
// is a model-level request that may come from the keyboard or from
// application code, so it carries no pointer event.
func (s PaginationState) OnChange(handler PageChangeHandler) PaginationState {
	s.onChange = handler
	return s
}

// CurrentPage returns the current page (U:pagination.rs:61-63).
func (s PaginationState) CurrentPage() int { return s.currentPage }

// TotalPages returns the page count (U:pagination.rs:65-67).
func (s PaginationState) TotalPages() int { return s.totalPages }

// IsDisabled reports the disabled flag (U:pagination.rs:69-71).
func (s PaginationState) IsDisabled() bool { return s.disabled }

// HasOnChange reports whether a change handler is set
// (U:pagination.rs:73-75).
func (s PaginationState) HasOnChange() bool { return s.onChange != nil }

// PreviousPage returns the previous page when enabled and not on the
// first page (U:pagination.rs:77-79).
func (s PaginationState) PreviousPage() (int, bool) {
	if !s.disabled && s.currentPage > 1 {
		return s.currentPage - 1, true
	}
	return 0, false
}

// NextPage returns the next page when enabled and not on the last page
// (U:pagination.rs:81-83).
func (s PaginationState) NextPage() (int, bool) {
	if !s.disabled && s.currentPage < s.totalPages {
		return s.currentPage + 1, true
	}
	return 0, false
}

// RequestPage requests a controlled page change after the disabled,
// bounds and current-page guards (U:pagination.rs:85-95).
func (s PaginationState) RequestPage(page int, w *gpui.Window, app *gpui.App) {
	if s.disabled || page == s.currentPage || page < 1 || page > s.totalPages {
		return
	}
	if s.onChange != nil {
		s.onChange(page, w, app)
	}
}

// Items returns the visible destinations (U:pagination.rs:97-99).
func (s PaginationState) Items() []PaginationItem {
	return paginationCalculateItems(s.currentPage, s.totalPages, s.visiblePages)
}

// Pagination is an unstyled pagination navigation landmark
// (U:pagination.rs:102-110).
type Pagination struct {
	gpui.Component[*Pagination]

	base               *gpui.DivElement
	state              PaginationState
	accessibilityLabel string
}

// NewPagination creates a pagination landmark labeled "Pagination"
// (U:pagination.rs:113-121).
func NewPagination[I gpui.IntoElementID](id I, state PaginationState) *Pagination {
	base := gpui.Div().Block().ID(id)
	return gpui.BindComponent(&Pagination{base: base, state: state, accessibilityLabel: "Pagination"}, base)
}

// AccessibilityLabel sets the landmark's accessible name
// (U:pagination.rs:123-126).
func (p *Pagination) AccessibilityLabel(label string) *Pagination {
	p.accessibilityLabel = label
	return p
}

// State returns the controlled state (U:pagination.rs:128-130).
func (p *Pagination) State() PaginationState { return p.state }

// RenderOnce implements gpui.RenderOnce (U:pagination.rs:153-161).
func (p *Pagination) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return p.render(w, app).IntoElement()
}

func (p *Pagination) render(_ *gpui.Window, _ *gpui.App) *gpui.DivElement {
	// The bar's items are buttons, like the UA stylesheet's button
	// rule (user-select: none, html.css:850/:904; browser-core 20's
	// default-on selection opts them back out through inheritance).
	d := p.base.SelectNone().Role(gpui.RoleNavigation).AriaLabel(p.accessibilityLabel).Extend(p.TakeChildren())
	style := p.Style()
	return StyledExtOf(d).RefineStyle(&style)
}

// paginationCalculateItems is calculate_items (U:pagination.rs:163-201).
func paginationCalculateItems(current, total, maxVisible int) []PaginationItem {
	if total <= 1 {
		return []PaginationItem{}
	}
	maxVisible = max(maxVisible, 5)
	if total <= maxVisible {
		pages := make([]PaginationItem, 0, total)
		for page := 1; page <= total; page++ {
			pages = append(pages, PaginationItemPage(page))
		}
		return pages
	}

	pages := []PaginationItem{PaginationItemPage(1)}
	sidePages := (maxVisible - 3) / 2
	var start int
	switch {
	case current <= sidePages+1:
		start = 2
	case current > total-sidePages-1:
		start = total - sidePages - 1
	default:
		start = current - sidePages
	}
	if start > 2 {
		pages = append(pages, PaginationItemEllipsis(2, start))
	}

	var end int
	switch {
	case current >= total-sidePages:
		end = total - 1
	case current <= sidePages+1:
		end = sidePages + 2
	default:
		end = current + sidePages
	}
	for page := start; page <= end; page++ {
		pages = append(pages, PaginationItemPage(page))
	}
	if end < total-1 {
		pages = append(pages, PaginationItemEllipsis(end+1, total))
	}
	return append(pages, PaginationItemPage(total))
}
