package base

// The window-scoped text selection model (the port of
// U:text_selection.rs) moved into the core as gpui/text_selection.go
// (browser-core ticket 19: static text selection, the platform highlight
// and the copy action are core behaviours). This file re-exports the
// model's public surface under the names Base's callers and the upstream
// port used, so every Base component built on it — SelectableText, the
// TextView selection adapter, the Root's copy handler — keeps compiling
// and behaving exactly as before: the type aliases are the core types,
// and the functions delegate. (browser-core 73: the Root's copy
// KEYSTROKE binding is gone off macOS — the core's copy command owns
// the key — but the Root's OnAction handler and everything else here
// stay.)
//
// Base also installs its own multi-click boundaries over the core's seam
// (text_boundary.go below): the upstream word and line rules
// (U:text_boundary.rs over base/segment), where the core's default is the
// browser pair. Base's double-click therefore still selects the input's
// word boundary and its triple-click the logical line without the break,
// unchanged from the pre-ticket port.

import "github.com/hoangvu12/gpui-go/gpui"

// The model's identities and snapshots (gpui/text_selection.go).
type (
	TextSelectionScopeID            = gpui.TextSelectionScopeID
	TextSelectionContentKey         = gpui.TextSelectionContentKey
	TextSelectionEndpoint           = gpui.TextSelectionEndpoint
	TextSelectionWindowPoints       = gpui.TextSelectionWindowPoints
	TextSelectionCoverage           = gpui.TextSelectionCoverage
	TextSelectionSnapshot           = gpui.TextSelectionSnapshot
	TextSelectionRegistration       = gpui.TextSelectionRegistration
	TextSelectionRun                = gpui.TextSelectionRun
	TextSelectionProjection         = gpui.TextSelectionProjection
	TextSelectionContentKeyResolver = gpui.TextSelectionContentKeyResolver
	TextSelectionEventKind          = gpui.TextSelectionEventKind
	TextSelectionEvent              = gpui.TextSelectionEvent
	TouchHandleLayout               = gpui.TouchHandleLayout
	TextSelectionHandle             = gpui.TextSelectionHandle
	TextSelectionLayer              = gpui.TextSelectionLayer
)

// NewTextSelectionScopeID allocates a process-unique scope identifier.
func NewTextSelectionScopeID() TextSelectionScopeID { return gpui.NewTextSelectionScopeID() }

// NewTextSelectionContentKey creates a key from a participant-defined
// stable content identity.
func NewTextSelectionContentKey(value uint64) TextSelectionContentKey {
	return gpui.NewTextSelectionContentKey(value)
}

// NewTextSelectionRegistration creates a registration with the default
// scope, order and scroll offset.
func NewTextSelectionRegistration(hitbox gpui.Hitbox, bounds gpui.Bounds) TextSelectionRegistration {
	return gpui.NewTextSelectionRegistration(hitbox, bounds)
}

// NewTextSelectionRun creates a laid-out text run.
func NewTextSelectionRun(text string, layout *gpui.TextLayout, bounds gpui.Bounds) TextSelectionRun {
	return gpui.NewTextSelectionRun(text, layout, bounds)
}

// NewTextSelectionHandle creates a participant with fallback text for
// copying, owned by owner.
func NewTextSelectionHandle(fallbackCopyText string, cx gpui.AppContext, owner *gpui.Scope) TextSelectionHandle {
	return gpui.NewTextSelectionHandle(fallbackCopyText, cx, owner)
}

// The coverage kinds.
const (
	TextSelectionCoverageBounded   = gpui.TextSelectionCoverageBounded
	TextSelectionCoverageFromStart = gpui.TextSelectionCoverageFromStart
	TextSelectionCoverageToEnd     = gpui.TextSelectionCoverageToEnd
	TextSelectionCoverageFull      = gpui.TextSelectionCoverageFull
)

// The event kinds.
const (
	TextSelectionEventSelectionChanged      = gpui.TextSelectionEventSelectionChanged
	TextSelectionEventAutoScroll            = gpui.TextSelectionEventAutoScroll
	TextSelectionEventCleared               = gpui.TextSelectionEventCleared
	TextSelectionEventTouchSelectionChanged = gpui.TextSelectionEventTouchSelectionChanged
)

// The TextSelection namespace (U:text_selection.rs:2283-2414), delegated.
func TextSelectionSelectedText(w *gpui.Window, app *gpui.App) string {
	return gpui.TextSelectionSelectedText(w, app)
}

func TextSelectionHasSelection(w *gpui.Window, app *gpui.App) bool {
	return gpui.TextSelectionHasSelection(w, app)
}

func TextSelectionClear(w *gpui.Window, app *gpui.App) {
	gpui.TextSelectionClear(w, app)
}

func TextSelectionClearForWindow(windowID gpui.WindowID, app *gpui.App) {
	gpui.TextSelectionClearForWindow(windowID, app)
}

func TextSelectionEnd(w *gpui.Window, app *gpui.App) {
	gpui.TextSelectionEnd(w, app)
}

func TextSelectionObserveTouchSelection(w *gpui.Window, app *gpui.App, callback func(app *gpui.App)) gpui.Subscription {
	return gpui.TextSelectionObserveTouchSelection(w, app, callback)
}

func TextSelectionTouchSelection(w *gpui.Window, app *gpui.App) gpui.Optional[TouchSelectionSnapshot] {
	return gpui.TextSelectionTouchSelection(w, app)
}

func TextSelectionRegisterTouchUI(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
	gpui.TextSelectionRegisterTouchUI(bounds, w, app)
}

func TextSelectionCloseEditMenu(w *gpui.Window, app *gpui.App) {
	gpui.TextSelectionCloseEditMenu(w, app)
}

func TextSelectionSelectAll(w *gpui.Window, app *gpui.App) {
	gpui.TextSelectionSelectAll(w, app)
}

func TextSelectionBeginEdgeDrag(edge SelectionEdge, finger gpui.Point, w *gpui.Window, app *gpui.App) {
	gpui.TextSelectionBeginEdgeDrag(edge, finger, w, app)
}

func TextSelectionUpdateEdgeDrag(finger gpui.Point, w *gpui.Window, app *gpui.App) {
	gpui.TextSelectionUpdateEdgeDrag(finger, w, app)
}

func TextSelectionEndEdgeDrag(w *gpui.Window, app *gpui.App) {
	gpui.TextSelectionEndEdgeDrag(w, app)
}

func TextSelectionActivateScope(scope TextSelectionScopeID, w *gpui.Window, app *gpui.App) {
	gpui.TextSelectionActivateScope(scope, w, app)
}
