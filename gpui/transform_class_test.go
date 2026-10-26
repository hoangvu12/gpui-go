package gpui

import "testing"

func TestTransformClassesApply(t *testing.T) {
	cases := []struct {
		classes string
		want    []TransformFunction
	}{
		{"translate-x-4", []TransformFunction{Translate(DefiniteRem(1), DefinitePx(0))}},
		{"-translate-x-4", []TransformFunction{Translate(DefiniteRem(-1), DefinitePx(0))}},
		{"translate-y-px", []TransformFunction{Translate(DefinitePx(0), DefinitePx(1))}},
		{"translate-x-full", []TransformFunction{Translate(Fraction(1), DefinitePx(0))}},
		{"translate-x-1/2", []TransformFunction{Translate(Fraction(0.5), DefinitePx(0))}},
		{"translate-x-[12px]", []TransformFunction{Translate(DefinitePx(12), DefinitePx(0))}},
		{"scale-95", []TransformFunction{Scale(0.95, 0.95)}},
		{"scale-[1.7]", []TransformFunction{Scale(1.7, 1.7)}},
		{"-scale-x-75", []TransformFunction{Scale(-0.75, 1)}},
		{"rotate-45", []TransformFunction{Rotate(45)}},
		{"-rotate-90", []TransformFunction{Rotate(-90)}},
		{"rotate-[30deg]", []TransformFunction{Rotate(30)}},
		{"skew-x-3", []TransformFunction{Skew(3, 0)}},
		{"skew-y-12", []TransformFunction{Skew(0, 12)}},
		{"translate-x-4 rotate-45 scale-95", []TransformFunction{
			Translate(DefiniteRem(1), DefinitePx(0)),
			Rotate(45),
			Scale(0.95, 0.95),
		}},
	}
	for _, c := range cases {
		set := parseClasses(c.classes)
		if set.err != nil {
			t.Errorf("%q: %v", c.classes, set.err)
			continue
		}
		got, ok := set.base.Transform.Get()
		if !ok {
			t.Errorf("%q: no transform set", c.classes)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("%q: got %d functions, want %d", c.classes, len(got), len(c.want))
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: function %d: got %+v, want %+v", c.classes, i, got[i], c.want[i])
			}
		}
	}
	// The origin keyword classes are method classes.
	for class, wantX := range map[string]float32{
		"origin-center": 0.5, "origin-top-left": 0, "origin-bottom-right": 1, "origin-left": 0,
	} {
		set := parseClasses(class)
		if set.err != nil {
			t.Errorf("%q: %v", class, set.err)
			continue
		}
		origin, ok := set.base.TransformOrigin.Get()
		if !ok {
			t.Errorf("%q: no origin set", class)
			continue
		}
		if origin.X.Kind != DefiniteLengthFraction || origin.X.Value != wantX {
			t.Errorf("%q: origin X = %+v, want fraction %v", class, origin.X, wantX)
		}
	}
	// Rejections.
	for _, class := range []string{"translate-x-45deg", "scale-45deg", "rotate-red-500", "skew-x-full"} {
		if err := CheckClasses(class); err == nil {
			t.Errorf("%q: accepted, want rejected", class)
		}
	}
}
