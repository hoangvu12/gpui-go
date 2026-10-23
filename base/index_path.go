package base

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
)

// IndexPath is an index path in a list: section, row and column, each 0
// by default (U:index_path.rs:5-16).
type IndexPath struct {
	// Section is the section index.
	Section int
	// Row is the item index in the section.
	Row int
	// Column is the column index.
	Column int
}

// NewIndexPath returns the path of row in section 0, column 0
// (U:index_path.rs:35-45 IndexPath::new).
func NewIndexPath(row int) IndexPath { return IndexPath{Row: row} }

// WithSection sets the section (U:index_path.rs:47-51 section; renamed
// because the field is Section).
func (p IndexPath) WithSection(section int) IndexPath { p.Section = section; return p }

// WithRow sets the row (U:index_path.rs:53-57 row).
func (p IndexPath) WithRow(row int) IndexPath { p.Row = row; return p }

// WithColumn sets the column (U:index_path.rs:59-63 column).
func (p IndexPath) WithColumn(column int) IndexPath { p.Column = column; return p }

// EqRow reports the same section and row (U:index_path.rs:65-68).
func (p IndexPath) EqRow(index IndexPath) bool {
	return p.Section == index.Section && p.Row == index.Row
}

// ElementID is From<IndexPath> for ElementId: the name
// "index-path(section,row,column)" (U:index_path.rs:18-22).
func (p IndexPath) ElementID() gpui.ElementID {
	return gpui.NameElementID(fmt.Sprintf("index-path(%d,%d,%d)", p.Section, p.Row, p.Column))
}

// String renders the path (U:index_path.rs:24-32 Display).
func (p IndexPath) String() string {
	return fmt.Sprintf("IndexPath(section: %d, row: %d, column: %d)", p.Section, p.Row, p.Column)
}
