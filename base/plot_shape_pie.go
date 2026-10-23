package base

// The pie generator (U:plot/shape/pie.rs, after d3-shape's pie).
//
// Go adaptations: the boxed `Fn(&T) -> Option<f32>` accessor is a func
// of the datum returning (value, ok); ArcData's borrowed `&'a T` is a
// pointer into the slice passed to Arcs.

import "math"

// Pie lays data out as arcs around a circle (U:plot/shape/pie.rs:7-13).
type Pie[T any] struct {
	value      func(datum T) (float32, bool)
	startAngle float32
	endAngle   float32
	padAngle   float32
}

// NewPie returns a full-circle pie whose accessor yields no values
// (U:plot/shape/pie.rs:15-29).
func NewPie[T any]() Pie[T] {
	return Pie[T]{
		value:    func(T) (float32, bool) { return 0, false },
		endAngle: 2 * math.Pi,
	}
}

// Value sets the value accessor (U:plot/shape/pie.rs:31-38).
func (p Pie[T]) Value(value func(datum T) (float32, bool)) Pie[T] {
	p.value = value
	return p
}

// StartAngle sets the start angle (U:plot/shape/pie.rs:40-44).
func (p Pie[T]) StartAngle(startAngle float32) Pie[T] { p.startAngle = startAngle; return p }

// EndAngle sets the end angle (U:plot/shape/pie.rs:46-50).
func (p Pie[T]) EndAngle(endAngle float32) Pie[T] { p.endAngle = endAngle; return p }

// PadAngle sets the pad angle (U:plot/shape/pie.rs:52-56).
func (p Pie[T]) PadAngle(padAngle float32) Pie[T] { p.padAngle = padAngle; return p }

// Arcs returns one arc per datum with a positive value, in data order
// (U:plot/shape/pie.rs:58-96).
func (p Pie[T]) Arcs(data []T) []ArcData[T] {
	type entry struct {
		index int
		value float32
	}
	var values []entry
	var sum float32
	for idx := range data {
		if value, ok := p.value(data[idx]); ok && value > 0 {
			sum += value
			values = append(values, entry{idx, value})
		}
	}

	arcs := make([]ArcData[T], 0, len(values))
	k := p.startAngle
	for _, e := range values {
		startAngle := k
		var delta float32
		if sum > 0 {
			delta = (e.value / sum) * (p.endAngle - p.startAngle)
		}
		k += delta
		arcs = append(arcs, ArcData[T]{
			Data:       &data[e.index],
			Index:      e.index,
			Value:      e.value,
			StartAngle: startAngle,
			EndAngle:   k,
			PadAngle:   p.padAngle,
		})
	}
	return arcs
}
