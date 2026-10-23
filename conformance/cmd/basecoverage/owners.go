package main

import "strings"

// ticketEvidence names each Base ticket's resolution evidence (the
// tickets that port upstream Base modules; 03-12/33-36 are core
// completion and own no Base module).
var ticketEvidence = map[string]string{
	"13": "ticket13-base-infra.json",
	"14": "ticket14-motion.json",
	"15": "ticket15-data-structures.json",
	"16": "ticket16-overlays.json",
	"17": "ticket17-simple-controls.json",
	"18": "ticket18-scroll-family.json",
	"19": "ticket19-input-data-layer.json",
	"20": "ticket20-single-line-input.json",
	"21": "ticket21-textarea.json",
	"22": "ticket22-input-controls.json",
	"23": "ticket23-editor-basics.json",
	"24": "ticket24-diagnostics-lsp.json",
	"25": "ticket25-composite-controls.json",
	"26": "ticket26-dock.json",
	"27": "ticket27-markdown-parse.json",
	"28": "ticket28-text-view.json",
	"29": "ticket29-text-selection.json",
	"30": "ticket30-text-extras.json",
	"31": "ticket31-plot.json",
	"36": "ticket36-selection-wiring-paint-bounds.json",
	"37": "", // open: request_autoscroll (the 11 reveal tests)
}

// moduleOwners maps an upstream path (relative to crates/base/src, or
// crates/base/ for paths outside src) to its owning tickets, per
// .scratch/gpui-base/map.md and each ticket's scope. A key ending in
// "/" is a directory prefix. The longest matching key wins.
var moduleOwners = map[string][]string{
	// 13 base infra
	"actions.rs": {"13"}, "async_util.rs": {"13"}, "component_traits.rs": {"13"},
	"element_ext.rs": {"13", "36"}, "event.rs": {"13"}, "geometry.rs": {"13"},
	"global_state.rs": {"13"}, "history.rs": {"13"}, "index_path.rs": {"13"},
	"lib.rs": {"13"}, "list_settings.rs": {"13"}, "macos_accessibility.rs": {"13"},
	"measure.rs": {"13"}, "observe.rs": {"13"}, "reduce_motion.rs": {"13"},
	"root.rs": {"13", "36"}, "state_style.rs": {"13"}, "styled.rs": {"13"},
	"test_support.rs": {"13"}, "theme.rs": {"13"}, "theme_tokens.rs": {"13"},
	"undo_history.rs": {"13"}, "../tests/element_ext.rs": {"13", "36"},
	// 14 motion
	"animation.rs": {"14"}, "motion.rs": {"14"}, "motion/": {"14"},
	// 16 overlays
	"focus_trap.rs": {"16"}, "positioner.rs": {"16"}, "popup.rs": {"16"},
	"popover.rs": {"16"}, "tooltip.rs": {"16"}, "hover_card.rs": {"16"},
	"dialog.rs": {"16"}, "alert_dialog.rs": {"16"}, "sheet.rs": {"16"}, "toast.rs": {"16"},
	// 17 simple controls
	"button.rs": {"17"}, "toggle.rs": {"17"}, "toggle_group.rs": {"17"},
	"checkbox.rs": {"17"}, "radio.rs": {"17"}, "radio_group.rs": {"17"},
	"switch.rs": {"17"}, "link.rs": {"17"}, "tabs.rs": {"17"}, "toolbar.rs": {"17"},
	"accordion.rs": {"17"}, "collapsible.rs": {"17"}, "avatar.rs": {"17"},
	"progress.rs": {"17"}, "slider.rs": {"17"}, "pagination.rs": {"17"}, "table.rs": {"17"},
	// 18 scroll family
	"scrollbar.rs": {"18"}, "scroll_bounce.rs": {"18"}, "scrollable_mask.rs": {"18"},
	"auto_scroll.rs": {"18"}, "virtual_list.rs": {"18"},
	// 19-24 input / editor
	"input/base/rope_ext.rs": {"19"}, "input/base/cursor.rs": {"19"},
	"input/base/selection.rs": {"19"}, "input/base/change.rs": {"19"},
	"input/base/undo_manager.rs": {"19"}, "input/base/mask_pattern.rs": {"19"},
	"input/base/": {"20"}, "input/input/mod.rs": {"20"}, "input/mod.rs": {"20"},
	"input/base/state.rs":         {"20", "21", "22", "23", "24"},
	"input/base/element.rs":       {"20", "23"},
	"input/base/inline_tokens.rs": {"21"}, "input/base/token_presentation.rs": {"21"},
	"input/editor/display_map/": {"21"}, "input/textarea/mod.rs": {"21"},
	"number_input.rs": {"22"}, "otp_input.rs": {"22"}, "time_field.rs": {"22"},
	"select.rs": {"22"}, "combobox.rs": {"22"}, "calendar.rs": {"22"},
	"date_picker.rs": {"22"}, "color_picker.rs": {"22"},
	"input/editor/": {"23"}, "input/editor/highlighting.rs": {"20", "23"},
	"input/editor/diagnostics.rs": {"24"}, "input/editor/lsp/": {"24"},
	// 25 composite controls, 26 dock
	"tree.rs": {"25"}, "nav_stack.rs": {"25"}, "resizable/": {"25"}, "questionnaire/": {"25"},
	"dock/": {"26"},
	// 27-30 text
	"text/document.rs": {"27"}, "text/format/markdown.rs": {"27", "30"},
	"text/format/mod.rs": {"27"}, "text/markdown_ext.rs": {"27"},
	"text/node.rs": {"27", "28"},
	"text/":        {"28"}, "text/inline.rs": {"28", "30"},
	"text/state.rs":                {"28", "30", "37"},
	"text/text_view.rs":            {"28", "29", "30"},
	"text/inline_virtual_tests.rs": {"28", "29"},
	"text_selection.rs":            {"29"}, "selectable_text.rs": {"29"}, "touch_selection.rs": {"29"},
	"text/selection.rs": {"29"}, "text/selection_adapter.rs": {"29"}, "text_boundary.rs": {"29", "15"},
	"text/stream_fade.rs": {"30"}, "text/range_highlight.rs": {"30"},
	"text/format/html.rs": {"30"}, "text/format/html5minify/": {"30"},
	// 31 plot
	"plot/": {"31"},
}

// ownersOf returns the owning tickets of an upstream src-relative path.
func ownersOf(rel string) []string {
	best := ""
	var owners []string
	for k, v := range moduleOwners {
		match := k == rel || strings.HasSuffix(k, "/") && strings.HasPrefix(rel, k)
		if match && len(k) > len(best) {
			best, owners = k, v
		}
	}
	return owners
}

// moduleDisposition records modules that are deliberately not ported,
// with the evidence that says so. Every other module must have Go
// files citing it, or it is a GAP.
var moduleDisposition = map[string]struct{ Status, Reason string }{
	"macos_accessibility.rs": {statusNA, "cfg(target_os = \"macos\") only (lib.rs:36); 0 tests (ticket13 evidence not_applicable; map.md Out of scope)"},
	"async_util.rs":          {statusNA, "channel re-export replaced by Go channels; 0 tests (ticket13 evidence not_applicable)"},
}

// examplesReason is the disposition of everything under
// crates/base/examples: the upstream showcase/sample apps (and their
// in-example tests) are not crate API; the Go counterpart is the
// examples/base-showcase program (ticket 32).
const examplesReason = "upstream example/showcase application code, not part of the crate API; the Go counterpart is examples/base-showcase (ticket 32)"

// otherFileDisposition covers the manifest's non-Rust files.
func otherFileDisposition(path string) (string, string) {
	switch {
	case strings.HasPrefix(path, "crates/base/src/dock/fixtures/"):
		return statusPorted, "frozen dock layout fixture, copied to base/testdata/dock (ticket26 evidence)"
	case strings.HasPrefix(path, "crates/base/examples/"):
		return statusOutOfScope, examplesReason
	case strings.HasSuffix(path, "Cargo.toml"):
		return statusNA, "Cargo manifest; the Go module is go.mod (dependencies per ticket 01)"
	case strings.HasSuffix(path, "LICENSE-APACHE"):
		return statusNA, "license text; attribution in THIRD_PARTY_NOTICES.md"
	case strings.HasSuffix(path, ".md"):
		return statusNA, "upstream documentation; the Go package doc is base/doc.go"
	}
	return "", ""
}
