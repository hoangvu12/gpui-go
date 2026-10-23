//go:build windows

// The browser-core ticket 39 (rev 15) layout-field gates: flow-root
// display, variable-length grid track lists, justify-items/self and the
// measure response's first baseline, driven through the native layout
// engine (the gpui Style grows them with the styling tickets).
package layoutspec

import (
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// rev15LayoutService opens the native layout service with the measure
// trampoline installed.
func rev15LayoutService(t *testing.T) *native.LayoutService {
	t.Helper()
	lib, err := native.Load(native.Options{})
	if err != nil {
		t.Fatalf("native.Load: %v", err)
	}
	svc, err := lib.Layout()
	if err != nil {
		t.Fatalf("lib.Layout: %v", err)
	}
	if err := svc.RegisterMeasureTrampoline(); err != nil {
		t.Fatalf("RegisterMeasureTrampoline: %v", err)
	}
	return svc
}

// TestFlowRootDisplayLaysOut: display tag 4 maps to Taffy's FlowRoot —
// the node computes and its children stack like a block container
// (margins do not escape through it).
func TestFlowRootDisplayLaysOut(t *testing.T) {
	svc := rev15LayoutService(t)
	engine, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Dispose()

	box := func(h float32) *native.LayoutStyleRecord {
		style := native.NewLayoutStyleRecord()
		style.Display = native.DisplayFlowRoot
		style.Size = native.LayoutSize{
			Width:  native.DefiniteLen(100),
			Height: native.DefiniteLen(h),
		}
		return &style
	}
	first, err := engine.CreateNode(box(20))
	if err != nil {
		t.Fatal(err)
	}
	second, err := engine.CreateNode(box(20))
	if err != nil {
		t.Fatal(err)
	}
	// The root itself is the flow-root: its children stack in block
	// order (the second below the first), the display FlowRoot maps to.
	rootStyle := native.NewLayoutStyleRecord()
	rootStyle.Display = native.DisplayFlowRoot
	rootStyle.Size = native.LayoutSize{
		Width:  native.DefiniteLen(100),
		Height: native.DefiniteLen(40),
	}
	root, err := engine.CreateNode(&rootStyle, first, second)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetStyle(root, &rootStyle); err != nil {
		t.Fatalf("SetStyle(root): %v", err)
	}
	_, err = engine.Compute(root, native.LayoutAvailSize{
		Width:  native.DefiniteAvail(100),
		Height: native.DefiniteAvail(40),
	}, 0)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	layout, err := engine.NodeLayout(second)
	if err != nil {
		t.Fatalf("NodeLayout: %v", err)
	}
	if layout.LocationY != 20 {
		t.Fatalf("flow-root second child y = %v, want 20 (block stacking)", layout.LocationY)
	}
	if layout.SizeW != 100 || layout.SizeH != 20 {
		t.Fatalf("flow-root child = %vx%v, want 100x20", layout.SizeW, layout.SizeH)
	}
}

// TestGridTrackListLaysOut: `grid-cols-[240px 1fr]` — a variable-length
// track list with a fixed track and an fr track — resolves the two
// children into 240px and the remaining 160px at a 400px width.
func TestGridTrackListLaysOut(t *testing.T) {
	svc := rev15LayoutService(t)
	engine, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Dispose()

	cell := func() *native.LayoutStyleRecord {
		style := native.NewLayoutStyleRecord()
		style.Size = native.LayoutSize{
			Width:  native.DefiniteLen(10),
			Height: native.DefiniteLen(10),
		}
		return &style
	}
	first, err := engine.CreateNode(cell())
	if err != nil {
		t.Fatal(err)
	}
	second, err := engine.CreateNode(cell())
	if err != nil {
		t.Fatal(err)
	}
	rootStyle := native.NewLayoutStyleRecord()
	rootStyle.Display = native.DisplayGrid
	rootStyle.Size = native.LayoutSize{
		Width:  native.DefiniteLen(400),
		Height: native.DefiniteLen(100),
	}
	root, err := engine.CreateNode(&rootStyle, first, second)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetStyle(root, &rootStyle); err != nil {
		t.Fatalf("SetStyle(root): %v", err)
	}
	tracks := []native.LayoutTrackRecord{
		{MinTag: native.TrackMinLength, MinBits: math.Float32bits(240)},
		{MinTag: native.TrackAuto, MaxTag: native.TrackFr, MaxBits: math.Float32bits(1)},
	}
	if err := engine.SetGridTracks(root, native.LayoutGridAxisColumns, tracks); err != nil {
		t.Fatalf("SetGridTracks: %v", err)
	}
	_, err = engine.Compute(root, native.LayoutAvailSize{
		Width:  native.DefiniteAvail(400),
		Height: native.DefiniteAvail(100),
	}, 0)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	firstLayout, err := engine.NodeLayout(first)
	if err != nil {
		t.Fatalf("NodeLayout(first): %v", err)
	}
	if firstLayout.LocationX != 0 {
		t.Fatalf("track 1 x = %v, want 0", firstLayout.LocationX)
	}
	secondLayout, err := engine.NodeLayout(second)
	if err != nil {
		t.Fatalf("NodeLayout(second): %v", err)
	}
	if secondLayout.LocationX < 239 || secondLayout.LocationX > 241 {
		t.Fatalf("track 2 x = %v, want 240 (240px then 1fr at a 400px width)", secondLayout.LocationX)
	}

	// Malformed tracks are typed errors.
	bad := []native.LayoutTrackRecord{{MinTag: 5}}
	if err := engine.SetGridTracks(root, native.LayoutGridAxisRows, bad); err == nil {
		t.Error("SetGridTracks accepted an unknown min tag")
	}
	badFr := []native.LayoutTrackRecord{{MaxTag: native.TrackFr, MaxBits: math.Float32bits(0)}}
	if err := engine.SetGridTracks(root, native.LayoutGridAxisRows, badFr); err == nil {
		t.Error("SetGridTracks accepted a zero fr factor")
	}
	if err := engine.SetGridTracks(root, 2, nil); err == nil {
		t.Error("SetGridTracks accepted an unknown axis")
	}
}

// TestJustifyItemsAndSelfAreAccepted: the style record's justify
// fields translate into Taffy's justify-items/justify-self and a grid
// with justify-items center computes (the child is centered in its
// track).
func TestJustifyItemsAndSelfAreAccepted(t *testing.T) {
	svc := rev15LayoutService(t)
	engine, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Dispose()

	cell := func() *native.LayoutStyleRecord {
		style := native.NewLayoutStyleRecord()
		style.Size = native.LayoutSize{
			Width:  native.DefiniteLen(10),
			Height: native.DefiniteLen(10),
		}
		return &style
	}
	first, err := engine.CreateNode(cell())
	if err != nil {
		t.Fatal(err)
	}
	rootStyle := native.NewLayoutStyleRecord()
	rootStyle.Display = native.DisplayGrid
	rootStyle.Size = native.LayoutSize{
		Width:  native.DefiniteLen(100),
		Height: native.DefiniteLen(40),
	}
	rootStyle.JustifyItemsPresent = 1
	rootStyle.JustifyItems = native.AlignCenter
	root, err := engine.CreateNode(&rootStyle, first)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetStyle(root, &rootStyle); err != nil {
		t.Fatalf("SetStyle(root): %v", err)
	}
	// A single fixed track: the 10px child centers in the 100px track.
	if err := engine.SetGridTracks(root, native.LayoutGridAxisColumns, []native.LayoutTrackRecord{
		{MinTag: native.TrackMinLength, MinBits: math.Float32bits(100)},
	}); err != nil {
		t.Fatalf("SetGridTracks: %v", err)
	}
	_, err = engine.Compute(root, native.LayoutAvailSize{
		Width:  native.DefiniteAvail(100),
		Height: native.DefiniteAvail(40),
	}, 0)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	layout, err := engine.NodeLayout(first)
	if err != nil {
		t.Fatalf("NodeLayout: %v", err)
	}
	if layout.LocationX < 44 || layout.LocationX > 46 {
		t.Fatalf("justify-items:center child x = %v, want ~45 (centered in a 100px track)", layout.LocationX)
	}

	// justify-self rides the child and overrides the container's
	// justify-items (start pushes it back to 0).
	childStyle := cell()
	childStyle.JustifySelfPresent = 1
	childStyle.JustifySelf = native.AlignFlexStart
	if err := engine.SetStyle(first, childStyle); err != nil {
		t.Fatalf("SetStyle(child): %v", err)
	}
	_, err = engine.Compute(root, native.LayoutAvailSize{
		Width:  native.DefiniteAvail(100),
		Height: native.DefiniteAvail(40),
	}, 1)
	if err != nil {
		t.Fatalf("Compute(justify-self): %v", err)
	}
	layout, err = engine.NodeLayout(first)
	if err != nil {
		t.Fatalf("NodeLayout: %v", err)
	}
	if layout.LocationX != 0 {
		t.Fatalf("justify-self:start child x = %v, want 0 (justify-self overrides justify-items)", layout.LocationX)
	}
}

// TestMeasureResponseBaselineEchoes: a measured node whose measure
// response reports a first baseline reads it back through the layout
// record (Taffy's stored layout drops baselines, so the stash is the
// report).
func TestMeasureResponseBaselineEchoes(t *testing.T) {
	svc := rev15LayoutService(t)
	engine, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Dispose()

	baseline := float32(12)
	token := engine.RegisterMeasure(func(req native.GoMeasureRequest) native.GoMeasureResponse {
		return native.GoMeasureResponse{Width: 60, Height: 20, FirstBaseline: &baseline}
	})
	measuredStyle := native.NewLayoutStyleRecord()
	measured, err := engine.CreateNode(&measuredStyle)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetMeasure(measured, token); err != nil {
		t.Fatal(err)
	}
	rootStyle := native.NewLayoutStyleRecord()
	rootStyle.Display = native.DisplayFlex
	rootStyle.Size = native.LayoutSize{
		Width:  native.DefiniteLen(60),
		Height: native.DefiniteLen(20),
	}
	root, err := engine.CreateNode(&rootStyle, measured)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetStyle(root, &rootStyle); err != nil {
		t.Fatalf("SetStyle(root): %v", err)
	}
	_, err = engine.Compute(root, native.LayoutAvailSize{
		Width:  native.DefiniteAvail(60),
		Height: native.DefiniteAvail(20),
	}, 0)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	layout, err := engine.NodeLayout(measured)
	if err != nil {
		t.Fatalf("NodeLayout: %v", err)
	}
	if layout.FirstBaselinePresent != 1 {
		t.Fatalf("first_baseline_present = %d, want 1 (the measure response carried one)", layout.FirstBaselinePresent)
	}
	if got := math.Float32frombits(layout.FirstBaselineBits); got != 12 {
		t.Fatalf("first_baseline = %v, want 12", got)
	}

	// A baseline-less measure leaves the echo absent.
	engine2, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine2.Dispose()
	token2 := engine2.RegisterMeasure(func(req native.GoMeasureRequest) native.GoMeasureResponse {
		return native.GoMeasureResponse{Width: 60, Height: 20}
	})
	measured2Style := native.NewLayoutStyleRecord()
	measured2, err := engine2.CreateNode(&measured2Style)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine2.SetMeasure(measured2, token2); err != nil {
		t.Fatal(err)
	}
	root2Style := native.NewLayoutStyleRecord()
	root2Style.Display = native.DisplayFlex
	root2Style.Size = native.LayoutSize{
		Width:  native.DefiniteLen(60),
		Height: native.DefiniteLen(20),
	}
	root2, err := engine2.CreateNode(&root2Style, measured2)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine2.SetStyle(root2, &root2Style); err != nil {
		t.Fatalf("SetStyle(root2): %v", err)
	}
	if _, err := engine2.Compute(root2, native.LayoutAvailSize{
		Width:  native.DefiniteAvail(60),
		Height: native.DefiniteAvail(20),
	}, 0); err != nil {
		t.Fatalf("Compute(root2): %v", err)
	}
	layout2, err := engine2.NodeLayout(measured2)
	if err != nil {
		t.Fatalf("NodeLayout(root2's measured): %v", err)
	}
	if layout2.FirstBaselinePresent != 0 {
		t.Fatalf("first_baseline_present = %d, want 0 (no baseline reported)", layout2.FirstBaselinePresent)
	}
}
