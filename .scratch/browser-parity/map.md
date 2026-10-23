# Browser parity: a browser-like core, components in their own repo

Labels: wayfinder:map

## Destination

On 2026-10-23, after the zterm test, the user said: "should we just try to make behaviour to look like the browser, but not actually shipping components? like it feels weird to ship the whole UI library into an UI framework". They confirmed: Base moves to a new repository, and deep research comes first, including reading browser source ("go ahead").

The destination for this map is a research-backed proposal. It covers which browser behaviours gpui-go's core should adopt (spec plus Chromium as the reference), where they differ from GPUI, and what belongs in the core versus in components. Nothing is implemented until the user accepts the proposal.

## Notes

- Sources:
  - The W3C/WHATWG specs.
  - Chromium, at the commit recorded in each report, a sparse checkout in `%TEMP%\research-src\chromium`: blink renderer and public, cc, components/viz, ui/events, ui/base/ime, ui/gfx, ui/native_theme.
  - Skia, in `%TEMP%\research-src\skia`.
  - Firefox where useful, via its GitHub mirror.
  - All of these are read only, never built.
- Earlier, related research: [research/app-gaps/](../../research/app-gaps/README.md) (zgui and gpui-fast) and [research/zgui-terminal/parity-report.md](../../research/zgui-terminal/parity-report.md).
- Workers: at most 3 at a time, so two batches (01-03, then 04-06), then 07.

## Decisions so far

- [TSX for UI scripting](../../docs/adr/0003-tsx-ui-scripting.md) (D15, the user, 2026-10-23): TSX via esbuild + goja, on zgui's model. It replaced Luau ([ADR 0002](../../docs/adr/0002-luau-ui-scripting.md)) after [the probe](../../research/browser-parity/09-tsx-goja-probe.md) met every threshold; yaegi was ruled out.

- [Synthesis](issues/07-synthesis.md): the proposal is research/browser-parity/README.md. The core gets browser behaviour without the look; Base becomes the look layer in its own repo. Decisions D0-D16 await the user, then /to-spec.

## Not yet specified

- The Base repo split: the name, public or private, and whether zterm and the gallery move with it. These are for the user.

## Out of scope

- Implementing anything before the user accepts 07.
