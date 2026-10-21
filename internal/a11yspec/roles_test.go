//go:build windows

package a11yspec

// Base ticket 11 through the REAL native UIA provider: a tree whose
// nodes carry the Base roles the frozen rev-11 decoder lacks
// (reference/native/src/a11y.rs:598-612 role_from_tag accepts 8 tags;
// any other tag fails the whole publish with ERR_BAD_VALUE) and the new
// states (which the rev-11 record has no slot for,
// internal/native/a11y.go:106-127). Ticket 02's decision: the Go
// snapshot keeps the full semantics, and publication maps each role to
// the nearest supported one and drops the states — the publish APPLIES
// instead of aborting. The activation is the synthetic UIA root query
// (no Narrator).

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// roleNode is a generic semantic element: a fixed-size box publishing
// one A11ySpec, optionally with children laid out in a column.
type roleNode struct {
	id       string
	spec     gpui.A11ySpec
	children []gpui.AnyElement
}

func (e *roleNode) ID() (gpui.ElementID, bool)           { return gpui.NameElementID(e.id), true }
func (e *roleNode) SourceLocation() *gpui.SourceLocation { return nil }

// A11ySpec implements gpui.A11yCompanion.
func (e *roleNode) A11ySpec() gpui.A11ySpec {
	spec := e.spec
	spec.AuthorID = e.id
	return spec
}

func (e *roleNode) style() gpui.Style {
	style := gpui.DefaultStyle()
	style.FlexDirection = gpui.FlexDirectionColumn
	if len(e.children) == 0 {
		style.Size = gpui.LengthSize{Width: gpui.PxLength(80), Height: gpui.PxLength(12)}
	}
	return style
}

func (e *roleNode) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	childIDs := make([]gpui.LayoutID, 0, len(e.children))
	for _, child := range e.children {
		childIDs = append(childIDs, child.RequestLayout(w, app))
	}
	id, err := gpui.RequestElementLayout(w, e.style(), childIDs...)
	if err != nil {
		panic(fmt.Sprintf("a11yspec: role node request_layout: %v", err))
	}
	return id, struct{}{}
}

func (e *roleNode) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	for _, child := range e.children {
		child.Prepaint(w, app)
	}
	return struct{}{}
}

func (e *roleNode) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
	for _, child := range e.children {
		child.Paint(w, app)
	}
}

// LayoutNodeStyle reports the root stretch style.
func (e *roleNode) LayoutNodeStyle() (gpui.Style, bool) { return e.style(), true }

func node(id string, spec gpui.A11ySpec, children ...gpui.AnyElement) gpui.AnyElement {
	return gpui.Accessibility(&roleNode{id: id, spec: spec, children: children})
}

// rolesRootView renders the Base-role corpus (stateless, like
// a11yRootView).
type rolesRootView struct{}

func (v *rolesRootView) EntityID() (gpui.EntityID, bool) { return 0, false }

func (v *rolesRootView) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	toggled := func(t gpui.Toggled) gpui.A11yAttributes {
		return gpui.A11yAttributes{Toggled: gpui.Some(t)}
	}
	tab := func(selected bool, pos int) gpui.A11ySpec {
		return gpui.A11ySpec{Role: gpui.RoleTab, A11yAttributes: gpui.A11yAttributes{
			Selected: gpui.Some(selected), PositionInSet: gpui.Some(pos), SizeOfSet: gpui.Some(2),
		}}
	}
	return node("roles-root", gpui.A11ySpec{Role: gpui.RoleRegion, Label: "Base roles"},
		node("switch", gpui.A11ySpec{Role: gpui.RoleSwitch, Label: "Wi-Fi", A11yAttributes: toggled(gpui.ToggledTrue), Actions: []gpui.A11yAction{gpui.A11yActionClick}}),
		node("radio", gpui.A11ySpec{Role: gpui.RoleRadioButton, A11yAttributes: toggled(gpui.ToggledFalse)}),
		node("tabs", gpui.A11ySpec{Role: gpui.RoleTabList, A11yAttributes: gpui.A11yAttributes{Orientation: gpui.Some(gpui.A11yOrientationHorizontal)}},
			node("tab-1", tab(true, 1)),
			node("tab-2", tab(false, 2)),
		),
		node("combo", gpui.A11ySpec{Role: gpui.RoleComboBox, A11yAttributes: gpui.A11yAttributes{Expanded: gpui.Some(false)}}),
		node("spin", gpui.A11ySpec{Role: gpui.RoleSpinButton, Value: "3", A11yAttributes: gpui.A11yAttributes{
			NumericValue: gpui.Some(3.0), MinNumericValue: gpui.Some(0.0), MaxNumericValue: gpui.Some(10.0), NumericValueStep: gpui.Some(1.0),
		}, Actions: []gpui.A11yAction{gpui.A11yActionIncrement, gpui.A11yActionDecrement}}),
		node("heading", gpui.A11ySpec{Role: gpui.RoleHeading, Label: "Settings", A11yAttributes: gpui.A11yAttributes{Level: gpui.Some(1)}}),
		node("table", gpui.A11ySpec{Role: gpui.RoleTable, A11yAttributes: gpui.A11yAttributes{RowCount: gpui.Some(1), ColumnCount: gpui.Some(1)}},
			node("row", gpui.A11ySpec{Role: gpui.RoleRow, A11yAttributes: gpui.A11yAttributes{RowIndex: gpui.Some(0)}},
				node("cell", gpui.A11ySpec{Role: gpui.RoleCell, Label: "A1", A11yAttributes: gpui.A11yAttributes{RowIndex: gpui.Some(0), ColumnIndex: gpui.Some(0)}}),
			),
		),
		node("tree", gpui.A11ySpec{Role: gpui.RoleTree},
			node("tree-item", gpui.A11ySpec{Role: gpui.RoleTreeItem, A11yAttributes: gpui.A11yAttributes{Expanded: gpui.Some(true), Level: gpui.Some(1)}}),
		),
		node("dialog", gpui.A11ySpec{Role: gpui.RoleDialog, Label: "Confirm", A11yAttributes: gpui.A11yAttributes{Modal: true}}),
		node("progress", gpui.A11ySpec{Role: gpui.RoleProgressIndicator, A11yAttributes: gpui.A11yAttributes{NumericValue: gpui.Some(0.25)}}),
		node("alert", gpui.A11ySpec{Role: gpui.RoleAlert, Value: "Saved", A11yAttributes: gpui.A11yAttributes{Live: gpui.Some(gpui.A11yLiveAssertive), LiveAtomic: true}}),
		node("time", gpui.A11ySpec{Role: gpui.RoleTimeInput, A11yAttributes: gpui.A11yAttributes{Disabled: true, Placeholder: "hh:mm"}}),
		node("link", gpui.A11ySpec{Role: gpui.RoleLink, Label: "Docs"}),
		node("hidden", gpui.A11ySpec{Hidden: true}),
	)
}

// TestA11yUnsupportedRolesPublishThroughProvider pins ticket 02's
// publication policy on the real UIA provider: the activated adapter
// APPLIES a tree full of roles the rev-11 decoder lacks (before ticket
// 11 the serializer failed the whole publish on the first such role,
// "has no native tag"), while the Go snapshot keeps every role and
// state exactly.
func TestA11yUnsupportedRolesPublishThroughProvider(t *testing.T) {
	f := bootA11yFixtureView(t, "Base Roles", func(gpui.Entity[a11yModel]) gpui.View { return &rolesRootView{} })
	gpui.ActivateWindowA11y(f.window)

	// Activate through the synthetic UIA root query (no Narrator).
	f.query(uiaRootObjectId)
	rec, _ := f.provider(t)
	if !rec.Available || !rec.Active {
		t.Fatalf("provider available/active after the root query = %v/%v, want true/true (err %q)", rec.Available, rec.Active, rec.Err)
	}

	snapshot := f.drawAndPublish(t)
	rec, _ = f.provider(t)
	if rec.Err != "" {
		t.Fatalf("provider error after publishing unsupported roles: %s (ticket 02: the publish must not abort)", rec.Err)
	}
	if rec.Publishes != 1 || !rec.LastActive || !rec.LastApplied {
		t.Fatalf("publications=%d active=%v applied=%v, want 1/true/true (the tree with unsupported roles applied)\n%s",
			rec.Publishes, rec.LastActive, rec.LastApplied, snapshot.Trace())
	}
	if snapshot.NodeCount() != 21 {
		t.Fatalf("snapshot has %d nodes, want 21 (root + 20 role nodes)\n%s", snapshot.NodeCount(), snapshot.Trace())
	}

	// The Go snapshot keeps the authored roles and states exactly.
	checks := map[string]func(n gpui.A11ySnapshotNode) bool{
		"switch": func(n gpui.A11ySnapshotNode) bool {
			v, ok := n.Toggled.Get()
			return n.Role == gpui.RoleSwitch && ok && v == gpui.ToggledTrue
		},
		"tab-1": func(n gpui.A11ySnapshotNode) bool {
			sel, ok := n.Selected.Get()
			pos, _ := n.PositionInSet.Get()
			size, _ := n.SizeOfSet.Get()
			return n.Role == gpui.RoleTab && ok && sel && pos == 1 && size == 2
		},
		"spin": func(n gpui.A11ySnapshotNode) bool {
			v, _ := n.NumericValue.Get()
			max, _ := n.MaxNumericValue.Get()
			return n.Role == gpui.RoleSpinButton && v == 3 && max == 10
		},
		"cell": func(n gpui.A11ySnapshotNode) bool {
			col, ok := n.ColumnIndex.Get()
			return n.Role == gpui.RoleCell && ok && col == 0
		},
		"dialog": func(n gpui.A11ySnapshotNode) bool { return n.Role == gpui.RoleDialog && n.Modal },
		"alert": func(n gpui.A11ySnapshotNode) bool {
			live, ok := n.Live.Get()
			return n.Role == gpui.RoleAlert && ok && live == gpui.A11yLiveAssertive && n.LiveAtomic
		},
		"time": func(n gpui.A11ySnapshotNode) bool {
			return n.Role == gpui.RoleTimeInput && n.Disabled && n.Placeholder == "hh:mm"
		},
		// A hidden node without a role is a GenericContainer
		// (element.rs:429-430).
		"hidden": func(n gpui.A11ySnapshotNode) bool { return n.Role == gpui.RoleGenericContainer && n.Hidden },
	}
	for author, check := range checks {
		if n := nodeByAuthor(t, snapshot, author); !check(n) {
			t.Errorf("snapshot node %q lost its semantics: %s", author, n)
		}
	}
	if trace := snapshot.Trace(); !strings.Contains(trace, `Switch id="switch" label="Wi-Fi" toggled=True actions=[Click]`) {
		t.Errorf("trace lacks the switch's state:\n%s", trace)
	}

	// A second frame publishes again (the policy holds per frame, not
	// just for the first update).
	f.model.Update(f.app, func(m *a11yModel, cx *gpui.Context[a11yModel]) { cx.Notify() })
	f.drawAndPublish(t)
	rec, _ = f.provider(t)
	if rec.Err != "" || rec.Publishes != 2 || !rec.LastApplied {
		t.Fatalf("second publication: err=%q publishes=%d applied=%v, want no error, 2, true", rec.Err, rec.Publishes, rec.LastApplied)
	}
}
