# AddFont reports only a generation; the registered family name(s) are invisible

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi worker
Blocked by: none

## Question

A font registered through `TextSystem.AddFont` lands in the catalog
under its **typographic** family name (the name table's nameID 16)
only — the pinned fontique `register_font_impl` picks
`english_or_first_name(TYPOGRAPHIC_FAMILY_NAME)` or else
`english_or_first_name(FAMILY_NAME)` (nameID 1) and registers the face
under exactly that one name. Fonts whose legacy family differs — the
Geist release is the case mordbard hit: `Geist-Medium.ttf` says family
"Geist Medium" (nameID 1) but typographic family "Geist" (nameID 16) —
cannot be requested by the legacy name: the descriptor resolves with
`FontIdentity.FamilyFound == false` and silently falls back through
the service chain (Arial on Windows), where a non-400 weight is
satisfied with synthetic emboldening.

mordbard's probe (register Geist-Regular, Geist-Medium, GeistMono,
then `ResolveFont`):

    "Geist"@400        -> familyFound=true   (the Regular)
    "Geist Medium"@500 -> familyFound=false  (Arial!)
    "Geist"@500        -> familyFound=true   (the real Medium)

Both weights are reachable through the one "Geist" family, so the
mismatch is easy to work around but hard to notice: `FontRegistration`
reports only the catalog `Generation`, never the family name(s) the
faces landed under, and nothing prompts an app to check a family it
believes it just registered. mordbard rendered every 500-weight label
in Arial with synthesized weight (36 % more ink than the reference
frame) before noticing.

## What to build

Make the registration's outcome observable: `FontRegistration`
reports the family name(s) the submitted bytes landed under, parsed
from the font's own name table with the pinned fontique precedence
(english-or-first nameID 16, else nameID 1; one entry per face,
deduplicated). Registration behavior itself stays pinned
(`add_fonts` semantics unchanged: no second family entry, no error for
a legacy-name mismatch), and the report is documented on `AddFont` so
a caller can see which name to request. The resolution fallback keeps
its `FamilyFound` diagnostic (ticket 03).

## Acceptance criteria

- [x] `AddFont` on a font whose nameID 1 differs from nameID 16
      (Geist Medium) reports the typographic family ("Geist"), and a
      follow-up `ResolveFont` for the legacy name ("Geist Medium")
      reports `FamilyFound == false` — the mismatch is visible without
      guessing.
- [x] `AddFont` on a font whose nameID 1 and 16 agree (Geist Regular)
      reports that family; a font carrying only the legacy name
      reports it.
- [x] A blob with multiple faces (a collection) reports one entry per
      face, in file order, deduplicated.
- [x] Malformed font data still fails with the typed error and
      reports no families (the report never invents names for rejected
      bytes).

## Answer

Resolved 2026-10-20 on `consumer-gaps-2` (commit "Report the families
an AddFont registration landed under"). The report option of the
ticket's suggested fixes; registration behavior itself stays pinned.

**What landed**: `FontRegistration.Families []string` — the family
names the submitted bytes landed under, one entry per face in file
order (deduplicated), parsed from the font's own name table by a new
Go-side reader (gpui/fontname.go) that reproduces the pinned
fontique precedence exactly: `english_or_first_name(TYPOGRAPHIC_
FAMILY_NAME)` (nameID 16) or else `english_or_first_name(FAMILY_NAME)`
(nameID 1), including fontique's english_or_first ranking (0x0409
wins outright, language 0 next, only the first record can rank
otherwise, other languages skipped). Collections ('ttcf') contribute
one entry per face. A rejected registration reports NO families (the
report never invents names for bytes the catalog would refuse); an
sfnt the catalog accepts whose name table cannot be decoded registers
normally and reports none. `AddFont`'s doc names the precedence and
the mismatch shape.

**Deliberately not done** (recorded): registering the face under BOTH
family names — the pinned `add_fonts`/fontique registers one family
per face, and dual registration would deviate from the pin for a
convenience mordbard does not need (both Geist weights are reachable
through the typographic family, which is what its `RegisterFonts`
already resolves). The resolve-side `FamilyFound` diagnostic (ticket
03) stays the fallback observable.

**Verification** (all green; red-by-construction for the report —
before the change `FontRegistration` carried no names at all):
- `gpui TestFontFamilyNames*` (7 tests): Geist Medium reports ["Geist"]
  (typographic wins over the legacy "Geist Medium"); Geist
  Regular/Mono report their families; non-fonts, truncated
  directories and out-of-range name tables are rejected (never
  guessed); the english-or-first ranking pins (0x0409, language 0,
  first-record, and the non-first non-English skip); a hand-built
  two-family 'ttcf' reports both in file order, and two faces of one
  family deduplicate to one entry.
- `internal/fontspec TestAddFontReportsTheLandedFamilies` (the public
  API gate, real catalog): Geist Medium's registration reports
  ["Geist"]; the reported family resolves at weight 500 with
  `FamilyFound` true; the legacy name "Geist Medium" reports
  `FamilyFound` false (the mismatch is checkable from the
  registration's own report — the ticket's probe); Geist Mono reports
  its family; garbage bytes error AND report no families.
- `go build ./...`, `go vet ./...`, `go test ./... -count=1` green
  except the documented environmental gates (`internal/headlessspec`
  needs the gitignored `reference/ce-source`; the a11y concurrent
  query gate flakes under full-suite load and passes isolated —
  re-verified twice).

**What mordbard should change**: nothing required — the "Geist"
family request is already right (the ticket filed it that way).
Optional: `ui.RegisterFonts` can now assert each intended family is
IN the registration's `Families` report instead of resolving it after
the fact, and any future embedded face with differing names surfaces
at registration time instead of as an Arial render.

## Comments

- 2026-10-20 (pi worker): claimed, implemented, verified, resolved.
