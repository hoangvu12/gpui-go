package base

import "testing"

// The scale tests of U:plot/scale/{band,linear,ordinal,point}.rs (25/25).
// Rust's Option<f32> is the (value, ok) pair; tickIs compares both.

func tickIs(t *testing.T, what string, v float32, ok bool, want float32, wantOK bool) {
	t.Helper()
	if ok != wantOK || (ok && v != want) {
		if wantOK {
			t.Fatalf("%s = (%v, %v), want Some(%v)", what, v, ok, want)
		}
		t.Fatalf("%s = (%v, %v), want None", what, v, ok)
	}
}

// U:plot/scale/band.rs:166-173
func TestScaleBand(t *testing.T) {
	s := NewScaleBand([]int{1, 2, 3}, [2]float32{0, 90})
	for i, want := range []float32{0, 30, 60} {
		v, ok := s.Tick(i + 1)
		tickIs(t, "tick", v, ok, want, true)
	}
	if s.BandWidth() != 30 {
		t.Fatalf("band width = %v", s.BandWidth())
	}
}

// U:plot/scale/band.rs:175-182
func TestScaleBandMaxBandWidthCapsTheWidthButNotTheTicks(t *testing.T) {
	wide := NewScaleBand([]int{1, 2}, [2]float32{0, 200})
	capped := NewScaleBand([]int{1, 2}, [2]float32{0, 200}).MaxBandWidth(30)
	if wide.BandWidth() != 100 || capped.BandWidth() != 30 {
		t.Fatalf("widths = %v, %v", wide.BandWidth(), capped.BandWidth())
	}
	a, aok := capped.Tick(2)
	b, bok := wide.Tick(2)
	if a != b || aok != bok {
		t.Fatalf("capped tick %v != wide tick %v", a, b)
	}
}

// U:plot/scale/band.rs:184-193
func TestScaleBandDedup(t *testing.T) {
	// Simulates a grouped bar chart: 2 series × 3 categories, 3 unique.
	s := NewScaleBand([]int{1, 2, 3, 1, 2, 3}, [2]float32{0, 90})
	if s.len() != 3 {
		t.Fatalf("len = %d", s.len())
	}
	for i, want := range []float32{0, 30, 60} {
		v, ok := s.Tick(i + 1)
		tickIs(t, "tick", v, ok, want, true)
	}
	if s.BandWidth() != 30 {
		t.Fatalf("band width = %v", s.BandWidth())
	}
}

func mustTick[T any](t *testing.T, s Scale[T], v T) float32 {
	t.Helper()
	x, ok := s.Tick(v)
	if !ok {
		t.Fatalf("tick(%v) = None", v)
	}
	return x
}

// U:plot/scale/band.rs:195-213
func TestScaleBandStep(t *testing.T) {
	s := NewScaleBand([]int{1, 2, 3}, [2]float32{0, 90})
	if s.Step() != mustTick[int](t, s, 2)-mustTick[int](t, s, 1) {
		t.Fatalf("step = %v", s.Step())
	}
	padded := NewScaleBand([]int{1, 2, 3}, [2]float32{0, 90}).PaddingInner(0.4).PaddingOuter(0.2)
	if d := padded.Step() - (mustTick[int](t, padded, 2) - mustTick[int](t, padded, 1)); abs32(d) >= 1e-4 {
		t.Fatalf("padded step off by %v", d)
	}
	// A single band spans the range.
	if st := NewScaleBand([]int{1}, [2]float32{0, 90}).Step(); st != 90 {
		t.Fatalf("single step = %v", st)
	}
}

// U:plot/scale/band.rs:215-240
func TestScaleBandCount(t *testing.T) {
	scale := func(domain []int) ScaleBand[int] {
		return NewScaleBand(domain, [2]float32{0, 100}).BandCount(4).PaddingInner(0.4).PaddingOuter(0.2)
	}
	short := scale([]int{1, 2})
	full := scale([]int{1, 2, 3, 4})
	if mustTick[int](t, short, 2) != mustTick[int](t, full, 2) || short.BandWidth() != full.BandWidth() || short.Step() != full.Step() {
		t.Fatal("the domain does not take the leading bands")
	}
	// An empty band resolves past the domain rather than to its last value.
	if i := short.NearestIndex(mustTick[int](t, full, 4)); i != 3 {
		t.Fatalf("nearest = %d, want 3", i)
	}
	// A single value sits in the first band instead of the center.
	if mustTick[int](t, scale([]int{1}), 1) != mustTick[int](t, full, 1) {
		t.Fatal("single value not in the first band")
	}
	// A count below the domain's length has no effect.
	v, ok := NewScaleBand([]int{1, 2, 3}, [2]float32{0, 90}).BandCount(2).Tick(3)
	tickIs(t, "tick", v, ok, 60, true)
}

// U:plot/scale/band.rs:242-255
func TestScaleBandZero(t *testing.T) {
	s := NewScaleBand([]int{}, [2]float32{0, 90})
	for _, d := range []int{1, 2, 3} {
		v, ok := s.Tick(d)
		tickIs(t, "tick", v, ok, 0, false)
	}
	if s.BandWidth() != 0 {
		t.Fatalf("band width = %v", s.BandWidth())
	}
	s = NewScaleBand([]int{1, 2, 3}, [2]float32{0, 0})
	for _, d := range []int{1, 2, 3} {
		v, ok := s.Tick(d)
		tickIs(t, "tick", v, ok, 0, true)
	}
	if s.BandWidth() != 0 {
		t.Fatalf("band width = %v", s.BandWidth())
	}
}

// U:plot/scale/band.rs:257-265
func TestScaleBandRangeStart(t *testing.T) {
	s := NewScaleBand([]int{1, 2, 3}, [2]float32{10, 100})
	v, ok := s.Tick(1)
	tickIs(t, "tick(1)", v, ok, 10, true)
	v, ok = s.Tick(2)
	tickIs(t, "tick(2)", v, ok, 40, true)
	if i := s.NearestIndex(41); i != 1 {
		t.Fatalf("nearest = %d", i)
	}
	// The lower end leads whichever way the range is written.
	v, ok = NewScaleBand([]int{1, 2, 3}, [2]float32{100, 10}).Tick(1)
	tickIs(t, "reversed tick(1)", v, ok, 10, true)
}

// U:plot/scale/linear.rs:51-62
func TestScaleLinear(t *testing.T) {
	s := NewScaleLinear([]float64{1, 2, 3}, [2]float32{0, 100})
	for i, want := range []float32{0, 50, 100} {
		v, ok := s.Tick(float64(i + 1))
		tickIs(t, "tick", v, ok, want, true)
	}
	s = NewScaleLinear([]float64{1, 2, 3}, [2]float32{100, 0})
	for i, want := range []float32{100, 50, 0} {
		v, ok := s.Tick(float64(i + 1))
		tickIs(t, "reversed tick", v, ok, want, true)
	}
}

// U:plot/scale/linear.rs:64-69
func TestScaleLinearUnorderedDomain(t *testing.T) {
	s := NewScaleLinear([]float64{3, 1, 2}, [2]float32{0, 100})
	v, ok := s.Tick(1)
	tickIs(t, "tick(1)", v, ok, 0, true)
	v, ok = s.Tick(3)
	tickIs(t, "tick(3)", v, ok, 100, true)
}

// U:plot/scale/linear.rs:71-76
func TestScaleLinearF32(t *testing.T) {
	s := NewScaleLinear([]float32{0, 4}, [2]float32{0, 100})
	v, ok := s.Tick(1)
	tickIs(t, "tick(1)", v, ok, 25, true)
	v, ok = s.Tick(4)
	tickIs(t, "tick(4)", v, ok, 100, true)
}

// U:plot/scale/linear.rs:78-89
func TestScaleLinearEmpty(t *testing.T) {
	s := NewScaleLinear([]float64{}, [2]float32{0, 100})
	for _, d := range []float64{1, 2, 3} {
		v, ok := s.Tick(d)
		tickIs(t, "tick", v, ok, 0, false)
	}
	s = NewScaleLinear([]float64{1, 2, 3}, [2]float32{0, 0})
	for _, d := range []float64{1, 2, 3} {
		v, ok := s.Tick(d)
		tickIs(t, "tick", v, ok, 0, true)
	}
}

func mapIs[R comparable](t *testing.T, v R, ok bool, want R, wantOK bool) {
	t.Helper()
	if ok != wantOK || (ok && v != want) {
		t.Fatalf("map = (%v, %v), want (%v, %v)", v, ok, want, wantOK)
	}
}

// U:plot/scale/ordinal.rs:71-81
func TestScaleOrdinal(t *testing.T) {
	s := NewScaleOrdinal([]string{"a", "b", "c", "d", "e"}, []int{10, 20, 30})
	for _, c := range []struct {
		in   string
		want int
		ok   bool
	}{{"a", 10, true}, {"b", 20, true}, {"c", 30, true}, {"d", 10, true}, {"e", 20, true}, {"f", 0, false}} {
		v, ok := s.Map(c.in)
		mapIs(t, v, ok, c.want, c.ok)
	}
}

// U:plot/scale/ordinal.rs:83-89
func TestScaleOrdinalUnknown(t *testing.T) {
	s := NewScaleOrdinal([]string{"a", "b", "c"}, []int{10, 20, 30}).Unknown(0)
	v, ok := s.Map("a")
	mapIs(t, v, ok, 10, true)
	v, ok = s.Map("f")
	mapIs(t, v, ok, 0, true)
}

// U:plot/scale/ordinal.rs:91-103
func TestScaleOrdinalColors(t *testing.T) {
	s := NewScaleOrdinal([]string{"a", "b", "c", "d"}, []string{"#1f77b4", "#ff7f0e", "#2ca02c"})
	for _, c := range [][2]string{{"a", "#1f77b4"}, {"b", "#ff7f0e"}, {"c", "#2ca02c"}, {"d", "#1f77b4"}} {
		v, ok := s.Map(c[0])
		mapIs(t, v, ok, c[1], true)
	}
}

// U:plot/scale/point.rs:95-101
func TestScalePoint(t *testing.T) {
	s := NewScalePoint([]int{1, 2, 3}, [2]float32{0, 100})
	for i, want := range []float32{0, 50, 100} {
		v, ok := s.Tick(i + 1)
		tickIs(t, "tick", v, ok, want, true)
	}
}

// U:plot/scale/point.rs:103-109
func TestScalePointRange(t *testing.T) {
	s := NewScalePoint([]int{1, 2, 3}, [2]float32{40, 80})
	for i, want := range []float32{40, 60, 80} {
		v, ok := s.Tick(i + 1)
		tickIs(t, "tick", v, ok, want, true)
	}
}

// U:plot/scale/point.rs:111-122
func TestScalePointEmpty(t *testing.T) {
	s := NewScalePoint([]int{}, [2]float32{0, 100})
	for _, d := range []int{1, 2, 3} {
		v, ok := s.Tick(d)
		tickIs(t, "tick", v, ok, 0, false)
	}
	s = NewScalePoint([]int{1, 2, 3}, [2]float32{0, 0})
	for _, d := range []int{1, 2, 3} {
		v, ok := s.Tick(d)
		tickIs(t, "tick", v, ok, 0, true)
	}
}

// U:plot/scale/point.rs:124-128
func TestScalePointSingle(t *testing.T) {
	v, ok := NewScalePoint([]int{1}, [2]float32{0, 100}).Tick(1)
	tickIs(t, "tick", v, ok, 50, true)
}

// U:plot/scale/point.rs:130-139
func TestScalePointTickAtMatchesTick(t *testing.T) {
	for _, domain := range [][]int{{}, {1}, {1, 2, 3}, {1, 2, 3, 4, 5}} {
		s := NewScalePoint(domain, [2]float32{40, 80})
		for i, value := range domain {
			a, aok := s.TickAt(i)
			b, bok := s.Tick(value)
			if a != b || aok != bok {
				t.Fatalf("tick_at(%d) = (%v,%v), tick = (%v,%v)", i, a, aok, b, bok)
			}
		}
		if _, ok := s.TickAt(len(domain)); ok {
			t.Fatal("tick_at past the domain is Some")
		}
	}
}

func nearestIs(t *testing.T, s Scale[int], cases map[float32]int) {
	t.Helper()
	for tick, want := range cases {
		if got := s.NearestIndex(tick); got != want {
			t.Fatalf("nearest_index(%v) = %d, want %d", tick, got, want)
		}
	}
}

// U:plot/scale/point.rs:141-161
func TestScalePointNearestIndexBasic(t *testing.T) {
	nearestIs(t, NewScalePoint([]int{1, 2, 3}, [2]float32{0, 100}), map[float32]int{
		0: 0, 50: 1, 100: 2, 24: 0, 25: 1, 26: 1, 74: 1, 75: 2, 76: 2, -10: 0, 150: 2,
	})
}

// U:plot/scale/point.rs:163-183
func TestScalePointNearestIndexWithOffset(t *testing.T) {
	nearestIs(t, NewScalePoint([]int{1, 2, 3}, [2]float32{40, 80}), map[float32]int{
		40: 0, 60: 1, 80: 2, 49: 0, 50: 1, 51: 1, 69: 1, 70: 2, 71: 2, 30: 0, 100: 2,
	})
}

// U:plot/scale/point.rs:185-191
func TestScalePointNearestIndexEmpty(t *testing.T) {
	nearestIs(t, NewScalePoint([]int{}, [2]float32{0, 100}), map[float32]int{0: 0, 50: 0, 100: 0})
}

// U:plot/scale/point.rs:193-199
func TestScalePointNearestIndexSingle(t *testing.T) {
	nearestIs(t, NewScalePoint([]int{1}, [2]float32{0, 100}), map[float32]int{0: 0, 50: 0, 100: 0})
}

// U:plot/scale/point.rs:201-207
func TestScalePointNearestIndexEmptyRange(t *testing.T) {
	nearestIs(t, NewScalePoint([]int{1, 2, 3}, [2]float32{0, 0}), map[float32]int{0: 0, 50: 0, 100: 0})
}

// U:plot/scale/point.rs:209-216
func TestScalePointReversedRange(t *testing.T) {
	s := NewScalePoint([]int{1, 2, 3}, [2]float32{100, 0})
	v, ok := s.Tick(1)
	tickIs(t, "tick(1)", v, ok, 100, true)
	v, ok = s.Tick(3)
	tickIs(t, "tick(3)", v, ok, 0, true)
	nearestIs(t, s, map[float32]int{90: 0, 10: 2})
}
