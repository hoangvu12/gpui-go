package ledger

import "strings"

// CrateGPUI is the package name of the public gpui crate in api-scan.json.
// All other crates are supporting crates and use their own package names.
const CrateGPUI = "gpui-ce"

// OwnerStructural is the special owner marker for rows dispositioned as
// non-observable structure by the ticket-34 triage. It is not a ticket id:
// structural rows have no implementation owner because they are not
// operations. Rows with this owner carry DispositionCoreStructure and a
// Notes entry citing the pinned source that justifies the disposition.
const OwnerStructural = "core-structure"

// OwnerRule is one row of the ordered family→owner assignment table derived
// from docs/conformance-inventory.md. The first matching rule wins; rows that
// match no rule are unassigned (owner "", owner_source "unassigned") and are
// surfaced in the ledger for later triage. Note is the disposition note the
// ticket-34 triage rules record on every row they match; pre-triage rules
// leave it empty.
type OwnerRule struct {
	ID          int
	Family      string
	Owner       string
	Description string
	Note        string
	Match       func(Row) bool
}

// OwnerRules returns the ordered owner-assignment rule table. The data is
// exported so generated reports can list the family→owner mappings actually
// used and tests can exercise every rule.
func OwnerRules() []OwnerRule {
	out := make([]OwnerRule, len(ownerRules))
	copy(out, ownerRules)
	return out
}

// AssignOwner returns the owning ticket id and provenance for one row. The
// source is "family:<name>" for the first matching rule or "unassigned" when
// no rule matches. The owner is a ticket id, the OwnerStructural marker for
// non-observable structure, or empty when unassigned.
func AssignOwner(row Row) (owner string, source string) {
	r := matchOwnerRule(row)
	if r == nil {
		return "", "unassigned"
	}
	return r.Owner, "family:" + r.Family
}

// DispositionNote returns the triage disposition note recorded for one row —
// the rationale and pinned-source citation of the first matching rule — or
// "" when no rule matches or the rule predates the ticket-34 triage. The
// generator prefixes it with "disposition: " in the row notes.
func DispositionNote(row Row) string {
	if r := matchOwnerRule(row); r != nil {
		return r.Note
	}
	return ""
}

// cratePlatformGates records the crate-level cfg attributes that remove whole
// platform crates from the Windows target. The api-scan records item cfg
// only, so rows of these crates would otherwise look Windows-applicable in
// every profile; the gate note corrects that with the pinned source line.
// Inapplicable-by-platform is an applicability fact, not an approved
// exception, so the rows keep their family owners and the disposition records
// the gate.
var cratePlatformGates = map[string]string{
	"gpui_ce_web":   `crate cfg(target_family = "wasm") at crates/gpui_web/src/gpui_web.rs:1`,
	"gpui_ce_linux": `crate cfg(any(target_os = "linux" , target_os = "freebsd")) at crates/gpui_linux/src/gpui_linux.rs:1`,
	"gpui_ce_macos": `crate cfg(target_os = "macos") at crates/gpui_macos/src/gpui_macos.rs:1`,
	"gpui_ce_apple": `crate cfg(target_os = "macos") at crates/gpui_apple/src/gpui_apple.rs:1`,
}

// PlatformGateNote returns the platform-gate note for one crate's rows (""
// when the crate is not crate-level gated off Windows). The generator writes
// it as a "platform-gate: " notes entry so the Windows inapplicability of
// every row of a gated crate stays visible next to the misleading
// all-profiles membership the scan implies.
func PlatformGateNote(crate string) string {
	return cratePlatformGates[crate]
}

// FamilyPlatformInapplicable names the ticket-34 family that records
// non-Windows platform surface with no Windows-side domain family.
const FamilyPlatformInapplicable = "platform-inapplicable"

// RowDisposition derives the disposition value of one row from its owner
// assignment and crate platform gate. Structural rows are marked by their
// owner; rows of crate-level gated platform crates (and rows placed in the
// platform-inapplicable family) record platform inapplicability even though
// they keep a family owner; everything else is a family assignment.
func RowDisposition(row Row, owner, ownerSource string) string {
	switch {
	case owner == OwnerStructural:
		return DispositionCoreStructure
	case ownerSource == "family:"+FamilyPlatformInapplicable || PlatformGateNote(row.Crate) != "":
		return DispositionPlatformInapplicable
	default:
		return DispositionFamily
	}
}

// RuleUsage reports how many rows each rule matched, in rule order. It is the
// data source for the generated report's family→owner table.
type RuleUsage struct {
	RuleID      int
	Family      string
	Owner       string
	Description string
	Matched     int
}

// RuleUsageForRows computes per-rule match counts over rows.
func RuleUsageForRows(rows []Row) []RuleUsage {
	usage := make([]RuleUsage, len(ownerRules))
	for i, r := range ownerRules {
		usage[i] = RuleUsage{RuleID: r.ID, Family: r.Family, Owner: r.Owner, Description: r.Description}
	}
	for _, row := range rows {
		if r := matchOwnerRule(row); r != nil {
			usage[r.ID-1].Matched++
		}
	}
	return usage
}

func matchOwnerRule(row Row) *OwnerRule {
	for i := range ownerRules {
		if ownerRules[i].Match != nil && ownerRules[i].Match(row) {
			return &ownerRules[i]
		}
	}
	return nil
}

// Matching helpers. All symbol checks are case-sensitive; Rust symbols are
// case-sensitive and the scan preserves case.

func isCrate(r Row, pkgs ...string) bool {
	for _, p := range pkgs {
		if r.Crate == p {
			return true
		}
	}
	return false
}

func isGPUI(r Row) bool { return r.Crate == CrateGPUI }

func symHas(r Row, subs ...string) bool {
	for _, s := range subs {
		if strings.Contains(r.Symbol, s) {
			return true
		}
	}
	return false
}

func symHasFold(r Row, sub string) bool {
	return strings.Contains(strings.ToLower(r.Symbol), sub)
}

func symPrefix(r Row, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(r.Symbol, p) {
			return true
		}
	}
	return false
}

func symIs(r Row, syms ...string) bool {
	for _, s := range syms {
		if r.Symbol == s {
			return true
		}
	}
	return false
}

// cfgFeatureIs reports whether any of the row's cfg expressions compares one
// of the given features.
func cfgFeatureIs(r Row, feats ...string) bool {
	for _, expr := range r.Cfg {
		for _, name := range CfgFeatureNames(expr) {
			for _, want := range feats {
				if name == want {
					return true
				}
			}
		}
	}
	return false
}

// cfgTestGated reports whether any cfg expression mentions the test-support
// feature or the bare test flag.
func cfgTestGated(r Row) bool {
	if cfgFeatureIs(r, "test-support") {
		return true
	}
	for _, expr := range r.Cfg {
		if CfgHasBareToken(expr, "test") {
			return true
		}
	}
	return false
}

// ownerRules is the ordered assignment table. Rule groups follow the
// conformance inventory's family list; within the table, cfg-driven
// applicability rules come first, then the symbol-based module families,
// then the per-crate fallbacks, and finally the ticket-34 triage
// dispositions that replaced the former core-support-triage catch-all.
var ownerRules = []OwnerRule{
	{ID: 1, Family: "debug-inspector", Owner: "27", Description: "cfg mentions feature \"inspector\"",
		Match: func(r Row) bool { return cfgFeatureIs(r, "inspector") }},
	{ID: 2, Family: "entities-effects-test", Owner: "03", Description: "test-gated symbol under gpui::app/executor/subscription (entity and effect test surface)",
		Match: func(r Row) bool {
			return cfgTestGated(r) && isGPUI(r) &&
				symPrefix(r, "gpui::app", "gpui::executor", "gpui::subscription")
		}},
	{ID: 3, Family: "profile-closure", Owner: "01", Description: "cfg mentions feature \"test-support\" or the test flag (test-support profile closure), excluding headless-readback rows which ticket 32 owns",
		Match: func(r Row) bool {
			// render_to_image / PlatformHeadlessRenderer rows stay with the
			// headless-readback family (ticket 32) even when test-gated.
			if isGPUI(r) && symHas(r, "Headless", "render_to_image") {
				return false
			}
			return cfgTestGated(r)
		}},
	{ID: 4, Family: "toolchain-applicability", Owner: "01", Description: "cfg mentions hot-patching/profiler/stacker/leak-detection/bench-support, or symbol under the feature-gated gpui::profiler module",
		Match: func(r Row) bool {
			return cfgFeatureIs(r, "hot-patching", "profiler", "stacker", "leak-detection", "bench-support") ||
				(isGPUI(r) && symPrefix(r, "gpui::profiler"))
		}},
	{ID: 5, Family: "alternate-gpu", Owner: "01", Description: "cfg mentions custom-gpu/wgpu-surfaces/wgpu, crate gpui_ce_wgpu, wgpu/GpuSpecs symbols, or WebBackendPreference (web GPU backend selection, wasm-only)",
		Match: func(r Row) bool {
			return cfgFeatureIs(r, "custom-gpu", "wgpu-surfaces", "wgpu") ||
				isCrate(r, "gpui_ce_wgpu") ||
				symHas(r, "GpuSpecs", "Wgpu", "wgpu", "WebBackendPreference")
		}},
	{ID: 6, Family: "device-loss", Owner: "29", Description: "gpui symbol for GPU device loss observation",
		Match: func(r Row) bool { return isGPUI(r) && symHas(r, "device_lost") }},
	{ID: 7, Family: "alternate-gpu-context", Owner: "01", Description: "gpui symbol exposing custom GPU contexts/specs",
		Match: func(r Row) bool { return isGPUI(r) && symHas(r, "gpu_context", "gpu_specs") }},
	{ID: 8, Family: "screen-capture", Owner: "28", Description: "cfg mentions screen-capture, ScreenCapture/screen_capture symbol, or capture-related gpui_ce_media symbol",
		Match: func(r Row) bool {
			return cfgFeatureIs(r, "screen-capture") ||
				symHas(r, "ScreenCapture", "screen_capture") ||
				(isCrate(r, "gpui_ce_media") && symHasFold(r, "capture"))
		}},
	{ID: 9, Family: "embedded-assets", Owner: "18", Description: "cfg mentions feature \"embedded-assets\"",
		Match: func(r Row) bool { return cfgFeatureIs(r, "embedded-assets") }},
	{ID: 10, Family: "layout", Owner: "06", Description: "taffy/style/styled/geometry modules, layout types (LayoutId, AvailableSpace) or refinement, or the refineable crates",
		Match: func(r Row) bool {
			return (isGPUI(r) &&
				(symPrefix(r, "gpui::taffy", "gpui::style", "gpui::geometry") ||
					symHas(r, "LayoutId", "layout_engine", "AvailableSpace", "refinement", "Refinement", "refine", "Refine", "Refineable") ||
					symIs(r, "gpui::Styled"))) ||
				symIs(r, "gpui::prelude::Styled") ||
				isCrate(r, "gpui_ce_refineable", "gpui_ce_derive_refineable")
		}},
	{ID: 11, Family: "drag-drop", Owner: "24", Description: "app-level external drag payloads (AnyDrag, ExternalDrag*)",
		Match: func(r Row) bool { return isGPUI(r) && symHas(r, "AnyDrag", "ExternalDrag") }},
	{ID: 12, Family: "entities-effects", Owner: "03", Description: "gpui::app/subscription/global modules, the AppContext/EventEmitter surface and entity reservations",
		Match: func(r Row) bool {
			return isGPUI(r) &&
				(symPrefix(r, "gpui::app", "gpui::subscription", "gpui::global", "gpui::AppContext", "gpui::BorrowAppContext", "gpui::EventEmitter", "gpui::Reservation") ||
					symIs(r, "gpui::prelude::BorrowAppContext", "gpui::prelude::Context"))
		}},
	{ID: 13, Family: "executors", Owner: "04", Description: "executor/platform_scheduler/queue modules, dispatchers, future timeouts, tokio and scheduler crates",
		Match: func(r Row) bool {
			return (isGPUI(r) &&
				(symPrefix(r, "gpui::executor", "gpui::platform_scheduler", "gpui::queue",
					"gpui::util::FutureExt", "gpui::util::WithTimeout", "gpui::util::Timeout") ||
					symIs(r, "gpui::FutureExt", "gpui::Timeout", "gpui::block_on",
						"gpui::PriorityQueueReceiver", "gpui::PriorityQueueSender") ||
					symHas(r, "Dispatcher", "dispatcher", "Runnable", "Timer", "FutureExt", "WithTimeout", "background_executor", "foreground_executor"))) ||
				isCrate(r, "gpui_ce_tokio", "gpui_ce_scheduler")
		}},
	{ID: 14, Family: "actions-keymaps", Owner: "12", Description: "action/keymap/key_dispatch modules, KeyBinding/KeymapContext/ActionRegistry, action macros",
		Match: func(r Row) bool {
			return (isGPUI(r) &&
				(symPrefix(r, "gpui::action", "gpui::keymap", "gpui::key_dispatch") ||
					symHas(r, "KeyBinding", "KeymapContext", "ActionRegistry"))) ||
				symHas(r, "register_action", "derive_action")
		}},
	{ID: 15, Family: "keyboard-input", Owner: "13", Description: "input/tab_stop modules, platform keystroke/keyboard, Keystroke/Modifiers symbols",
		Match: func(r Row) bool {
			return isGPUI(r) &&
				(symPrefix(r, "gpui::input", "gpui::tab_stop", "gpui::platform::keystroke", "gpui::platform::keyboard") ||
					symHas(r, "Keystroke", "Modifiers"))
		}},
	{ID: 16, Family: "inline-layout", Owner: "15", Description: "text_system line_layout module and inline layout requests",
		Match: func(r Row) bool {
			return isGPUI(r) &&
				(symPrefix(r, "gpui::text_system::line_layout") ||
					(symPrefix(r, "gpui::text_system") && symHas(r, "Inline")))
		}},
	{ID: 17, Family: "glyph-raster", Owner: "10", Description: "text_system raster/glyph/atlas/subpixel items and raster/glyph/atlas items of gpui_ce_parley",
		Match: func(r Row) bool {
			return (isGPUI(r) && symPrefix(r, "gpui::text_system") &&
				symHas(r, "raster", "Raster", "glyph", "Glyph", "atlas", "Atlas", "subpixel", "Subpixel")) ||
				(isCrate(r, "gpui_ce_parley") &&
					symHas(r, "raster", "Raster", "glyph", "Glyph", "atlas", "Atlas"))
		}},
	{ID: 18, Family: "text-geometry", Owner: "09", Description: "text_system module, TextSystem symbols, or the gpui_ce_parley crate",
		Match: func(r Row) bool {
			return (isGPUI(r) &&
				(symPrefix(r, "gpui::text_system") || symHas(r, "TextSystem", "text_system"))) ||
				isCrate(r, "gpui_ce_parley")
		}},
	{ID: 19, Family: "paths-filters", Owner: "16", Description: "path_builder, elements::surface, scene path/filter/surface/gradient/blend/shadow/shader items, ContentMask",
		Match: func(r Row) bool {
			return isGPUI(r) &&
				(symPrefix(r, "gpui::path_builder", "gpui::elements::surface") ||
					(symPrefix(r, "gpui::scene") &&
						symHas(r, "Path", "Filter", "Surface", "Gradient", "Blend", "Shadow", "Shader")) ||
					symHas(r, "ContentMask"))
		}},
	{ID: 20, Family: "scene-kernel", Owner: "08", Description: "scene/bounds_tree modules, Primitive/Batch/Sprite/Quad/Underline/atlas symbols",
		Match: func(r Row) bool {
			return isGPUI(r) &&
				(symPrefix(r, "gpui::scene", "gpui::bounds_tree") ||
					symHas(r, "Primitive", "Batch", "Sprite", "Quad", "Underline", "atlas", "Atlas"))
		}},
	{ID: 21, Family: "platform-test", Owner: "01", Description: "platform test/visual_test modules not already caught by cfg",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::platform::test", "gpui::platform::tests", "gpui::platform::visual_test")
		}},
	{ID: 22, Family: "platform-clipboard", Owner: "23", Description: "platform clipboard/pasteboard items",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::platform") &&
				symHas(r, "Clipboard", "clipboard", "pasteboard", "Pasteboard")
		}},
	{ID: 23, Family: "platform-dialogs-credentials", Owner: "25", Description: "platform prompt/credential/keyring items",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::platform") &&
				symHas(r, "prompt", "Prompt", "credential", "Credential", "keyring", "Keyring", "files_and_dirs")
		}},
	{ID: 24, Family: "platform-keyboard", Owner: "13", Description: "platform keyboard_mapper/keyboard_layout items",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::platform") &&
				symHas(r, "keyboard", "Keyboard")
		}},
	{ID: 25, Family: "platform-ime", Owner: "20", Description: "platform input handler, text input, IME and soft keyboard items",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::platform") &&
				symHas(r, "input_handler", "InputHandler", "text_input", "TextInput", "ime", "Ime",
					"soft_keyboard", "SoftKeyboard", "autocapitalize", "Autocapitalize", "UTF16")
		}},
	{ID: 26, Family: "platform-desktop", Owner: "26", Description: "platform menus, jump lists, dock, notifications, restart, reveal, urls, thermal, app path, lifecycle, popup, cursor hide and other desktop operations",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::platform") &&
				symHas(r, "menu", "Menu", "notification", "Notification", "jump", "destination",
					"dock", "Dock", "restart", "reveal", "url", "Url", "thermal", "Thermal",
					"app_path", "auxiliary", "lifecycle", "Lifecycle", "button_layout", "ButtonLayout",
					"popup", "character_palette", "system_bell", "compositor", "recent_document",
					"app_identity", "mac_activation", "scrollbar", "hide", "open_with",
					"wake", "reopen", "memory", "quit", "Tasks")
		}},
	{ID: 27, Family: "platform-drag-touch", Owner: "24", Description: "platform drag/drop/touch/haptic/gesture items",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::platform") &&
				symHas(r, "drag", "Drag", "drop", "Drop", "touch", "Touch", "haptic", "Haptic", "gesture", "Gesture")
		}},
	{ID: 28, Family: "accessibility-tree", Owner: "21", Description: "a11y symbols (tree building, node bounds, actions)",
		Match: func(r Row) bool { return isGPUI(r) && symHas(r, "a11y", "A11y") }},
	{ID: 29, Family: "headless-readback", Owner: "32", Description: "PlatformHeadlessRenderer and render_to_image",
		Match: func(r Row) bool { return isGPUI(r) && symHas(r, "Headless", "render_to_image") }},
	{ID: 30, Family: "image-codecs", Owner: "17", Description: "platform Image and ImageFormat types",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::platform::Image", "gpui::platform::ImageFormat")
		}},
	{ID: 31, Family: "platform-cursor", Owner: "14", Description: "platform cursor style items",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::platform") && symHas(r, "cursor", "Cursor")
		}},
	{ID: 32, Family: "platform-frame-present", Owner: "07", Description: "platform window draw/present entry points",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::platform") && symHas(r, "draw", "present", "swap_buffers")
		}},
	{ID: 33, Family: "platform-window", Owner: "05", Description: "remaining gpui::platform items: Platform/PlatformWindow window operations and window types",
		Match: func(r Row) bool { return isGPUI(r) && symPrefix(r, "gpui::platform") }},
	{ID: 34, Family: "window-prompts", Owner: "25", Description: "window prompts module and prompt methods",
		Match: func(r Row) bool {
			return isGPUI(r) &&
				(symPrefix(r, "gpui::window::prompts") ||
					(symPrefix(r, "gpui::window") && symHas(r, "prompt", "Prompt")))
		}},
	{ID: 35, Family: "window-inspector", Owner: "27", Description: "window inspector picking/state methods",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") && symHas(r, "inspector", "Inspector")
		}},
	{ID: 36, Family: "window-tasks", Owner: "04", Description: "window spawn/to_async task methods",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") && symHas(r, "spawn", "to_async")
		}},
	{ID: 37, Family: "window-desktop", Owner: "26", Description: "window system bell, character palette, window menu and layout/appearance observers",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") &&
				symHas(r, "bell", "character_palette", "window_menu", "button_layout", "window_appearance")
		}},
	{ID: 38, Family: "window-entities", Owner: "03", Description: "window observe/subscribe/transact entity effects",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") &&
				symHas(r, "observe", "subscribe", "transact")
		}},
	{ID: 39, Family: "window-image-cache", Owner: "18", Description: "window image cache methods",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") && symHas(r, "image_cache", "ImageCache")
		}},
	{ID: 40, Family: "window-image", Owner: "17", Description: "window image paint/drop methods",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") && symHas(r, "image", "Image")
		}},
	{ID: 41, Family: "window-svg", Owner: "19", Description: "window svg paint methods",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") && symHas(r, "svg", "Svg")
		}},
	{ID: 42, Family: "window-glyph", Owner: "10", Description: "window glyph/emoji paint methods",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") && symHas(r, "glyph", "Glyph", "emoji", "Emoji")
		}},
	{ID: 43, Family: "window-drag-drop", Owner: "24", Description: "window external drag methods",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") && symHas(r, "drag", "Drag")
		}},
	{ID: 44, Family: "window-paths-filters", Owner: "16", Description: "window path/filter/surface/shadow/gradient/blend paint methods",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") &&
				symHas(r, "paint_path", "Path", "filter", "Filter", "surface", "Surface",
					"shadow", "Shadow", "gradient", "Gradient", "blend", "Blend")
		}},
	{ID: 45, Family: "window-keyboard", Owner: "13", Description: "window keyboard/focus/dispatch/action/binding methods and focus types",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") &&
				symHas(r, "keyboard", "Keyboard", "keystroke", "Keystroke", "key_context", "keymap",
					"key_event", "focus", "Focus", "blur", "dispatch", "Dispatch", "action", "Action",
					"binding", "Binding", "Navigation", "pending_input", "capslock", "InputRate")
		}},
	{ID: 46, Family: "window-pointer", Owner: "14", Description: "window hitbox, mouse, scroll, cursor, tooltip, deferred draw, keyed state, arena and refresh items",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") &&
				symHas(r, "hitbox", "Hitbox", "HitTest", "mouse", "Mouse", "scroll", "Scroll",
					"cursor", "Cursor", "tooltip", "Tooltip", "defer", "refresh", "pointer",
					"capture", "long_press", "touch_prediction", "keyed", "state", "arena", "Arena",
					"frame", "current_view", "view_id", "root", "simulate", "invalidat", "Invalidator", "Dismiss")
		}},
	{ID: 47, Family: "window-scene", Owner: "08", Description: "window paint/draw/render/quad methods and PaintQuad",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") &&
				(symHas(r, "paint", "Paint", "draw", "Draw", "render", "Render", "quad", "sprite", "fill", "outline") ||
					symIs(r, "gpui::window::quad", "gpui::window::fill", "gpui::window::outline"))
		}},
	{ID: 48, Family: "window-inline-text", Owner: "15", Description: "window text_style stack methods",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") && symHas(r, "text_style", "TextStyle")
		}},
	{ID: 49, Family: "window-layout", Owner: "06", Description: "window layout computation, pixel snapping, rem size, line height and element offsets",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") &&
				symHas(r, "layout", "Layout", "pixel_snap", "rem_size", "line_height", "measured", "offset")
		}},
	{ID: 50, Family: "window-assets", Owner: "18", Description: "window asset access methods",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") && symHas(r, "asset", "Asset")
		}},
	{ID: 51, Family: "window-authoring", Owner: "11", Description: "window ElementId/ManagedView authoring items",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") &&
				symHas(r, "ElementId", "Element", "ManagedView")
		}},
	{ID: 52, Family: "window-viewport", Owner: "05", Description: "window viewport size",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::window") && symHas(r, "viewport")
		}},
	{ID: 53, Family: "window-foreground", Owner: "05", Description: "remaining gpui::window items: window management operations routed to the platform window",
		Match: func(r Row) bool { return isGPUI(r) && symPrefix(r, "gpui::window") }},
	{ID: 54, Family: "arena", Owner: "14", Description: "element arena (deferred draw scratch state)",
		Match: func(r Row) bool { return isGPUI(r) && symPrefix(r, "gpui::arena") }},
	{ID: 55, Family: "elements-img", Owner: "18", Description: "elements::img and image_cache modules, image elements",
		Match: func(r Row) bool {
			return isGPUI(r) &&
				(symPrefix(r, "gpui::elements::img", "gpui::elements::image_cache") ||
					symHas(r, "Image", "image"))
		}},
	{ID: 56, Family: "svg-fonts", Owner: "19", Description: "svg_renderer and elements::svg modules, svg symbols",
		Match: func(r Row) bool {
			return isGPUI(r) &&
				(symPrefix(r, "gpui::svg_renderer", "gpui::elements::svg") || symHas(r, "svg", "Svg"))
		}},
	{ID: 57, Family: "animation", Owner: "18", Description: "motion/spring/transition/animated/lerp modules and the animation element",
		Match: func(r Row) bool {
			return isGPUI(r) &&
				symPrefix(r, "gpui::motion", "gpui::spring", "gpui::transition", "gpui::animated", "gpui::lerp", "gpui::elements::animation")
		}},
	{ID: 58, Family: "elements-list", Owner: "14", Description: "elements::list and uniform_list modules, scrollbar symbols",
		Match: func(r Row) bool {
			return isGPUI(r) &&
				(symPrefix(r, "gpui::elements::list", "gpui::elements::uniform_list") ||
					symHas(r, "uniform_list", "scrollbar", "Scrollbar"))
		}},
	{ID: 59, Family: "elements-text", Owner: "15", Description: "elements::text inline text elements",
		Match: func(r Row) bool { return isGPUI(r) && symPrefix(r, "gpui::elements::text") }},
	{ID: 60, Family: "elements-deferred", Owner: "14", Description: "elements deferred/anchored overlay adapters",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::elements") &&
				symHas(r, "deferred", "Deferred", "anchored", "Anchored")
		}},
	{ID: 61, Family: "elements-canvas", Owner: "08", Description: "elements canvas custom paint adapter",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::elements") && symHas(r, "canvas", "Canvas")
		}},
	{ID: 62, Family: "elements-container-query", Owner: "06", Description: "elements container query adapter (layout-driven restyling)",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::elements") && symHas(r, "container_query", "ContainerQuery")
		}},
	{ID: 63, Family: "elements-authoring", Owner: "11", Description: "remaining elements:: items: div authoring, interactivity, stateful wrappers",
		Match: func(r Row) bool { return isGPUI(r) && symPrefix(r, "gpui::elements") }},
	{ID: 64, Family: "interactive-keyboard", Owner: "13", Description: "interactive keyboard/modifier/navigation event types",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::interactive") &&
				symHas(r, "Key", "Keyboard", "Navigation")
		}},
	{ID: 65, Family: "interactive-drag-drop", Owner: "24", Description: "interactive drag/drop/external path event types",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::interactive") &&
				symHas(r, "Drag", "drag", "Drop", "drop", "External", "File")
		}},
	{ID: 66, Family: "interactive-touch", Owner: "24", Description: "interactive touch/pinch/gesture event types",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::interactive") &&
				symHas(r, "Touch", "touch", "Pinch", "Gesture")
		}},
	{ID: 67, Family: "interactive-pointer", Owner: "14", Description: "remaining interactive:: items: mouse, click and scroll event types",
		Match: func(r Row) bool { return isGPUI(r) && symPrefix(r, "gpui::interactive") }},
	{ID: 68, Family: "authoring", Owner: "11", Description: "element/view/prelude modules, IntoElement/ParentElement/InteractiveElement/Render traits, element ids",
		Match: func(r Row) bool {
			return isGPUI(r) &&
				(symPrefix(r, "gpui::element", "gpui::view", "gpui::prelude", "gpui::util::FluentBuilder",
					"gpui::IntoElement", "gpui::ParentElement", "gpui::InteractiveElement",
					"gpui::StatefulInteractiveElement", "gpui::Render", "gpui::VisualContext") ||
					symHas(r, "GlobalElementId", "InspectorElementId", "any_view", "AnyView", "ViewElement"))
		}},
	{ID: 69, Family: "assets-http", Owner: "18", Description: "assets, http_client, asset_cache and shared_uri modules",
		Match: func(r Row) bool {
			return isGPUI(r) &&
				symPrefix(r, "gpui::assets", "gpui::http_client", "gpui::asset_cache", "gpui::shared_uri")
		}},
	{ID: 70, Family: "gestures-scroll", Owner: "14", Description: "gesture scroll physics",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::gestures") && symHas(r, "Scroll")
		}},
	{ID: 71, Family: "gestures-touch", Owner: "24", Description: "gesture tuning for touch and multi-tap recognition",
		Match: func(r Row) bool { return isGPUI(r) && symPrefix(r, "gpui::gestures") }},
	{ID: 72, Family: "uia-provider", Owner: "22", Description: "accesskit re-exports and a11y role types",
		Match: func(r Row) bool {
			return isGPUI(r) &&
				(symPrefix(r, "gpui::accesskit") ||
					symIs(r, "gpui::Orientation", "gpui::Role", "gpui::Toggled", "gpui::AccessibleAction"))
		}},
	{ID: 73, Family: "theme-palette", Owner: "06", Description: "colors module (default theme palette used by styling)",
		Match: func(r Row) bool {
			return isGPUI(r) && (symPrefix(r, "gpui::colors::") || symIs(r, "gpui::colors"))
		}},
	{ID: 74, Family: "color", Owner: "16", Description: "color module types and conversions (rendering color rules)",
		Match: func(r Row) bool {
			return isGPUI(r) && (symPrefix(r, "gpui::color::") || symIs(r, "gpui::color"))
		}},
	{ID: 75, Family: "test-macros", Owner: "01", Description: "root test/bench/proptest macros and the test module",
		Match: func(r Row) bool {
			return isGPUI(r) &&
				(symPrefix(r, "gpui::test", "gpui::proptest") ||
					symIs(r, "gpui::bench", "gpui::bench_group", "gpui::bench_main", "gpui::property_test"))
		}},
	{ID: 76, Family: "core-exports", Owner: "01", Description: "root wildcard re-export rows (the crate's export surface)",
		Match: func(r Row) bool { return symIs(r, "gpui::*") }},
	{ID: 77, Family: "inspector-module", Owner: "27", Description: "remaining gpui::inspector module items",
		Match: func(r Row) bool { return isGPUI(r) && symPrefix(r, "gpui::inspector") }},

	// Per-crate fallbacks for the supporting crates.
	{ID: 78, Family: "windows-clipboard", Owner: "23", Description: "gpui_ce_windows clipboard module",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_windows") && symHas(r, "clipboard") }},
	{ID: 79, Family: "windows-jump-list", Owner: "26", Description: "gpui_ce_windows destination list (jump list)",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_windows") && symHas(r, "destination_list", "jump") }},
	{ID: 80, Family: "windows-direct-manipulation", Owner: "24", Description: "gpui_ce_windows direct manipulation",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_windows") && symHas(r, "direct_manipulation") }},
	{ID: 81, Family: "windows-raster", Owner: "10", Description: "gpui_ce_windows directx atlas and font rasterizer",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_windows") && symHas(r, "directx_atlas", "font_rasterizer", "atlas", "raster")
		}},
	{ID: 82, Family: "windows-renderer", Owner: "07", Description: "gpui_ce_windows directx devices/renderer and vsync",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_windows") && symHas(r, "directx_devices", "directx_renderer", "vsync", "renderer")
		}},
	{ID: 83, Family: "windows-dispatcher", Owner: "04", Description: "gpui_ce_windows dispatcher",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_windows") && symHas(r, "dispatcher") }},
	{ID: 84, Family: "windows-keyboard", Owner: "13", Description: "gpui_ce_windows events and keyboard modules",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_windows") && symHas(r, "events", "keyboard")
		}},
	{ID: 85, Family: "windows-desktop", Owner: "26", Description: "gpui_ce_windows notifications and system settings",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_windows") && symHas(r, "system_notifications", "system_settings", "notification", "settings")
		}},
	{ID: 86, Family: "windows-window", Owner: "05", Description: "gpui_ce_windows window, display, platform and wrapper modules",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_windows") && symHas(r, "window", "display", "platform", "wrapper")
		}},
	{ID: 87, Family: "render-paths-filters", Owner: "16", Description: "gpui_ce_render blur, path types and shaders",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_render") && symHas(r, "blur", "path_types", "shaders", "shader", "path")
		}},
	{ID: 88, Family: "render-scene", Owner: "08", Description: "gpui_ce_render instances and InstanceRange",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_render") && symHas(r, "instances", "InstanceRange")
		}},
	{ID: 89, Family: "render-artifacts", Owner: "07", Description: "gpui_ce_render build artifacts",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_render") && symHas(r, "artifacts") }},
	{ID: 90, Family: "platform-crate-executor", Owner: "04", Description: "gpui_ce_platform background executor",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_platform") && symHas(r, "background_executor") }},
	{ID: 91, Family: "platform-crate-headless", Owner: "32", Description: "gpui_ce_platform headless renderer selection",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_platform") && symHas(r, "headless") }},
	{ID: 92, Family: "platform-crate-window", Owner: "05", Description: "gpui_ce_platform application and platform selection",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_platform") && symHas(r, "application", "current_platform", "Platform")
		}},
	{ID: 93, Family: "editable-text", Owner: "20", Description: "gpui_ce_elements editable text editor (IME and text input surface)",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_elements") && symPrefix(r, "gpui_ce_elements::editable_text")
		}},
	{ID: 94, Family: "elements-crate-authoring", Owner: "11", Description: "remaining gpui_ce_elements items",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_elements") }},
	{ID: 95, Family: "macros-authoring", Owner: "11", Description: "gpui_ce_macros authoring macros (styles, derives, tests)",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_macros") }},
	{ID: 96, Family: "media-codecs", Owner: "17", Description: "gpui_ce_media CoreMedia/CoreVideo bindings (image codec surface)",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_media") }},
	{ID: 97, Family: "path-ops", Owner: "26", Description: "gpui_ce_path path styles and operations",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_path") }},
	{ID: 98, Family: "os-clipboard", Owner: "23", Description: "macos/web pasteboard or clipboard items",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_macos", "gpui_ce_web") && symHas(r, "clipboard", "Clipboard", "pasteboard", "Pasteboard")
		}},
	{ID: 99, Family: "os-text", Owner: "09", Description: "macos text system",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_macos") && symHas(r, "text_system") }},
	{ID: 100, Family: "os-input", Owner: "13", Description: "macos/web/linux keyboard and event items",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_macos", "gpui_ce_web", "gpui_ce_linux") && symHas(r, "keyboard", "Keyboard", "events")
		}},
	{ID: 101, Family: "os-ime", Owner: "20", Description: "web ime mirror",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_web") && symHas(r, "ime") }},
	{ID: 102, Family: "os-http", Owner: "18", Description: "web fetch http client",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_web") && symHas(r, "http_client", "HttpClient") }},
	{ID: 103, Family: "os-raster", Owner: "10", Description: "apple metal atlas",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_apple") && symHas(r, "atlas") }},
	{ID: 104, Family: "os-renderer", Owner: "07", Description: "apple/macos renderers",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_apple", "gpui_ce_macos") && symHas(r, "renderer")
		}},
	{ID: 105, Family: "os-haptic", Owner: "24", Description: "macos haptic feedback",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_macos") && symHas(r, "haptic") }},
	{ID: 106, Family: "os-notifications", Owner: "26", Description: "macos system notifications",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_macos") && symHas(r, "notification") }},
	{ID: 107, Family: "os-dispatcher", Owner: "04", Description: "macos/web dispatcher",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_macos", "gpui_ce_web") && symHas(r, "dispatcher", "Dispatcher")
		}},
	{ID: 108, Family: "os-window", Owner: "05", Description: "macos/apple/linux/web window, display and platform items",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_macos", "gpui_ce_apple", "gpui_ce_linux", "gpui_ce_web") &&
				symHas(r, "window", "Window", "display", "Display", "platform", "Platform", "window_appearance")
		}},
	{ID: 109, Family: "embedded-assets", Owner: "18", Description: "zed_util rust-embed/fs-embed asset machinery",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_zed_util") && symHas(r, "embed", "Embed")
		}},

	// Ticket 34 (core-support triage) dispositions. These rules replace the
	// former core-support-triage catch-all: every row that used to land there
	// now has a concrete disposition — an owner ticket for the family whose
	// public API exercises it, the OwnerStructural marker for non-observable
	// structure, or a platform-inapplicability record kept by the applicability
	// owner (ticket 01). Each rule's Note is written onto every row it matches
	// as the machine-greppable provenance for the disposition; triage.md in the
	// generated output holds the full group-by-group record.
	{ID: 110, Family: "shared-string-value", Owner: "03", Description: "gpui_ce_shared_string: SharedString value type re-exported wholesale through gpui",
		Note:  "gpui public value type (pub use gpui_shared_string::* at crates/gpui/src/gpui.rs:143) whose interning, PartialEq-across-representations, Display/Deref/AsRef, From and serde semantics are exercised through every consuming public API: action names and Unbind payloads (action.rs:447, ticket 12), keymap context keys and binding action_input (keymap/context.rs:16-18, keymap/binding.rs:124, ticket 13), div groups and aria labels (elements/div.rs:901-1635, tickets 11/21), text elements and font families (elements/text.rs, text_system.rs:350,886, style.rs:598, tickets 15/09/06), window/tab titles (app.rs:406,558, ticket 05), asset/cache paths (tickets 18) and menu labels (platform/app_menu.rs, ticket 26); assigned to the entities/data-plumbing family as the shared value-type owner per ticket 34",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_shared_string") }},
	{ID: 111, Family: "collections-maps", Owner: "03", Description: "gpui_ce_collections map/hash machinery gpui's app and entity plumbing uses",
		Note: "deterministic Fx-hash map machinery backing gpui app/entity data plumbing: exposed through App::tab_groups (app.rs:447), Window::take_views (window.rs:282), EntityMap::extend_accessed/accessed_entities (entity_map.rs:58,174 — ticket 03 access dependency tracking) and the action registry maps (action.rs:402-410, ticket 12); iteration order of the Fx-hash maps is deterministic and observable through those APIs; assigned to 03 per ticket 34",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_collections") &&
				symIs(r, "collections::HashMap", "collections::HashSet", "collections::TypeIdHashMap", "collections::TypeIdHashSet",
					"collections::FxBuildHasher", "collections::FxHashMap", "collections::FxHashSet", "collections::FxHasher")
		}},
	{ID: 112, Family: "collections-unused", Owner: OwnerStructural, Description: "gpui_ce_collections items no Windows-compiled crate uses (IndexMap/IndexSet/Equivalent/vecmap/std wildcard)",
		Note: "non-observable structure: IndexMap/IndexSet appear only in gpui_macos/src/window.rs (crate-gated off Windows), Equivalent is indexmap lookup machinery for those maps, vecmap has no gpui call site and the * row only re-exports std::collections (std behavior exercised through consuming APIs); no gpui public API exercises any of these per ticket 34",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_collections") &&
				symIs(r, "collections::IndexMap", "collections::IndexSet", "collections::Equivalent", "collections::*", "collections::vecmap")
		}},
	{ID: 113, Family: "sum-tree-lists", Owner: "14", Description: "gpui_ce_sum_tree machinery behind the list/uniform_list item trees and tab-stop order",
		Note:  "sum-tree machinery behind gpui's list data structures: elements/list.rs:19,65,1024 drives List/uniform_list measured items, scrolling and keyed state (impls at list.rs:1667-1742 exercise Item/Summary/Dimension/SeekTarget) and tab_stop.rs:3,15,72 drives the tab-stop focus order (ticket 13's tab order evidence); assigned to the lists/retained-scroll family as the primary consumer per ticket 34",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_sum_tree") }},
	{ID: 114, Family: "util-arc-cow", Owner: OwnerStructural, Description: "gpui_ce_util arc_cow: ArcCow re-exported through gpui but unused by any gpui API",
		Note: "non-observable structure: ArcCow is re-exported at crates/gpui/src/gpui.rs:144 but no gpui public API (nor any Windows-compiled crate) uses it — generic copy-on-write machinery with no gpui-observable operation per ticket 34; see also the gpui::ArcCow re-export row (family gpui-reexport-structure)",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_util") && symPrefix(r, "gpui_util::arc_cow")
		}},
	{ID: 115, Family: "util-windows-shell", Owner: "26", Description: "gpui_ce_util Windows process helpers used by the Windows platform restart path",
		Note: `Windows process helpers exercised by WindowsPlatform::restart: get_powershell and new_std_command (target_os = "windows") at crates/gpui_windows/src/platform.rs:15,499-511 (gpui::Platform::restart, family platform-desktop); the new_std_command variant gated not(target_os = "windows") is non-Windows-only (cfg records it); assigned to 26 per ticket 34`,
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_util") &&
				symIs(r, "gpui_util::new_std_command", "gpui_util::get_powershell")
		}},
	{ID: 116, Family: "util-async-errors", Owner: "04", Description: "gpui_ce_util async/result error propagation machinery used by the executor and app error paths",
		Note: "async/result error propagation machinery: executor.rs:5 and asset_cache.rs use TryFutureExt/TryFutureExtBacktrace for task error logging, ResultExt/log_err propagate errors across app.rs:36, assets.rs:183, elements/* and window.rs:38; exercised through task outcomes and error traces (ticket 04's output handoff and late-result discard); assigned to 04 per ticket 34",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_util") &&
				(symPrefix(r, "gpui_util::ResultExt", "gpui_util::TryFutureExt", "gpui_util::LogErrorFuture",
					"gpui_util::LogErrorWithBacktraceFuture", "gpui_util::UnwrapFuture", "gpui_util::Future for ") ||
					symIs(r, "gpui_util::log_err", "gpui_util::std::fmt::Display for DebugAsDisplay::fmt"))
		}},
	{ID: 117, Family: "util-deferred", Owner: "03", Description: "gpui_ce_util Deferred scopeguard behind on_drop, deferred effects and timer-resolution guards",
		Note: "scopeguard exercised by public effect and dispatcher APIs: Context::on_drop returns Deferred (app/context.rs:276-284, ticket 03 logical release), async effect deferral (app/async_context.rs:307-310), dispatcher decrement on drop (platform/threaded_dispatcher.rs:61) and PlatformDispatcher::increase_timer_resolution returns TimerResolutionGuard (platform.rs:1136,1171-1173, ticket 04); assigned to entities/effects per ticket 34",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_util") &&
				(symPrefix(r, "gpui_util::Deferred") ||
					symIs(r, "gpui_util::defer", "gpui_util::Drop for Deferred::drop"))
		}},
	{ID: 118, Family: "util-entity-plumbing", Owner: "03", Description: "gpui_ce_util entity-id counter and TypeId hash keys behind typed access",
		Note: "entity identity plumbing: post_inc drives entity handle ids (app/entity_map.rs:967), subscription.rs and window.rs counters; TypeIdHashBuilder/TypeIdHasher key the TypeIdHashMap globals, event listeners and window invalidators (app.rs:755,781,802) and the action registry names_by_type_id (action.rs:235, ticket 12); exercised through ticket 03's typed access and generations evidence; assigned to 03 per ticket 34",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_util") &&
				(symIs(r, "gpui_util::post_inc", "gpui_util::TypeIdHashBuilder", "gpui_util::TypeIdHasher") ||
					symPrefix(r, "gpui_util::std::hash::"))
		}},
	{ID: 119, Family: "util-debug-diagnostics", Owner: "27", Description: "gpui_ce_util debug assertions and env-gated timing diagnostics",
		Note: "debug diagnostics: debug_panic/some_or_debug_panic back entity lifecycle assertions (app.rs:36) and measure records frame durations under ZED_MEASUREMENTS (window.rs:38,1806); the inventory records plain debug assertions and diagnostics with ticket 27 (public debug/test behavior, profile distinctions); assigned to 27 per ticket 34",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_util") &&
				symIs(r, "gpui_util::debug_panic", "gpui_util::some_or_debug_panic", "gpui_util::measure")
		}},
	{ID: 120, Family: "util-unused", Owner: OwnerStructural, Description: "gpui_ce_util items with no gpui call site (maybe!, truncate_to_bottom_n_sorted_by, get_windows_system_shell)",
		Note: "non-observable structure: maybe! is trivial closure-invocation sugar (lib.rs:203-213) with no gpui call site, truncate_to_bottom_n_sorted_by is only referenced by the unused gpui_ce_zed_util tests, and get_windows_system_shell is only called from the unused gpui_ce_zed_util shell module; no gpui public API exercises them per ticket 34",
		Match: func(r Row) bool {
			return isCrate(r, "gpui_ce_util") &&
				symIs(r, "gpui_util::maybe", "gpui_util::truncate_to_bottom_n_sorted_by", "gpui_util::get_windows_system_shell")
		}},
	{ID: 121, Family: "zed-util-unused", Owner: OwnerStructural, Description: "gpui_ce_zed_util: workspace member with no dependents; not linked into the gpui reference graph",
		Note:  "non-observable structure: gpui_ce_zed_util is a workspace member with no dependents — no crate's Cargo.toml references the workspace 'util' dependency (gpui_ce_zed_util), so none of its items is linked into the gpui reference graph on any platform and no gpui public API exercises them per ticket 34",
		Match: func(r Row) bool { return isCrate(r, "gpui_ce_zed_util") }},
	{ID: 122, Family: "perf-tooling", Owner: "01", Description: "perf: dev-toolchain test-performance runner, not a gpui dependency",
		Note:  "dev-toolchain instrumentation: the perf crate is the test runner selected by .cargo/config.toml's perf-test alias (target cfg(true).runner) and depends only on collections/serde/serde_json — it is not a gpui dependency and not observable through any gpui public API; ticket 01 (applicability owner) records the toolchain applicability and any Go replacement per ticket 34",
		Match: func(r Row) bool { return isCrate(r, "perf") }},
	{ID: 123, Family: FamilyPlatformInapplicable, Owner: "01", Description: "wasm/linux-only platform surface with no Windows-side domain family (web logging/init, linux module, platform web init)",
		Note: `platform-inapplicable (not an exception): wasm/linux-only platform support — gpui_ce_web::logging/init_logging (crate cfg(target_family = "wasm")), gpui_ce_linux::linux (crate cfg linux/freebsd) and gpui_ce_platform::single_threaded_web/web_init (cfg target_family = "wasm", only reachable from wasm entrypoints); Windows platform selection runs through gpui_ce_platform::current_platform (ticket 05); applicability record kept by ticket 01 per ticket 34`,
		Match: func(r Row) bool {
			return (isCrate(r, "gpui_ce_web") &&
				(symPrefix(r, "gpui_ce_web::logging") || symIs(r, "gpui_ce_web::init_logging"))) ||
				(isCrate(r, "gpui_ce_linux") && symIs(r, "gpui_ce_linux::linux")) ||
				(isCrate(r, "gpui_ce_platform") &&
					symIs(r, "gpui_ce_platform::single_threaded_web", "gpui_ce_platform::web_init"))
		}},
	{ID: 124, Family: "gpui-private-structure", Owner: OwnerStructural, Description: "gpui private/doc-hidden module paths (private, seal, util)",
		Note: "non-observable structure: gpui::private is a doc(hidden) macro-support re-export module (gpui.rs:80-87), gpui::seal is a private sealed-trait mechanism module (gpui.rs:89-93) and gpui::util is a private module whose members are re-exported individually (gpui.rs:66,175 — FluentBuilder, FutureExt, Timeout have their own rows); none is an observable operation per ticket 34",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::private", "gpui::seal", "gpui::util")
		}},
	{ID: 125, Family: "gpui-doc-structure", Owner: OwnerStructural, Description: "gpui doc-only modules (cfg(doc))",
		Note: "non-observable structure: _accessibility and _ownership_and_data_flow are declared under cfg(doc) (gpui.rs:73-76) and are never compiled into any profile — documentation modules only, no observable operation per ticket 34",
		Match: func(r Row) bool {
			return isGPUI(r) && symPrefix(r, "gpui::_accessibility", "gpui::_ownership_and_data_flow")
		}},
	{ID: 126, Family: "gpui-reexport-structure", Owner: OwnerStructural, Description: "gpui bare re-exports (Result, ctor, ArcCow)",
		Note: "non-observable structure: gpui::Result is the anyhow::Result type alias re-export (gpui.rs:96) whose error behavior is exercised per-API by the owning families, gpui::ctor re-exports the ctor module-initializer attribute macro (gpui.rs:99, Rust-specific mechanism), and gpui::ArcCow re-exports the unused gpui_util type (gpui.rs:144); no own operation per ticket 34",
		Match: func(r Row) bool {
			return isGPUI(r) && symIs(r, "gpui::Result", "gpui::ctor", "gpui::ArcCow")
		}},
}
