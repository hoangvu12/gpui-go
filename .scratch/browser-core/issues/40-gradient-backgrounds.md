# 40 - Gradient backgrounds and Tailwind classes

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 6 (Claude Code); worker chat d8f26dc5
Blocked by: 39
Triage: ready-for-agent

**What to build:** Elements can have linear gradient backgrounds, including the Tailwind bg-linear/from/via/to classes, interpolated like CSS.

**Blocked by:** 39 (R15 native rebuild)

**Status:** ready-for-agent

- [x] Fixtures for angles, transparent stops and diagonal boxes match Chrome within tolerance (the diagonal normalization risk is tested first).
- [x] The class mapper no longer rejects these classes.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 6: verified after merging onto main (rev 15). I resolved the fixture-registry import conflicts by keeping both sides. `go test ./gpui/... ./base ./internal/parity/... ./internal/scenespec ./internal/native` is ok, and the 3 gradient fixtures pass. The magic-corner citation (css_gradient_value.cc:1425-1431) matches. Recorded divergence: Tailwind v4 defaults gradients to oklab, but gpui-go draws sRGB until R16, so `bg-linear-to-r` midtones differ slightly from a real Tailwind page. `via-*` and non-sRGB modes are rejected with named errors pointing at 52.

## Answer

Done on `core/40-gradients` (this branch). The rev-15 scene record's
gradient arm (`GpuiGoSceneBackground`, native ticket 39) is now reachable
from the Go styling side: a public two-stop gradient `Background`, the
Tailwind v4 `bg-linear-*`/`from-*`/`to-*` classes, and three
browser-parity fixtures recorded in headless Chrome (155.0.8059.85) whose
replay matches within the measured tolerances.

### 1. The public API (gpui/gradient.go, gpui/paint.go)

`Background` grew the gradient arm (`Kind` + `Gradient LinearGradient`;
the zero value stays a transparent solid, so every existing
`Background{Solid: c}` site is unchanged):

- `gpui.LinearGradientBackground(angle, from, to GradientStop)` and
  `gpui.LinearGradientToBackground(direction, …)` — `GradientStop{Color
  Hsla, Position float32}` (0 = the gradient line's start, 1 = its end);
  `GradientDirection` is an angle or one of CSS's eight keywords.
- `Background.Opacity` scales each stop's alpha and `IsTransparent`
  reports both stops at a == 0 — both the pinned `Background::opacity` /
  `::is_transparent` semantics over the gradient arm
  (crates/gpui/src/color.rs:505-523 @ 254b5dbd).
- Styling flows unchanged: `.Bg(gradient)` (the existing `IntoBackground`
  constraint already accepts a Background), through `StyleRefinement`,
  div paint, `PaintQuad` into the ABI record
  (gpui/scene_kernel.go:sceneBackgroundRecord).

**CSS semantics, verified against Chromium 0d4dcd4f** (the sparse
checkout at %TEMP%\research-src\chromium):

- **Geometry** — the angle is CSS degrees (0 up, clockwise; the shader's
  `linear_gradient_ratio` implements the CSS line length
  |W·sinθ|+|H·cosθ|, `EndPointsFromAngle`, core/css/css_gradient_value.cc
  :1284-1350). The corner keywords follow the magic-corner rule
  (css-images-3 §linear-gradient; Chromium `CreateGradient`,
  css_gradient_value.cc:1425-1431: bearing = 90 − atan2(W, H) for `to top
  right`, mirrored per quadrant), resolved at paint time against the
  painted device bounds (gpui/gradient.go:resolveGradientAngle) because
  the angle depends on the box's aspect ratio.
- **Interpolation** — premultiplied, in the sRGB (encoded) color space,
  CSS's legacy default for `<color>` stops (css-images-4
  §coloring-gradient-line; Chromium maps legacy colors to Skia `kSRGB`
  and premultiplies, platform/graphics/gradient.cc:247 and :287-291).
  Both are the rev-15 shader's recorded divergences
  (reference/native/shaders/mod.rs §2); nothing native changed here.
- **Paths** — `PathRecord.Color` widened from `Hsla` to `Background`
  (the pinned `Path.color` IS a Background, scene.rs:1152; the pinned
  path shader already evaluates it through `paint_color`,
  gpui_render/src/shaders/paths.rs:63), so gradient path fills ride the
  same record for free.

### 2. The Tailwind classes (gpui/class.go:gradientClass)

The `bg-linear`/`from`/`via`/`to` prefixes left the
`unsupportedClasses` table; a new class stage applies them (Tailwind v4
source, packages/tailwindcss/src/utilities.ts @ main, read directly):

- `bg-linear-to-{t,tr,r,br,b,bl,l,tl}` — the eight directions
  (`linearGradientDirections`); axis forms are fixed angles, corner forms
  the keyword (magic-corner at paint time).
- `bg-linear-<n>` / `-bg-linear-<n>` — the angle form (`isPositiveInteger`
  → `${n}deg`); `bg-linear-[45deg]` (also rad/grad/turn) — the arbitrary
  angle form.
- `from-*` / `to-*` — a stop's color (palette, `transparent`, `[#hex]`,
  the `/50` opacity modifier) or its percentage position (`from-10%`,
  `from-[12.5%]`). Tailwind's defaults: both stops `#0000` at 0%/100%
  (`gradientStopProperties`), so `bg-linear-to-r` alone paints nothing,
  exactly like v4.
- Composition is chain order: `bg-linear-*` starts the gradient (keeping
  stops an earlier `bg-linear-*` set — Tailwind's direction and stops are
  independent CSS variables), `from-*/to-*` set one stop of the gradient
  the same string (or the same state variant) started; under a variant
  the whole gradient must live in that variant (`hover:bg-linear-to-r
  hover:from-red-500 hover:to-blue-500`).

**The via / oklab decision (evidence, not silent):**

- **`via-*` and `via-none` are rejected** with
  `gpui.ErrGradientViaUnsupported` ("needs a three-stop gradient: the
  scene record (rev 15) carries exactly two stops (ticket 52…)"),
  wrappable through `errors.Is` from `CheckClasses` (classError grew an
  `Unwrap`). The record is structurally two-stop, so >2 stops cannot be
  faked; the public constructors likewise cannot express them.
- **Non-sRGB interpolation is rejected** with
  `gpui.ErrGradientInterpolationUnsupported`: Tailwind v4's default
  interpolation is oklab (`resolveInterpolationModifier` returns
  `in oklab`, and the named/numeric forms emit it through an `@supports`
  rule), but the ABI's Oklab arm is the PINNED path — a 2.2-curve
  transfer and a straight (non-premultiplied) mix
  (shaders/common.rs:496-511, 695-701) — which is NOT CSS's
  `linear-gradient(… in oklab …)` (proper sRGB↔Oklab matrices,
  premultiplied). Drawing it would silently differ from both Tailwind
  and Chrome, so the class layer draws CSS's legacy sRGB and rejects
  every non-`srgb` modifier (`/oklab`, `/oklch`, `/hsl`, `/longer`, …).
  **The recorded divergence:** `bg-linear-[45deg]` (the arbitrary angle
  form) matches a real Tailwind page bit for bit — v4's arbitrary path
  emits no interpolation method, so CSS's legacy sRGB applies (verified
  by the gradient-classes fixture) — while `bg-linear-to-r` and
  `bg-linear-45` render the equivalent CSS *without* the `in oklab`
  Tailwind emits (mid-gradient colors between far-apart stops differ
  slightly from a real Tailwind page). The `/srgb` modifier opts into
  exactly the one space drawn. Matching Tailwind's oklab exactly is left
  to ticket 52's R16 gradient redesign (recorded as an open question
  there, not approximated here).
- Equal stop positions (a CSS hard stop) and descending pairs (CSS fixup
  clamps them into a hard stop) are rejected with reasons pointing at
  ticket 52 — the ABI's record requires distinct ascending positions.
- `bg-gradient-*` (the v3 name, removed in v4 — verified against
  utilities.ts at v4.0.0 and main) is rejected with a rename hint;
  `bg-radial-*`/`bg-conic-*` point at ticket 52.

docs/tailwind.md documents all of it (the supported surface, the oklab
divergence, the composition rules, the rejections).

### 3. The fixtures (internal/parity/fixtures/gradient, testdata/gradient-*)

Three registered fixtures, recorded in headless Chrome
(`go run ./cmd/parityrecord -fixture gradient-…`) and replayed by
TestFixtures; all text-free, so Tier C carries no font noise:

- **gradient-angles** (the diagonal risk, tested first): six NON-SQUARE
  160×100 boxes at 0, 45, 90, 135, 180 and 37deg, red-500→blue-500.
  Chrome record vs replay: whole mean 0.2131 / hot 0.00000, per box up
  to 0.5750 (the two sides' gradient dither — the shader's ±2/255
  triangular noise vs Skia's own); gates 0.5 / 2.5 / 0.002.
- **gradient-transparent**: a color fading to `transparent` keeps its
  hue (premultiplied: no grey fringe), a transparent→color fade, and two
  stops with different alphas (blue 0.6 → red) whose premultiplied
  midpoints match Chrome to ±1 byte. Measured 0.0946 whole / 0.5360 per
  box; gates 0.25 / 1.5 / 0.002.
- **gradient-classes**: the class-built twin — `bg-linear-to-r`, the
  angle form, `bg-linear-[117deg]`, and `bg-linear-to-tr` (the magic
  corner on a non-square box) against pages of the equivalent CSS.
  Measured 0.1450 whole / 0.6014 per box; gates 0.35 / 2.5 / 0.002.

Discrimination tests (gradient_test.go) keep deliberately-wrong views
that must FAIL Tier C with a readable diff while Tier A still passes:

- `AnglesPinnedDiagonal` — the PIN's diagonal geometry (rev 14 and
  earlier), emulated exactly: the pinned `linear_gradient_ratio`
  (crates/gpui_render/src/shaders/common.rs:620-645 @ 254b5dbd) squashes
  the direction by the aspect ratio and normalizes by the dominant axis,
  expressible as one gradient (effective bearing + stop positions
  (L−D)/(2L)…(L+D)/(2L)); measures 1.6111 whole / 8.1150 in the 45deg
  ROI, with the axis boxes identical to the correct view (0.57), exactly
  as the pin drew them.
- `TransparentThroughBlack` — the fade going through a half-alpha black
  end (the naive non-premultiplied mix's visible signature; the naive
  curve itself is quadratic in the premultiplied channels and not
  expressible as a two-stop premultiplied gradient — noted in the test);
  measures 4.4479 whole / 35.3017 ROI / 0.4917 hot.
- `ClassesCornerAsDiagonal` — the corner keyword read as the square's
  45deg (ignoring the magic corner); measures 5.4079 in the
  `class-corner` ROI only.

Unit tests: gpui/gradient_test.go (opacity, is_transparent, equality,
scene round-trip through the native kernel, the corner resolution, the
named errors, Bg on a refinement) and the class tests
(gpui/class_test.go: TestClassGradients, TestClassGradientStateVariants,
TestClassGradientRejections).

### 4. Verification

- `go vet ./...` clean; `gofmt -l` clean on every touched file;
  `go build ./...` green.
- `go test ./internal/parity/... ./gpui/... ./base` — all pass
  (gpui 16s, parity 1s, base 2.4s). `go test ./internal/scenespec
  ./internal/native` also pass (the ABI mirror is unchanged and the
  rev-15 gates stay green).
- Pre-existing, ignored per the ticket: `internal/portfixture`
  TestFx0004 fails on main (font count 94 vs 95 — proven pre-existing at
  the base commit in ticket 39's evidence).
- No native code, DLL or shader changed; the scene ABI consumption is
  additive (the previously solid-only call sites now pack/read the
  gradient arm the record already carried).

### 5. Limitations and open questions

- **Oklab (Tailwind's default) is not drawn** — the record's Oklab arm
  is the pinned 2.2-curve non-premultiplied path; class-level gradients
  interpolate in CSS's legacy sRGB (recorded divergence, §2). Exact
  oklab is an open question for ticket 52's redesign.
- **Three-plus stops** (`via-*`), hard stops, descending stop pairs,
  length stop positions (`from-[10px]`) and radial/conic/repeating
  gradients all need R16 (ticket 52); each is rejected with a named
  error that says so.
- **One background per element**: a gradient replaces a solid background
  (no CSS `background-color` under `background-image` layer).
- The magic-corner angle resolves against the SNAPPED device bounds
  (Chrome uses the fractional layout box); for sub-pixel-sized boxes the
  angle can differ by a fraction of a degree. Integer-size layouts (all
  fixtures) are exact.
