# Zero-blur inset shadows hide under the element border (CSS casts them from the padding edge)

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi worker
Blocked by: none

## Question

mordbard's frames use the CSS `inset 0 1px 0 rgba(255,255,255,0.03)`
inner top highlight on bordered, rounded panels. Through `Styled.Shadow`
a `BoxShadow{Inset: true, BlurRadius: 0, OffsetY: 1}` paints nothing
on those panels; changing only the blur to 3 makes a gradient appear.
(mordbard's draft ticket suspected the rasterizer drops the blur-0
case — it does not: a direct `Scene.PaintShadow` inset record with
zero blur rasterizes correctly, verified on the real GPU.)

The real cause is geometry: gpui-go (like the pinned reference, whose
`paint_inset_shadows` it ports) casts the inner shadow from the
element's **border box** — `hole = (bounds + offset).dilate(-spread)`
with `bounds` the border box — and paints the border quad **after**
the inset shadow. A zero-blur `0 1px` inset band is therefore exactly
the 1 px row under the 1 px border and is fully covered by it (the
blur-3 gradient stays visible only because it spreads past the border
into rows the border does not cover). CSS casts inner shadows from
the **padding edge** ("as if everything outside the padding edge were
opaque"), so the same `inset 0 1px 0` draws a hard 1 px band just
*inside* the border — where mordbard measured it in the reference
frames (#202124 over the panel background) and where its current
workaround overlay paints it.

## What to build

Cast inset shadows from the padding edge, matching CSS: the inset
shadow's element region and hole are derived from the element bounds
inset by its border widths (a no-op for borderless elements, which
keep today's geometry), with the region's corner radii reduced by the
border. Drop shadows keep the pinned border-box geometry. This is a
recorded deviation from the pin (whose inner shadows also sit under
the border) taken for CSS parity — the parity a 1:1 HTML port needs.

## Acceptance criteria

- [x] A bordered, rounded panel with
      `Shadow(BoxShadow{Inset: true, OffsetY: 1})` paints the hard
      1 px band in the row just inside its top border, on the real
      GPU (surface readback: the row differs from the plain
      background by the shadow color's blend, like the reference
      frames' #202124).
- [x] The same shadow with a non-zero blur still paints its gradient
      (now starting at the padding edge).
- [x] A borderless element's inset shadows paint exactly as today
      (the padding edge equals the border box).
- [x] Drop shadows and the scene-level `PaintShadow` API are
      unchanged.

## Answer

Resolved 2026-10-20 on `consumer-gaps-2` (commit "Cast inset shadows
from the padding edge like CSS").

**Root cause** (verified before filing; mordbard's draft suspected the
rasterizer, which is wrong — a blur-0 inset record rasterizes
correctly on the real GPU): the inset shadow's region and hole were
cast from the element's **border box** (the pin's
`paint_inset_shadows` geometry) and the border quad paints **after**
the inset shadow, so a hard band at most as thick as the border is
completely covered by it. `inset 0 1px 0` on a 1px-bordered panel is
exactly that case — the band IS the row under the border. CSS casts
inner shadows from the **padding edge** ("as if everything outside the
padding edge were opaque"), which puts the band just inside the border
where mordbard measured it (#202124) in the reference frames. The
blur-3 variant only appeared because its gradient spreads past the
border into rows the border does not cover.

**Fix** (recorded deviation from the pin, taken for CSS parity — the
parity a 1:1 HTML port needs): `Style.paintBox` derives the inset
ring's and inset shadows' element region and hole from the element
bounds **inset by the border widths**, with the region's corner radii
reduced by the border (the corner's larger adjacent width, clamped at
zero). A borderless element's padding box IS its border box, so its
records keep the pinned geometry exactly; drop shadows and the
window-level `PaintShadow` / `PaintInsetShadows` seams (the pin's
direct seams) are untouched — the port's test corpus that pins those
seams still passes unchanged.

**Verification**:
- `gpui TestInsetShadow*` (4 tests, deterministic scene records): a
  1px-bordered panel's `inset 0 1px 0` record carries the padding-box
  region (2,2,236x116 device at scale 2) and hole (2,4,236x116); the
  radii shrink by the border (12-2, scaled); a borderless element's
  record is byte-identical to today; the drop-shadow record keeps the
  border box and the element radii.
- `internal/scenespec TestInsetShadow*OnRealGPU` (3 tests, real GPU
  readback through RenderScene + ReadPixels with the element-path
  frame): the hard band paints in the row just inside the 1px border
  (row average 255 vs background 38 vs border 64), the blur-3 gradient
  paints from the padding edge, and the borderless band still paints
  at the element edge. Windows show without activating; no real input.
- All gates were re-run red on the pin geometry (the inset calls
  temporarily reverted to the border-box bounds/radii) and green after.
- `TestDivPaintsTheBoxInPinnedOrder`'s inset expectations were updated
  to the padding-edge geometry (the paint ORDER itself is unchanged);
  every other existing suite passes — `go build ./...`, `go vet ./...`,
  `go test ./... -count=1` green except `internal/headlessspec` (the
  documented environmental gate: this worktree has no
  `reference/ce-source` checkout).

**What mordbard should change**: drop the painted inset highlight
overlay (`ui.insetPanelHighlight`'s absolutely-positioned 1px div in
app/ui/widgets.go) and put the frames' `inset 0 1px 0
rgba(255,255,255,0.03)` back on `insetPanel` through
`Shadow([]gpui.BoxShadow{...})` — the band now paints just inside the
top border, clear of the 12px corner arcs (the radii shrink by the
border so the band follows the padding edge's corners).

## Comments

- 2026-10-20 (pi worker): claimed, implemented, verified, resolved.
  The rasterizer drop suspicion in mordbard's draft was disproven
  before filing (a direct blur-0 inset record rasterizes correctly);
  the ticket was filed with the verified geometry cause.
