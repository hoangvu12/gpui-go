# AddFont reports only a generation; the registered family name(s) are invisible

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
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

- [ ] `AddFont` on a font whose nameID 1 differs from nameID 16
      (Geist Medium) reports the typographic family ("Geist"), and a
      follow-up `ResolveFont` for the legacy name ("Geist Medium")
      reports `FamilyFound == false` — the mismatch is visible without
      guessing.
- [ ] `AddFont` on a font whose nameID 1 and 16 agree (Geist Regular)
      reports that family; a font carrying only the legacy name
      reports it.
- [ ] A blob with multiple faces (a collection) reports one entry per
      face, in file order, deduplicated.
- [ ] Malformed font data still fails with the typed error and
      reports no families (the report never invents names for rejected
      bytes).
