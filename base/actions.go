package base

// Common actions shared by keyboard-driven controls (U:actions.rs), in the
// `ui` namespace. Each action is a payload type plus its canonical
// descriptor (the XAction variable).

import "github.com/hoangvu12/gpui-go/gpui"

// Confirm confirms; Secondary selects the secondary action
// (U:actions.rs:6-11; #[action(namespace = ui, no_json)]).
type Confirm struct {
	// Secondary reports whether the confirmation uses the secondary
	// action.
	Secondary bool
}

// ConfirmAction is the ui::Confirm descriptor. no_json: it has no
// FromJSON service (U:actions.rs:7).
var ConfirmAction = gpui.DefineAction(gpui.ActionSpec[Confirm]{
	Name:  "ui::Confirm",
	Clone: func(c Confirm) Confirm { return c },
	Equal: func(a, b Confirm) bool { return a == b },
})

// The unit actions of actions!(ui, [...]) (U:actions.rs:13-28).
type (
	// Cancel cancels.
	Cancel struct{}
	// SelectUp moves the selection up.
	SelectUp struct{}
	// SelectDown moves the selection down.
	SelectDown struct{}
	// SelectLeft moves the selection left.
	SelectLeft struct{}
	// SelectRight moves the selection right.
	SelectRight struct{}
	// SelectFirst selects the first item.
	SelectFirst struct{}
	// SelectLast selects the last item.
	SelectLast struct{}
	// SelectPrevColumn selects the previous column.
	SelectPrevColumn struct{}
	// SelectNextColumn selects the next column.
	SelectNextColumn struct{}
	// SelectPageUp moves the selection up one page.
	SelectPageUp struct{}
	// SelectPageDown moves the selection down one page.
	SelectPageDown struct{}
)

// Descriptors of the ui unit actions (U:actions.rs:13-28).
var (
	CancelAction           = gpui.DefineUnitAction[Cancel]("ui::Cancel")
	SelectUpAction         = gpui.DefineUnitAction[SelectUp]("ui::SelectUp")
	SelectDownAction       = gpui.DefineUnitAction[SelectDown]("ui::SelectDown")
	SelectLeftAction       = gpui.DefineUnitAction[SelectLeft]("ui::SelectLeft")
	SelectRightAction      = gpui.DefineUnitAction[SelectRight]("ui::SelectRight")
	SelectFirstAction      = gpui.DefineUnitAction[SelectFirst]("ui::SelectFirst")
	SelectLastAction       = gpui.DefineUnitAction[SelectLast]("ui::SelectLast")
	SelectPrevColumnAction = gpui.DefineUnitAction[SelectPrevColumn]("ui::SelectPrevColumn")
	SelectNextColumnAction = gpui.DefineUnitAction[SelectNextColumn]("ui::SelectNextColumn")
	SelectPageUpAction     = gpui.DefineUnitAction[SelectPageUp]("ui::SelectPageUp")
	SelectPageDownAction   = gpui.DefineUnitAction[SelectPageDown]("ui::SelectPageDown")
)
