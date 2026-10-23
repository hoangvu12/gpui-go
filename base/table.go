package base

// Unstyled semantic table parts (U:table.rs): Table, TableHeader,
// TableBody, TableRow, TableHead, TableCell and TableCaption. Each is a
// stateful div with its role and accessibility indices; layout is the
// application's.
//
// Go adaptation: the table_part! and table_cell! macros
// (U:table.rs:10-65, 216-274) expand to one Go type per part sharing the
// tableRender body. The macros' track_focus override forwards to the
// base (U:table.rs:48-51), which is what the promoted
// gpui.Interactive.TrackFocus already does.
//
// Display (map gap 2b): every part is a plain div upstream
// (U:table.rs:24, 82, 173, 231, 295), display Block under crates.io gpui
// 0.2.2 (the pin
// defaults to Flex, R:style.rs:1024-1027), so each such base is built
// with Block() before the caller's style refines over it: the upstream
// default layout holds and a caller's Flex() still wins.

import "github.com/hoangvu12/gpui-go/gpui"

// tableRender is the shared render body: role, extra a11y, children,
// then the instance style (U:table.rs:56-63, 138-151, 206-213, 264-272,
// 327-330).
func tableRender(base *gpui.DivElement, role *gpui.Role, style gpui.StyleRefinement, children []gpui.AnyElement, a11y func(*gpui.DivElement)) *gpui.DivElement {
	if role != nil {
		base.Role(*role)
	}
	if a11y != nil {
		a11y(base)
	}
	base.Extend(children)
	return StyledExtOf(base).RefineStyle(&style)
}

// tableBase is the shared stateful base div (`div().id(id).test_support()`).
func tableBase[I gpui.IntoElementID](id I) *gpui.DivElement {
	return TestSupport(gpui.Div().Block().ID(id))
}

func tableRole(role gpui.Role) *gpui.Role { return &role }

// Table is an unstyled semantic table root (U:table.rs:67-76).
type Table struct {
	gpui.Component[*Table]

	base               *gpui.DivElement
	rowCount           *int
	columnCount        *int
	accessibilityLabel *string
}

// NewTable creates a table root (U:table.rs:79-89).
func NewTable[I gpui.IntoElementID](id I) *Table {
	base := tableBase(id)
	return gpui.BindComponent(&Table{base: base}, base)
}

// RowCount sets the total number of rows, including rows outside the
// rendered range, so assistive technology can announce "row 5 of 200"
// for a virtualized table (U:table.rs:91-97).
func (t *Table) RowCount(count int) *Table {
	t.rowCount = &count
	return t
}

// ColumnCount sets the total number of columns, including columns
// outside the rendered range (U:table.rs:99-104).
func (t *Table) ColumnCount(count int) *Table {
	t.columnCount = &count
	return t
}

// AccessibilityLabel sets the table's accessible name
// (U:table.rs:106-110).
func (t *Table) AccessibilityLabel(label string) *Table {
	t.accessibilityLabel = &label
	return t
}

// RenderOnce implements gpui.RenderOnce (U:table.rs:138-152).
func (t *Table) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return t.render(w, app).IntoElement()
}

func (t *Table) render(_ *gpui.Window, _ *gpui.App) *gpui.DivElement {
	return tableRender(t.base, tableRole(gpui.RoleTable), t.Style(), t.TakeChildren(), func(d *gpui.DivElement) {
		if t.accessibilityLabel != nil {
			d.AriaLabel(*t.accessibilityLabel)
		}
		if t.rowCount != nil {
			d.AriaRowCount(*t.rowCount)
		}
		if t.columnCount != nil {
			d.AriaColumnCount(*t.columnCount)
		}
	})
}

// TableHeader is an unstyled table header group (U:table.rs:153-157).
type TableHeader struct {
	gpui.Component[*TableHeader]

	base *gpui.DivElement
}

// NewTableHeader creates a header group (U:table.rs:21-28).
func NewTableHeader[I gpui.IntoElementID](id I) *TableHeader {
	base := tableBase(id)
	return gpui.BindComponent(&TableHeader{base: base}, base)
}

// RenderOnce implements gpui.RenderOnce (U:table.rs:56-63).
func (t *TableHeader) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return t.render(w, app).IntoElement()
}

func (t *TableHeader) render(_ *gpui.Window, _ *gpui.App) *gpui.DivElement {
	return tableRender(t.base, tableRole(gpui.RoleRowGroup), t.Style(), t.TakeChildren(), nil)
}

// TableBody is an unstyled table body group (U:table.rs:158).
type TableBody struct {
	gpui.Component[*TableBody]

	base *gpui.DivElement
}

// NewTableBody creates a body group (U:table.rs:21-28).
func NewTableBody[I gpui.IntoElementID](id I) *TableBody {
	base := tableBase(id)
	return gpui.BindComponent(&TableBody{base: base}, base)
}

// RenderOnce implements gpui.RenderOnce (U:table.rs:56-63).
func (t *TableBody) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return t.render(w, app).IntoElement()
}

func (t *TableBody) render(_ *gpui.Window, _ *gpui.App) *gpui.DivElement {
	return tableRender(t.base, tableRole(gpui.RoleRowGroup), t.Style(), t.TakeChildren(), nil)
}

// TableRow is an unstyled semantic table row (U:table.rs:160-167).
type TableRow struct {
	gpui.Component[*TableRow]

	base     *gpui.DivElement
	rowIndex int
}

// NewTableRow creates a row with a one-based accessibility index
// (U:table.rs:169-179).
func NewTableRow[I gpui.IntoElementID](id I, rowIndex int) *TableRow {
	base := tableBase(id)
	return gpui.BindComponent(&TableRow{base: base, rowIndex: rowIndex}, base)
}

// RenderOnce implements gpui.RenderOnce (U:table.rs:206-214).
func (t *TableRow) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return t.render(w, app).IntoElement()
}

func (t *TableRow) render(_ *gpui.Window, _ *gpui.App) *gpui.DivElement {
	return tableRender(t.base, tableRole(gpui.RoleRow), t.Style(), t.TakeChildren(), func(d *gpui.DivElement) {
		d.AriaRowIndex(t.rowIndex)
	})
}

// TableHead is an unstyled table column header (U:table.rs:276-280).
type TableHead struct {
	gpui.Component[*TableHead]

	base        *gpui.DivElement
	columnIndex int
}

// NewTableHead creates a column header with a one-based accessibility
// index (U:table.rs:227-237).
func NewTableHead[I gpui.IntoElementID](id I, columnIndex int) *TableHead {
	base := tableBase(id)
	return gpui.BindComponent(&TableHead{base: base, columnIndex: columnIndex}, base)
}

// RenderOnce implements gpui.RenderOnce (U:table.rs:264-273).
func (t *TableHead) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return t.render(w, app).IntoElement()
}

func (t *TableHead) render(_ *gpui.Window, _ *gpui.App) *gpui.DivElement {
	return tableRender(t.base, tableRole(gpui.RoleColumnHeader), t.Style(), t.TakeChildren(), func(d *gpui.DivElement) {
		d.AriaColumnIndex(t.columnIndex)
	})
}

// TableCell is an unstyled table data cell (U:table.rs:281).
type TableCell struct {
	gpui.Component[*TableCell]

	base        *gpui.DivElement
	columnIndex int
}

// NewTableCell creates a data cell with a one-based accessibility index
// (U:table.rs:227-237).
func NewTableCell[I gpui.IntoElementID](id I, columnIndex int) *TableCell {
	base := tableBase(id)
	return gpui.BindComponent(&TableCell{base: base, columnIndex: columnIndex}, base)
}

// RenderOnce implements gpui.RenderOnce (U:table.rs:264-273).
func (t *TableCell) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return t.render(w, app).IntoElement()
}

func (t *TableCell) render(_ *gpui.Window, _ *gpui.App) *gpui.DivElement {
	return tableRender(t.base, tableRole(gpui.RoleCell), t.Style(), t.TakeChildren(), func(d *gpui.DivElement) {
		d.AriaColumnIndex(t.columnIndex)
	})
}

// TableCaption is an unstyled table caption slot; it has no role
// (U:table.rs:283-331).
type TableCaption struct {
	gpui.Component[*TableCaption]

	base *gpui.DivElement
}

// NewTableCaption creates a caption slot (U:table.rs:291-300).
func NewTableCaption[I gpui.IntoElementID](id I) *TableCaption {
	base := tableBase(id)
	return gpui.BindComponent(&TableCaption{base: base}, base)
}

// RenderOnce implements gpui.RenderOnce (U:table.rs:327-331).
func (t *TableCaption) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return t.render(w, app).IntoElement()
}

func (t *TableCaption) render(_ *gpui.Window, _ *gpui.App) *gpui.DivElement {
	return tableRender(t.base, nil, t.Style(), t.TakeChildren(), nil)
}
