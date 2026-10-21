# Register fonts from memory (embedded font bytes)

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi worker
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

- [x] Font bytes loaded from an embedded asset (`embed.FS`) resolve by
      family name and shape text distinct from the system default
      (glyph pixels differ in a staging readback).
- [x] A family that resolves nowhere either errors or reports the
      fallback, so a missing embedded font is diagnosable.

## Answer

Resolved 2026-10-19 by the pi worker on branch
`consumer-gaps/03-register-fonts-from-memory`. Complete record:
[evidence/consumer-gaps-03-register-fonts.json](../../../evidence/consumer-gaps-03-register-fonts.json).

**The seam the ticket suspected exists and is now surfaced.** The
pinned `PlatformTextSystem::add_fonts` (gpui_ce_parley's
`ParleyTextSystem` impl: `fontique::Blob` →
`CatalogState::register_blobs`) was always callable on the native
stack; nothing exposed it across the ABI. This ticket surfaces it:

- **Native (rev 12, text service v2)**: `reference/native/src/text.rs`
  gains the `text_font_add` entry (table slot 15): one font blob per
  call, bytes copied into service-owned state, registered through the
  exact pinned `add_fonts` code path, returning the new catalog
  generation. New typed status `-8` (font data rejected — the pinned
  `register_blobs` bail when the bytes hold no supported face). The
  font record's v1 `reserved` word became `family_found`; the table
  grew to 16 pointers + 21 scalars (216 bytes) with `max_font_bytes`
  (64 MiB). TEXT_ABI.md documents the v2 contract. The DLL was rebuilt
  through the documented maintainer process (cargo release build,
  crt-static, freshly re-fetched pinned CE checkout; measure tool
  refreshed the embedded pair; the consumerspec evidence constants and
  the glyphspec/scenespec revision pins rotated in lockstep).
- **Public API**: `TextSystem.AddFont(data []byte) (FontRegistration,
  error)` — the pinned `add_fonts` semantics (family names come from
  each font's embedded name table; they appear in `FontNames` and
  resolve by name through `ResolveFont`; shaping AND rasterization see
  them — one Fontique catalog for the process). `FontRegistration`
  carries the post-registration catalog `Generation`. `ErrTextFontData`
  for no-face bytes, `ErrTextValue` for empty/oversize data.
- **The unresolved-family decision (documented, per the ticket's ask)**:
  the resolve *behavior* stays pinned (no error, the fallback font's
  id — changing it would break the reference's fallback semantics),
  but the fallback is now REPORTED: `FontIdentity.FamilyFound` is false
  when the descriptor's primary family is absent from the catalog (an
  embedded font that failed to register, or a name that differs from
  the font's internal family name). Computed against `all_font_names`,
  cached per catalog generation so the resolve path stays cheap.

**Acceptance 1** — `internal/fontspec` (real DLL, real window, FAIL not
skip): Geist + Geist Mono (SIL OFL, license committed beside them) load
from an `embed.FS`, register through `AddFont`, appear in `FontNames`,
resolve by family name with `FamilyFound` true and distinct canonical
ids; the consumer's exact size ramp (11/12/13/17/22/28 + Mono 12)
shapes real glyph runs; and a 480×200 real window on the real GPU
renders the same 28px text in Geist and in the system default through
the public scene path, read back with `RenderScene` + `ReadPixels`:
2990 Geist ink pixels vs 2639 default ink pixels, 10,329 differing byte
values — then presented through the ledger with retirement.
**Acceptance 2** — the same package plus `internal/native` and the
`gpui` seam test: nonsense families (including the evidence's
"Definitely Not A Font 12345" and a misspelled registered family)
resolve without error and report `FamilyFound` false; bad `AddFont`
input is typed errors that publish nothing.

**Verification** (this machine, Windows 11, go1.27.1, CGO_ENABLED=0):
`go build ./...`, `go vet ./...`, gofmt clean; the consolidated ticket
run (`internal/fontspec`, `internal/native`, `internal/textspec`,
`internal/glyphspec`, `internal/portfixture`, `internal/consumerspec`,
`gpui`) all ok; the full `go test ./... -count=1` run green except two
environmental flakes, each verified to pass isolated (the capturespec
monitor-content gate — the captured monitor sees whatever occludes the
test window during the long suite — and the shutdownspec modal-close
cascade under suite load). The ticket26 cursor gates of the documented
environmental class (one re-verified pre-existing against the pristine
rev-11 tree) passed the final run. The recorded conformance traces
(textspec fx-0004, glyphspec fx-0005) stayed bit-exact through the
artifact rotation.

**Honest findings** (recorded, not silently worked around):

1. `cargo test -p gpui-go-native` does not compile the lib test target
   on this checkout — five PRE-EXISTING errors in the wave-4 test code
   (a11y.rs:1361/1367/1385: a missing `accesskit::ActionHandler` trait
   import and an unsafe call outside a block; capture.rs:1510:
   `ID3D11Texture2D::default()`, absent in windows 0.62.2;
   capture.rs:1531: an unsafe call outside a block), verified
   pre-existing by stash-and-rebuild. The new Rust tests for
   `text_font_add` are in place and compile, but cannot run until that
   debt is repaired by its owners; the identical native code paths are
   verified through the Go gates against the rebuilt artifact.
2. One blob per `AddFont` call (the pin's `add_fonts` takes a `Vec`);
   N calls are N registration batches (N generation bumps). FontIds,
   families and resolution are unaffected — documented on the method
   and in TEXT_ABI.md.
3. `FamilyFound` reports family PRESENCE in the catalog, not which face
   Fontique matched — the pinned public API exposes no matched-family
   report, and presence is the honest observable.

## Comments

- 2026-10-19 — claimed and implemented by the pi worker (this file's
  resolution); sibling tickets 01 (atlas present path) and 02 (shaping
  leak) untouched — no present-path, atlas or div.go changes.
