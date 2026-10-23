package main

// override resolves an upstream test that no Go file cites by line
// or name (or, with Force, records a deviation the ticket evidence
// names but the citing Go comment does not spell out). Every entry
// names its ticket and paraphrases that ticket's evidence JSON or the
// porting Go file; the tool lists entries that match nothing as unused.
type override struct {
	Status string
	Ticket string
	Reason string
	Force  bool
}

const (
	revealReason = "not ported yet: needs a block laid out on many rows (the TestApp text system lays a document on one visual line); all but on_reveal/gives_up also need Window::request_autoscroll — owned by open ticket 37 (ticket30 evidence reveal_not_ported; base/text_range_highlight_reveal_test.go header)"
	openedURL    = "not applicable: asserts cx.opened_url(), which gpui-go's TestApp does not record (ticket28 evidence text_view.rs; base/text_view_upstream_test.go header)"
)

// overrides is keyed "module::fn" (module relative to crates/base/src).
var overrides = map[string]override{
	// Ported, but the Go tests cite stale line numbers
	// (U:text/selection.rs:198-222 / 224-253 in a 172-line file).
	"text/selection.rs::test_char_type_from_char": {statusPorted, "29", "ported as TestTextCharTypeFromChar (base/text_selection_word_test.go; its U: line citation is stale); ticket29 evidence text/selection.rs 2/2", false},
	"text/selection.rs::test_word_range_at":       {statusPorted, "29", "ported as TestTextSelectionWordRangeAt (base/text_selection_word_test.go; its U: line citation is stale); ticket29 evidence text/selection.rs 2/2", false},

	// crates/base/tests/element_ext.rs: the property is checked by a
	// Go test marked Go-own that cites the src module instead.
	"../tests/element_ext.rs::prepaint_callback_observes_the_parent_bounds": {statusDeviated, "13", "covered by TestElementExtOnPrepaintReportsBounds (base/window_test.go, cites U:element_ext.rs:15-28): the prepaint callback reports the element's bounds (a 40x30 child at y=10 instead of an 80x80 root)", false},

	// Not applicable, per the ticket evidence.
	"input/base/state.rs::test_input_does_not_invalidate_cached_parent_during_paint": {statusNA, "20", "not applicable: the cached-parent test needs AnyView::cached, absent in the Go core (ticket20 evidence not_applicable_reasons)", false},
	"text/inline.rs::shortening_an_inline_flow_releases_obsolete_fragment_layouts":   {statusNA, "28", "not applicable: the Go retained-layout table is weak-keyed, so a dropped fragment's entry lives until the next sweep; the live-entry count is ported in inline_flow_fragments_keep_their_layouts_across_frames (ticket28 evidence inline.rs records it deferred; ticket 30 did not take it up; base/text_inline_test.go header)", false},
	"text/text_view.rs::list_item_renders_fenced_code_block_at_document_width":       {statusNA, "28", "not applicable: needs h_resizable/resizable_panel inside a TextView list item, not ported (ticket28 evidence text_view.rs '1 h_resizable not ported')", false},
	"text/text_view.rs::clipped_markdown_link_does_not_open":                         {statusNA, "28", openedURL, false},
	"text/text_view.rs::right_click_does_not_open_url_without_handler":               {statusNA, "28", openedURL, false},

	// Deferred to the open ticket 37 (request_autoscroll).
	"text/state.rs::a_scrollable_view_scrolls_to_a_line_inside_a_long_paragraph":              {statusDeferred, "37", revealReason, false},
	"text/state.rs::revealing_a_visible_line_does_not_scroll":                                 {statusDeferred, "37", revealReason, false},
	"text/state.rs::an_enclosing_list_scrolls_to_a_line_of_a_fit_content_view":                {statusDeferred, "37", revealReason, false},
	"text/state.rs::on_reveal_scrolls_a_container_that_ignores_scroll_requests":               {statusDeferred, "37", revealReason, false},
	"text/state.rs::a_reveal_that_cannot_be_shown_gives_up":                                   {statusDeferred, "37", revealReason, false},
	"text/state.rs::a_position_after_the_last_character_reveals_its_line":                     {statusDeferred, "37", revealReason, false},
	"text/state.rs::the_end_of_the_text_and_its_separators_reveal_the_last_line":              {statusDeferred, "37", revealReason, false},
	"text/state.rs::the_end_of_a_block_above_reveals_its_last_line":                           {statusDeferred, "37", revealReason, false},
	"text/state.rs::a_line_of_an_inline_flow_counts_as_shown_once_scrolled_to":                {statusDeferred, "37", revealReason, false},
	"text/state.rs::a_range_starting_on_a_line_break_reveals_the_next_line":                   {statusDeferred, "37", revealReason, false},
	"text/state.rs::a_range_starting_on_a_line_break_in_an_inline_flow_reveals_the_next_line": {statusDeferred, "37", revealReason, false},

	// Deviations the evidence names that the citing Go comment does not.
	"global_state.rs::initialization_is_idempotent_and_suppression_can_be_reset":  {statusDeviated, "13", "adapted: drop(token) becomes Release() (ticket13 evidence upstream_ported_detail)", true},
	"global_state.rs::a_dropped_registration_closes_the_deferred_context":         {statusDeviated, "13", "adapted: drop(token) becomes Release() (ticket13 evidence upstream_ported_detail)", true},
	"observe.rs::observation_preserves_identity_and_native_type_in_normal_builds": {statusDeviated, "13", "adapted: the normal-build branch only (no test-support feature) (ticket13 evidence upstream_ported_detail)", true},
	"motion.rs::status_transition_reports_delay_running_and_finished":             {statusDeviated, "14", "passes only with ticket14 deviation 1 (f64 duration scaling) (ticket14 evidence deviated_list)", true},
	"motion.rs::a_completed_transition_stops_requesting_frames":                   {statusDeviated, "14", "passes only with ticket14 deviation 1 (f64 duration scaling) (ticket14 evidence deviated_list)", true},
	"motion.rs::presence_enters_exits_and_only_unmounts_after_exit":               {statusDeviated, "14", "passes only with ticket14 deviation 1 (f64 duration scaling) (ticket14 evidence deviated_list)", true},
	"avatar.rs::image_slot_takes_precedence_over_fallback":                        {statusDeviated, "17", "the core image element has no debug selector; asserts root size and fallback absence instead of reading avatar-image (ticket17 evidence deviated_list)", true},
	"avatar.rs::fallback_renders_without_an_image":                                {statusDeviated, "17", "the core image element has no debug selector; asserts root size and fallback absence instead of reading avatar-image (ticket17 evidence deviated_list)", true},
	"input/base/state.rs::only_a_multi_line_input_paints_scrollbars":              {statusDeviated, "20", "the single-line half only (ticket20 evidence deviations; base/input_state_ops_test.go)", true},
	"text/text_view.rs::inline_hover_card_is_lazy_and_retains_atomic_copy":        {statusDeviated, "29", "the hover-card content is a plain div (ticket29 evidence, 28's deferred tests)", true},
}
