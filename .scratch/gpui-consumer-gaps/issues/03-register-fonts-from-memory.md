# Register fonts from memory (embedded font bytes)

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: none

## Question

The public `TextSystem` can only enumerate and resolve fonts installed
on the system (`FontNames`, `ResolveFont`); there is no API to register
font bytes. Apps that embed their typefaces in the binary (mordbard
embeds Geist and Geist Mono, SIL OFL, per its design spec) cannot use
them. Worse, `ResolveFont` for an absent family does not error: it
silently returns the same `FontID` as a nonsense family name
("Definitely Not A Font 12345" — verified), so an app asking for its
embedded font renders the wrong font with no diagnostic.

The reference registers font bytes (`TextSystem` font sources /
Fontique registration, `add_fonts`). The native Fontique-backed
catalog presumably has the seam; it is not surfaced.

## What to build

A public registration path, e.g. `TextSystem.AddFont(bytes []byte)`
(or `RegisterFontSource`), feeding the native catalog, plus resolution
of registered families by name through the existing `ResolveFont`.
Decide and document the behavior for unresolved families: today's
silent fallback should at least be observable.

## Acceptance criteria

- [ ] Font bytes loaded from an embedded asset (`embed.FS`) resolve by
      family name and shape text distinct from the system default
      (glyph pixels differ in a staging readback).
- [ ] A family that resolves nowhere either errors or reports the
      fallback, so a missing embedded font is diagnosable.
