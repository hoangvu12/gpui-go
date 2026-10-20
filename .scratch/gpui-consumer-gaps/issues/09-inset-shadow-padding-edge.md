# Zero-blur inset shadows hide under the element border (CSS casts them from the padding edge)

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
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

- [ ] A bordered, rounded panel with
      `Shadow(BoxShadow{Inset: true, OffsetY: 1})` paints the hard
      1 px band in the row just inside its top border, on the real
      GPU (surface readback: the row differs from the plain
      background by the shadow color's blend, like the reference
      frames' #202124).
- [ ] The same shadow with a non-zero blur still paints its gradient
      (now starting at the padding edge).
- [ ] A borderless element's inset shadows paint exactly as today
      (the padding edge equals the border box).
- [ ] Drop shadows and the scene-level `PaintShadow` API are
      unchanged.
