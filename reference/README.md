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
