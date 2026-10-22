# Port the motion system

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-22)
Blocked by: 03, 04, 08, 13

## Question

Transitions, keyframes, springs, presence, reveal, sequence and stagger animate in real windows and in deterministic tests, honoring reduced motion.

## Scope

- Base files: animation.rs, motion.rs (43 tests), motion/{easing,keyframes,presence,reveal,sequence,stagger,timing}.rs.

## Acceptance criteria

- [ ] The windowed motion tests run under the virtual clock (including `requested_animation_frames_resample_without_manual_refresh`, motion.rs:1586).
- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-01-infra-overlays.md](../research/slice-01-infra-overlays.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-22 — Claimed by the base-port-2 orchestrator and delegated (files: base/animation.go, base/motion*.go in the root base package; a base/motion subpackage would cycle through GlobalState/reduce motion).

## Answer

Resolved 2026-10-22. The motion system lives in the root `base` package (animation.go, motion*.go): easings, timing, keyframes, stagger, Transition/`MotionTransition`, Spring/`MotionSpring`, MotionValue, Presence, Sequence, MotionReveal, honoring reduced motion. All 48 upstream tests ported (3 rely on the duration-scaling deviation), windowed ones on the virtual clock including `requested_animation_frames_resample_without_manual_refresh`. Deviations: f64 duration scaling instead of Rust's f32 `mul_f32` (not cross-checked against Rust), Hsla lerp without the palette degrees round trip, collision renames. The core gained `AnyElement.PrepaintAt` (the shipped `PrepainAt` kept as a deprecated alias). [Evidence](../../../evidence/base/ticket14-motion.json).
