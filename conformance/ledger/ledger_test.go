package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// realScanPath is the pinned api-scan artifact, relative to this package's
// directory. Tests that need it skip when it is absent.
const realScanPath = "../../reference/out/api-scan.json"

func realScanAvailable(t *testing.T) bool {
	t.Helper()
	if _, err := os.Stat(realScanPath); err != nil {
		t.Skipf("api-scan artifact not available: %v", err)
		return false
	}
	return true
}

func mustProfiles(t *testing.T) map[string]ProfileRecord {
	t.Helper()
	return DefaultProfiles("../../reference/out/profiles")
}

// --- cfg evaluator -------------------------------------------------------

func TestEvaluateCfg(t *testing.T) {
	on := map[string]bool{"test-support": true, "leak-detection": true, "inspector": true}
	off := map[string]bool{}

	cases := []struct {
		name string
		expr string
		set  map[string]bool
		want bool
	}{
		{"feature on", `feature = "test-support"`, on, true},
		{"feature off", `feature = "test-support"`, off, false},
		{"target os windows", `target_os = "windows"`, off, true},
		{"target os other", `target_os = "macos"`, off, false},
		{"target family windows", `target_family = "windows"`, off, true},
		{"target family wasm", `target_family = "wasm"`, off, false},
		{"bare test flag", `test`, off, false},
		{"bare unix flag", `unix`, off, false},
		{"not feature", `not (feature = "x11")`, off, true},
		{"not feature on", `not (feature = "test-support")`, on, false},
		{"any one true", `any (test , feature = "test-support")`, on, true},
		{"any all false", `any (test , feature = "test-support")`, off, false},
		{"any mixed with os", `any (target_os = "windows" , feature = "test-support")`, off, true},
		{"any with macos", `any (target_os = "macos" , feature = "inspector")`, on, true},
		{"all true", `all (target_os = "windows" , feature = "inspector")`, on, true},
		{"all one false", `all (target_os = "windows" , feature = "inspector")`, off, false},
		{"nested any in all", `all (target_os = "windows" , any (test , feature = "test-support"))`, on, true},
		{"nested any in all false", `all (target_os = "windows" , any (test , feature = "test-support"))`, off, false},
		{"nested not in any", `any (feature = "inspector" , debug_assertions)`, on, true},
		{"nested not in any off", `any (feature = "inspector" , debug_assertions)`, off, false},
		{"all of not", `all (debug_assertions , not (feature = "debug-embed"))`, off, false},
		{"deep nesting", `any (all (target_family = "wasm" , feature = "custom-gpu") , target_os = "macos" , all (target_os = "windows" , feature = "wgpu-surfaces"))`, off, false},
		{"deep nesting windows", `any (all (target_family = "wasm" , feature = "custom-gpu") , target_os = "macos" , all (target_os = "windows" , feature = "wgpu-surfaces"))`, map[string]bool{"wgpu-surfaces": true}, true},
		{"default feature always true", `feature = "default"`, off, true},
		{"space between head and paren", `any (test , feature = "test-support" , feature = "bench-support")`, on, true},
		{"no spaces", `any(test,feature="test-support")`, on, true},
		{"multi-feature or", `any (feature = "wayland" , feature = "x11")`, map[string]bool{"x11": true}, true},
		{"not any", `not (any (test , feature = "test-support"))`, on, false},
		{"not any off", `not (any (test , feature = "test-support"))`, off, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EvaluateCfg(tc.expr, tc.set)
			if err != nil {
				t.Fatalf("EvaluateCfg(%q) error: %v", tc.expr, err)
			}
			if got != tc.want {
				t.Errorf("EvaluateCfg(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

func TestEvaluateCfgErrors(t *testing.T) {
	cases := []string{
		"",
		"feature = ",
		"any (",
		"any ()",
		"not feature",
		"unknown_flag",
		"unknown_pred = \"x\"",
		"feature = \"x\" , feature = \"y\"",
		"any (test feature)",
		`feature = "unterminated`,
	}
	for _, expr := range cases {
		if _, err := EvaluateCfg(expr, map[string]bool{}); err == nil {
			t.Errorf("EvaluateCfg(%q) succeeded, want error", expr)
		}
	}
}

func TestCfgFeatureNames(t *testing.T) {
	got := CfgFeatureNames(`any (test , feature = "test-support" , feature = "bench-support")`)
	if len(got) != 2 || got[0] != "test-support" || got[1] != "bench-support" {
		t.Errorf("CfgFeatureNames = %v, want [test-support bench-support]", got)
	}
	if names := CfgFeatureNames(`target_os = "windows"`); len(names) != 0 {
		t.Errorf("CfgFeatureNames(target_os) = %v, want empty", names)
	}
	// Lenient fallback for a string the tokenizer rejects.
	if names := CfgFeatureNames(`feature = "x" @`); len(names) != 1 || names[0] != "x" {
		t.Errorf("CfgFeatureNames fallback = %v, want [x]", names)
	}
}

func TestCfgHasBareToken(t *testing.T) {
	if !CfgHasBareToken(`any (test , feature = "test-support")`, "test") {
		t.Error("bare test token not found")
	}
	if CfgHasBareToken(`any (target_os = "test" , feature = "x")`, "test") {
		t.Error("quoted string value must not count as a bare token")
	}
	if CfgHasBareToken(`feature = "test-support"`, "test") {
		t.Error("feature name must not count as a bare test token")
	}
}

// --- profiles ------------------------------------------------------------

func TestDefaultProfiles(t *testing.T) {
	profiles := DefaultProfiles("some/dir")
	if len(profiles) != len(DefaultProfileOrder) {
		t.Fatalf("got %d profiles, want %d", len(profiles), len(DefaultProfileOrder))
	}
	wantFeatures := map[string][]string{
		ProfileNativeDefault:           {"default", "wayland", "x11", "windows-manifest"},
		ProfileTestSupport:             {"default", "wayland", "x11", "windows-manifest", "test-support", "leak-detection"},
		ProfileInspectorCapture:        {"default", "wayland", "x11", "windows-manifest", "inspector", "screen-capture"},
		ProfileCustomGPUWgpu:           {"wgpu-surfaces", "custom-gpu"},
		ProfileHotpatchProfilerStacker: {"default", "wayland", "x11", "windows-manifest", "hot-patching", "profiler", "stacker"},
	}
	for id, want := range wantFeatures {
		rec, ok := profiles[id]
		if !ok {
			t.Fatalf("profile %s missing", id)
		}
		if strings.Join(rec.CargoFeatures, ",") != strings.Join(want, ",") {
			t.Errorf("profile %s features = %v, want %v", id, rec.CargoFeatures, want)
		}
		if rec.Name != id {
			t.Errorf("profile %s name = %q, want %q", id, rec.Name, id)
		}
		if rec.Description == "" {
			t.Errorf("profile %s has empty description", id)
		}
		if !strings.HasPrefix(rec.GraphFile, "some/dir/") {
			t.Errorf("profile %s graph file = %q, want under some/dir/", id, rec.GraphFile)
		}
	}
}

func TestResolveProfilesMembership(t *testing.T) {
	profiles := DefaultProfiles("")

	// TestAppContext exists only in the test-support profile.
	res := ResolveProfiles([]string{`any (test , feature = "test-support")`}, profiles)
	if len(res.Profiles) != 1 || res.Profiles[0] != ProfileTestSupport {
		t.Errorf("TestAppContext-like cfg resolved to %v, want [test-support]", res.Profiles)
	}

	// Empty cfg exists in every profile.
	res = ResolveProfiles(nil, profiles)
	if len(res.Profiles) != len(DefaultProfileOrder) {
		t.Errorf("empty cfg resolved to %v, want all %d profiles", res.Profiles, len(DefaultProfileOrder))
	}

	// inspector-gated items exist only under inspector-capture
	// (debug_assertions is false in the recorded release profiles).
	res = ResolveProfiles([]string{`any (feature = "inspector" , debug_assertions)`}, profiles)
	if len(res.Profiles) != 1 || res.Profiles[0] != ProfileInspectorCapture {
		t.Errorf("inspector cfg resolved to %v, want [inspector-capture]", res.Profiles)
	}

	// A macOS-only symbol exists in no Windows profile.
	res = ResolveProfiles([]string{`all (target_os = "macos" , any (test , feature = "test-support"))`}, profiles)
	if len(res.Profiles) != 0 {
		t.Errorf("macos cfg resolved to %v, want none", res.Profiles)
	}

	// Unparseable cfg is conservatively present everywhere with a reported
	// failure.
	res = ResolveProfiles([]string{"garbage cfg ("}, profiles)
	if len(res.Profiles) != len(DefaultProfileOrder) || len(res.FailedCfg) != 1 {
		t.Errorf("garbage cfg resolved to %v (failures %v), want all profiles and one failure", res.Profiles, res.FailedCfg)
	}

	// screen capture needs both the feature and windows.
	res = ResolveProfiles([]string{`all (feature = "screen-capture" , target_os = "windows")`}, profiles)
	if len(res.Profiles) != 1 || res.Profiles[0] != ProfileInspectorCapture {
		t.Errorf("screen-capture cfg resolved to %v, want [inspector-capture]", res.Profiles)
	}
}

// --- owner assignment ----------------------------------------------------

func TestOwnerRulesExercised(t *testing.T) {
	// One synthetic row per rule; each must produce exactly the rule's
	// documented owner and family. gpui crate unless stated otherwise.
	examples := map[int]Row{
		1:   {Symbol: "gpui::window::Window::with_inspector_state", Crate: CrateGPUI, Cfg: []string{`any (feature = "inspector" , debug_assertions)`}},
		2:   {Symbol: "gpui::app::test_context::TestAppContext", Crate: CrateGPUI, Cfg: []string{`any (test , feature = "test-support")`}},
		3:   {Symbol: "gpui::elements::list::tests::uniform_sample", Crate: CrateGPUI, Cfg: []string{`any (test , feature = "test-support")`}},
		4:   {Symbol: "gpui::profiler::journal::Journal", Crate: CrateGPUI},
		5:   {Symbol: "gpui_ce_wgpu::WgpuRenderer", Crate: "gpui_ce_wgpu"},
		6:   {Symbol: "gpui::window::Window::gpu_device_lost", Crate: CrateGPUI},
		7:   {Symbol: "gpui::window::Window::gpu_context", Crate: CrateGPUI},
		8:   {Symbol: "gpui::platform::windows_screen_capture::WindowsScreenCaptureFrame", Crate: CrateGPUI, Cfg: []string{`target_os = "windows"`}},
		9:   {Symbol: "gpui::assets::AssetsSource", Crate: CrateGPUI, Cfg: []string{`feature = "embedded-assets"`}},
		10:  {Symbol: "gpui::taffy::TaffyLayout::layout", Crate: CrateGPUI},
		11:  {Symbol: "gpui::app::AnyDrag::value", Crate: CrateGPUI},
		12:  {Symbol: "gpui::app::App::on_release", Crate: CrateGPUI},
		13:  {Symbol: "gpui::executor::BackgroundExecutor::spawn", Crate: CrateGPUI},
		14:  {Symbol: "gpui::action::Action::boxed_clone", Crate: CrateGPUI},
		15:  {Symbol: "gpui::platform::keystroke::Keystroke::key", Crate: CrateGPUI},
		16:  {Symbol: "gpui::text_system::line_layout::LineLayout::wrap", Crate: CrateGPUI},
		17:  {Symbol: "gpui::text_system::RasterizedGlyph::bounds", Crate: CrateGPUI},
		18:  {Symbol: "gpui::text_system::TextSystem::shape_text", Crate: CrateGPUI},
		19:  {Symbol: "gpui::path_builder::PathBuilder::line_to", Crate: CrateGPUI},
		20:  {Symbol: "gpui::scene::Scene::draw_order", Crate: CrateGPUI},
		21:  {Symbol: "gpui::platform::test::TestPlatform", Crate: CrateGPUI},
		22:  {Symbol: "gpui::platform::ClipboardItem::text", Crate: CrateGPUI},
		23:  {Symbol: "gpui::platform::Platform::prompt_for_paths", Crate: CrateGPUI},
		24:  {Symbol: "gpui::platform::Platform::keyboard_mapper", Crate: CrateGPUI},
		25:  {Symbol: "gpui::platform::PlatformInputHandler::marked_text_range", Crate: CrateGPUI},
		26:  {Symbol: "gpui::platform::Platform::show_system_notification", Crate: CrateGPUI},
		27:  {Symbol: "gpui::platform::Platform::gestures", Crate: CrateGPUI},
		28:  {Symbol: "gpui::window::Window::a11y_tree", Crate: CrateGPUI},
		29:  {Symbol: "gpui::platform::PlatformHeadlessRenderer::render", Crate: CrateGPUI},
		30:  {Symbol: "gpui::platform::ImageFormat::is_png", Crate: CrateGPUI},
		31:  {Symbol: "gpui::platform::CursorStyle::crosshair", Crate: CrateGPUI},
		32:  {Symbol: "gpui::platform::PlatformWindow::draw", Crate: CrateGPUI},
		33:  {Symbol: "gpui::platform::PlatformWindow::set_title", Crate: CrateGPUI},
		34:  {Symbol: "gpui::window::Window::prompt", Crate: CrateGPUI},
		35:  {Symbol: "gpui::window::Window::is_inspector_picking", Crate: CrateGPUI},
		36:  {Symbol: "gpui::window::Window::spawn", Crate: CrateGPUI},
		37:  {Symbol: "gpui::window::Window::play_system_bell", Crate: CrateGPUI},
		38:  {Symbol: "gpui::window::Window::observe_global", Crate: CrateGPUI},
		39:  {Symbol: "gpui::window::Window::with_image_cache", Crate: CrateGPUI},
		40:  {Symbol: "gpui::window::Window::paint_image", Crate: CrateGPUI},
		41:  {Symbol: "gpui::window::Window::paint_svg", Crate: CrateGPUI},
		42:  {Symbol: "gpui::window::Window::paint_glyph", Crate: CrateGPUI},
		43:  {Symbol: "gpui::window::Window::start_external_drag", Crate: CrateGPUI},
		44:  {Symbol: "gpui::window::Window::paint_backdrop_filter", Crate: CrateGPUI},
		45:  {Symbol: "gpui::window::Window::dispatch_action", Crate: CrateGPUI},
		46:  {Symbol: "gpui::window::Window::insert_hitbox", Crate: CrateGPUI},
		47:  {Symbol: "gpui::window::Window::paint_layer", Crate: CrateGPUI},
		48:  {Symbol: "gpui::window::Window::text_style", Crate: CrateGPUI},
		49:  {Symbol: "gpui::window::Window::pixel_snap_point", Crate: CrateGPUI},
		50:  {Symbol: "gpui::window::Window::get_asset", Crate: CrateGPUI},
		51:  {Symbol: "gpui::window::ElementId::new", Crate: CrateGPUI},
		52:  {Symbol: "gpui::window::Window::viewport_size", Crate: CrateGPUI},
		53:  {Symbol: "gpui::window::Window::set_window_title", Crate: CrateGPUI},
		54:  {Symbol: "gpui::arena::Arena::alloc", Crate: CrateGPUI},
		55:  {Symbol: "gpui::elements::img::Img::source", Crate: CrateGPUI},
		56:  {Symbol: "gpui::svg_renderer::SvgRenderer::new", Crate: CrateGPUI},
		57:  {Symbol: "gpui::motion::Motion::new", Crate: CrateGPUI},
		58:  {Symbol: "gpui::elements::list::List::new", Crate: CrateGPUI},
		59:  {Symbol: "gpui::elements::text::Text::new", Crate: CrateGPUI},
		60:  {Symbol: "gpui::elements::deferred::Deferred::new", Crate: CrateGPUI},
		61:  {Symbol: "gpui::elements::canvas::Canvas::new", Crate: CrateGPUI},
		62:  {Symbol: "gpui::elements::container_query::ContainerQuery::new", Crate: CrateGPUI},
		63:  {Symbol: "gpui::elements::div::Div::text", Crate: CrateGPUI},
		64:  {Symbol: "gpui::interactive::KeyDownEvent::key", Crate: CrateGPUI},
		65:  {Symbol: "gpui::interactive::FileDropEvent::paths", Crate: CrateGPUI},
		66:  {Symbol: "gpui::interactive::PinchEvent::delta", Crate: CrateGPUI},
		67:  {Symbol: "gpui::interactive::MouseDownEvent::button", Crate: CrateGPUI},
		68:  {Symbol: "gpui::element::Element::request_layout", Crate: CrateGPUI},
		69:  {Symbol: "gpui::assets::Assets::load", Crate: CrateGPUI},
		70:  {Symbol: "gpui::gestures::ScrollPhysics::fling_velocity", Crate: CrateGPUI},
		71:  {Symbol: "gpui::gestures::GestureTuning::touch_slop", Crate: CrateGPUI},
		72:  {Symbol: "gpui::Role", Crate: CrateGPUI},
		73:  {Symbol: "gpui::colors::Colors::text", Crate: CrateGPUI},
		74:  {Symbol: "gpui::color::Hsla", Crate: CrateGPUI},
		75:  {Symbol: "gpui::bench", Crate: CrateGPUI},
		76:  {Symbol: "gpui::*", Crate: CrateGPUI},
		77:  {Symbol: "gpui::inspector::Into::into", Crate: CrateGPUI},
		78:  {Symbol: "gpui_ce_windows::clipboard", Crate: "gpui_ce_windows"},
		79:  {Symbol: "gpui_ce_windows::destination_list", Crate: "gpui_ce_windows"},
		80:  {Symbol: "gpui_ce_windows::direct_manipulation", Crate: "gpui_ce_windows"},
		81:  {Symbol: "gpui_ce_windows::directx_atlas", Crate: "gpui_ce_windows"},
		82:  {Symbol: "gpui_ce_windows::directx_renderer", Crate: "gpui_ce_windows"},
		83:  {Symbol: "gpui_ce_windows::dispatcher", Crate: "gpui_ce_windows"},
		84:  {Symbol: "gpui_ce_windows::events", Crate: "gpui_ce_windows"},
		85:  {Symbol: "gpui_ce_windows::system_notifications", Crate: "gpui_ce_windows"},
		86:  {Symbol: "gpui_ce_windows::window", Crate: "gpui_ce_windows"},
		87:  {Symbol: "gpui_ce_render::blur", Crate: "gpui_ce_render"},
		88:  {Symbol: "gpui_ce_render::InstanceRange", Crate: "gpui_ce_render"},
		89:  {Symbol: "gpui_ce_render::artifacts", Crate: "gpui_ce_render"},
		90:  {Symbol: "gpui_ce_platform::background_executor", Crate: "gpui_ce_platform"},
		91:  {Symbol: "gpui_ce_platform::current_headless_renderer", Crate: "gpui_ce_platform"},
		92:  {Symbol: "gpui_ce_platform::application", Crate: "gpui_ce_platform"},
		93:  {Symbol: "gpui_ce_elements::editable_text::state::EditorState", Crate: "gpui_ce_elements"},
		94:  {Symbol: "gpui_ce_elements::other_element", Crate: "gpui_ce_elements"},
		95:  {Symbol: "gpui_macros::styles", Crate: "gpui_ce_macros"},
		96:  {Symbol: "media::core_video::kCVPixelFormatType_32BGRA", Crate: "gpui_ce_media"},
		97:  {Symbol: "path::PathStyle::Unix", Crate: "gpui_ce_path"},
		98:  {Symbol: "gpui_ce_macos::pasteboard", Crate: "gpui_ce_macos"},
		99:  {Symbol: "gpui_ce_macos::text_system", Crate: "gpui_ce_macos"},
		100: {Symbol: "gpui_ce_web::keyboard", Crate: "gpui_ce_web"},
		101: {Symbol: "gpui_ce_web::ime_mirror", Crate: "gpui_ce_web"},
		102: {Symbol: "gpui_ce_web::http_client", Crate: "gpui_ce_web"},
		103: {Symbol: "gpui_apple::metal_atlas", Crate: "gpui_ce_apple"},
		104: {Symbol: "gpui_apple::metal_renderer", Crate: "gpui_ce_apple"},
		105: {Symbol: "gpui_ce_macos::haptic_feedback", Crate: "gpui_ce_macos"},
		106: {Symbol: "gpui_ce_macos::system_notifications", Crate: "gpui_ce_macos"},
		107: {Symbol: "gpui_ce_macos::dispatcher", Crate: "gpui_ce_macos"},
		108: {Symbol: "gpui_ce_linux::current_platform", Crate: "gpui_ce_linux"},
		109: {Symbol: "util::fs_embed", Crate: "gpui_ce_zed_util"},
		// Ticket-34 triage dispositions (the former core-support-triage
		// catch-all, replaced by concrete placements).
		110: {Symbol: "gpui_ce_shared_string::SharedString::new", Crate: "gpui_ce_shared_string"},
		111: {Symbol: "collections::HashMap", Crate: "gpui_ce_collections"},
		112: {Symbol: "collections::IndexMap", Crate: "gpui_ce_collections"},
		113: {Symbol: "sum_tree::SumTree::new", Crate: "gpui_ce_sum_tree"},
		114: {Symbol: "gpui_util::arc_cow::ArcCow::Borrowed", Crate: "gpui_ce_util"},
		115: {Symbol: "gpui_util::new_std_command", Crate: "gpui_ce_util"},
		116: {Symbol: "gpui_util::ResultExt::log_err", Crate: "gpui_ce_util"},
		117: {Symbol: "gpui_util::defer", Crate: "gpui_ce_util"},
		118: {Symbol: "gpui_util::post_inc", Crate: "gpui_ce_util"},
		119: {Symbol: "gpui_util::debug_panic", Crate: "gpui_ce_util"},
		120: {Symbol: "gpui_util::maybe", Crate: "gpui_ce_util"},
		121: {Symbol: "util::fs", Crate: "gpui_ce_zed_util"},
		122: {Symbol: "perf::implementation::consts::N", Crate: "perf"},
		123: {Symbol: "gpui_ce_web::init_logging", Crate: "gpui_ce_web"},
		124: {Symbol: "gpui::private::serde", Crate: CrateGPUI},
		125: {Symbol: "gpui::_accessibility", Crate: CrateGPUI},
		126: {Symbol: "gpui::Result", Crate: CrateGPUI},
	}

	for _, rule := range OwnerRules() {
		row, ok := examples[rule.ID]
		if !ok {
			t.Errorf("rule %d (%s) has no example row", rule.ID, rule.Family)
			continue
		}
		owner, source := AssignOwner(row)
		if owner != rule.Owner || source != "family:"+rule.Family {
			t.Errorf("rule %d (%s): AssignOwner(%q) = (%q, %q), want (%q, family:%s)",
				rule.ID, rule.Family, row.Symbol, owner, source, rule.Owner, rule.Family)
		}
	}
	if len(examples) != len(OwnerRules()) {
		t.Errorf("examples has %d entries for %d rules", len(examples), len(OwnerRules()))
	}
}

func TestOwnerAssignment(t *testing.T) {
	cases := []struct {
		name   string
		row    Row
		owner  string
		source string
	}{
		{"unmatched is unassigned", Row{Symbol: "unknowncrate::thing::Unknown", Crate: "unknowncrate"}, "", "unassigned"},
		{"gpui private path is structural", Row{Symbol: "gpui::private::serde", Crate: CrateGPUI}, OwnerStructural, "family:gpui-private-structure"},
		{"gpui seal trait is structural", Row{Symbol: "gpui::seal::Sealed", Crate: CrateGPUI}, OwnerStructural, "family:gpui-private-structure"},
		{"gpui bare Result re-export is structural", Row{Symbol: "gpui::Result", Crate: CrateGPUI}, OwnerStructural, "family:gpui-reexport-structure"},
		{"doc-only module is structural", Row{Symbol: "gpui::_ownership_and_data_flow", Crate: CrateGPUI}, OwnerStructural, "family:gpui-doc-structure"},
		{"sum_tree goes to the list family", Row{Symbol: "sum_tree::TreeMap::new", Crate: "gpui_ce_sum_tree"}, "14", "family:sum-tree-lists"},
		{"SharedString goes to entities/data plumbing", Row{Symbol: "gpui_ce_shared_string::SharedString::new", Crate: "gpui_ce_shared_string"}, "03", "family:shared-string-value"},
		{"collections map machinery goes to entities/data plumbing", Row{Symbol: "collections::FxHashMap", Crate: "gpui_ce_collections"}, "03", "family:collections-maps"},
		{"unused collections items are structural", Row{Symbol: "collections::vecmap", Crate: "gpui_ce_collections"}, OwnerStructural, "family:collections-unused"},
		{"ArcCow machinery is structural", Row{Symbol: "gpui_util::arc_cow::ArcCow::Borrowed", Crate: "gpui_ce_util"}, OwnerStructural, "family:util-arc-cow"},
		{"util unused items are structural", Row{Symbol: "gpui_util::truncate_to_bottom_n_sorted_by", Crate: "gpui_ce_util"}, OwnerStructural, "family:util-unused"},
		{"unused zed_util crate is structural", Row{Symbol: "util::shell::run", Crate: "gpui_ce_zed_util"}, OwnerStructural, "family:zed-util-unused"},
		{"Windows shell helpers go to desktop operations", Row{Symbol: "gpui_util::get_powershell", Crate: "gpui_ce_util"}, "26", "family:util-windows-shell"},
		{"defer goes to entities/effects", Row{Symbol: "gpui_util::defer", Crate: "gpui_ce_util"}, "03", "family:util-deferred"},
		{"post_inc goes to entities/data plumbing", Row{Symbol: "gpui_util::post_inc", Crate: "gpui_ce_util"}, "03", "family:util-entity-plumbing"},
		{"async error machinery goes to executors", Row{Symbol: "gpui_util::TryFutureExt::log_err", Crate: "gpui_ce_util"}, "04", "family:util-async-errors"},
		{"debug assertions go to the inspector ticket", Row{Symbol: "gpui_util::debug_panic", Crate: "gpui_ce_util"}, "27", "family:util-debug-diagnostics"},
		{"perf tooling goes to the applicability owner", Row{Symbol: "perf::implementation::consts::N", Crate: "perf"}, "01", "family:perf-tooling"},
		{"web backend preference is alternate-gpu", Row{Symbol: "gpui_ce_web::WebBackendPreference", Crate: "gpui_ce_web"}, "01", "family:alternate-gpu"},
		{"platform web backend preference is alternate-gpu", Row{Symbol: "gpui_ce_platform::WebBackendPreference", Crate: "gpui_ce_platform"}, "01", "family:alternate-gpu"},
		{"wasm-only platform init is platform-inapplicable", Row{Symbol: "gpui_ce_platform::web_init", Crate: "gpui_ce_platform"}, "01", "family:platform-inapplicable"},
		{"web logging is platform-inapplicable", Row{Symbol: "gpui_ce_web::init_logging", Crate: "gpui_ce_web"}, "01", "family:platform-inapplicable"},
		{"linux module is platform-inapplicable", Row{Symbol: "gpui_ce_linux::linux", Crate: "gpui_ce_linux"}, "01", "family:platform-inapplicable"},
		{"InspectorElementId goes to authoring", Row{Symbol: "gpui::inspector::InspectorElementId", Crate: CrateGPUI}, "11", "family:authoring"},
		{"prelude Styled goes to layout", Row{Symbol: "gpui::prelude::Styled", Crate: CrateGPUI}, "06", "family:layout"},
		{"prelude StyledImage goes to img", Row{Symbol: "gpui::prelude::StyledImage", Crate: CrateGPUI}, "18", "family:elements-img"},
		{"TextAppContext test gated under app", Row{Symbol: "gpui::app::test_context::TestAppContext::app", Crate: CrateGPUI, Cfg: []string{`any (test , feature = "test-support")`}}, "03", "family:entities-effects-test"},
		{"platform::test module row", Row{Symbol: "gpui::platform::test", Crate: CrateGPUI}, "01", "family:platform-test"},
		{"macos screen capture module", Row{Symbol: "gpui_ce_macos::screen_capture", Crate: "gpui_ce_macos"}, "28", "family:screen-capture"},
		{"macos wgpu renderer", Row{Symbol: "gpui_ce_macos::wgpu_renderer", Crate: "gpui_ce_macos"}, "01", "family:alternate-gpu"},
		{"credentials on Platform", Row{Symbol: "gpui::platform::Platform::write_credentials", Crate: CrateGPUI}, "25", "family:platform-dialogs-credentials"},
		{"clipboard write on Platform", Row{Symbol: "gpui::platform::Platform::write_to_clipboard", Crate: CrateGPUI}, "23", "family:platform-clipboard"},
		{"jump list update", Row{Symbol: "gpui::platform::Platform::update_jump_list", Crate: CrateGPUI}, "26", "family:platform-desktop"},
		{"ime position on PlatformWindow", Row{Symbol: "gpui::platform::PlatformWindow::update_ime_position", Crate: CrateGPUI}, "20", "family:platform-ime"},
		{"a11y on PlatformWindow", Row{Symbol: "gpui::platform::PlatformWindow::a11y_init", Crate: CrateGPUI}, "21", "family:accessibility-tree"},
		{"open_window on Platform", Row{Symbol: "gpui::platform::Platform::open_window", Crate: CrateGPUI}, "05", "family:platform-window"},
		{"Window render_to_image", Row{Symbol: "gpui::window::Window::render_to_image", Crate: CrateGPUI}, "32", "family:headless-readback"},
		{"FocusHandle", Row{Symbol: "gpui::window::FocusHandle::id", Crate: CrateGPUI}, "13", "family:window-keyboard"},
		{"PaintQuad", Row{Symbol: "gpui::window::PaintQuad::bounds", Crate: CrateGPUI}, "08", "family:scene-kernel"},
		{"line layout items", Row{Symbol: "gpui::text_system::line_layout::LineLayout::shape", Crate: CrateGPUI}, "15", "family:inline-layout"},
		{"div styled impl", Row{Symbol: "gpui::elements::div::Styled for Div::style", Crate: CrateGPUI}, "11", "family:elements-authoring"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner, source := AssignOwner(tc.row)
			if owner != tc.owner || source != tc.source {
				t.Errorf("AssignOwner(%q) = (%q, %q), want (%q, %q)", tc.row.Symbol, owner, source, tc.owner, tc.source)
			}
		})
	}
}

// --- ticket-34 disposition mechanism -----------------------------------

func TestDispositionMechanism(t *testing.T) {
	// Pre-triage rules carry no disposition note; triage rules do.
	pre := Row{Symbol: "gpui::app::App::on_release", Crate: CrateGPUI}
	if note := DispositionNote(pre); note != "" {
		t.Errorf("DispositionNote(pre-triage row) = %q, want empty", note)
	}
	triaged := Row{Symbol: "gpui_ce_shared_string::SharedString::new", Crate: "gpui_ce_shared_string"}
	if note := DispositionNote(triaged); !strings.Contains(note, "gpui.rs:143") {
		t.Errorf("DispositionNote(SharedString) = %q, want a note citing gpui.rs:143", note)
	}

	// Platform gates exist only for the crate-level gated platform crates.
	for crate, want := range map[string]bool{
		"gpui_ce_web": true, "gpui_ce_linux": true, "gpui_ce_macos": true, "gpui_ce_apple": true,
		"gpui-ce": false, "gpui_ce_windows": false, "gpui_ce_platform": false, "perf": false,
	} {
		got := PlatformGateNote(crate) != ""
		if got != want {
			t.Errorf("PlatformGateNote(%s) present = %v, want %v", crate, got, want)
		}
	}

	// RowDisposition derives the value from owner, source and crate gate.
	cases := []struct {
		name string
		row  Row
		own  string
		src  string
		want string
	}{
		{"family row", Row{Symbol: "gpui::a", Crate: CrateGPUI}, "03", "family:entities-effects", DispositionFamily},
		{"structural row", Row{Symbol: "gpui::Result", Crate: CrateGPUI}, OwnerStructural, "family:gpui-reexport-structure", DispositionCoreStructure},
		{"gated crate row keeps family owner", Row{Symbol: "gpui_ce_macos::pasteboard", Crate: "gpui_ce_macos"}, "23", "family:os-clipboard", DispositionPlatformInapplicable},
		{"platform-inapplicable family row", Row{Symbol: "gpui_ce_platform::web_init", Crate: "gpui_ce_platform"}, "01", "family:" + FamilyPlatformInapplicable, DispositionPlatformInapplicable},
		{"item-cfg-gated row stays family", Row{Symbol: "gpui_util::new_std_command", Crate: "gpui_ce_util"}, "26", "family:util-windows-shell", DispositionFamily},
		{"unassigned row is still a family disposition", Row{Symbol: "x::y", Crate: "unknown"}, "", "unassigned", DispositionFamily},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RowDisposition(tc.row, tc.own, tc.src); got != tc.want {
				t.Errorf("RowDisposition = %q, want %q", got, tc.want)
			}
		})
	}
}

// --- generation ----------------------------------------------------------

func TestEvaluatorAgainstRealScan(t *testing.T) {
	if !realScanAvailable(t) {
		return
	}
	scan, err := LoadApiScan(realScanPath)
	if err != nil {
		t.Fatalf("LoadApiScan: %v", err)
	}
	profiles := DefaultProfiles("")
	features := FeatureSet(profiles[ProfileNativeDefault])
	failures := 0
	checked := 0
	check := func(expr string) {
		if strings.TrimSpace(expr) == "" {
			return
		}
		checked++
		if _, err := EvaluateCfg(expr, features); err != nil {
			failures++
			t.Errorf("EvaluateCfg(%q): %v", expr, err)
		}
	}
	for i := range scan.Crates {
		for j := range scan.Crates[i].Items {
			for _, expr := range scan.Crates[i].Items[j].Cfg {
				check(expr)
			}
		}
	}
	for _, sites := range scan.FeatureTrace {
		for _, site := range sites {
			check(site.Cfg)
		}
	}
	t.Logf("evaluated %d cfg expressions with %d failures", checked, failures)
	if failures != 0 {
		t.Fatalf("%d cfg evaluation failures", failures)
	}
}

func TestGenerateRealScan(t *testing.T) {
	if !realScanAvailable(t) {
		return
	}
	profiles := mustProfiles(t)
	led, err := Generate(realScanPath, profiles, time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := led.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if led.Schema != LedgerSchema {
		t.Errorf("schema = %q, want %q", led.Schema, LedgerSchema)
	}
	if led.GeneratedFrom.ApiScanSchema != ApiScanSchema {
		t.Errorf("api scan schema = %q", led.GeneratedFrom.ApiScanSchema)
	}
	if led.GeneratedFrom.ApiScanCommit == "" {
		t.Error("empty source commit")
	}
	if led.GeneratedFrom.Toolchain == "" {
		t.Error("empty toolchain")
	}
	if want := "2026-10-05T12:00:00Z"; led.GeneratedFrom.GeneratedAt != want {
		t.Errorf("generated at = %q, want %q", led.GeneratedFrom.GeneratedAt, want)
	}

	const wantRows = 6911
	if led.Summary.TotalRows != wantRows {
		t.Errorf("TotalRows = %d, want %d", led.Summary.TotalRows, wantRows)
	}
	if len(led.Rows) != wantRows {
		t.Errorf("len(Rows) = %d, want %d", len(led.Rows), wantRows)
	}

	// Rows keep scan order: the first row is the gpui::action module.
	if len(led.Rows) > 0 && led.Rows[0].Symbol != "gpui::action" {
		t.Errorf("first row symbol = %q, want gpui::action", led.Rows[0].Symbol)
	}

	// Every row is planned, with symbol, source and consistent applicability.
	emptyProfile := 0
	notesWithEvalFailure := 0
	coreExport := 0
	for i := range led.Rows {
		r := &led.Rows[i]
		if r.State != StatePlanned {
			t.Fatalf("row %q state = %q, want planned", r.Symbol, r.State)
		}
		if r.Symbol == "" || r.Source == "" {
			t.Fatalf("row %d has empty symbol or source", i)
		}
		if r.Crate == "" || r.Kind == "" {
			t.Fatalf("row %q has empty crate or kind", r.Symbol)
		}
		if r.WindowsApplicable != (len(r.Profile) > 0) {
			t.Fatalf("row %q: WindowsApplicable=%v but Profile=%v", r.Symbol, r.WindowsApplicable, r.Profile)
		}
		if strings.Contains(r.Notes, "cfg-eval-failed") {
			notesWithEvalFailure++
			t.Errorf("row %q has cfg evaluation failure: %s", r.Symbol, r.Notes)
		}
		if strings.Contains(r.Notes, "core-export") {
			coreExport++
		}
		if len(r.Profile) == 0 {
			emptyProfile++
		}
	}
	if notesWithEvalFailure != 0 {
		t.Fatalf("%d rows have cfg evaluation failures", notesWithEvalFailure)
	}
	if coreExport == 0 {
		t.Error("no core-export notes recorded")
	}

	// Profile membership is exactly "all cfg conditions true under the
	// profile's feature set". A row with an empty profile list is a row whose
	// cfg is parseable but false for every profile on the Windows target
	// (macOS/Linux/Wasm-only items and test-only items, since the recorded
	// profiles are release graphs), so empty membership is always explained
	// by the cfg, never by an unresolvable expression.
	mismatched := 0
	for i := range led.Rows {
		r := &led.Rows[i]
		for _, id := range ProfileOrder(profiles) {
			allTrue := true
			for _, expr := range r.Cfg {
				v, err := EvaluateCfg(expr, FeatureSet(profiles[id]))
				if err != nil {
					t.Fatalf("row %q cfg %q: %v", r.Symbol, expr, err)
				}
				if !v {
					allTrue = false
					break
				}
			}
			if containsStr(r.Profile, id) != allTrue {
				mismatched++
				t.Errorf("row %q: profile membership %v disagrees with cfg evaluation under %s", r.Symbol, r.Profile, id)
			}
		}
	}
	t.Logf("rows with empty profile (not applicable on Windows in any profile): %d", emptyProfile)
	if mismatched != 0 {
		t.Errorf("%d profile membership mismatches", mismatched)
	}

	// TestAppContext exists in test-support but not native-default.
	var testAppContext *Row
	for i := range led.Rows {
		if led.Rows[i].Symbol == "gpui::app::test_context::TestAppContext" {
			testAppContext = &led.Rows[i]
			break
		}
	}
	if testAppContext == nil {
		t.Fatal("gpui::app::test_context::TestAppContext not found in ledger")
	}
	if !containsStr(testAppContext.Profile, ProfileTestSupport) {
		t.Errorf("TestAppContext profiles = %v, want to contain test-support", testAppContext.Profile)
	}
	if containsStr(testAppContext.Profile, ProfileNativeDefault) {
		t.Errorf("TestAppContext profiles = %v, must not contain native-default", testAppContext.Profile)
	}

	// Anything positively cfg-gated on inspector appears only under
	// inspector-capture (debug_assertions is false in the recorded release
	// profiles). Rows carrying the negated form stay out of it; membership
	// for both is covered by the invariant above.
	inspectorRows := 0
	inspectorForm := `any (feature = "inspector" , debug_assertions)`
	for i := range led.Rows {
		r := &led.Rows[i]
		positive := len(r.Cfg) > 0
		for _, expr := range r.Cfg {
			if expr != inspectorForm {
				positive = false
				break
			}
		}
		if !positive {
			continue
		}
		inspectorRows++
		if len(r.Profile) != 1 || r.Profile[0] != ProfileInspectorCapture {
			t.Errorf("inspector-gated row %q profiles = %v, want [inspector-capture]", r.Symbol, r.Profile)
		}
	}
	if inspectorRows == 0 {
		t.Error("no inspector-gated rows found")
	}
	t.Logf("inspector-gated rows: %d", inspectorRows)

	// Summary consistency.
	if led.Summary.UnassignedCount != len(led.Unassigned) {
		t.Errorf("UnassignedCount = %d, want %d", led.Summary.UnassignedCount, len(led.Unassigned))
	}
	if led.Summary.UnimplementedCount == 0 {
		t.Error("UnimplementedCount = 0, want > 0 (scan records unimplemented/no-op markers)")
	}
	if led.Summary.FeatureGatedCount == 0 {
		t.Error("FeatureGatedCount = 0, want > 0")
	}
	sum := 0
	for _, n := range led.Summary.ByOwner {
		sum += n
	}
	if sum+led.Summary.UnassignedCount != led.Summary.TotalRows {
		t.Errorf("ByOwner sum %d + unassigned %d != TotalRows %d", sum, led.Summary.UnassignedCount, led.Summary.TotalRows)
	}

	// The gpui::profiler parent-module gate is visible in notes.
	gated := 0
	for i := range led.Rows {
		if strings.Contains(led.Rows[i].Notes, `parent-module-gate: feature="profiler"`) {
			gated++
		}
	}
	if gated == 0 {
		t.Error("no parent-module-gate notes for gpui::profiler rows")
	}

	// Ticket 34: no row is owned by the triage ticket, every row carries a
	// disposition and the summary's disposition counts cover all rows.
	if got := led.Summary.ByOwner["34"]; got != 0 {
		t.Errorf("ByOwner[34] = %d, want 0 (core-support triage complete)", got)
	}
	for i := range led.Rows {
		r := &led.Rows[i]
		if !ValidDisposition(r.Disposition) {
			t.Fatalf("row %q has invalid disposition %q", r.Symbol, r.Disposition)
		}
		if r.Owner == "" {
			t.Fatalf("row %q is unassigned", r.Symbol)
		}
	}
	sumDisp := 0
	for _, n := range led.Summary.ByDisposition {
		sumDisp += n
	}
	if sumDisp != led.Summary.TotalRows {
		t.Errorf("ByDisposition sums to %d, want %d", sumDisp, led.Summary.TotalRows)
	}
	if led.Summary.ByDisposition[DispositionCoreStructure] == 0 {
		t.Error("no core-structure rows recorded")
	}
	if led.Summary.ByDisposition[DispositionPlatformInapplicable] == 0 {
		t.Error("no platform-inapplicable rows recorded")
	}
	if led.Summary.ByOwner[OwnerStructural] != led.Summary.ByDisposition[DispositionCoreStructure] {
		t.Errorf("ByOwner[%s] = %d disagrees with ByDisposition[core-structure] = %d",
			OwnerStructural, led.Summary.ByOwner[OwnerStructural], led.Summary.ByDisposition[DispositionCoreStructure])
	}
	t.Logf("dispositions: family=%d core-structure=%d platform-inapplicable=%d",
		led.Summary.ByDisposition[DispositionFamily],
		led.Summary.ByDisposition[DispositionCoreStructure],
		led.Summary.ByDisposition[DispositionPlatformInapplicable])
}

// TestTicket34CoreSupportTriage verifies the ticket-34 completion facts on
// the real scan: the former core-support-triage rows all carry concrete
// dispositions, every disposition rule records its provenance note on the
// rows it matched, and platform-gate notes keep the crate-level non-Windows
// gates visible next to the misleading all-profiles membership the scan
// implies for those crates.
func TestTicket34CoreSupportTriage(t *testing.T) {
	if !realScanAvailable(t) {
		return
	}
	profiles := mustProfiles(t)
	led, err := Generate(realScanPath, profiles, time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// The triage families replaced core-support-triage; none of them may be
	// empty (a disposition rule matching zero rows would mean the scan drifted
	// and rows fell back to unassigned or another family).
	usage := RuleUsageForRows(led.Rows)
	rules := map[int]OwnerRule{}
	for _, r := range OwnerRules() {
		rules[r.ID] = r
	}
	for _, u := range usage {
		r := rules[u.RuleID]
		if r.Note == "" {
			continue // pre-triage rule
		}
		if u.Matched == 0 {
			t.Errorf("disposition rule %d (%s) matched no rows", u.RuleID, u.Family)
		}
		if u.Family == "core-support-triage" {
			t.Errorf("core-support-triage family still present as rule %d", u.RuleID)
		}
	}

	// Every row matched by a disposition rule carries that rule's note.
	missing := 0
	for i := range led.Rows {
		r := &led.Rows[i]
		note := DispositionNote(*r)
		if note == "" {
			continue
		}
		if !strings.Contains(r.Notes, "disposition: "+note) {
			missing++
			t.Errorf("row %q lacks its disposition note", r.Symbol)
		}
	}
	if missing != 0 {
		t.Fatalf("%d rows lack disposition notes", missing)
	}

	// Every row of a crate-level gated platform crate carries the gate note
	// and the platform-inapplicable disposition, while keeping its family
	// owner (inapplicable-by-platform, not an exception).
	for _, crate := range []string{"gpui_ce_web", "gpui_ce_linux", "gpui_ce_macos", "gpui_ce_apple"} {
		rows := 0
		gate := PlatformGateNote(crate)
		if gate == "" {
			t.Fatalf("crate %s has no platform gate recorded", crate)
		}
		for i := range led.Rows {
			r := &led.Rows[i]
			if r.Crate != crate {
				continue
			}
			rows++
			if !strings.Contains(r.Notes, "platform-gate: "+gate) {
				t.Errorf("row %q lacks the %s platform-gate note", r.Symbol, crate)
			}
			if r.Disposition != DispositionPlatformInapplicable {
				t.Errorf("row %q disposition = %q, want %s", r.Symbol, r.Disposition, DispositionPlatformInapplicable)
			}
			if r.Owner == "" || r.Owner == "34" {
				t.Errorf("row %q lost its family owner: %q", r.Symbol, r.Owner)
			}
		}
		if rows == 0 {
			t.Errorf("crate %s has no ledger rows", crate)
		}
		t.Logf("%s: %d rows carry the platform gate note", crate, rows)
	}

	// Structural rows carry the marker owner and a disposition note citing
	// the pinned source.
	structural := 0
	for i := range led.Rows {
		r := &led.Rows[i]
		if r.Owner != OwnerStructural {
			continue
		}
		structural++
		if r.Disposition != DispositionCoreStructure {
			t.Errorf("structural row %q disposition = %q", r.Symbol, r.Disposition)
		}
		if !strings.Contains(r.Notes, "disposition: ") {
			t.Errorf("structural row %q lacks a disposition note", r.Symbol)
		}
	}
	if structural == 0 {
		t.Fatal("no structural rows recorded")
	}
	t.Logf("structural rows: %d", structural)

	// The concrete reassignments the triage made, spot-checked by symbol.
	want := map[string]struct{ owner, source, disposition string }{
		"gpui_ce_shared_string::SharedString::new": {"03", "family:shared-string-value", DispositionFamily},
		"collections::HashMap":                     {"03", "family:collections-maps", DispositionFamily},
		"collections::IndexMap":                    {OwnerStructural, "family:collections-unused", DispositionCoreStructure},
		"sum_tree::SumTree::new":                   {"14", "family:sum-tree-lists", DispositionFamily},
		"gpui_util::arc_cow::ArcCow::Borrowed":     {OwnerStructural, "family:util-arc-cow", DispositionCoreStructure},
		"gpui_util::new_std_command":               {"26", "family:util-windows-shell", DispositionFamily},
		"gpui_util::ResultExt::log_err":            {"04", "family:util-async-errors", DispositionFamily},
		"gpui_util::defer":                         {"03", "family:util-deferred", DispositionFamily},
		"gpui_util::post_inc":                      {"03", "family:util-entity-plumbing", DispositionFamily},
		"gpui_util::debug_panic":                   {"27", "family:util-debug-diagnostics", DispositionFamily},
		"gpui_util::maybe":                         {OwnerStructural, "family:util-unused", DispositionCoreStructure},
		"util::fs":                                 {OwnerStructural, "family:zed-util-unused", DispositionCoreStructure},
		"perf::implementation::consts::SUF_NORMAL": {"01", "family:perf-tooling", DispositionFamily},
		"gpui_ce_web::WebBackendPreference":        {"01", "family:alternate-gpu", DispositionPlatformInapplicable},
		"gpui_ce_platform::WebBackendPreference":   {"01", "family:alternate-gpu", DispositionFamily},
		"gpui_ce_web::init_logging":                {"01", "family:platform-inapplicable", DispositionPlatformInapplicable},
		"gpui_ce_linux::linux":                     {"01", "family:platform-inapplicable", DispositionPlatformInapplicable},
		"gpui_ce_platform::web_init":               {"01", "family:platform-inapplicable", DispositionPlatformInapplicable},
		"gpui::private":                            {OwnerStructural, "family:gpui-private-structure", DispositionCoreStructure},
		"gpui::_accessibility":                     {OwnerStructural, "family:gpui-doc-structure", DispositionCoreStructure},
		"gpui::Result":                             {OwnerStructural, "family:gpui-reexport-structure", DispositionCoreStructure},
	}
	for i := range led.Rows {
		r := &led.Rows[i]
		w, ok := want[r.Symbol]
		if !ok {
			continue
		}
		if r.Owner != w.owner || r.OwnerSource != w.source || r.Disposition != w.disposition {
			t.Errorf("row %q = (%q, %q, %q), want (%q, %q, %q)",
				r.Symbol, r.Owner, r.OwnerSource, r.Disposition, w.owner, w.source, w.disposition)
		}
	}
}

func TestFromRows(t *testing.T) {
	rows := []Row{
		{Symbol: "gpui::a", Crate: CrateGPUI, Kind: "fn", Source: "crates/gpui/x.rs:1", State: StatePlanned, Owner: "03", OwnerSource: "family:entities-effects", Disposition: DispositionFamily, Profile: []string{ProfileNativeDefault}},
		{Symbol: "gpui::b", Crate: "other", Kind: "fn", Source: "crates/other/x.rs:2", State: StatePlanned, Cfg: []string{`target_os = "macos"`}, Profile: []string{}, Owner: "05", OwnerSource: "family:platform-window", Disposition: DispositionFamily},
		{Symbol: "gpui::c", Crate: CrateGPUI, Kind: "fn", Source: "crates/gpui/x.rs:3", State: StatePlanned, Owner: "", OwnerSource: "unassigned", Disposition: DispositionFamily, Profile: []string{ProfileNativeDefault}, BodyMarker: "noop"},
		{Symbol: "gpui::private::serde", Crate: CrateGPUI, Kind: "use", Source: "crates/gpui/gpui.rs:82", State: StatePlanned, Owner: OwnerStructural, OwnerSource: "family:gpui-private-structure", Disposition: DispositionCoreStructure, Profile: []string{ProfileNativeDefault}, Notes: "disposition: non-observable structure"},
	}
	profiles := DefaultProfiles("")
	led := FromRows(rows, SourceIdentity{
		ApiScanSchema: ApiScanSchema,
		ApiScanCommit: "c0ffee",
		GeneratedAt:   "2026-10-05T12:00:00Z",
		Toolchain:     "gotest",
		Profiles:      profiles,
	})
	if led.Schema != LedgerSchema {
		t.Errorf("schema = %q", led.Schema)
	}
	if led.Summary.TotalRows != 4 {
		t.Errorf("TotalRows = %d, want 4", led.Summary.TotalRows)
	}
	if led.Summary.UnassignedCount != 1 || len(led.Unassigned) != 1 || led.Unassigned[0].Symbol != "gpui::c" {
		t.Errorf("unassigned = %d/%v", led.Summary.UnassignedCount, led.Unassigned)
	}
	if led.Summary.UnimplementedCount != 1 {
		t.Errorf("UnimplementedCount = %d, want 1", led.Summary.UnimplementedCount)
	}
	if led.Summary.ByOwner["03"] != 1 {
		t.Errorf("ByOwner[03] = %d, want 1", led.Summary.ByOwner["03"])
	}
	if led.Summary.ByOwner[OwnerStructural] != 1 {
		t.Errorf("ByOwner[%s] = %d, want 1", OwnerStructural, led.Summary.ByOwner[OwnerStructural])
	}
	if led.Summary.ByDisposition[DispositionFamily] != 3 || led.Summary.ByDisposition[DispositionCoreStructure] != 1 {
		t.Errorf("ByDisposition = %v, want 3 family and 1 core-structure", led.Summary.ByDisposition)
	}
	if led.Summary.ByProfile[ProfileNativeDefault] != 3 {
		t.Errorf("ByProfile[native-default] = %d, want 3", led.Summary.ByProfile[ProfileNativeDefault])
	}
	if led.Summary.ByState[StatePlanned] != 4 {
		t.Errorf("ByState[planned] = %d, want 4", led.Summary.ByState[StatePlanned])
	}
	if err := led.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}

	// The structural-marker/owner invariant is enforced by Validate.
	bad := append([]Row{}, rows...)
	bad[3].Disposition = DispositionFamily
	badLed := FromRows(bad, SourceIdentity{ApiScanSchema: ApiScanSchema, ApiScanCommit: "c0ffee", GeneratedAt: "2026-10-05T12:00:00Z", Toolchain: "gotest", Profiles: profiles})
	if err := badLed.Validate(); err == nil {
		t.Error("Validate accepted a core-structure owner with a family disposition")
	}
	bad2 := append([]Row{}, rows...)
	bad2[0].Owner = OwnerStructural
	bad2[0].OwnerSource = "family:gpui-private-structure"
	badLed2 := FromRows(bad2, SourceIdentity{ApiScanSchema: ApiScanSchema, ApiScanCommit: "c0ffee", GeneratedAt: "2026-10-05T12:00:00Z", Toolchain: "gotest", Profiles: profiles})
	if err := badLed2.Validate(); err == nil {
		t.Error("Validate accepted a family disposition with a structural owner")
	}
}

func TestRowMentionsFeature(t *testing.T) {
	r := Row{Cfg: []string{`any (test , feature = "test-support" , feature = "bench-support")`}}
	if !RowMentionsFeature(r, "test-support") || !RowMentionsFeature(r, "bench-support") {
		t.Error("feature mentions not detected")
	}
	if RowMentionsFeature(r, "inspector") {
		t.Error("false feature mention")
	}
}

func TestMissingScanFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.json")
	if _, err := Generate(missing, DefaultProfiles(""), time.Now()); err == nil {
		t.Fatal("Generate with missing scan succeeded, want error")
	}
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
