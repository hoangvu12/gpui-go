package base

// The stack generator (U:plot/shape/stack.rs, after d3-shape's stack).
//
// Go adaptations: the boxed `Fn(&T, &str) -> Option<f32>` accessor is a
// func returning (value, ok); `T: Clone` data is copied by value.

// StackPoint is a stacked series data point with its lower and upper
// values (U:plot/shape/stack.rs:3-13).
type StackPoint[T any] struct {
	// Y0 is the lower value (baseline).
	Y0 float32
	// Y1 is the upper value (topline).
	Y1 float32
	// Data is the original datum.
	Data T
}

// StackSeries is one stacked series (U:plot/shape/stack.rs:15-25).
type StackSeries[T any] struct {
	// Key is the key of this series.
	Key string
	// Index is the index of this series.
	Index int
	// Points are the points of this series.
	Points []StackPoint[T]
}

// Stack stacks the values of several keyed series
// (U:plot/shape/stack.rs:27-32).
type Stack[T any] struct {
	data  []T
	keys  []string
	value func(datum T, key string) (float32, bool)
}

// NewStack returns an empty stack whose accessor yields no values
// (U:plot/shape/stack.rs:34-47).
func NewStack[T any]() Stack[T] {
	return Stack[T]{value: func(T, string) (float32, bool) { return 0, false }}
}

// Data sets the data to be stacked (U:plot/shape/stack.rs:49-56).
func (s Stack[T]) Data(data []T) Stack[T] {
	s.data = append([]T(nil), data...)
	return s
}

// Keys sets the keys (series) for stacking (U:plot/shape/stack.rs:58-66).
func (s Stack[T]) Keys(keys ...string) Stack[T] {
	s.keys = append([]string(nil), keys...)
	return s
}

// Value sets the value accessor (U:plot/shape/stack.rs:68-75).
func (s Stack[T]) Value(value func(datum T, key string) (float32, bool)) Stack[T] {
	s.value = value
	return s
}

// Series computes the stacked series in the natural key order
// (U:plot/shape/stack.rs:77-135).
func (s Stack[T]) Series() []StackSeries[T] {
	if len(s.data) == 0 || len(s.keys) == 0 {
		return nil
	}
	n, m := len(s.data), len(s.keys)

	// Extract values into a matrix: series x data points.
	matrix := make([][]float32, m)
	for i, key := range s.keys {
		row := make([]float32, n)
		for j, datum := range s.data {
			if v, ok := s.value(datum, key); ok {
				row[j] = v
			}
		}
		matrix[i] = row
	}

	type span struct{ y0, y1 float32 }
	stacks := make([][]span, m)
	for i := range stacks {
		stacks[i] = make([]span, n)
	}
	for j := 0; j < n; j++ {
		var y0 float32
		for i := 0; i < m; i++ {
			y1 := y0 + matrix[i][j]
			stacks[i][j] = span{y0, y1}
			y0 = y1
		}
	}

	result := make([]StackSeries[T], 0, m)
	for i, key := range s.keys {
		points := make([]StackPoint[T], n)
		for j, datum := range s.data {
			points[j] = StackPoint[T]{Y0: stacks[i][j].y0, Y1: stacks[i][j].y1, Data: datum}
		}
		result = append(result, StackSeries[T]{Key: key, Index: i, Points: points})
	}
	return result
}
