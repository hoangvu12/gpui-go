package base

// Honors the operating system's reduced-motion preference
// (U:reduce_motion.rs).
//
// Every Base transition, spring, presence and reveal consults
// App.ReduceMotion, but the core never reads the platform setting into
// that flag. reduceMotionInit reads it when Base initializes and writes it
// into the flag, so an application inherits the user's choice by calling
// Init.
//
// The setting is read on Windows: SystemParametersInfoW
// (SPI_GETCLIENTAREAANIMATION), the "Animation effects" switch under
// Accessibility > Visual effects, whose off state asks for reduced
// motion (reduce_motion_windows.go). Elsewhere the flag is left alone
// (the macOS NSWorkspace and Linux XDG-portal readers are out of scope
// for the Windows port).
//
// An application owns the flag once it sets it: Base writes the flag only
// while it still holds what Base last wrote (or the initial false), so a
// later App.SetReduceMotion is never overridden; to follow the system
// again, call ApplySystemReduceMotion.
//
// Under the test scheduler the platform is never consulted.

import "github.com/hoangvu12/gpui-go/gpui"

// systemReduceMotion is what Base last wrote into the flag, and whether
// it already follows the platform (U:reduce_motion.rs:37-45). The global
// is stored as *systemReduceMotion (DefaultGlobal).
type systemReduceMotion struct {
	applied    bool
	hasApplied bool
	following  bool
}

// reduceMotionInit applies the system preference during Init
// (U:reduce_motion.rs:47-50).
func reduceMotionInit(app *gpui.App) { ApplySystemReduceMotion(app) }

// ApplySystemReduceMotion reads the operating system's reduced-motion
// preference into App.SetReduceMotion (U:reduce_motion.rs:52-75). Init
// calls it once; call it again to re-read the preference (Windows posts no
// notification Base can follow without a window). The application wins
// over the system; where the platform cannot say the flag is left as it
// is; under the test scheduler the platform is not asked at all.
func ApplySystemReduceMotion(app *gpui.App) {
	if isTestScheduler(app) {
		return
	}
	reduce, ok := platformReadReduceMotion()
	applyReduceMotionPreference(reduce, ok, app)
	state := gpui.DefaultGlobal[systemReduceMotion](app)
	if !state.following {
		state.following = true
		platformFollowReduceMotion(app)
	}
}

// isTestScheduler reports whether the application runs on the
// deterministic test scheduler (U:reduce_motion.rs:77-85).
//
// Go adaptation: the core exposes no scheduler kind; an application
// without a platform host (App.Host() == nil) is the test application
// (gpui/window.go Host: "nil for test apps"). A real application must
// therefore call Init after attaching its host (inside the host's run, or
// after App.Attach) for the system preference to be read.
func isTestScheduler(app *gpui.App) bool { return app.Host() == nil }

// applyReduceMotionPreference writes one reading of the system preference
// into the flag, unless the application took the flag over since the
// reading Base last applied (U:reduce_motion.rs:87-101). ok == false is
// the unknown preference (Rust None), which leaves the flag alone.
func applyReduceMotionPreference(reduce, ok bool, app *gpui.App) {
	if !ok {
		return
	}
	applied := false
	if state, has := gpui.TryGlobal[*systemReduceMotion](app); has && state.hasApplied {
		applied = state.applied
	}
	if app.ReduceMotion() != applied {
		return
	}
	app.SetReduceMotion(reduce)
	state := gpui.DefaultGlobal[systemReduceMotion](app)
	state.applied, state.hasApplied = reduce, true
}
