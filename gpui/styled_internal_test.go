package gpui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"
)

// ownMethods parses the package's non-test sources and returns the
// method names each named type declares itself.
func ownMethods(t *testing.T, types ...string) map[string]map[string]bool {
	t.Helper()
	want := map[string]map[string]bool{}
	for _, name := range types {
		want[name] = map[string]bool{}
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range pkgs["gpui"].Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 {
				continue
			}
			recv := fd.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
			}
			if ix, ok := recv.(*ast.IndexExpr); ok {
				recv = ix.X
			}
			if id, ok := recv.(*ast.Ident); ok && want[id.Name] != nil {
				want[id.Name][fd.Name.Name] = true
			}
		}
	}
	return want
}

// TestStyledGeneratedVocabularyIsComplete checks the generated method
// index against the pinned macro tables (styles.rs) and that no built-in
// element shadows a generated method except the known overrides.
func TestStyledGeneratedVocabularyIsComplete(t *testing.T) {
	// styles.rs:1000-1056 generate_box_style_methods: per prefix one
	// custom setter, the 46 non-auto suffixes twice (plain and _neg_),
	// plus the auto suffix when the prefix allows it.
	const boxSuffixes, nonAuto = 46, 45
	boxCount := func(prefixes, autoAllowed int) int {
		return prefixes*(1+2*nonAuto) + autoAllowed
	}
	_ = boxSuffixes
	want := 0
	want += boxCount(12, 9) // box_prefixes: gap, gap_x, gap_y disallow auto
	want += boxCount(7, 7)  // margin
	want += boxCount(7, 0)  // padding
	want += boxCount(5, 5)  // position inset/top/bottom/left/right
	want += 2               // relative, absolute
	want += 9 * (1 + 9)     // corner prefixes x (custom + corner_suffixes)
	want += 11              // corner smoothing suffixes
	want += 2               // visible, invisible
	want += 3               // overflow_hidden, overflow_x_hidden, overflow_y_hidden
	want += 1 + 23          // cursor + cursor_* presets
	want += 1 + 7*(1+17)    // border_color + border prefixes x (custom + border_suffixes)
	want += 6 + 10 + 7      // shadow/shadow_none/ring/ring_color/inset_ring/inset_ring_color, ring widths, presets
	if got := len(generatedStyledMethods); got != want {
		t.Fatalf("generated methods = %d, want %d (the styles.rs tables)", got, want)
	}

	// Every vocabulary method is generic (the lazy marker), so reflection
	// cannot see it; promotion onto the built-in elements is the
	// language's. What can go wrong is an element method shadowing a
	// vocabulary method: only the known overrides may.
	overrides := map[string]map[string]bool{
		// The port's legacy two-argument Size and Gap (div.go).
		"DivElement": {"Size": true, "Gap": true},
	}
	declared := ownMethods(t, "DivElement", "ImgElement", "CanvasElement")
	for owner, names := range declared {
		for name := range names {
			if _, vocab := generatedStyledMethods[name]; vocab && !overrides[owner][name] {
				t.Errorf("%s.%s shadows the generated Styled method %s", owner, name, generatedStyledMethods[name])
			}
		}
	}
	// A spot check of the naming rule.
	for rust, goName := range map[string]string{
		"w_1_2": "W1_2", "w_neg_0p5": "WNeg0p5", "px_px": "PxPx", "rounded_t_2xl": "RoundedT2xl",
		"gap_x_1_12": "GapX1_12", "rounded_smoothing_0p1": "RoundedSmoothing0p1", "inset_ring_4": "InsetRing4",
	} {
		if generatedStyledMethods[goName] != rust {
			t.Errorf("generated %s = %q, want %q", goName, generatedStyledMethods[goName], rust)
		}
	}
	for name := range generatedStyledMethods {
		if strings.Contains(name, "__") {
			t.Errorf("malformed name %s", name)
		}
	}
}

// TestOuterRingIsHollowAndComposesWithShadows ports style.rs:1850-1906
// outer_ring_is_hollow_and_composes_with_shadows (the gradient ring
// color is replaced by a solid color: the port's Background is
// solid-only).
func TestOuterRingIsHollowAndComposesWithShadows(t *testing.T) {
	dropShadow := NewBoxShadow(0, 2, SolidBackground(Hsla{H: 240.0 / 360, S: 1, L: 0.5, A: 1})).WithBlurRadius(4)
	style := DefaultStyle()
	r1 := StyleRefinement{RingWidth: Some(float32(2)), BoxShadow: Some([]BoxShadow{dropShadow})}
	style.Refine(&r1)
	current := Hsla{H: 0.7, S: 0.5, L: 0.4, A: 0.8}
	r2 := StyleRefinement{RingWidth: Some(float32(3.5))}
	style.Refine(&r2)
	elementBounds := Bounds{Origin: Point{X: 10, Y: 20}, Size: Size{Width: 100, Height: 40}}
	elementRadii := Corners{TopLeft: 0, TopRight: 4, BottomRight: 10, BottomLeft: 20}
	ring, ok := style.Ring.outerQuad(elementBounds, elementRadii, current)
	if !ok {
		t.Fatal("ring missing")
	}
	if dilate(ring.Bounds, -3.5) != elementBounds {
		t.Errorf("ring bounds = %v", ring.Bounds)
	}
	if ring.BorderWidths != (Edges{Top: 3.5, Right: 3.5, Bottom: 3.5, Left: 3.5}) {
		t.Errorf("ring widths = %+v", ring.BorderWidths)
	}
	shrunk := Corners{TopLeft: ring.CornerRadii.TopLeft - 3.5, TopRight: ring.CornerRadii.TopRight - 3.5, BottomRight: ring.CornerRadii.BottomRight - 3.5, BottomLeft: ring.CornerRadii.BottomLeft - 3.5}
	if shrunk != elementRadii {
		t.Errorf("ring radii = %+v", ring.CornerRadii)
	}
	if !ring.Background.IsTransparent() || ring.BorderColor != SolidBackground(current) || ring.BorderStyle != BorderStyleSolid {
		t.Errorf("ring paint = %+v", ring)
	}
	if !reflect.DeepEqual(style.BoxShadow, []BoxShadow{dropShadow}) {
		t.Errorf("shadows = %+v", style.BoxShadow)
	}
	solid := SolidBackground(Hsla{H: 0, S: 1, L: 0.5, A: 1})
	r3 := StyleRefinement{RingColor: Some(RingColorOf(solid))}
	style.Refine(&r3)
	if ring, _ := style.Ring.outerQuad(elementBounds, elementRadii, current); ring.BorderColor != solid {
		t.Errorf("explicit ring color = %+v", ring.BorderColor)
	}
	r4 := StyleRefinement{RingWidth: Some(float32(0))}
	style.Refine(&r4)
	if _, ok := style.Ring.outerQuad(elementBounds, elementRadii, current); ok {
		t.Error("ring_0 still paints")
	}
	if len(style.BoxShadow) != 1 {
		t.Error("ring refinement touched the shadows")
	}
}

// TestInsetRingLowersToAnInsetShadow ports style.rs:1908-1927
// inset_ring_utility_refines_independently_and_lowers_to_an_inset_shadow
// (solid color for the gradient).
func TestInsetRingLowersToAnInsetShadow(t *testing.T) {
	color := SolidBackground(Hsla{H: 0.3, S: 1, L: 0.5, A: 1})
	style := DefaultStyle()
	r1 := StyleRefinement{InsetRingWidth: Some(float32(1)), InsetRingColor: Some(RingColorOf(color))}
	style.Refine(&r1)
	r2 := StyleRefinement{InsetRingWidth: Some(float32(4))}
	style.Refine(&r2)
	inset, ok := style.InsetRing.insetShadow(Hsla{H: 240.0 / 360, S: 1, L: 0.5, A: 1})
	if !ok {
		t.Fatal("inset ring missing")
	}
	if inset.OffsetX != 0 || inset.OffsetY != 0 || inset.BlurRadius != 0 || inset.SpreadRadius != 4 || inset.Color != color || !inset.Inset {
		t.Errorf("inset ring shadow = %+v", inset)
	}
	if style.Ring != (RingStyle{}) {
		t.Errorf("outer ring changed: %+v", style.Ring)
	}
}
