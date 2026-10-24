package base

// The system's selection colours (browser-core 28, D11's third clause:
// "selection highlight colours come from the system").
//
// Selected text paints with the colours the platform reports — the
// ::selection default Blink resolves from the theme's active selection
// background (highlight_style_utils.cc DefaultBackgroundColor,
// third_party/blink/renderer/core/highlight/highlight_style_utils.cc:
// 188-201), which on Windows is the system Highlight colour converted
// to its blend-with-white wash (layout_theme.cc:365-371) with
// HighlightText as the foreground (kHighlight/kHighlighttext,
// layout_theme.cc:725-728). The theme token and the per-component
// overrides remain the app's explicit ::selection styling.
//
// The read goes through gpui's preference-version cache
// (App.SelectionColorsForPaint) so a render or paint pass derives the
// colours at most once per system-preference record version, never per
// element (ticket 29's pattern). The var is the test seam: an app
// without a host answers the default record.

import "github.com/hoangvu12/gpui-go/gpui"

// systemSelectionColors reads the platform's selection colours.
var systemSelectionColors = func(app *gpui.App) gpui.SelectionColors {
	if app == nil {
		return gpui.SelectionColors{}
	}
	return app.SelectionColorsForPaint()
}
