package base

import (
	"runtime"

	"github.com/hoangvu12/gpui-go/gpui"
)

// IsMobile reports whether the application is compiled for iOS or
// Android (U:lib.rs:219-225). It is a platform check, not a screen-size
// or input-device check.
func IsMobile() bool {
	return runtime.GOOS == "ios" || runtime.GOOS == "android"
}

// Init initializes the global infrastructure owned by the base layer
// (U:lib.rs:227-245): the Theme global, GlobalState, the system
// reduced-motion preference, the dialog, focus_trap, popover, sheet,
// combobox, color_picker, select, number_input, time_field, input and text
// initializers, and the Root key bindings.
//
// The upstream init also runs the tree initializer (U:lib.rs:242); it
// joins this function with the ticket that ports its module.
func Init(app *gpui.App) {
	_ = ThemeGlobalMut(app)
	GlobalStateInit(app)
	reduceMotionInit(app)
	dialogInit(app)
	focusTrapInit(app)
	popoverInit(app)
	sheetInit(app)
	comboboxInit(app)
	colorPickerInit(app)
	selectInit(app)
	numberInputInit(app)
	timeFieldInit(app)
	inputInit(app)
	textInit(app)
	rootInit(app)
}
