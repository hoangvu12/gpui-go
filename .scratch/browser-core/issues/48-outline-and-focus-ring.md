# 48 - Outline and the default focus ring

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: worker 48 (branch core/48-outline-focus-ring)
Blocked by: 12
Triage: ready-for-agent

**What to build:** outline (width, offset, colour, style) paints above the content, and focus-visible elements get Chrome's default auto focus ring.

**Blocked by:** 12 (Click on the common ancestor, activation and focus-visible)

**Status:** ready-for-agent

- [x] Fixtures for outline with offset and the auto focus ring match Chrome within tolerance.
- [x] Changed fixtures carry divergence notes.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

- **API:** `Style.Outline OutlineStyle` (width, kind, colour, offset; `gpui/style.go:1060`, the type at `:1492`) — CSS css-ui-4 §outline as the element's last decoration, painted ABOVE the content inside the filter group (`gpui/div.go` paintBox, after the border) with the outline extent feeding the filter group's ink bounds like the ring's width. Kinds: auto (the CSS initial, never paints by itself), none, solid, dashed, focus-ring (the materialized UA rule; `interact.go` computeStyleInternal turns auto into it on a focused `focusVisibleMatches` element — 12's origin rule — and into none everywhere else). Fluent: `.Outline(w)`, `.OutlineColor(c)`, `.OutlineOffset(o)`, `.OutlineDashed()`, `.OutlineNone()` (`gpui/styled.go`); refinement fields + merge (`gpui/styled_refinement.go`).
- **The ring look** (recorded Chrome 155, this machine): ONE 2px stroke of #101010 centred on the border box (1px inside, 1px outside), corner radii = element radii + outline-offset floored at 3, pulled 1px onto a ≥1px border; the outline colour or the default #101010 (the resolved `-webkit-focus-ring-color`; the in-tree default is 0xFFE59700, `renderer_preferences.h:45`). `outline-none` opts out per element; a click focus draws nothing; forced colours map the ring colour to Highlight (`forced_colors.go`, `forced_colors.css:18-19`).
- **Divergence toward the record (not ported):** Chromium main 158 paints the two-tone refresh — the outline colour over a scheme-dependent outer (light: #E59700 under white; dark: white over #101010; `outline_painter.cc` PaintFocusRing) — but the recorded stable Chrome 155 draws the single stroke, so the port matches the record; re-record `outline-focusring-*` when the recorder's Chrome updates past the refresh.
- **Files:** `gpui/style.go` (types + `paintQuads`/`frameQuad`/`focusRingQuads`/`inkOutset`), `gpui/div.go` (the paint + ink), `gpui/interact.go` (the UA rule), `gpui/forced_colors.go`, `gpui/styled.go` + `styled_refinement.go`, `gpui/class.go` + generated tables, `internal/tsxgen/classes.go` (dts arms), `docs/tailwind.md`, `docs/browser-parity-harness.md`.
- **Classes:** `outline` (1px solid), `outline-<n>`, `outline-[3px]`, `outline-offset-<n>`, `outline-offset-[3px]`, `outline-<color>`, `outline-none`, `outline-dashed` (registered in `class.go`; `class_generated.go` and `gpui-script.d.ts` regenerated — `go generate ./gpui/script/types`).
- **Fixtures:** `internal/parity/fixtures/outline` — `outline-offset` (positive/zero/negative offsets, 1/2/3/6px widths, colours, a ring over a rounded border), `outline-dashed`, `outline-focusring-tab` (the ring; record emulates the light scheme + a ComputedScript pinning the resolved ring colour), `outline-focusring-click` (no ring on mouse focus), `outline-focusring-none` (the opt-out); the wrong view (outline-none on the tab fixture) fails only Tier C by ~100× (`outline_test.go`). Measured correct views: 0.0032 mean (offset), 0.0003 (ring), 0.0704 (dashed — the pin's dash rhythm vs Skia's, a within-a-pixel divergence); tolerances at ≥2× those.
- **Existing fixtures:** none of the records changed. The pages that pin `outline: none` (keyorder, activation, tabnav, contextmenu, imecompose, keycode) gained the pin's port twin `.OutlineNone()` on their focusable views, so the default ring landing changed no recorded pixel — the divergence note is in `docs/browser-parity-harness.md` (the "No default focus ring" entry now records the fix + the note).
- **Key citations:** `html.css:1701-1703` (the UA `:focus-visible { outline: auto 1px -webkit-focus-ring-color }`); `paint_phase.h:46-47` ("outlines are painted over the foreground"); `outline_painter.cc:40-57` (stroke 3, outer third 2), `:60-73` (FocusRingOffset's border pull), `:75-79` (negative-offset clamp), `:759-781` (the 3px corner floor / radii follow the element), `:1073-1082` (OutlineOutsetExtent → `layout_box.cc:3485-3492` visual overflow → `paint_layer.cc:2199-2205` the filter reference box); `float_rounded_rect.cc` Radii::Outset ("zero radii are kept zero so that sharp corners are still sharp" — square outlines stay square); `web_view_impl.cc:3836` (the renderer-preference focus ring colour); `forced_colors.css:18-19`.
- **Limitations:** dotted/double/groove/ridge/inset/outset have no native spelling (the scene's border records are solid and dashed, `reference/native/src/scene.rs:345`); the dashed pattern is the pin's dash rhythm, not Skia's center-path stroke; the two-tone refresh is not ported (see the divergence above); `outline-width`/`outline-color` are not in the transition system's property slots (they switch instantly); negative Tailwind offsets have no class (`.OutlineOffset(-3)`); the text-input "always ring" arm and the a11y override of `:focus-visible` stay future work (12's note).
