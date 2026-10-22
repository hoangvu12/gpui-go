package base

// Timer-based auto-scrolling during drag interactions (U:auto_scroll.rs).
//
// Go adaptations: the shared Arc<Mutex<Option<Pixels>>> is a mutex-guarded
// cell the task reads each tick; the pinned Task is cancelled by drop, so
// the port cancels it explicitly where Rust drops it (set(None), stop and
// a restart). cx.spawn (an entity-bound foreground task, U:auto_scroll.rs:80)
// is App.Spawn over a weak handle of the context's entity, and the 16ms
// background timer is a background task sleeping on the scheduler clock
// (BackgroundExecutor::timer, R:executor.rs:193-202), so TestApp's
// virtual clock drives it.

import (
	"sync"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// autoScrollTick is the per-frame tick interval (U:auto_scroll.rs:94-96,
// Duration::from_millis(16)).
const autoScrollTick = 16 * time.Millisecond

// autoScrollShared is the cell shared between the owner and the task
// (U:auto_scroll.rs:11-13): writing None is the stop signal; the task
// exits on its next tick.
type autoScrollShared struct {
	mu    sync.Mutex
	delta gpui.Optional[float32]
}

func (s *autoScrollShared) get() gpui.Optional[float32] {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.delta
}

func (s *autoScrollShared) set(delta gpui.Optional[float32]) {
	s.mu.Lock()
	s.delta = delta
	s.mu.Unlock()
}

// AutoScroll manages timer-based auto-scrolling during drag interactions
// (U:auto_scroll.rs:6-17). Delta convention: positive moves toward the
// bottom and negative moves toward the top.
//
// The zero value is the pinned Default (U:auto_scroll.rs:19-27). Use an
// AutoScroll through one pointer: like the Rust value (which is not
// Clone), a copy would share the running task.
type AutoScroll struct {
	shared  *autoScrollShared
	task    gpui.Task[struct{}]
	hasTask bool
	// LastDragPosition is the last drag position, available to the
	// interaction that owns selection (U:auto_scroll.rs:15-16).
	LastDragPosition gpui.Optional[gpui.Point]
}

// DefaultAutoScroll returns an idle auto-scroll (U:auto_scroll.rs:19-27).
func DefaultAutoScroll() AutoScroll { return AutoScroll{} }

func (s *AutoScroll) cell() *autoScrollShared {
	if s.shared == nil {
		s.shared = &autoScrollShared{}
	}
	return s.shared
}

// Delta returns the current scroll delta (U:auto_scroll.rs:30-33).
func (s *AutoScroll) Delta() gpui.Optional[float32] {
	return s.cell().get()
}

// AutoScrollComputeDelta computes the scroll delta for a pointer y
// position within the viewport (U:auto_scroll.rs:35-59): a dead zone in
// the middle, and symmetric ramps from 12 to 64 px per tick that start 16
// px inside each edge and reach full speed 80 px past it.
func AutoScrollComputeDelta(y float32, bounds gpui.Bounds) gpui.Optional[float32] {
	const minSpeed = 12.0
	const maxSpeed = 64.0
	// The trigger starts this far inside the bounds so scrolling works
	// even full-screen, where the mouse cannot travel far outside.
	const innerZone = 16.0
	// The distance from the bounds edge to reach maxSpeed.
	const outerRamp = 80.0

	bottomTrigger := bounds.Bottom() - innerZone
	topTrigger := bounds.Top() + innerZone

	switch {
	case y > bottomTrigger:
		t := min((y-bottomTrigger)/(innerZone+outerRamp), 1)
		return gpui.Some(float32(minSpeed + t*(maxSpeed-minSpeed)))
	case y < topTrigger:
		t := min((topTrigger-y)/(innerZone+outerRamp), 1)
		return gpui.Some(-float32(minSpeed + t*(maxSpeed-minSpeed)))
	}
	return gpui.Optional[float32]{}
}

// Set updates the scroll delta and starts the tick task when it was idle
// (U:auto_scroll.rs:61-82). tick runs each frame (~60 fps) with the
// current delta and performs the actual scroll for the entity; an absent
// delta stops the task.
func (s *AutoScroll) Set[T any](delta gpui.Optional[float32], cx *gpui.Context[T], tick func(delta float32, state *T, cx *gpui.Context[T])) {
	wasIdle := !s.hasTask
	shared := s.cell()
	shared.set(delta)

	if !delta.IsSome() {
		s.dropTask()
		return
	}

	if wasIdle {
		s.task = autoScrollTaskLoop(cx.App(), cx.Entity().Downgrade(), shared, tick)
		s.hasTask = true
	}
}

// autoScrollTaskLoop is U:auto_scroll.rs:84-111: every 16ms read the
// shared delta (absent: exit) and tick the entity (released: exit).
func autoScrollTaskLoop[T any](app *gpui.App, this gpui.WeakEntity[T], shared *autoScrollShared, tick func(float32, *T, *gpui.Context[T])) gpui.Task[struct{}] {
	return app.Spawn(func(cx *gpui.AsyncApp) struct{} {
		for {
			cx.Await(cx.App().Background().Spawn(func(run *gpui.TaskRun) struct{} {
				run.Sleep(autoScrollTick)
				return struct{}{}
			}))
			delta, ok := shared.get().Get()
			if !ok {
				break
			}
			alive := false
			cx.Update(func(app *gpui.App) {
				alive = this.TryUpdate(app, func(state *T, ecx *gpui.Context[T]) {
					tick(delta, state, ecx)
				}) == nil
			})
			if !alive {
				break
			}
		}
		return struct{}{}
	})
}

// dropTask is the pinned `self.task = None` (dropping a Task cancels it).
func (s *AutoScroll) dropTask() {
	if s.hasTask {
		s.task.Cancel()
		s.task = gpui.Task[struct{}]{}
		s.hasTask = false
	}
}

// IsActive reports whether automatic scrolling is active
// (U:auto_scroll.rs:113-116).
func (s *AutoScroll) IsActive() bool {
	return s.Delta().IsSome()
}

// Stop stops automatic scrolling and clears the last drag position
// (U:auto_scroll.rs:118-123).
func (s *AutoScroll) Stop() {
	s.cell().set(gpui.Optional[float32]{})
	s.dropTask()
	s.LastDragPosition = gpui.Optional[gpui.Point]{}
}
