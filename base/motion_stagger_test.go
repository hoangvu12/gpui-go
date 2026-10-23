package base

import "testing"

// The stagger test of motion.rs's motion_track_tests (1).

// U:motion.rs:843-855
func TestStaggerOriginsProduceStableDelaysWithoutAllocatingASchedule(t *testing.T) {
	interval := ms(20)
	first := NewStagger(interval, StaggerOriginFirst())
	last := NewStagger(interval, StaggerOriginLast())
	center := NewStagger(interval, StaggerOriginCenter())
	for _, c := range []struct {
		got, want any
	}{
		{first.Delay(3, 5), ms(60)},
		{last.Delay(3, 5), ms(20)},
		{center.Delay(2, 5), ms(0)},
		{center.Delay(0, 5), ms(40)},
		{first.Delay(7, 0), ms(0)},
	} {
		if c.got != c.want {
			t.Fatalf("delay = %v, want %v", c.got, c.want)
		}
	}
	// Go-own: an explicit origin index is clamped to the last item.
	if got := NewStagger(interval, StaggerOriginIndex(9)).Delay(0, 5); got != ms(80) {
		t.Fatalf("index origin delay = %v", got)
	}
}
