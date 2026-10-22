package base

// Mount presence: an element that enters, stays, exits and only unmounts
// after its exit finished (U:motion/presence.rs).

import (
	"fmt"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// PresencePhase is where a presence is in its lifecycle
// (U:motion/presence.rs:7-13).
type PresencePhase uint8

const (
	PresencePhaseEntering PresencePhase = iota
	PresencePhasePresent
	PresencePhaseExiting
	PresencePhaseAbsent
)

// String is the Debug rendering.
func (p PresencePhase) String() string {
	switch p {
	case PresencePhaseEntering:
		return "Entering"
	case PresencePhasePresent:
		return "Present"
	case PresencePhaseExiting:
		return "Exiting"
	case PresencePhaseAbsent:
		return "Absent"
	}
	return fmt.Sprintf("PresencePhase(%d)", uint8(p))
}

// PresenceSample is a presence's phase and its 0 (absent) to 1 (present)
// progress (U:motion/presence.rs:15-20).
type PresenceSample struct {
	Phase    PresencePhase
	Progress float32
	Status   MotionStatus
}

// ShouldRender reports whether the element is still mounted: every phase
// but Absent (U:motion/presence.rs:22-26).
func (s PresenceSample) ShouldRender() bool { return s.Phase != PresencePhaseAbsent }

// Presence samples the mount state of one keyed element
// (U:motion/presence.rs:28-32).
type Presence struct {
	id         TransitionID
	present    bool
	transition Transition
}

// NewPresence is a presence that switches instantly until Transition sets
// its timing (U:motion/presence.rs:35-41).
func NewPresence[I IntoTransitionID](id I, present bool) Presence {
	return Presence{id: TransitionIDOf(id), present: present, transition: NewTransition(0)}
}

// Transition sets the enter and exit timing (U:motion/presence.rs:43-46).
func (p Presence) Transition(transition Transition) Presence {
	p.transition = transition
	return p
}

// presenceState is the keyed state (U:motion/presence.rs:128-135).
type presenceState struct {
	from, target    float32
	startedAt       time.Duration
	reversingFactor float32
	duration        time.Duration
}

// Sample samples the presence and requests a frame while it moves
// (U:motion/presence.rs:48-125). A first sample of a present element
// enters from 0; reduced motion and a zero duration resolve at once.
func (p Presence) Sample(w *gpui.Window, cx *gpui.App) PresenceSample {
	key := gpui.NamedChildElementID(p.id.id, "__presence")
	now := cx.Now()
	var target float32
	if p.present {
		target = 1
	}
	state := w.UseKeyedState[presenceState](key, cx, func(*gpui.Window, *gpui.Context[presenceState]) presenceState {
		return presenceState{from: 0, target: target, startedAt: now, reversingFactor: 1, duration: p.transition.duration}
	})
	snapshot := state.Read(cx, func(s *presenceState, _ *gpui.App) presenceState { return *s })

	if cx.ReduceMotion() || p.transition.duration == 0 {
		if snapshot.from != target || snapshot.target != target {
			state.Update(cx, func(s *presenceState, _ *gpui.Context[presenceState]) {
				s.from = target
				s.target = target
				s.startedAt = now
				s.reversingFactor = 1
				s.duration = p.transition.duration
			})
		}
		return stablePresenceSample(p.present)
	}

	elapsed := satSubDuration(now, snapshot.startedAt)
	progress, status := p.transition.progress(elapsed, snapshot.duration)
	sampled := lerp32(snapshot.from, snapshot.target, p.transition.sample(progress))

	if snapshot.target != target {
		reversing := target == snapshot.from
		reversingFactor := float32(1)
		if reversing {
			reversingFactor = clamp32(
				float32(p.transition.sample(progress)*snapshot.reversingFactor)+(1-snapshot.reversingFactor), 0, 1)
		}
		duration := mulF32(p.transition.duration, reversingFactor)
		state.Update(cx, func(s *presenceState, _ *gpui.Context[presenceState]) {
			s.from = sampled
			s.target = target
			s.startedAt = now
			s.reversingFactor = reversingFactor
			s.duration = duration
		})
		initial, initialStatus := p.transition.progress(0, duration)
		progress, status = lerp32(sampled, target, p.transition.sample(initial)), initialStatus
	} else {
		progress = sampled
	}

	if status.active() {
		gpui.RequestAnimationFrame(w)
	}

	if status == MotionStatusFinished {
		return stablePresenceSample(p.present)
	}
	phase := PresencePhaseExiting
	if p.present {
		phase = PresencePhaseEntering
	}
	return PresenceSample{Phase: phase, Progress: progress, Status: status}
}

// stablePresenceSample is U:motion/presence.rs:137-147.
func stablePresenceSample(present bool) PresenceSample {
	if present {
		return PresenceSample{Phase: PresencePhasePresent, Progress: 1, Status: MotionStatusFinished}
	}
	return PresenceSample{Phase: PresencePhaseAbsent, Progress: 0, Status: MotionStatusFinished}
}
