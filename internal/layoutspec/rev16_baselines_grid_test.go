//go:build windows

// The browser-core ticket 42 (rev 16) gates: baseline alignment driven
// by the measured first baseline (the trait-driven Taffy compute), and
// the named grid areas (grid-template-areas + grid-area), through the
// native layout engine.
package layoutspec

import (
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// TestMeasuredBaselineAlignsFlexRow: two measured leaves of different
// heights whose measure responses carry first baselines — a 20px-tall
// item with its baseline at 5 and a 40px-tall one at 30 — align their
// baselines in an align-items: baseline flex row (the pre-rev-16
// algorithms fell back to the items' bottom edges; the trait-driven
// compute feeds the baselines into flexbox's §8.3 alignment, which is
// what the browser-parity fixtures record against Chrome).
func TestMeasuredBaselineAlignsFlexRow(t *testing.T) {
	svc := rev15LayoutService(t)
	engine, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Dispose()

	smallBaseline, largeBaseline := float32(5), float32(30)
	var calls int
	measureOf := func(width, height float32, baseline *float32) native.MeasureFunc {
		return func(req native.GoMeasureRequest) native.GoMeasureResponse {
			calls++
			return native.GoMeasureResponse{Width: width, Height: height, FirstBaseline: baseline}
		}
	}
	smallToken := engine.RegisterMeasure(measureOf(60, 20, &smallBaseline))
	largeToken := engine.RegisterMeasure(measureOf(60, 40, &largeBaseline))

	leaf := func(token uint64) native.NodeHandle {
		style := native.NewLayoutStyleRecord()
		node, err := engine.CreateNode(&style)
		if err != nil {
			t.Fatal(err)
		}
		if err := engine.SetMeasure(node, token); err != nil {
			t.Fatal(err)
		}
		return node
	}
	small := leaf(smallToken)
	large := leaf(largeToken)

	rootStyle := native.NewLayoutStyleRecord()
	rootStyle.Display = native.DisplayFlex
	// align-items: baseline.
	rootStyle.AlignItemsPresent = 1
	rootStyle.AlignItems = native.AlignBaseline
	rootStyle.Size = native.LayoutSize{
		Width:  native.DefiniteLen(200),
		Height: native.AbsentLen(),
	}
	root, err := engine.CreateNode(&rootStyle, small, large)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetStyle(root, &rootStyle); err != nil {
		t.Fatalf("SetStyle(root): %v", err)
	}
	if _, err := engine.Compute(root, native.LayoutAvailSize{
		Width:  native.DefiniteAvail(200),
		Height: native.MinContentAvail(),
	}, 0); err != nil {
		t.Fatalf("Compute: %v", err)
	}

	// The small item's baseline (5) aligns to the row's max baseline
	// (30): it shifts down by 25; the large one sits at the top.
	smallLayout, err := engine.NodeLayout(small)
	if err != nil {
		t.Fatalf("NodeLayout(small): %v", err)
	}
	if smallLayout.LocationY != 25 {
		t.Fatalf("small item y = %v, want 25 (baseline 5 aligned to the row's 30)", smallLayout.LocationY)
	}
	if smallLayout.SizeW != 60 || smallLayout.SizeH != 20 {
		t.Fatalf("small item size = %vx%v, want 60x20", smallLayout.SizeW, smallLayout.SizeH)
	}
	largeLayout, err := engine.NodeLayout(large)
	if err != nil {
		t.Fatalf("NodeLayout(large): %v", err)
	}
	if largeLayout.LocationY != 0 {
		t.Fatalf("large item y = %v, want 0", largeLayout.LocationY)
	}
	// The row's auto height: max baseline 30 + the largest
	// below-baseline distance (the small item's 20-5) = 45.
	rootLayout, err := engine.NodeLayout(root)
	if err != nil {
		t.Fatalf("NodeLayout(root): %v", err)
	}
	if rootLayout.SizeH != 45 {
		t.Fatalf("row height = %v, want 45 (baseline-summed line size)", rootLayout.SizeH)
	}
	// The echo still reports the raw measure-response baselines.
	if smallLayout.FirstBaselinePresent != 1 || math.Float32frombits(smallLayout.FirstBaselineBits) != 5 {
		t.Fatalf("small echo baseline = present(%d) %v, want present 5", smallLayout.FirstBaselinePresent, math.Float32frombits(smallLayout.FirstBaselineBits))
	}
	if calls == 0 {
		t.Fatal("the measure functions were never invoked")
	}
}

// TestGridBaselineAlignsRow: the same alignment inside a grid row
// (css-grid-1 §11.5.1's baseline shim): the small item is shifted down
// so its baseline matches the row's max baseline, and the row grows to
// the baseline-summed size.
func TestGridBaselineAlignsRow(t *testing.T) {
	svc := rev15LayoutService(t)
	engine, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Dispose()

	smallBaseline, largeBaseline := float32(5), float32(30)
	smallToken := engine.RegisterMeasure(func(req native.GoMeasureRequest) native.GoMeasureResponse {
		return native.GoMeasureResponse{Width: 60, Height: 20, FirstBaseline: &smallBaseline}
	})
	largeToken := engine.RegisterMeasure(func(req native.GoMeasureRequest) native.GoMeasureResponse {
		return native.GoMeasureResponse{Width: 60, Height: 40, FirstBaseline: &largeBaseline}
	})
	leaf := func(token uint64) native.NodeHandle {
		style := native.NewLayoutStyleRecord()
		node, err := engine.CreateNode(&style)
		if err != nil {
			t.Fatal(err)
		}
		if err := engine.SetMeasure(node, token); err != nil {
			t.Fatal(err)
		}
		return node
	}
	small := leaf(smallToken)
	large := leaf(largeToken)

	rootStyle := native.NewLayoutStyleRecord()
	rootStyle.Display = native.DisplayGrid
	rootStyle.AlignItemsPresent = 1
	rootStyle.AlignItems = native.AlignBaseline
	rootStyle.Size = native.LayoutSize{
		Width:  native.DefiniteLen(200),
		Height: native.AbsentLen(),
	}
	root, err := engine.CreateNode(&rootStyle, small, large)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetStyle(root, &rootStyle); err != nil {
		t.Fatalf("SetStyle(root): %v", err)
	}
	// Two fixed columns keep the items on one row.
	tracks := []native.LayoutTrackRecord{
		{MinTag: native.TrackMinLength, MinBits: math.Float32bits(100)},
		{MinTag: native.TrackMinLength, MinBits: math.Float32bits(100)},
	}
	if err := engine.SetGridTracks(root, native.LayoutGridAxisColumns, tracks); err != nil {
		t.Fatalf("SetGridTracks: %v", err)
	}
	if _, err := engine.Compute(root, native.LayoutAvailSize{
		Width:  native.DefiniteAvail(200),
		Height: native.MinContentAvail(),
	}, 0); err != nil {
		t.Fatalf("Compute: %v", err)
	}

	smallLayout, err := engine.NodeLayout(small)
	if err != nil {
		t.Fatalf("NodeLayout(small): %v", err)
	}
	if smallLayout.LocationY != 25 {
		t.Fatalf("small grid item y = %v, want 25 (baseline 5 shimmed to the row's 30)", smallLayout.LocationY)
	}
	largeLayout, err := engine.NodeLayout(large)
	if err != nil {
		t.Fatalf("NodeLayout(large): %v", err)
	}
	if largeLayout.LocationY != 0 {
		t.Fatalf("large grid item y = %v, want 0", largeLayout.LocationY)
	}
}

// TestGridTemplateAreasPlaceItems: a two-row area template
// (`"head head" "side main"`) places three items by name — head spans
// both columns of row 1, side and main split row 2.
func TestGridTemplateAreasPlaceItems(t *testing.T) {
	svc := rev15LayoutService(t)
	engine, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Dispose()

	cell := func() native.NodeHandle {
		style := native.NewLayoutStyleRecord()
		style.Size = native.LayoutSize{
			Width:  native.DefiniteLen(20),
			Height: native.DefiniteLen(30),
		}
		node, err := engine.CreateNode(&style)
		if err != nil {
			t.Fatal(err)
		}
		return node
	}
	head, side, main := cell(), cell(), cell()

	rootStyle := native.NewLayoutStyleRecord()
	rootStyle.Display = native.DisplayGrid
	rootStyle.Size = native.LayoutSize{
		Width:  native.DefiniteLen(300),
		Height: native.AbsentLen(),
	}
	root, err := engine.CreateNode(&rootStyle, head, side, main)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetStyle(root, &rootStyle); err != nil {
		t.Fatalf("SetStyle(root): %v", err)
	}
	names := []byte("headsidemain")
	areas := []native.LayoutGridAreaRecord{
		{RowStart: 1, RowEnd: 2, ColumnStart: 1, ColumnEnd: 3, NameOffset: 0, NameLen: 4},
		{RowStart: 2, RowEnd: 3, ColumnStart: 1, ColumnEnd: 2, NameOffset: 4, NameLen: 4},
		{RowStart: 2, RowEnd: 3, ColumnStart: 2, ColumnEnd: 3, NameOffset: 8, NameLen: 4},
	}
	if err := engine.SetGridTemplateAreas(root, 2, 2, areas, names); err != nil {
		t.Fatalf("SetGridTemplateAreas: %v", err)
	}
	if err := engine.SetGridArea(head, "head"); err != nil {
		t.Fatalf("SetGridArea(head): %v", err)
	}
	if err := engine.SetGridArea(side, "side"); err != nil {
		t.Fatalf("SetGridArea(side): %v", err)
	}
	if err := engine.SetGridArea(main, "main"); err != nil {
		t.Fatalf("SetGridArea(main): %v", err)
	}
	// A 150px column track; the second column is implicit (auto).
	columns := []native.LayoutTrackRecord{{MinTag: native.TrackMinLength, MinBits: math.Float32bits(150)}}
	if err := engine.SetGridTracks(root, native.LayoutGridAxisColumns, columns); err != nil {
		t.Fatalf("SetGridTracks: %v", err)
	}
	if _, err := engine.Compute(root, native.LayoutAvailSize{
		Width:  native.DefiniteAvail(300),
		Height: native.MinContentAvail(),
	}, 0); err != nil {
		t.Fatalf("Compute: %v", err)
	}

	for _, placement := range []struct {
		name string
		node native.NodeHandle
		x, y float32
	}{
		{"head", head, 0, 0},
		{"side", side, 0, 30},
		{"main", main, 150, 30},
	} {
		layout, err := engine.NodeLayout(placement.node)
		if err != nil {
			t.Fatalf("NodeLayout(%s): %v", placement.name, err)
		}
		if layout.LocationX != placement.x || layout.LocationY != placement.y {
			t.Fatalf("%s at (%v, %v), want (%v, %v)", placement.name, layout.LocationX, layout.LocationY, placement.x, placement.y)
		}
	}

	// Validation: a duplicate area name and a bad name slice are caller
	// errors, and clearing the template (count 0) succeeds.
	duplicate := []native.LayoutGridAreaRecord{
		{RowStart: 1, RowEnd: 2, ColumnStart: 1, ColumnEnd: 2, NameOffset: 0, NameLen: 4},
		{RowStart: 1, RowEnd: 2, ColumnStart: 1, ColumnEnd: 2, NameOffset: 0, NameLen: 4},
	}
	if err := engine.SetGridTemplateAreas(root, 2, 2, duplicate, names); err == nil {
		t.Fatal("a duplicate area name must be a caller error")
	}
	badSlice := []native.LayoutGridAreaRecord{
		{RowStart: 1, RowEnd: 2, ColumnStart: 1, ColumnEnd: 2, NameOffset: 100, NameLen: 4},
	}
	if err := engine.SetGridTemplateAreas(root, 1, 1, badSlice, names); err == nil {
		t.Fatal("an out-of-range name slice must be a caller error")
	}
	if err := engine.SetGridTemplateAreas(root, 0, 0, nil, nil); err != nil {
		t.Fatalf("clearing the template: %v", err)
	}
	if err := engine.SetGridArea(head, ""); err != nil {
		t.Fatalf("clearing the area placement: %v", err)
	}
}

// TestGridAreaNameUnknownIsAccepted: an area name the template does not
// define is accepted at set time (CSS resolves unknown line names to
// the line after the explicit grid; the placement falls back there).
func TestGridAreaNameUnknownIsAccepted(t *testing.T) {
	svc := rev15LayoutService(t)
	engine, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Dispose()

	style := native.NewLayoutStyleRecord()
	node, err := engine.CreateNode(&style)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetGridArea(node, "missing"); err != nil {
		t.Fatalf("SetGridArea(missing): %v (set time must accept unknown names)", err)
	}
	// An over-long name is a caller error.
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'a'
	}
	if err := engine.SetGridArea(node, string(long)); err == nil {
		t.Fatal("an over-long area name must be a caller error")
	}
}
