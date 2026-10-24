package base

// Mount presence: an element that enters, stays, exits and only unmounts
// after its exit finished (U:motion/presence.rs).
//
// Ticket 74: the phases are Base's, but the transition itself runs on
// the core's keyed engine — Sample is a thin mapping over
// gpui.TransitionFromWithStatus, which seeds the keyed state from 0 the
// way presence's own state machine used to (U:motion/presence.rs:128-135)
// and applies the engine's retarget, reversal and reduced-motion rules.

import (
	"fmt"

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

// Sample samples the presence and requests a frame while it moves
// (U:motion/presence.rs:48-125). A first sample of a present element
// enters from 0 (the engine seam seeds its keyed state with from = 0);
// an element that was never present still runs its exit duration out —
// that clock is what a transit driven by an outgoing presence measures
// (nav_stack). Reduced motion and a zero duration resolve at once, both
// through the engine's single reduced-motion source (App.MotionReduced).
func (p Presence) Sample(w *gpui.Window, cx *gpui.App) PresenceSample {
	var target float32
	if p.present {
		target = 1
	}
	value := gpui.TransitionFromWithStatus(gpui.TransitionIDWithChannel(p.id, "__presence"),
		float32(0), target, p.transition, w, cx)
	if value.Status == MotionStatusFinished {
		return stablePresenceSample(p.present)
	}
	phase := PresencePhaseExiting
	if p.present {
		phase = PresencePhaseEntering
	}
	return PresenceSample{Phase: phase, Progress: value.Value, Status: value.Status}
}

// stablePresenceSample is U:motion/presence.rs:137-147.
func stablePresenceSample(present bool) PresenceSample {
	if present {
		return PresenceSample{Phase: PresencePhasePresent, Progress: 1, Status: MotionStatusFinished}
	}
	return PresenceSample{Phase: PresencePhaseAbsent, Progress: 0, Status: MotionStatusFinished}
}
