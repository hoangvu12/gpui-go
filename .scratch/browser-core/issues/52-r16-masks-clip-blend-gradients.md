# 52 - R16: masks, clip-path, blend modes, drop-shadow and gradients

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 11 (Claude Code); R16 integration branch native/r16
Blocked by: 39, 49
Triage: ready-for-agent

**What to build:** On the R16 integration branch: mask composite (edge fades, gradient and image masks, alpha and luminance), clip-path coverage, blend modes including plus-lighter, the drop-shadow filter, and n-stop, radial, conic and repeating gradients.

**Blocked by:** 39 (R15 native rebuild; R16 records extend it), 49 (R16 compositor design)

**Status:** ready-for-agent

**Design ([docs/design/r16-compositor.md](../../../docs/design/r16-compositor.md) §3.5-3.9, §7, §8.1; approved 2026-10-24):**
- Masks composite `dst_in` from a gradient, atlas-image or A8 coverage source, in alpha or luminance mode.
- Clip-path is rendered as a coverage mask built from the tessellated path.
- Blend groups always take the offscreen route and blend against a backdrop copy in the shader. Plus-lighter is premultiplied clamp(s+d). Blend groups have **no size cap** for now; they are measured at 54, and any later cap must not change pixels.
- Gradients use a scene-level gradient table plus premultiplied sRGB ramp rows. Allow **up to 64 stops**; above that, Go resamples to 64 and warns in debug builds. R15's 2-stop path is untouched.
- Drop-shadow is a filter-op chain appended to the R15 filter record. The new fields are append-only, and 0/0 means R15 semantics.

- [ ] Native spec tests for each primitive.
- [ ] R15 filter and gradient records keep their exact outputs; the 64-stop limit and the resampling above it are tested.
- [ ] Integration branch only; green at ticket 54.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
