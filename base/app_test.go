package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported app-level tests: global_state.rs (2/2). Go-own tests are
// marked. The reduce_motion.rs tests are gone with Base's reader
// (browser-core ticket 74): reduced motion is the core's single source
// (App.MotionReduced, covered in motion_reduced_test.go through the
// pinned SystemPreferences record), so there is no Base-side preference
// application left to test.

// U:global_state.rs:156-173. The deferred-context assertions are
// gone with the liveness list (browser-core ticket 72: the top-layer
// entry's registration is the popup's liveness).
func TestGlobalStateInitializationIsIdempotentAndSuppressionCanBeReset(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		GlobalStateInit(app)
		GlobalStateSuppressTextSelection(app)
		GlobalStateInit(app)
		if !GlobalStateIsTextSelectionSuppressed(app) {
			t.Fatal("re-init reset the suppression")
		}

		GlobalStateResetTextSelectionSuppression(app)
		if GlobalStateIsTextSelectionSuppressed(app) {
			t.Fatal("suppression not reset")
		}
	})
}

// Go-own: the touch press window (U:global_state.rs:44-58).
func TestGlobalStateTouchPress(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		if GlobalStateIsTouchPress(app) {
			t.Fatal("touch press before any touch")
		}
		GlobalStateNoteTouch(app)
		if !GlobalStateIsTouchPress(app) {
			t.Fatal("a fresh touch is not a touch press")
		}
	})
}

// Go-own: Init installs the globals (U:lib.rs:228-245).
func TestInitInstallsTheBaseGlobals(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		Init(app)
		if !gpui.HasGlobal[*Theme](app) || !gpui.HasGlobal[*GlobalState](app) {
			t.Fatal("Init did not install the Theme and GlobalState globals")
		}
		if ThemeGlobal(app).Tokens.Colors != LightColorTokens() {
			t.Fatal("the default theme is not the light palette")
		}
		ThemeGlobalMut(app).Appearance = ThemeAppearanceDark
		if ThemeGlobal(app).Appearance != ThemeAppearanceDark {
			t.Fatal("ThemeGlobalMut did not reach the global")
		}
	})
}
