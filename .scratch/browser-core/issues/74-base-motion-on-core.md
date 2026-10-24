# 74 - Base motion on the core engine

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 27, 44
Triage: ready-for-agent

**What to build:** Base's motion presets (presence, stagger, sequence, reveal, springs) run on the core timing, easing and keyframe engine, and Base reads reduced motion from SystemPreferences.

**Blocked by:** 27 (SystemPreferences, system colours and live reduced motion), 44 (Transition style property and the core motion engine)

**Status:** ready-for-agent

- [x] Base motion tests pass; Base's own timing, easing, keyframe and reduced-motion code is removed.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

**Removed (Base's own motion code)**

- `base/reduce_motion.go`, `reduce_motion_windows.go`, `reduce_motion_other.go` (the one-shot SPI_GETCLIENTAREAANIMATION read, the flag-application dance and the no-op live follow), plus `Init`'s `reduceMotionInit` call.
- The private helpers block at the end of `base/motion.go` (`reduceMotionGate`, `finite32`, `in01`, `secsF32`, `mulF32`, `satAddDuration`, `satSubDuration` — "copies of engine internals" per their own old comment). The spring keeps two one-liners of its own (`springFinite32`, `springSecsF32` — Rust `f32::is_finite` / `Duration::as_secs_f32` equivalents its validation and integrator need); sequence and spring compute elapsed with plain subtraction (the clock is monotonic, so the saturating guard was dead); `toast.go`'s remaining-seconds clamp is `max(remaining-delta, 0)`.
- `presenceState` and presence's re-implementation of the engine's retarget/reversal/frame machinery: `Presence.Sample` is now a phase mapping over one core call.

**Presets on the core engine**

- New core seam `gpui.TransitionFromWithStatus(id, from, target, policy, w, cx)` (gpui/motion.go): `TransitionWithStatus` with a caller-owned seed — the mount-presence seeding (U:motion/presence.rs:128-135). Presence (base/motion_presence.go) maps `MotionValue` to Entering/Present/Exiting/Absent; the engine does the timing, easing, retarget, CSS reversal, frame requests and reduced-motion gate. One contract detail, pinned by a core test and load-bearing for `nav_stack`: a seeded state whose from equals its target still runs out its policy duration (the mount clock an outgoing presence owes even from nothing) instead of reporting the adopt-form's Idle — `TestNavStackTheOutgoingViewIsDroppedOnceItsExitHasRun` caught this.
- Sequence (base/motion_sequence.go) keeps its chaining state machine (component orchestration per the research split) and calls the core's `Transition.Progress/Sample/FinishesAfter` and `InterpolateValue` (aliases), gating on the core gate. Stagger and reveal never had engine code (index math and a layout element) — unchanged.

**Springs: stay in Base.** The research split's component half: 02-motion-and-compositing.md §5 ("Keyframes and springs can stay in components"), the README table row "Presence, stagger, sequence, reveal, springs as tokens → Components", README:332 (interpolation core, presets components) and spec story 109. Only the spring's gate was retargeted to the core source.

**Reduced motion: one source.** `App.MotionReduced()` (gpui/motion.go) = `App.ReduceMotion() || SystemPreferences().ReducedMotion` — the old private `reduceMotionActive` made public as the single gate; the keyed engine, keyframe playback, the style-diff transition engine and Base's presence/sequence/spring all read it (no per-frame host calls: the host caches per preference version, tickets 27/29). Test seam: `TestApp.WithSystemPreferences` pins the record a hostless app reports (an `App.testSystemPreferences` field consulted by `App.SystemPreferences`).

**API notes**

- Removed (public): `base.ApplySystemReduceMotion` — the one-shot read this ticket retires; nothing outside the deleted files and `app_test.go`'s reader tests called it. `base.Init` no longer touches reduced motion.
- Behaviour note: the OS preference and the App flag now OR (the core's policy since 44) instead of the system writing into the flag at Init with the application winning — so `SetReduceMotion(false)` no longer un-reduces a system-reduced machine, and the state is live (WM_SETTINGCHANGE) instead of read once.
- New core API: `gpui.TransitionFromWithStatus`, `(*App).MotionReduced`, `(*TestApp).WithSystemPreferences` (test-only surface, next to `WithHTTPClient`). `gpui.TransitionWithStatus`/`MotionTransition` are byte-identical (the seed and the `adopting` Idle rule are the only body changes).
- Base's alias surface (base/motion.go) is unchanged, so Base stays source-compatible; the gallery's reduced-motion note now reads `App.MotionReduced`.

**Files**

- Core: `gpui/motion.go` (seam, gate, file comment), `gpui/transition.go` (gate call), `gpui/app.go` + `gpui/system_prefs.go` + `gpui/testapp.go` (the preference pin), `gpui/motion_test.go` (new).
- Base: `base/motion.go`, `base/motion_presence.go`, `base/motion_sequence.go`, `base/toast.go`, `base/lib.go`, `base/app_test.go` (the six reduce_motion.rs reader tests deleted with the reader), `base/motion_reduced_test.go` (new); deleted `base/reduce_motion{,_windows,_other}.go`; `examples/base-gallery/pages_foundations.go` (one note line).

**Tests**

- New `gpui/motion_test.go`: the seeded first sample (enter from 0, reversal shortening, finish), the seeded from==target mount clock, the adopt form still Idle, `MotionReduced` (flag OR preference), the seeded form completing at once under the pinned preference.
- New `base/motion_reduced_test.go` (the acceptance test): pinned `SystemPreferences{ReducedMotion: true}` completes the transition, presence, sequence and spring presets on their first sample with 0 pending frames, and a preference turned on mid-flight resolves a moving transition at once.
- Base's motion tests pass unchanged (motion_test/presence/sequence/stagger/reveal/easing/timing/keyframes); the only deletions are the six reader tests of the removed one-shot read.
