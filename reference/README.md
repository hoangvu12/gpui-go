# Reference harness and capability enumeration (ticket01)

Maintainer-only tooling for the gpui-go conformance work. Everything under
`reference/` is **not** part of the shipped Go module: consumers never need
Rust. The Go side lives in [`conformance/`](../conformance/).

## Directory layout

| Path | Purpose |
|---|---|
| `ce-source/` | Pinned GPUI-CE source checkout. Commit `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a` ("Implement character palette support on Windows and Linux (#308)", 2026-10-03), shallow-fetched from `https://github.com/gpui-ce/gpui-ce`. Never modified: the oracle must stay an independent build of the upstream code (zero instrumentation patches; all observation happens through public APIs). It is its own cargo workspace, `exclude`d from ours. |
| `harness/` | `gpui-reference-harness`: the independent reference oracle binary. Depends on `gpui-ce` (crate path above) with `features = ["test-support"]` and runs fixture envelopes from the pinned implementation, emitting a versioned trace. |
| `api-scan/` | `gpui-api-scan`: syn-based enumerator of the public API surface of all 24 workspace crates, with per-item cfg conditions, body markers (`unimplemented`/`todo`/`noop`) and a feature-cfg call-site trace. |
| `Cargo.lock` | Resolved dependency graph for the oracle build (recorded in ticket01 evidence). |
| `out/api-scan.json` | Generated enumeration (schema `gpui-go/api-scan@1`). |
| `out/profiles/*.txt` | Effective feature/dependency graphs for the recorded profiles (cargo tree, windows-msvc target): native default, test-support, inspector+screen-capture, custom-gpu/wgpu-surfaces (no-default), hot-patching+profiler+stacker, plus the harness workspace graph. |
| `target/` | Build output (debug profile used for the recorded test-profile trace). |

## The oracle contract

The harness never reads its own outputs as expectations and the Go port never
validates itself: `conformance/cmd/recordreference` runs this binary against a
fixture envelope (shared with the port via
[`conformance/fixtures/`](../conformance/fixtures/)) and records raw traces
plus run metadata under
[`conformance/recorded/`](../conformance/recorded/). Traces are
byte-deterministic across runs (verified: three consecutive runs produce
identical SHA-256). f32 values are serialized as exact IEEE-754 bit patterns
(`f32:XXXXXXXX`).

## Building and running (maintainer)

```powershell
cd reference
cargo build -p gpui-reference-harness        # debug profile; deps build once (~10–20 min)
cargo build --release -p gpui-api-scan       # fast, no gpui dependency

# Enumerate the API surface:
./target/release/gpui-api-scan.exe ce-source out/api-scan.json 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a

# Record the reference trace for a fixture:
cd ..
go run ./conformance/cmd/recordreference -harness reference/target/debug/gpui-reference-harness.exe -envelope conformance/fixtures/fx-0001-layout-effects.json -out conformance/recorded

# Regenerate the capability ledger:
go run ./conformance/cmd/ledgergen -scan reference/out/api-scan.json -out conformance/ledger/generated -profiles-dir reference/out/profiles
```

Updating the CE pin is a deliberate conformance decision (see the conformance
contract); do not pull new commits into `ce-source/` casually.

## Recorded identities (2026-10-05)

- Toolchain: `rustc 1.97.0 (2d8144b78 2026-07-07)`, `cargo 1.97.0 (c980f4866 2026-06-30)`, host `x86_64-pc-windows-msvc`, VS 2022 BuildTools.
- Harness binary (debug): SHA-256 `b58cbb746ed6121135d555bc2e64b3bd9035bc7d17e1de21a474447555832df5`.
- `reference/Cargo.lock`: SHA-256 `6148263b183ccb6a686ba66d93adb159409cfd15b90fe077a2d1d3bd975f9fc7`.
- Fixture envelope `fx-0001-layout-effects`: SHA-256 `f55e0db6780f95da864484c3af6b63024b7f292258537cbed706b8da52fd8197`.
- Effective oracle feature graph: `default + test-support` on the windows-msvc target (`out/profiles/harness-workspace.txt`).

See [ticket01 evidence](../evidence/ticket01-reference-harness.json) for the
complete record.

## Native bootstrap + layout artifact (tickets 02/06)

`native/` is the maintainer-built native artifact: one `cdylib`
(`gpui-go-native`) exposing a fixed C ABI bootstrap table with the checked
buffer round trip (ticket02) and the layout service (ticket06: Taffy
`=0.13.0`, pinned by the layout contract — the crate's one authorized
dependency — following the pinned gpui-CE adapter semantics; gpui-ce itself
is **not** linked). The Go consumer side is
[`internal/native/`](../internal/native/) (loader, tests,
`cmd/demo`). Building and running the Go module needs the prebuilt artifact
already embedded under `internal/native/artifacts/`; consumers never need
Rust, and `internal/native` builds with `CGO_ENABLED=0`.

### C ABI

`gpui_go_abi()` returns a pointer to the static `GpuiGoAbiTable`
(`#[repr(C)]`, windows/amd64 layout):

| Offset | Size | Field | Value / meaning |
|---|---|---|---|
| 0 | 8 | `buffer_round_trip` | function pointer, non-null |
| 8 | 64 | `reserved[0..8]` | future service-table slots, all null |
| 72 | 4 | `magic` | `0x4750474F` ("GPGO", big-endian reading) |
| 76 | 4 | `abi_version` | 1 |
| 80 | 4 | `native_revision` | 1 (bridge source revision) |
| 84 | 40 | `ce_commit` | ASCII hex `254b5dbd...`, zero padded |
| 128 | 8 | `capabilities` | u64 bitmask (4 bytes padding before) |
| 136 | 4 | `size_of_table` | 152 |
| 140 | 4 | `align_of_table` | 8 |
| 144 | 4 | `size_of_buffer_request` | 24 |
| 148 | 4 | `size_of_buffer_response` | 24 |

`GpuiGoBufferRequest { magic u32, abi_version u32, len u32, *const u8 }` and
`GpuiGoBufferResponse { status i32, echoed_len u32, checksum u64, *mut u8 }`
are 24 bytes each. The caller owns and provides all memory (request bytes and
echo buffer); the DLL retains nothing across calls and stays loaded for the
process lifetime. `checksum` is FNV-1a 64 of the request bytes.

Status codes: `0` ok, `1` bad magic, `2` bad ABI version, `3` null request,
`4` length over max (4096), `5` null data with non-zero length, `6` null
response (or null response data while bytes must be written), `7` panic
contained (via `catch_unwind`; the workspace release profile pins
`panic = "unwind"`). Validation order: null records, magic, ABI version,
length, data nullability.

Capability bits: bit 0 = `bootstrap-buffer-roundtrip` (the only assigned bit;
bits 1–63 reserved for the planned layout/text/scene/image/SVG/accessibility
service tables). Loaders reject missing *required* bits, never unknown extra
bits.

Two exports: `gpui_go_abi` (table) and `gpui_go_panic_probe` (test-only
diagnostic that panics on purpose so panic containment is observable; the
round trip is reachable only through the table pointer).

### Build and measure (maintainer)

```powershell
cd reference
$env:RUSTFLAGS = "-C target-feature=+crt-static"
cargo build --release -p gpui-go-native --target x86_64-pc-windows-msvc
cargo test  --release -p gpui-go-native --target x86_64-pc-windows-msvc   # 16 unit tests

cd ..
go run ./reference/native/tools/measure `
  -dll reference/target/x86_64-pc-windows-msvc/release/gpui_go_native.dll `
  -out  reference/out/native-bootstrap.json `
  -manifest internal/native/artifacts/manifest.json `
  -artifact-dir internal/native/artifacts
```

The measure tool records provenance to `reference/out/native-bootstrap.json`
and refreshes the embedded pair `internal/native/artifacts/
{gpui_go_native.dll,manifest.json}`. **Every rebuild must re-run it**: the
MSVC linker embeds a timestamp, so identical sources produce different
SHA-256s (observed during this ticket).

### Recorded measurements (2026-10-24, the browser-core ticket 77 rebuild — rev 15.1)

- Toolchain: `rustc 1.97.0`, host/target `x86_64-pc-windows-msvc`,
  `RUSTFLAGS=-C target-feature=+crt-static`, `cargo -j 4` (the
  full-graph build predates the PC-health note and ran `-j 6`).
  `ce-source/` is a copy of the main checkout's pinned `254b5dbd…`
  tree (unmodified).
- DLL: 19,107,328 bytes, SHA-256
  `ef337ad3051a382b7c9d98e5c10c9926f72df6e7d3c0d4d2a1012ec7cb322f4b`,
  gzip-9 7,591,778 bytes. Native revision **151** (the integer
  ENCODING of the point release “15.1” — the field is a u32
  equality-compared by the ABI table, the manifest and the Go
  constants, and 16 stays reserved for ticket 54's R16 “rebuilt as
  native rev 16”; the choice is documented at every pin site), the
  embedded artifact before it was revision 15, SHA `8b944eae…`.
  Capability mask `0xFFF`. CRT outcome: **static**.
- Changes: the rev-15 extension-cbuffer upload actually copies its
  struct (ticket 41's `&[*uniforms]` reference bug — colour filters
  and the backdrop crop were GPU no-ops); the filter record carries
  one 4×5 matrix PER colour function (188 → 780 bytes,
  `MAX_SCENE_FILTERS` 4 → 8, scene service v5) and the vendored
  composite shader applies them in chain order with the **[0,1]
  clamp between functions** (Chrome clamps each function's output
  8-bit — the filters-chain fixture measured a 15.47 mean channel
  diff against Chrome under the composed payload, and is now
  pixel-exact); the synthetic italic shears by Chrome's exact **−0.25**
  in the raster transform instead of DirectWrite's oblique simulation
  (measured −0.34 — the 08 probe now reads −0.2500 on both sides);
  and CSS `text-wrap: nowrap` drives the wrap policy (the one
  wireable rev-15 text-run field — the rest stay recorded behind the
  pinned adapter/Taffy seams in TEXT_ABI.md / LAYOUT_ABI.md).
  Evidence:
  [`evidence/browser-core/ticket77-native-rebuild.json`](../evidence/browser-core/ticket77-native-rebuild.json).
- `cargo test -p gpui-go-native` still fails to compile the lib test
  target with the same 5 pre-existing a11y.rs/capture.rs errors; the
  changes are verified through the Go gates
  (`internal/parity`'s filters/shadow/textpaint fixtures, the 08
  probes, `internal/textspec`, `internal/scenespec`, and the rest of
  the headless suite).

### Recorded measurements (2026-10-24, the browser-core ticket 39 rebuild)

- Toolchain: `rustc 1.97.0`, host/target `x86_64-pc-windows-msvc`,
  `RUSTFLAGS=-C target-feature=+crt-static`. `ce-source/` is a copy of
  the main checkout's pinned `254b5dbd…` tree (unmodified).
- DLL: 19,104,256 bytes, SHA-256
  `8b944eae962a75e693ce3fdc8c97c274ad5b8e80c5e1487fd2fd038ebc10c40a`,
  gzip-9 7,590,625 bytes. Native revision 15 (the embedded artifact
  before it was revision 14, SHA `14e76993…`), capability mask `0xFFF`.
  CRT outcome: **static**.
- Changes: Chrome's actual Windows bold rule (synthesize only when the
  request is at least 600 over a face below 600 — the rule Windows
  compiles, not the `>200` non-Windows rule rev 14 followed), the scene
  records grew gradient backgrounds, colour-matrix filter payloads and
  per-sprite pixelated flags, drop shadows clip out of the border box,
  the backdrop blur input is cropped at the element edge with mirrored
  margins, sprite batches can draw through a point sampler, the text run
  record grew the browser text fields (with the run_info echo), and the
  layout records grew flow-root, justify-items/self, variable grid
  track lists and the measure-response baseline. Three shader-level
  behaviors the pinned blobs cannot express are compiled from a
  VENDORED, PATCHED copy of the pinned shader modules
  (`native/shaders`, built by `native/build.rs` through the same
  wgsl-rs → Naga 24 → HLSL sm 5.0 → D3DCompile chain; the frozen
  `ce-source` stays unmodified). Evidence:
  [`evidence/browser-core/ticket39-native-rebuild.json`](../evidence/browser-core/ticket39-native-rebuild.json).
- `cargo test -p gpui-go-native` still fails to compile the lib test
  target with the same 5 pre-existing a11y.rs/capture.rs errors; the
  changes are verified through the Go gates
  (`internal/fontspec`, `internal/scenespec`, `internal/textspec`,
  `internal/layoutspec`, and the rest of the headless suite).

### Recorded measurements (2026-10-23, the zterm ticket 13 rebuild)

- Toolchain: `rustc 1.97.0`, host/target `x86_64-pc-windows-msvc`,
  `RUSTFLAGS=-C target-feature=+crt-static`. `ce-source/` is a copy of
  the main checkout's pinned `254b5dbd…` tree (unmodified).
- DLL: 18,877,952 bytes, SHA-256
  `14e76993da41b89b063e26cdfc8adc9c82300e29d532abb0bbd765dd8f8d52d0`,
  gzip-9 7,539,221 bytes. Native revision 14 (the embedded artifact
  before it was revision 13, SHA `0b6e9265…`), capability mask `0xFFF`.
  CRT outcome: **static**.
- Change: the text service keeps Fontique's bold synthesis on a static
  face only when the request is at least 600 and more than 200 above the
  face's OS/2 weight (Blink's rule); otherwise it requests the face's
  own weight. 700 over SemiBold now draws SemiBold as is; 700 over
  Regular and 900 over SemiBold still synthesize. Synthetic strength
  matches Chrome's (700 over 400: ink ratio 1.521 against Chrome's
  1.517), so it is unchanged. Evidence:
  [`evidence/zterm/ticket13-native-rebuild.json`](../evidence/zterm/ticket13-native-rebuild.json).
- `cargo test -p gpui-go-native` still fails to compile the lib test
  target with the same 5 pre-existing a11y.rs/capture.rs errors; the
  change is verified through the Go gates
  (`internal/fontspec` `TestBoldSynthesisFollowsBrowserRule`).

### Recorded measurements (2026-10-23, the gpui-perf ticket 05 rebuild)

- Toolchain: `rustc 1.97.0`, host/target `x86_64-pc-windows-msvc`,
  `RUSTFLAGS=-C target-feature=+crt-static`. `ce-source/` is a copy of
  the main checkout's pinned `254b5dbd…` tree (unmodified).
- DLL: 18,877,952 bytes, SHA-256
  `0b6e9265e0d519ecaf56aba6f6905efab0bdc7be936d6085ea42cab28e234450`,
  gzip-9 7,539,285 bytes. Native revision 13 (the embedded artifact
  before it was revision 12, SHA `88595f13…`), capability mask `0xFFF`.
  CRT outcome: **static**.
- Changes: scene service v4 (`insert_batch`: one packed op stream per
  call), renderer service v4 (frame-latency waitable swap chains with
  maximum latency 1, `surface_frame_latency_handle`), and the text
  service replaces a requested weight below 600 with the selected
  static face's own weight when Fontique would synthesize bold. Evidence:
  [`evidence/perf/ticket05-native-rebuild.json`](../evidence/perf/ticket05-native-rebuild.json).
- `cargo test -p gpui-go-native` still does not compile the lib test
  target (the pre-existing a11y.rs/capture.rs errors recorded below);
  the changes are verified through the Go gates.

### Recorded measurements (2026-10-19, the consumer-gaps font-registration rebuild)

- Toolchain: `rustc 1.97.0`, host/target `x86_64-pc-windows-msvc`,
  `RUSTFLAGS=-C target-feature=+crt-static`. The pinned CE source was
  re-fetched into `ce-source/` at `254b5dbd…` for this rebuild (the
  worktree's fresh checkout; the oracle tree is never modified).
- DLL: 18,866,688 bytes, SHA-256
  `88595f1332a26f4e2e9bc5d2d899e8abe330b6290078747a0fb2a738e654a941`,
  gzip-9 7,534,757 bytes. Native revision 12, capability mask `0xFFF`.
  CRT outcome: **static** — only OS system DLLs imported. The text
  service is v2: `text_font_add` (slot 15) registers font bytes through
  the pinned `add_fonts` path, the font record reports `family_found`,
  and the table gained `max_font_bytes` (216 bytes, service version 2).
- Known breakage, recorded honestly (pre-existing, NOT this rebuild's):
  `cargo test -p gpui-go-native` does not compile the lib test target
  on this checkout — five errors in the wave-4 test code
  (a11y.rs:1361/1367/1385, capture.rs:1510/1531: a missing
  `accesskit::ActionHandler` trait import, `ID3D11Texture2D::default()`
  which windows 0.62.2 does not provide, and two unsafe calls outside
  `unsafe` blocks). The text-service behavior is verified through the
  Go gates against this artifact instead (`internal/native`,
  `internal/fontspec`); the Rust text tests are in place and will run
  once that debt is fixed by its owners.

### Recorded measurements (2026-10-10, ticket17 rebuild)

- Toolchain: `rustc 1.97.0`, host/target `x86_64-pc-windows-msvc`,
  `RUSTFLAGS=-C target-feature=+crt-static`, `-j 4`.
- DLL: 14,453,248 bytes, SHA-256
  `29781ff7be4d3b8c7ae2164f5e2accf0de5e95a8f9979edb62a9cf3ab232c2b2`,
  gzip-9 5,790,413 bytes. Native revision 8, capability mask `0x1FF`
  (bootstrap + layout + renderer + scene + text + glyph + atlas +
  scene-draw + image-codecs). CRT outcome: **static** — only OS
  system DLLs imported. The image crate resolves to 0.25.10 (the
  workspace lock's resolution of the pin's `image = "0.25.1"`),
  recorded with its checksum and feature graph in
  `internal/native/artifacts/manifest.json`.

### Recorded measurements (2026-10-05)

- Toolchain: `rustc 1.97.0 (2d8144b78 2026-07-07)`, host/target
  `x86_64-pc-windows-msvc`, `RUSTFLAGS=-C target-feature=+crt-static`.
- DLL: 202,752 bytes, SHA-256
  `b8a836dc5a24661f836239259b38adc025e970c03fce8c43adb1bfc1deb4fd18`,
  gzip-9 (Go stdlib compress/gzip) 107,381 bytes. Machine `0x8664`
  (amd64), subsystem windows-gui.
- CRT outcome: **static** — the PE imports no VCRUNTIME/MSVCP/UCRT/
  api-ms-win-crt components; only OS system DLLs are imported
  (`KERNEL32.dll` 82 symbols, `api-ms-win-core-synch-l1-2-0.dll` 3,
  `ntdll.dll` 2). No VC++ redistributable is required. Full import table in
  `reference/out/native-bootstrap.json`.
- Module format impact: `go build -o demo.exe ./internal/native/cmd/demo`
  (CGO_ENABLED=0) = 4,900,352 bytes; the same program built with
  `-tags gpui_native_noembed` = 4,697,088 bytes. Embedding the DLL+manifest
  contributes ~203 KiB (≈ DLL 202,752 + manifest 969 + embed machinery),
  ~0.04% of Go's 500 MiB module ceiling — far below any module-format
  reopening threshold.
- Go gates: `CGO_ENABLED=0 go test ./internal/native/...` — 16 tests pass
  (identity, round trips incl. 0/1/4096, wrong hash/arch/ABI/capability
  rejection, corrupt-final-file typed error + bundle recovery, stale staging
  cleanup, unwritable cache, concurrent publication, forced GC, real-DLL
  status codes, bundle override, exactly-once release, reparse redirect,
  verified-handle write/delete denial, record sizes); `go vet` and `gofmt`
  clean; `go run ./internal/native/cmd/demo` prints identity + PASS lines.
- Byte reproducibility is NOT claimed and is not expected to hold: per the
  distribution contract ("two builds have not yet demonstrated byte
  reproducibility") and the MSVC link timestamp, rebuilding the same source
  yields a new SHA-256 (observed in this ticket). Expected flow: rebuild →
  new artifact identity → re-run the measure tool to refresh the manifest and
  embedded pair before any Go build.

### Loader policy notes (aligned with the distribution contract)

- Loading: `LoadLibraryExW` with the absolute verified path and
  `LOAD_LIBRARY_SEARCH_SYSTEM32` (0x800) only: no application-dir, CWD or
  PATH search; dependencies resolve from System32. The artifact imports only
  OS system components, so no companion resolution is needed;
  `LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR` (0x100) is deliberately unused and stays
  reserved for future artifacts that ship verified private companion DLLs
  (contract: "revise the manifest and use a verified artifact directory plus
  LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR; do not add global DLL search paths").
- Cache (contract formula): `<UserCacheDir>/gpui-go/native/windows-amd64/
  <full-sha256>/gpui_go_native-<full-sha256>.dll` — the DLL basename also
  carries its full hash, so distinct artifact identities never collide.
  Publication happens under a cross-process `LockFileEx` lock
  (`publication.lock` in the cache root), via a uniquely named staging file +
  fsync + rename, with the final file's full SHA-256 verified through the
  retained handle before use.
- Corrupt final file: a typed `ErrCacheCorrupt` error, never an overwrite —
  the file could be mapped by another process. Recovery is the explicit
  verified bundle override (`Options.BundleDir` / `GPUI_GO_NATIVE_BUNDLE`)
  or manual cache cleanup. Crash recovery proper is the staging files:
  uniquely named remnants of a killed writer are ignored and cleaned under
  the publication lock.
- Verified-handle retention (contract): the selected DLL is opened for read
  with sharing that DENIES write and delete (`CreateFileW` with
  `FILE_SHARE_READ` only), hashed through that same open handle, and the
  handle is retained across loading for the module's process lifetime.
  `Library.Close` releases it at shutdown time (and the OS module stays
  resident — no `FreeLibrary`).
- Bundle override (`Options.BundleDir` or `GPUI_GO_NATIVE_BUNDLE`) must be an
  absolute directory with `gpui_go_native.dll` + `manifest.json`; the
  embedded manifest stays authoritative (a bundle manifest cannot authorize a
  replacement hash/identity).
- `syscall.NewLazySystemDLL` no longer exists in Go 1.27; `internal/native`
  uses `syscall.NewLazyDLL("kernel32.dll")`, which loads System32-only
  because the stdlib registers kernel32.dll in its internal system-DLL
  registry at init.

### Embedded manifest fields (`internal/native/artifacts/manifest.json`)

| Field | Meaning |
|---|---|
| `schema` | `gpui-go/native-manifest@1` |
| `name` | artifact file name (`gpui_go_native.dll`) |
| `sha256` | exact SHA-256 of the DLL bytes — the authoritative artifact hash |
| `bytes` | DLL size in bytes |
| `gzip_bytes` / `gzip_sha256` | gzip-9 (Go stdlib) size and hash, provenance only |
| `ce_commit` | pinned gpui-CE commit of the artifact family |
| `abi_version` | private ABI schema major version (must equal the table's) |
| `native_revision` | native bridge source revision |
| `capabilities_mask` / `capabilities` | compiled capability bitmask and assigned bit names |
| `target` / `machine` | build triple and PE machine |
| `built_at` / `rustc` | build provenance |
| `crt_static_requested` / `crt_static_outcome` | requested flag and what the PE imports actually show |
| `imports` | imported-DLL summary (full table in the provenance JSON) |
| `provenance` | path of the full provenance record |
