package base

import (
	"runtime"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported app-level tests: global_state.rs (2/2), reduce_motion.rs (5/5).
// Go-own tests are marked.

// U:global_state.rs:156-173
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

		if GlobalStateIsInDeferredContext(app) {
			t.Fatal("deferred context open before any registration")
		}
		popover := GlobalStateRegisterDeferredPopover(app)
		if !GlobalStateIsInDeferredContext(app) {
			t.Fatal("registration did not open the deferred context")
		}
		popover.Release() // drop(popover)
		if GlobalStateIsInDeferredContext(app) {
			t.Fatal("release did not close the deferred context")
		}
	})
}

// U:global_state.rs:175-199
func TestGlobalStateADroppedRegistrationClosesTheDeferredContext(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		GlobalStateInit(app)

		outer := GlobalStateRegisterDeferredPopover(app)
		{
			inner := GlobalStateRegisterDeferredPopover(app)
			if !GlobalStateIsInDeferredContext(app) {
				t.Fatal("not in deferred context")
			}
			inner.Release() // the inner scope ends
		}
		if !GlobalStateIsInDeferredContext(app) {
			t.Fatal("the outer registration closed with the inner one")
		}

		outer.Release()
		if GlobalStateIsInDeferredContext(app) {
			t.Fatal("still in deferred context")
		}

		// Registering again must not resurrect the collected ones.
		popover := GlobalStateRegisterDeferredPopover(app)
		if n := len(GlobalStateGlobal(app).deferredPopovers); n != 1 {
			t.Fatalf("registrations = %d, want 1", n)
		}
		popover.Release()
		if GlobalStateIsInDeferredContext(app) {
			t.Fatal("still in deferred context")
		}
	})
}

// Go-own: a token dropped without Release closes the context once it is
// collected (the Rc-drop guarantee, U:global_state.rs:130-140).
func TestGlobalStateAnUnreleasedTokenClosesWhenCollected(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		GlobalStateInit(app)
		func() {
			_ = GlobalStateRegisterDeferredPopover(app) // never released
		}()
		runtime.GC()
		if GlobalStateIsInDeferredContext(app) {
			t.Fatal("a collected token kept the deferred context open")
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

// U:reduce_motion.rs:191-197
func TestReduceMotionASystemPreferenceForReducedMotionSetsTheFlag(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		applyReduceMotionPreference(true, true, app)
		if !app.ReduceMotion() {
			t.Fatal("flag not set")
		}
	})
}

// U:reduce_motion.rs:199-209
func TestReduceMotionAnUnknownSystemPreferenceLeavesTheFlagAlone(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		applyReduceMotionPreference(false, false, app)
		if app.ReduceMotion() {
			t.Fatal("unknown preference set the flag")
		}
		app.SetReduceMotion(true)
		applyReduceMotionPreference(false, false, app)
		if !app.ReduceMotion() {
			t.Fatal("unknown preference cleared the flag")
		}
	})
}

// U:reduce_motion.rs:211-224
func TestReduceMotionTheSystemDrivesTheFlagUntilTheApplicationSetsIt(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		applyReduceMotionPreference(true, true, app)
		applyReduceMotionPreference(false, true, app)
		if app.ReduceMotion() {
			t.Fatal("system false not applied")
		}
		applyReduceMotionPreference(true, true, app)
		if !app.ReduceMotion() {
			t.Fatal("system true not applied")
		}

		app.SetReduceMotion(false)
		applyReduceMotionPreference(true, true, app)
		if app.ReduceMotion() {
			t.Fatal("the system overrode the application")
		}
	})
}

// U:reduce_motion.rs:226-234
func TestReduceMotionTheTestSchedulerIsNeverAskedForThePlatformPreference(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		ApplySystemReduceMotion(app)
		if app.ReduceMotion() {
			t.Fatal("flag set under the test scheduler")
		}
		if gpui.HasGlobal[*systemReduceMotion](app) {
			t.Fatal("the platform path ran under the test scheduler")
		}
	})
	ta.RunUntilParked()
	ta.Update(func(app *gpui.App) {
		if app.ReduceMotion() {
			t.Fatal("flag set after parking")
		}
	})
}

// U:reduce_motion.rs:236-243
func TestReduceMotionAFlagTheApplicationSetBeforeTheFirstReadingIsKept(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		app.SetReduceMotion(true)
		applyReduceMotionPreference(false, true, app)
		if !app.ReduceMotion() {
			t.Fatal("the system overrode the application's first setting")
		}
	})
}

// Go-own: the Windows reader answers through SystemParametersInfoW
// (U:reduce_motion.rs:115-139). It reads the real system setting, so it
// asserts only that the call succeeds on Windows.
func TestReduceMotionPlatformReaderAnswersOnWindows(t *testing.T) {
	_, ok := platformReadReduceMotion()
	if runtime.GOOS == "windows" && !ok {
		t.Fatal("SystemParametersInfoW(SPI_GETCLIENTAREAANIMATION) failed")
	}
	if runtime.GOOS != "windows" && ok {
		t.Fatal("a non-Windows platform reported a preference")
	}
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
