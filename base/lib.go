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
// (U:lib.rs:227-245): the Theme global, GlobalState, the dialog,
// focus_trap, popover, sheet, combobox, color_picker, select,
// number_input, time_field, input, tree and text initializers, and the
// Root key bindings. (The system reduced-motion preference is no longer
// read here: motion gates on the core's App.MotionReduced, which reads
// the live SystemPreferences record, browser-core ticket 74.)
func Init(app *gpui.App) {
	_ = ThemeGlobalMut(app)
	GlobalStateInit(app)
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
	treeInit(app)
	textInit(app)
	rootInit(app)
}
