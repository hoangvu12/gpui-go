# Resume after compaction or explicit chat transfer

## Build speed (2026-10-21, merged to main)

Fluent methods of the generic mixins (Styled, Parent, Interactive) carry the `[_ lazy]` type-parameter marker (gpui/styled.go `lazy`; cmd/stylegen emits it). Go compiles every ordinary method of an instantiated generic type in each package that references it, so plain methods made every UI package recompile the ~3,100-method vocabulary (mordbard UI edit: 37 s → ~2 s; five Component[Self] components: 171 MB → 4.8 MB). Any new Self-returning method on a mixin needs the marker; internal/buildcostspec fails otherwise. Open runtime items (measured, not yet fixed): the >128-distinct-text real-window failure (native shaping handle cap), per-frame glyph re-rasterization (draw_text.go paintGlyphFromAtlas skips the pinned raster_metadata cache), per-node/per-primitive FFI calls, ~60k allocations/frame at 1,000 elements.

## ACTIVE EFFORT (2026-10-23): recreate zgui's terminal app ("zterm") on gpui-go

**HANDOFF 2026-10-24 (orchestrator 8 → orchestrator 9).** The user asked for it. The handoff doc is `%TEMP%\gpui-go-handoff-orchestrator9.md`: it covers resuming the 4 workers that Roboco's restart interrupted, the user's rules from today, the merge lessons, the open questions and the frontier. The predecessor is Roboco chat 08b88b23.

**ORCHESTRATOR 9 STATUS (2026-10-24). CURRENT.** Claude Code, Opus 5.5; Roboco chat b38c2f03. It took over from orchestrator 8 (chat 08b88b23; handoff doc `%TEMP%\gpui-go-handoff-orchestrator9.md`). The rules are those in orchestrator 8's block below, unchanged.
- **Resolved (54 of 77):** orchestrator 8's 45, plus 15 (wheel latching, overscroll-behavior, keyboard scroll target, 100/3 px line step), 22 (close watchers + Escape), 35 (position: fixed), 19 (core text selection), 16 (drag and drop sequence), 23 (ShowModal), 24 (tooltip hook) 20 (text selectable by default) and 72 (Base overlays on core). Each has a verification comment and a map gist. The broad batch set passed in full after 15/22/35/19.
- **Pi processes:** a pi chat's `cmd.exe` + `node.exe` pair (150-270 MB) stays alive after its turn, and it holds the worktree folder locked. After each merge, kill that worker's tree: find the `cmd.exe` whose command line names `--session ...gpui-go-wt-core-NN--` and `taskkill /PID <it> /T /F`. Roboco respawns pi with `--session` if the chat gets a new message. Never kill a running worker's pair.
- **research-src:** `%TEMP%\research-src` was deleted in a user-approved disk cleanup (Pane chat, 14:24 UTC). Chromium is restored as a shallow blobless clone at 0d4dcd4ffc1d23d05eea824a8462be66288d2528 (`git show HEAD:<path>` fetches blobs on demand). Skia (e71ecd07) is not restored; re-fetch it the same way if needed. The Go build cache is 69 GB, but the user didn't pick clearing it, so leave it.
- **Merge lesson, again:** branches that predate 48 need `.OutlineNone()` on focusable fixture elements whose pages pin `outline: none` (fixed in wheel and closewatcher on main). Check this on every merge of an older branch.
- **Running:** 73 (chat 18f60856, `gpui-go-wt-core-73`, `core/73-base-editing`); 45 (chat 842bffc6, `gpui-go-wt-core-45`, `core/45-smooth-scroll`).
- **Perf follow-up:** 20 adds ~21 allocations per idle selectable text per frame.
- **Pi pairs of NEW chats** don't name their session on the command line; match a chat by its createdAt time to the `cmd.exe` CreationDate (local = UTC+7), and confirm it has no busy bash/go children before killing it.
- **Frontier (Go-first):** 37 and 68 (repo-wide; run nearly alone). 72 and 73 → 75 (with 71). clipspec's PowerShell interop test fails while another process holds the OS clipboard (environmental).

**ORCHESTRATOR 8 STATUS (2026-10-24). History now.** Claude Code, Opus 5.5. It took over from orchestrator 7 (Roboco chat d209bee2; handoff doc `%TEMP%\gpui-go-handoff-orchestrator8.md`, which still holds the rules, the rev 15.1 list, why workers are slow, and the frontier).
- **Resolved (45 of 77; orchestrator 7's "22" was a miscount, it was 21):** 01, 02, 03, 04, 05, 06, 07, 08, 09, 10, 11, 12, 13, 14, 17, 18, 21, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 36, 38, 77 (rev 15.1 = native revision 151, DLL ef337ad3), 39 (rev 15, DLL 8b944eae), 40, 41, 44, 46, 47, 48, 49 (the R16 design, approved), 62, 63, 64, 65, 66, 67, 74. Each has a verification comment, and the map's Decisions list a gist of each. Orchestrator 8 verified and merged 33 (position: static opt-in), then 25 (anchored to an element). The merges had no conflicts, and the required packages pass on main.
- **Known flakes:** `gpui` TestWin32TranslateKeyDownCarriesCodeAndLocation (it reads the live Shift key; fails while the user types), TestFrameLoopPreferredFrameIntervalDrawsEveryOtherVblank and TestSystemNotificationResponseDrain and gpui/script TestFrameOverheadIsUnder1msAtTheProbeSize (load-sensitive), and authorspec TestRealTwoWindowCounterSharedModel (rare). Re-run them.
- **Open follow-up from 33+25:** Anchored, Deferred and custom containers don't call `RequestContainerLayout`, so a `Static()` div inside them defers its absolute children past the overlay (opt-in only; for 76).
- 29 forced colours, 30 spellcheck and 77 (rev 15.1) are merged and resolved. 77 left open: the partial-region backdrop crop zoom (fold into R16), and the 42/43 text fields, which are blocked behind the pinned Parley/Taffy seams (per-field plans in TEXT_ABI.md/LAYOUT_ABI.md). Watch authorspec TestRealTwoWindowCounterSharedModel (a rare reentrancy panic, seen once; then 19/19).
- **INTERRUPTED (2026-10-24 ~20:02): Roboco restarted** (all roboco.exe processes started at 20:02), which killed every running worker; 16 reported exit 0x40010004 (terminated from outside). This chat's Roboco tools disconnected, so the workers can't be read or resumed from here. Their work is on disk, mostly uncommitted:
  - 22+35 (chat 54b805fb, `gpui-go-wt-core-22`, `core/22-35-escape-fixed`): 22 committed (cf2b1ac); 35 in progress (13 changed files)
  - 15 (chat d728884c, `gpui-go-wt-core-15`, `core/15-wheel-latching`): uncommitted (26 changed files)
  - 16 (chat 16397ca0, `gpui-go-wt-core-16`, `core/16-drag-drop`): uncommitted (11 changed files)
  - 19 (chat 1c05b939, `gpui-go-wt-core-19`, `core/19-text-selection`): uncommitted (41 changed files)
  Next: once Roboco is reachable again, resume each chat with send_messages ("you were interrupted by a Roboco restart; your worktree has your uncommitted work; continue from it"), or start fresh workers on the same worktrees. Don't delete these worktrees.
- The user asked for more speed (2026-10-24, "speed things uppppp"). 5 workers are running, with GOFLAGS=-p=4 and targeted tests.
- **Rules:** as in orchestrator 7's block below and the handoff doc, with these changes from the user (2026-10-24):
  - KEEP the Go build cache. Clear it only when it passes about 40 GB (`go env GOCACHE`), because a cold cache makes every worker recompile gpui, which freezes the PC.
  - Workers run go commands with `GOFLAGS=-p=4` and cargo with `-j 4`.
  - IMPLEMENTATION FOCUS (the user, 2026-10-24: "just focus on implementing"): workers keep Chromium citations to the key rules only, write a short bullet Answer, add at most one wrong-view test, and give small related tickets to one worker together. The Chrome fixtures stay, because they are the acceptance tests. The orchestrator verifies with the worker's test run, the touched packages plus parity on the merge, and a quick diff scan; no line-by-line citation checks. (Later the same day the user said "just take ur time": no rush, so the orchestrator still spot-checks the key citations and keeps the lighter worker process.)
  - GO-FIRST (the user, 2026-10-24): do all the Go-only tickets first. Defer the native ones: R16 (50-54), the tickets built on it (55-61) and the blocked text fields (42, 43). That leaves 32 Go-only open tickets, plus the 5 running.
  - Any ticket that adds a Tailwind class or a TSX style key must run `go generate ./gpui/script/types` (63's drift tests fail otherwise). Tell workers this, and regenerate after merges.
  - Don't rerun the whole suite each time. Workers run targeted tests plus the packages their diff touches. The orchestrator verifies a merge with the touched packages plus the parity suite. The broad set (the commonly forgotten packages: authorspec, dropspec, inspectorspec, richtextspec, focusspec, imespec, winhostspec, actionspec, examples/...) runs once per batch of merges, not per merge.

**ORCHESTRATOR 7 STATUS (2026-10-24). History now.** This was Roboco chat d209bee2 (Claude Code, Opus 5.5). It took over from orchestrator 6 (chat 3d6efd46; handoff doc `%TEMP%\gpui-go-handoff-orchestrator7.md`).
- **Resolved (22 of 76):** 01, 02, 03, 04, 05, 06, 08, 09, 10, 11, 13, 27, 31, 39 (rev 15, DLL 8b944eae), 40, 41, 47, 49 (the R16 design, approved), 62, 64, 65. Each has a verification comment, and the map's Decisions list a gist of each.
- **Off-screen test windows: merged** (e3e3476). Under `go test`, real windows open at +(20000, 20000), and so do SetBounds and synthetic WM_DPICHANGED rectangles. Switches: `GPUI_TEST_ONSCREEN=1` shows them; `GPUI_TEST_REPORT_ONSCREEN=1` logs any test window on a monitor; `GPUI_REAL_INPUT_TESTS=1` turns it off. The full suite on the off-screen branch had no window on a monitor. The two maximize tests are behind `realinput.Require`. Fixed along the way: dropspec's drag points follow the window, authorspec's drag preview dispatches input directly, and inspectorspec counts only the move (since 10's hover sweeps).
- **Full suite** (`go test -p 1 ./... -count=1`, off-screen): 65 packages ok. The only failure left is portfixture TestFx0004 (the system font count went from 94 to 95). clipspec's PowerShell clipboard test failed once with "Requested Clipboard operation did not succeed" (the clipboard was held by something else); it passed on the next run.
- **No workers running.** 33 (c4cf11c, `core/33-position-static`) and 25 (f986cc4, `core/25-anchored`) are both FINISHED but not yet verified or merged. Merge 33 first, then 25; see the handoff doc.
- **Claimed, not yet delegated:** 29, 30. The user reported the PC freezing; another Claude session was running parallel cargo/wasmtime builds. Start workers gradually.
- **Waiting on the user:** the native follow-up rebuild, rev 15.1:
  - the text-run fields and baseline for 42/43
  - the italic shear from 08
  - the extension-uniform upload bug: draw.rs:2327/2330 `&[*uniforms]` copies a reference, so colour filters are no-ops (41) and the backdrop crop is most likely broken by it too (04)
  The alternative is to fold it into R16; rev 16 (54) needs its own approval.
- **Rules:**
  - Up to 6 workers (pi + iroha/dashscope/glm-5.3).
  - Clean up merged worktrees and branches after each merge (the user asked).
  - Verify before merging.
  - No push, tag or repo creation.
  - Headless only.
  - Never touch the `gpui-go` main checkout.
  - Never `taskkill` go.exe globally; it kills other sessions' runs.
- 37 (line-height: normal) changes the default line height repo-wide, so run it nearly alone, like 68.
- Worktree folders: all leftovers were removed on 2026-10-24, except the empty `gpui-go-wt-core-74` (finished-worker folders usually release after a while), held by Roboco's per-chat processes for the finished workers (no process command line names them; don't kill roboco.exe); retry later. A stuck Roboco `git diff` on a deleted worktree can hold a folder; stop only that PID (find it by command line).

Workers leave tickets claimed. The orchestrator verifies each branch, merges it and then resolves the ticket.

**DECISIONS IN (2026-10-23):** D0-D14 accepted as recommended. D16: `gpui-go-base`, a public repo; zterm and the gallery are for testing only and never pushed. Rev 15 is approved, to be built as the spec's R15 group. D15 is TSX (ADR 0003). The spec is written: `.scratch/browser-parity/spec.md` (ready-for-agent). The user confirmed the test seams (a browser-parity harness against headless Chrome fixtures, the native spec packages, the TSX host through the harness, and Base against the public API). Tickets are DONE: 76 in `.scratch/browser-core/` (map + issues/01-76, the user approved the breakdown). Frontier: 01 (the browser-parity harness, which most tickets need), 02, 03, 30, 31, 39 (R15, approved), 47, 49 (R16 design, for the user to review), 68. Next: claim and delegate the frontier, at most 3 workers.

**NEW DIRECTION (2026-10-23, the user; see PROJECT.md "Direction change under research"):**
- The core should behave like the browser, without shipping components.
- Base moves to a new repo. Its name, visibility, and whether zterm and the gallery move with it are still open for the user.
- First, deep research that reads browser source: map [.scratch/browser-parity/map.md](../.scratch/browser-parity/map.md).
- Chromium 0d4dcd4f (sparse) and Skia e71ecd07 are cloned under `%TEMP%\research-src\`.
- Batch 1 is running in worktree `gpui-go-wt-browser` (branch `research/browser-parity`), where the workers write files but don't commit: 01 visual primitives (chat a44ba398), 02 motion and compositing (fa3bcda3), 03 input and events (e9cd4b8c).
- DONE: 01-06 and 08 are merged. 07 (the synthesis) is `research/browser-parity/README.md`, with 23 quick wins, 3 native rebuild groups (R15/R16/R17+) and decisions D0-D16 for the user. Next: the user decides, then runs /to-spec (the skill can't be invoked by the agent). Also pending: the rev 15 bold fix (part of R15). D15 is DECIDED: **TSX via esbuild + goja on zgui's model** (ADR 0003, superseding Luau in ADR 0002). The probe (research/browser-parity/09, prototype research/tsx-probe/, merged 2e0137e) measured reload ~4 ms and bound updates 0.001 ms; it costs ~12 MB of binary and has no per-VM memory cap. yaegi was ruled out.
- The zgui/gpui-fast benchmark is dropped for now. The user said zgui doesn't support Windows; it actually does, with weaker Windows support.

**ORCHESTRATOR 5 STATUS (2026-10-23; current, read with the handoff below).** This is Roboco chat 43f0a1a1. 06 is resolved and merged (6d61f35). 13 is resolved and merged (ae17675; native rev 14, DLL `14e76993…`). Open for the user: matching Chrome's web-font bold rule would take another rebuild. 09 phase A is merged: `research/zgui-terminal/parity-report.md` counts 91 matched, 18 approximated and 4 missing. Frame times are CPU-only and headless; my re-run had one opening frame at 18.8 ms because the palette draws twice on open. 09 is still claimed. Phase B (the live zgui side-by-side and the on-screen frame pacing) is pending; the worker is chat 2bfd6d53, worktree `gpui-go-wt-zterm-parity`. The user tested both apps by hand ("its pretty alright"). The user asked what gpui-go lacks for apps in general vs zgui and gpui-fast, read from their real source: the answer is in `research/app-gaps/README.md` (f9b0426). PROPOSED to the user, answer pending: (1) a perf benchmark of zterm against zgui using the chart's metrics; (2) Go-side layout animation and fuller drag events in gpui, beyond upstream; (3) one native rebuild for scale/rotate and gradient masks, plus optionally Chrome's web-font bold rule; (4) re-measure. `gpui-go-wt-zterm-dnd` and `gpui-go-wt-zterm-bold` can be removed. Their branches are merged.

**HANDOFF 2026-10-23 (orchestrator 4 → orchestrator 5). CURRENT AND AUTHORITATIVE; everything below it is history.** The user asked for this handoff. The predecessor is the Roboco chat "gpui-go zterm recreation (orchestrator 4)", id 6351756b.

State: `main` in this worktree is 40+ commits ahead of `origin/main`. Nothing has been pushed; push or tag only with the user. The map is [.scratch/zterm/map.md](../.scratch/zterm/map.md). Scope (the user's words): UI only, Windows only, placeholder panes.
- **Resolved and merged:** 01-05, 07, 08 and 10-12.
  - gpui gained a hidden titlebar, WindowControlArea, StartWindowMove, DirectComposition, ellipsis truncation, can_drop and drag-over styles, and `Window.WindowControlAreaAt`.
  - `examples/zterm` has the titlebar and tabs, the sidebar, splits, the palette, the confirm sheet, persistence and Geist (Regular/Medium/SemiBold/Bold).
- **Full corpus** (real windows, no real-input tests): 52 packages ok at 2f1b913.
- **06, drag and drop, is IN PROGRESS.** A background subagent of orchestrator 4's session is working in `gpui-go-wt-zterm-dnd` (branch `zterm/dnd`). It was resumed after a restart; at handoff the branch had no commits and 2 changed files. The successor cannot message it. Orchestrator 4 forwards its result if its own chat is still alive. Otherwise, when `zterm/dnd` has a commit and a clean tree, verify and merge it. If it stalls, start a fresh worker that continues from the files on disk. 06 also owes one fix: the palette and confirm scrims must cover the whole window, sidebar and titlebar included.
- **13, browser-like bold synthesis, is APPROVED** by the user (native rebuild rev 14, "yeah go ahead"). See [ticket 13](../.scratch/zterm/issues/13-browser-like-bold-synthesis.md), with Chrome's rule cited from Chromium source. Build it like rev 13 (`.scratch/gpui-perf/issues/05-native-rev-perf-and-weight500.md`, `reference/README.md` "Build and measure"). Bit-exact fixtures stay unchanged unless the change is intended.
- **09, the parity review, comes after 06.** Compare against the live zgui build: `%TEMP%\zgui-term-yXn8\zgui`, patched; run `cargo run --release --locked -p zgui-desktop --features ghostty --example terminal`. If %TEMP% was cleaned, re-clone at c804b80 and apply `research/zgui-terminal/zgui-windows.patch`. Collect every "limitations met" note from tickets 03-08 and 10-13.
- **Owed hand checks** (the user ran the app once and only reported the bold): snap layouts when hovering maximize, double-click maximize, the caption hover colours, and DPI above 100%.

Notes:
- `reference/ce-source` is copied into this worktree (gitignored).
- Worktrees left: `gpui-go-wt-base` (main), `gpui-go-wt-zterm-dnd` (06), and the main checkout `gpui-go` (another chat's uncommitted `fast-test-loop` work; never touch it).
- Captures: `ZTERM_CAPTURE_DIR=<dir> [ZTERM_CAPTURE_STATES=a,b] go test ./examples/zterm -run TestCapture`. Run the app with `go build -o %TEMP%\zterm-run\zterm.exe ./examples/zterm`, then start it.

Conventions:
- At most 3 workers, each in its own worktree. Verify their claims on the merged tree.
- No push or tag without the user. A native rebuild only with the user's approval (13 has it).
- When the user is gaming: no visible windows.
- Never set `GPUI_REAL_INPUT_TESTS=1` for a full run while the user uses the PC; it drives the real mouse and keyboard.
- PowerShell `Get-Content`/`Set-Content` garble UTF-8 (⌘, →). Use `[IO.File]::ReadAllText`/`WriteAllText` with UTF-8 without BOM, or the Write/Edit tools.
- Report to the user in plain, short updates.
**ORCHESTRATOR 4 STATUS (2026-10-23; history now):** the user scoped it as UI only ("only the UI, skip logic"), Windows only, placeholder panes. Map and tickets 01-09: [.scratch/zterm/map.md](../.scratch/zterm/map.md). zterm's Luau source is copied, read-only, to `C:\Users\ADMIN\Desktop\nguyenvu\zgui-zterm-src\`. 01 (audit) and 02 (zgui runs on Windows with a patch; the user approved cargo) are resolved and merged. Worktrees (2026-10-23, user asked to clean up): only `gpui-go-wt-base` (main), `gpui-go-wt-zterm-tabs` (W7) and the main checkout `gpui-go` (another chat's uncommitted `fast-test-loop` work; never touch) remain; `gpui-go-wt-tailwind` (clean, merged) and the empty `gpui-go-wt-flake` folder were removed. Resolved and merged: 01, 02, 03, 04, 05, 07, 08, 10, 11, 12. Running: 06 drag and drop (`gpui-go-wt-zterm-dnd`, `zterm/dnd`, W10). Then 09. Ticket 10's on-screen tests pass (6acee58); the app needs `DirectComposition` for the acrylic tint. The session restarted once; W7 (ticket 04: titlebar/tabs/sidebar/Geist, worktree `gpui-go-wt-zterm-tabs`, branch `zterm/tabs`) was resumed by SendMessage and is running. Next: 06 (drag and drop) after 04, then 09 (parity report against the live zgui build in %TEMP%\zgui-term-yXn8\zgui). Full corpus (`go test -p 1 ./... -count=1`, real windows, no real-input tests) on main at 2f1b913: 52 packages ok, 0 failures; `reference/ce-source` was copied in for headlessspec. Never set `GPUI_REAL_INPUT_TESTS=1` for a full run while the user uses the PC (it drives the real mouse and keyboard).

**HANDOFF 2026-10-23 (orchestrator 3 → orchestrator 4). History now.** The predecessor is the Roboco chat this was launched from (orchestrator 3).

**State: all done and pushed, on `main` at e25da53.**
- The Base port is complete. v0.2.0 is tagged on 7a1b25b.
- gpui-perf tickets 01-08 are resolved (`.scratch/gpui-perf/`):
  - native DLL rev 13 (`0b6e9265…`): the weight-500 face, batched scene insert, and a waitable swapchain with maximum latency 1
  - the glyph raster cache
  - load-once
  - allocation cuts
  - view caching (`gpui.CachedView`)
  - the scene-leak fix
  - ticket 08: the inset flake was a test-teardown race; both cleanups now poll `Alive` directly
- Results, per frame: the text page went from 200 to 28 ms and 1,000 divs from 11.3 to 7.5 ms (`evidence/perf/combined-final.json`).
- The full suite passes, 51 packages, once `reference/ce-source` has been copied in from `C:\Users\ADMIN\Desktop\nguyenvu\gpui-go\reference\ce-source` (gitignored).
- Worktrees in use: this one (on `main`), `gpui-go`, and `gpui-go-wt-tailwind` (another effort; leave it alone).

**New goal (the user, 2026-10-23):** "the zgui has an example app: the terminal … that's our next target. see if we can recreate that app on gpui-go, and that will test it." Start with [the research brief](../research/zgui-terminal/README.md). Its key facts:
- zterm is `zeronsh/zgui` `crates/zgui-desktop/examples/terminal.rs` (@c804b80). It is macOS-only.
- Each pane embeds Ghostty, which does the grid, glyphs and IME; zgui only draws the shell around the panes (tabs, splits, the ⌘K palette, the sidebar, the theme).
- So gpui-go must render the terminal grid itself: a ConPTY session through `x/sys/windows`, plus a pure-Go VT emulator (e.g. `charmbracelet/x/vt`).
- Follow Zed's `terminal`/`terminal_view` design for GPUI parity: style-run batching, merged background rects, and PTY event batching.
- Known gaps:
  - `ShapeLine` rejects a forced cell width
  - the cap of 256 live shaped lines
  - about 31 ms per 300-line text frame, so cache each pane and repaint only changed rows
- The perf comparison should run on this PC against Zed's and zeron's terminals, not zterm.

The first step is to plan it with the user (Wayfinder map under `.scratch/`), then implement.

**Conventions:**
- At most 3 workers, each in its own worktree.
- Verify worker claims on a snapshot.
- No push or tag without the user, unless they hand an effort over.
- A native DLL rebuild needs the user's approval.
- When the user is gaming, no visible windows (see the skip lists below).
- Heredocs with quotes break in the Bash tool here, so use the Write tool.

**HANDOFF 2026-10-23 (orchestrator 2 → orchestrator 3). History now.** The predecessor is the Roboco chat "GPUI Base port (orchestrator 2, base-port-2)", id e5e2c030. It handed off at about 43% context, at the user's request.

**State**
- The Base port is COMPLETE on `base-port-2` (worktree `C:\Users\ADMIN\Desktop\nguyenvu\gpui-go-wt-base`).
- Every ticket in `.scratch/gpui-base/issues/` is resolved except 32's release step: tagging v0.2.0. That includes the follow-ups 36-44, which came from the coverage audit, the gallery, the user's cursor report and the tab question.
- Coverage: `go run ./conformance/cmd/basecoverage -check` passes. 1226 upstream crate tests: 1180 ported, 39 deviated, 7 not applicable, 0 gaps. 43 user-facing components.
- `base-port-2` is merged into `main` and PUSHED (origin/main = 7a1b25b, with ticket 44). The user approved the merge, the push and the credits. gpui-component is credited in NOTICE, THIRD_PARTY_NOTICES and gen.py. Symbol font fallback is left as is (it would need a native change).
- Real-window run: the user was present, and `CGO_ENABLED=0 go test -p 1 ./... -count=1` passed 50 packages. Only headlessspec fails, environmentally: it needs reference/ce-source, which is absent in this worktree.
- Gallery: `go run ./examples/base-gallery` (55 pages). The user looked at it and said "looks good".

**UPDATE 2026-10-23 (orchestrator 3)**
- **Bold:** done, with no Go fix. Weight 700 is the correct real Segoe UI Bold. Weight 500 (and 450-500) maps to Regular plus fake bold in the native layer (fontique font.rs:97-103, matching.rs:386, then native glyph.rs:1192-1199). The fix needs DLL rev 12, which is the user's decision. Evidence is in `%TEMP%\boldprobe\out\`.
- **v0.2.0:** tagged on 7a1b25b and pushed.
- **zgui / GPUI Fast research:** done, in `research/zgui-vs-gpui-fast/README.md`. Proposals await the user.
- **Corrections to the items above:** the shaping cap is 256 and evicts LRU since ticket 40. The 60k allocs/frame figure is unmeasured.
- **NEW EFFORT `.scratch/gpui-perf/`:** the user accepted every proposal, including ONE native rebuild for this effort ("try to fix all of them, im not using pc so its all urs"). Real windows are OK while the user is away. Three Roboco side-chat workers, each on its own branch and worktree, all branched from 9bec2ec:

  | Worker | Chat | Branch | Tickets |
  |---|---|---|---|
  | W1 | 33a2450b | perf-go-a | 01 benchmark, 03 load-once, 02 glyph cache, 06 allocations |
  | W2 | 92ed3c05 | perf-viewcache | 04 view caching |
  | W3 | 4e9dc0d8 | perf-native | 05 native rebuild: weight 500, bulk insert, waitable swapchain, optional Present1 |

  **DONE 2026-10-23.** All of tickets 01-07 are resolved and merged into base-port-2 at 8444628. The native DLL is now rev 13, SHA 0b6e9265…. The full suite passed 51 packages, headlessspec included, with ce-source copied in (gitignored). The orchestrator checked the merged tree on a snapshot: the bit-exact specs ran with 0 skips and no fixture changed.

  Combined results against the ticket 01 baseline, per frame (`evidence/perf/combined-final.json`):
  - text page: 200 → 27.6 ms, 129k → 2.8k native calls
  - 1,000 divs: 11.3 → 7.5 ms, 16.5k → 5.5k allocs

  Ticket 07 fixed a native-scene leak of one scene per frame, which predated this effort.

  Still open:
  - Ticket 08: an inset-shadow real-GPU test flakes about 1 in 60.
  - Present1 dirty rects: deferred. A damage source now exists in the bounds of view-cache-reused views.

**Open (history)**
1. The user said bold text looks different from a browser. A worker started by orchestrator 2 is investigating: Base's requested weight, real Segoe UI Bold vs synthetic bold, whether the weight reaches the rasterizer, and a side-by-side against plain DirectWrite, rendered offscreen. Orchestrator 2 forwards its result to you by Roboco message. If it's a Go-side fix, verify it (headless, with the bit-exact textspec/glyphspec fixtures unchanged), commit it, and merge it to main. If it needs native work, tell the user, because the DLL is frozen at rev 11.
2. Then tag v0.2.0 on main (annotated, same style as v0.1.0) and push the tag. The user approved the release; the tag only waited for the real-window run (green) and the bold result.
3. The user's NEW research request is your first task: "look at zgui and gpui-fast, to see how they made it so it's better than gpui". The user's chart (`C:\Users\ADMIN\AppData\Local\Roboco\profiles\local\uploads\9665e9a9-image.png`) compares zgui with gpui-fast, a fast GPUI variant, not base GPUI. zgui wins on:
   - CPU: idle 11x less; busy, heavy and animation 16-28% less
   - GPU: 12-63% less
   - memory: up to 8% less
   - smoothness: 16-19% smoother

   zgui loses on startup work (10% more) and app size (2.7x bigger).

   Find both projects. A local Zed-family fork called "zui" is at `C:\Users\ADMIN\Desktop\nguyenvu\zui`, with cargo checkouts `~/.cargo/git/checkouts/zui-*`; confirm whether that is zgui. gpui-fast isn't on disk. Explain the techniques behind each win: idle frame skipping, damage tracking, retained scenes, batching, glyph caching, present modes, allocation behaviour, and so on. Map them to gpui-go's known runtime costs: per-frame glyph re-rasterization (draw_text.go paintGlyphFromAtlas skips the raster_metadata cache), per-node/per-primitive FFI calls, ~60k allocations/frame at 1,000 elements, and the shaping-handle cap. Research only. Recommendations are proposals until the user accepts them (AGENTS.md). Use the research skill / docs/research-index.md conventions.

**Conventions that still hold**
- Max 3 workers.
- No cargo, and no DLL rebuild without the user.
- No push or tag without the user.
- When the user is gaming: no visible windows. Use `go test ./base -skip Real`, the gpui skip list `'Real|Win32|Desktop|Cursor.*Window|PowerBroadcast|EndSession|MouseWheelSettings|FreshWindow|WindowBorn|DragDrop|Clipboard|^TestRenderer|DirectManipulationSimulated|DirectManipulationDrain|DropTarget|ThemeChangeObserver'`, `-skip '^TestReal|OnRealGPU|RealWindow'` in the spec packages, and offscreen capture via `GALLERY_CAPTURE_DIR`.
- Verify worker claims on a `git checkout-index` snapshot.
- Evidence goes in `evidence/base/ticketNN-*.json`.
- Heredocs that contain quotes break in the Bash tool here; write files with the Write tool instead.


**Restart 2026-10-22 (this worktree):** the user re-authorized the port ("restart the GPUI Base port?" → "yea. go ahead"). Work happens in the worktree `C:\Users\ADMIN\Desktop\nguyenvu\gpui-go-wt-base`, branch `base-port-2` (from `tailwind-authoring` @ ab997d2 = main @ 8eed385 + the Tailwind layer, ADR 0001). Overrides below: **no push and no merge to main without asking the user** (commit per verified batch on base-port-2 only); at most 3 implementer workers at once (the user games on this PC); per-package tests only. Do NOT touch the main checkout `C:\Users\ADMIN\Desktop\nguyenvu\gpui-go` (another chat's uncommitted `fast-test-loop` work); its gitignored `reference/ce-source` may be READ by workers. headlessspec fails in this worktree because ce-source is absent (environmental). Upstream Base: `C:\Users\ADMIN\Desktop\nguyenvu\upstream-base\ce\crates\base\src`; legacy text API: `~/.cargo/git/checkouts/gpui-ce-6758b1db058c6497/beb5a6d`. Workers are background Agent-tool subagents (general-purpose). Inside Base components `Class(...)`/`gpui.Styles()`/`tw` are allowed, but upstream names/tokens come first.

Batches A+B (2026-10-22) RESOLVED and committed: 10 (gpui rich text + legacy text facade), 13 (base/*.go infra; map gap 2b = crates.io 0.2.2 defaults divs to Block, our pin to Flex), 15 (base/{rope,sumtree,segment,dataurl,strftime}, uax29 in go.mod, notices + gen.py GO_SOURCES), 35 (per-run paint in inline paragraphs; plain Text publishes ToRun), 14 (motion in base/animation.go + motion*.go), 19 (input data layer in base/input_*.go). Core API fix: AnyElement.PrepaintAt (PrepainAt kept deprecated). RESOLVED in batch C: 17 (2e348bf; gap-2b convention: constructors over a plain upstream div() call Block() first), 16 (3bfcc65; Init wires dialog/focus_trap/popover/sheet), 18 (8e99b9c). Workers must NOT spawn sub-agents (17's worker forked 3, exceeding the cap). Running (batch D): 27 markdown parse (owns base/text_{markdown,markdown_ext,document,node,mdast}.go, go.mod/go.sum, notices + gen.py; node.rs data model in 27, rendering half in 28), 31 plot (owns base/plot*.go + the Plot field in theme.go), 20 single-line input (owns new base/input_{kind,mode,blink_cursor,movement,state*,element,layout,base}.go + base/input.go; multi-line state.rs tests left for 21). Next frontier: 21 after 20; 22 after 20; 28 after 27; 30 after 28; 25 after 22.

**Successor orchestrator (2026-10-23, Claude Code session, took over from fb54f31d after the usage-limit stop):** batch D resumed with 3 fresh background workers continuing from the files on disk. 27 was audited at handoff: all 57 markdown tests ported (3 skip for html.rs parse → 30), 30/37 node.rs tests, so the worker finishes the rest or defers it to 28. 31 was missing the 11 appear.rs tests. 20 had 5 non-compiling files. The orchestrator put `//go:build basewip20` on base/input_{blink_cursor,kind,layout,mode,state}.go so `base` compiles for 27/31 (`go vet ./base` OK, 520 tests pass with 27+31 in). 20's worker builds with `-tags basewip20` and removes the tags when its files compile. **If a successor finds those tags still present, ticket 20 is unfinished.** 27 RESOLVED (66a7b55; 466 pass / 3 skip on the snapshot). 28 claimed and delegated (owns new base/text_{inline*,node_render,state,view,style,utils}*.go; may add render-only methods to text_node.go; told to stop at a compiling layer boundary if low on context). 31 RESOLVED (8631875; 67/67, 533 pass). 20 RESOLVED (be26ef8; 84/98 ported, 14 n/a; 618 pass; orchestrator wired inputInit in lib.go). 22 + 28 RESOLVED (747 pass on the snapshot, -count=3 ok). The core fix landed with them: App.Shutdown disposes the app image codec, which holds 1 of the 8 native atlas slots. Base test helpers (openTestView, textTestApp(t)) call shutdownAtCleanup. New tests that build a TestApp directly and decode images should do the same, or the real-window test runs out of atlas slots. Earlier batch E: 28 (TextView), 21 (textarea; owns the ticket-20 engine files + display map/inline tokens/textarea), 22 (input controls; owns number_input/otp_input/time_field/select/combobox/calendar/date_picker/color_picker + Init lines in lib.go; reports engine changes instead of editing input*.go). Still running: 21 (forwarded 22's NumberStep request; may edit base/number_input_step.go for it). 21 RESOLVED (824 pass on the snapshot). Batch F done: 25 (8a8a3d3), 23 (ca08122), 29 (6544feb); base at 1012 pass / 3 skip on the snapshot. Batch G running (9d989d7): 26 dock (base/dock*.go + base/testdata/dock/), 24 diagnostics/LSP (new editor_{diagnostics,lsp*}, lsp_types*; may edit input*/editor* engine files), 36 NEW ticket from 29's report: Root mounts TextSelectionLayer + Copy, ElementExt.TextSelectionScope, and a CORE fix in gpui/element.go:612 (paint must use the prepaint bounds, R:element.rs:537-600; may retire 28's paintOrigin workaround). 36 RESOLVED (3742f4c; base 1017 pass, gpui + 9 spec packages ok). 24 and 26 hit the usage limit again and were resumed with SendMessage after the reset. 26 RESOLVED (1169 pass on the snapshot). 24 RESOLVED (1188 pass). 30 RESOLVED (632c476; 1268 pass, 0 skip). Opened 37 (core Window.RequestAutoscroll + List autoscroll for in-block reveal, 8 reveal tests, and a per-row inline walk for paint speed). Batch H running (4b59fb8): 37, and 32 part 1 (coverage ledger tool conformance/cmd/basecoverage → evidence/base/coverage-ledger.json + COVERAGE.md, examples/base-showcase, README Base section). 32 part 1 COMMITTED (e1ca426): ledger totals 1238 upstream tests = 1180 ported, 38 deviated, 7 n/a, 12 out of scope (example apps), 1 GAP (text_view.rs:2227 linked-image clicks, handed to 37). The tool also found a go1.27.1 compiler deadlock for importers naming base.InputState and the related types, caused by the generic alias inputCx in exported signatures; fixed in 235e4f1 (the alias was removed). The showcase now has Input/Textarea/Editor (f84069d). Known core gap seen in the showcase log: the gpui image cache's SVG fallback for img() still reports 'SVG rendering is a deferred service' (gpui/imagecache.go:68, image.go:247) although the native SVG service landed in core ticket 19; wiring it is Go-only. Done as ticket 38 (bf04de3): img() renders SVG via RenderSingleFrame at the smooth 2x scale. 37 RESOLVED (c016a60; base 1280 pass, 0 skip). Coverage ledger regenerated (d53d71d): 1226 crate tests = 1180 ported, 39 deviated, 7 n/a, 0 gaps; `go run ./conformance/cmd/basecoverage -check` exits 0. ALL TICKETS EXCEPT 32's release steps are resolved. Later on 2026-10-23: full corpus done (all ok except headlessspec, which needs ce-source; the wheel test fixed in 38e24de). Ticket 39 (cursors) resolved (a022054): no bug; upstream Base Button/Link set no cursor, the styled crate does. examples/base-gallery committed (f543ba3; 55 pages, offscreen PNG capture via GALLERY_CAPTURE_DIR). The gallery found bugs → tickets 40 (core: app-atlas image sprites fail at present 'texture 0'; ~256 live shaped runs latch a permanent failure), 41 (TextView tables on one line, HTML inline paragraph empty, hard tabs as missing glyphs), 42 (Sheet clipped in scroll areas; symbol glyph fallback, maybe native). Batch I running 40/41/42 (c40f67f). 40 RESOLVED (window sprite atlas; shaping-handle LRU parking; no latch). 42 RESOLVED (matches upstream). Headless gpui skip list: 'Real|Win32|Desktop|Cursor.*Window|PowerBroadcast|EndSession|MouseWheelSettings|FreshWindow|WindowBorn|DragDrop|Clipboard|^TestRenderer|DirectManipulationSimulated|DirectManipulationDrain|DropTarget|ThemeChangeObserver'; rendererspec and textframespec show windows. A real-window re-run is owed when the machine is free. 41 RESOLVED (a719b61: table rows stack, HTML paragraphs keep their layout). 43 RESOLVED (negative wrap width clamped to 0 in gpui/text.go). USER ANSWERS (2026-10-23): release yes, credits yes, symbol fallback leave as is; tab question explained. DONE: gpui-component credited in NOTICE + THIRD_PARTY_NOTICES + gen.py (c331f60); base-port-2 merged into main (5d296f9 + d4490ac, README conflict resolved) and main PUSHED (09a4978..d4490ac). Ticket 44 (tabs) RESOLVED (0f06b06) and merged into main + pushed (7a1b25b) at the user's request, without real-window tests. REAL-WINDOW RUN DONE (2026-10-23, user present): `go test -p 1 ./... -count=1` gives 50 packages ok, including the base TestReal*, the gpui real-host tests and the cursor gate; only headlessspec fails (needs ce-source, environmental). The user checked the gallery: 'looks good', except that bold text looks different from a browser, under investigation (worker). Tag v0.2.0 after that result. HELD: the v0.2.0 tag, until the real-window run passes after tickets 40/43 changed rendering (the user is gaming; ask before tagging if they prefer). Earlier list, kept for history:  (a) hard tabs paint as missing glyphs under the pin (Base's 0.2.2 line drew blank space): fix in gpui or keep the pin? (b) merge base-port-2 to main, push, tag v0.2.0; (c) credit GPUI-CE and gpui-component in THIRD_PARTY_NOTICES; (d) automatic symbol fallback would need a native change (ticket 42). Owed when the machine is free: a real-window test run (base TestReal*, gpui real-host, rendererspec, textframespec, the cursor gate) and the hand checks listed in the ticket 39/40 evidence. THE USER GAMES ON THE MAIN MONITOR: no visible windows — base tests with `-skip Real`, gpui real-host tests skipped, offscreen capture only. Next: the full corpus `CGO_ENABLED=0 go test -p 1 ./... -count=1` (output in /tmp/fullcorpus.txt while it runs), then evidence/base/ticket32-*.json, then ASK THE USER: merge base-port-2 → main (main is at ea48343, local only), push, tag v0.2.0, and whether to credit GPUI-CE/gpui-component (Apache-2.0) in THIRD_PARTY_NOTICES for the Go ports. Environmental class added: TestWin32InputRealWindowWheelScrolls fails while Shift is physically held (GetAsyncKeyState showed it). Open question for the user at release: THIRD_PARTY_NOTICES credits GPUI-CE only via the DLL section; the Go ports (gpui/, base/ from gpui-component crates/base Apache-2.0) aren't credited there. After both: regenerate the ledger (`go run ./conformance/cmd/basecoverage`, then delete the 11 now-unused reveal overrides in overrides.go), then the orchestrator runs the full corpus `CGO_ENABLED=0 go test ./... -count=1` (-p 1 for the capture gate), then ASKS THE USER before merging base-port-2 to main, pushing, or tagging v0.2.0. 30 claimed and delegated (06f5274): html/minifier/range highlight/stream fade, x/net/html into go.mod + notices, 65 tests deferred from 28, and the 3 markdown HTML skips. Then 32 (audit + showcase + v0.2.0; release steps need the user).

Read [.scratch/gpui-base/map.md](../.scratch/gpui-base/map.md) first — the gap map, the waves and the decisions. Branch `base-port` (commit per verified batch; merge to main + push at wave ends; v0.2.0 tag at the end). Authorization: the user approved the plan, the dependency policy (goldmark, uax29, x/net/html as modules; rope/sum tree/lsp types/strftime/search/data-url recreated) and the frozen-artifact fallbacks (ticket 02) with "sounds good then" (2026-10-19). The native artifact stays FROZEN at rev 11 — a genuine native gap is a decision for the user, never a rebuild.

Pattern (same as the core): the orchestrator claims tickets in `.scratch/gpui-base/issues/` before delegating; parallel background implementers with LEAN prompts and strict disjoint file ownership (they report needed changes in files they don't own instead of editing them); the orchestrator re-runs tests itself — on a `git checkout-index` snapshot of exactly the staged files when other workers are mid-edit — then writes `evidence/base/ticketNN-*.json` + `## Answer` + the map gist, and commits per batch. Snapshot-only artifacts: headlessspec needs the gitignored `reference/ce-source` (fails in a snapshot copy, passes live). Known environmental classes: the cursor gate (TestCursorStyleAndVisibilityAppLevel / TestCursorHideUntilMoveOnRealWindow / TestSetCursorMessagePolicy — fail identically on the pre-wave commit), the capture pixel gate under parallel packages (-p 1), clipspec under clipboard contention (rerun).

Wave 1 (core completion, Go-side) state: resolved 03, 04, 05, 06, 07, 09, 11, 12, 33, 34; 08 (full corpus green); 10 (rich text) in progress. Then wave 2: 13 (base package infra), 15 (data structures, adds uax29), 14 (motion).

## Core effort (complete; historical)

Working directory: `C:\Users\ADMIN\Desktop\nguyenvu\gpui-go`. Updated 2026-10-11 by the wave-4 orchestrator chat (the user's standing pre-authorization: "spawn a new chat as orchestrator, and try to implement everything, dont need to ask me pls, just implement everything" — INCLUDING the native rebuilds; harness `pi`, model `iroha/dashscope/glm-5.3`, branch `implement-core-remaining`). THE BACKLOG IS COMPLETE (34/34, 2026-10-11). Batch 8 = the third bounded native rebuild (the wave-4 native surface: the a11y + capture services, the renderer v3 device-loss/render-to-image entries, the real capture surface import, AtlasKey::Svg, fx-0008, the cursor-gate fix) + the Go FFI clients. Batch 9 = the four parallel Go-side waves (the A11yProvider, the capture model, the device-loss controller, the render-to-image model — delegated to background implementers). Batch 10 = the rev-11 fix-up rebuild (the workers' four honest native findings: the scene surface export gate, the ERR_RECOVERING status collision, the device-rebuild resource order, the a11y action-mask decode) + the tickets 22/28/29/32 resolutions. Batch 11 = ticket 30 (the shutdown-integration corpus; the worker also fixed the terminal-retirement dispatch cancelled by App.Shutdown's drain and wired the real-window element-runtime teardown). Batch 12 = ticket 33 (the final audit: the ledger reconciliation COMPLETE 6911/6911, the visual calibration with the committed tools, the full corpus on rev 11). Predecessor: wave-3 chat `3a946d3c` (batches 4-7: tickets 18, 23, 24, 27, 19). Wave-2 chat `efb5f590` (batches 2-3).

## Implementation authorization (supersedes all older planning-only restrictions)

**2026-10-05 — full implementation authorized** ("ok now use roboco mcp, create a new chat that use /implement skill to do all those tickets"). This chat and its predecessor implement ALL tickets in `.scratch/gpui-core-implementation/plan.md` (01–34) until complete or a concrete external blocker. Routine engineering decisions are delegated; do not re-ask whether to start/continue.

**2026-10-06 — user hard constraints (successor-chat handoff doc):**
- **Minimize Rust. NO cargo/DLL rebuilds without explicit user approval.** Pure-Go/Win32 tickets by default. Tickets that would want DLL/native additions: 17 (image codecs), 19 (SVG/resvg), 22 (AccessKit UIA), 28 (capture device exposure — the renderer ABI has no device/shared-texture export; `SurfaceSource::Unsupported` stand-in returns `ERR_UNSUPPORTED_SOURCE`). Defer these or propose pure-Go/Win32 alternatives per-ticket; surface to the user at checkpoints. Pure-Go path remaining without them: 13, 14, 15, 18(partially—needs 17), 20, 21, 23(needs 17), 24, 25, 26, 27, 30(partial), 31, 32(needs 28/29)… see frontier below.
- Resource care: keep artifacts small; never full-workspace `cargo test`; if a rebuild is ever approved: `-j 4`, idle hours only.
- Subagent discipline: parallel implementers with LEAN prompts (large prompts correlated with aborts); never auto-retry aborted subagents — surface and continue elsewhere or do the slice in the orchestrator.
- **Git:** remote https://github.com/hoangvu12/gpui-go (PUBLIC), branch `main`; commit and push after each resolved ticket batch. Work happens on `implement-core-remaining` (draft PR referencing spec+plan; created after the first batch commit since GitHub needs a diff); each batch merges to `main` and pushes. **No LICENSE file — the user must choose one (GPUI-CE is Apache-2.0); do not add one.**
- Verification: orchestrator re-runs tests itself; full suite `CGO_ENABLED=0 go test ./... -count=1` (18 packages green at handoff, 2026-10-06). Plain `go test ./...` for quick cached iterations.

`reference/ce-source` and `reference/target` are gitignored (re-fetch via reference/README.md). The embedded DLL and recorded traces are committed, so Go-only work needs no Rust. `.gitattributes` pins `* -text`. Toolchain: Go 1.27.1 (GOTOOLCHAIN=local), CGO_ENABLED=0 everywhere (race detector unavailable; forced-GC tests cover memory hazards).

## The delegation pattern (KEEP USING IT)

Parallel background implementer subagents (Agent tool, `background: true`, LEAN prompts: ticket path + contract pointers + pinned-source pointers + file ownership + per-package test command + "no git, no cargo, honest deviations"). Sessions resumable, but never auto-retry aborts.

1. Disjoint file ownership per subagent; same checkout (no worktrees — proven pattern). Subagents keep the `gpui` package compiling at all times; they run ONLY their packages (`CGO_ENABLED=0 go test ./gpui ./internal/<spec>` — never `./...` while others run).
2. Oracle-first applies to native-seam tickets (fixture kind + recorded trace + bit-exact `conformance.CompareTraces` gate). Go-only tickets verify with spec packages whose exact expectations derive from pinned CE semantics + existing fx gates stay green (no new recorded fixtures without Rust approval).
3. Orchestrator (this chat): claims tickets in the issue files before delegating, verifies subagent claims by re-running tests, resolves tickets (`## Answer`, evidence link), writes `evidence/ticketNN-*.json`, updates `evidence/README.md` + this note, commits and pushes per batch.
4. Ticket lifecycle per docs/agents/issue-tracker.md: `Status: claimed/Assignee` before work; resolve only with acceptance evidence.

## Completed (28 tickets, all with executed evidence in evidence/)

| Ticket | Result | Gate |
|---|---|---|
| 01 reference harness + ledger | pinned CE oracle, envelope/trace v1, capability ledger (6911 rows, schema @2) | fx-0001 mismatch demonstration |
| 02 native bootstrap | DLL loader (embed/bundle/cache, SYSTEM32-only, verified handle), bootstrap ABI | round trips + 16 loader tests |
| 03 entities/effects | real root gpui runtime (entities, leases, scopes, events, effects, executors) | fx-0001 effects 47/47 |
| 04 scoped tasks | exactly-once delivery/discard, generation gating, shutdown drain, seeded schedules | 24 tests |
| 05 Win32 window host | OLE-STA/PerMonitorV2 host, two-window lifecycle, pooled-thread WM_QUIT deadlock fixed | 7 real-window tests, 10/10 runs |
| 06 layout roundtrip | taffy 0.13.0 native service + Go adapter with pinned rounding | fx-0001 54/54 + fx-0002 63/63 bit-exact |
| 07 present+retire | D3D11 renderer service, both swap-chain modes, event-query retirement | real-GPU ledger proofs (RTX 5050) |
| 08 scene kernel | pinned scene.rs/plan.rs native kernel + Go paint API | fx-0003 142/142 bit-exact |
| 09 text geometry | Parley stack native service + Go text adapter | fx-0004 339/339 bit-exact |
| 10 draw text | DirectWrite glyph raster + D3D11 atlas service + Go draw path (ported pinned rasterizer with citations) | fx-0005 + real-window pixel verification |
| 11 authoring counter | typed counter + fluent custom components + two-window corpus | fx-0006 + authorspec |
| 12 action registry | typed actions, NoAction/Unbind reference semantics, boxed clones | 27 tests |
| 16 paths/filters | scene v3 path kernel + renderer v2 full draw pipeline (68 prebuilt DXBC, MSAA paths, filters, surfaces pipeline, staging readback) | fx-0007 428/428 bit-exact + pixel-verified composite draw |
| 34 ledger triage | 351 support rows dispositioned; regeneration deterministic | — |
| 13 focus/keyboard | focus/keys/keymap/dispatch runtimes + the gpui_windows keyboard path (accelerator pre-dispatch, AltGr/dead-key translation, surrogate WM_CHAR, layout reports) | 17 reference test ports + focusspec 12 tests (10 deterministic transcripts incl. two-window isolation + 2 real-host input evidences) |
| 18 asset loading | the pure-Go asset machinery over the frozen rev-8 codec: registry (duplicate diagnostics, embedded/pre-loaded/on-demand), HTTP policy (Null default zero-attempt, Blocked permission errors, explicit net/http adapter), three-state image cache with shared-task dedup + scope cancellation/result discard, rational-delay animation with reduce-motion + 200ms loading delay | assetspec 46 tests + gpui 150 green |
| 23 clipboard | the Win32 clipboard exchange over the real system clipboard: six registered formats, multi-entry text/metadata (seahash-gated)/files/images, CF_DIB→BMP, exactly-once guard/lock, HGLOBAL ownership transfer, busy/malformed handling, primary/find-pasteboard unavailable, real PowerShell native interop | clipspec 11 tests |
| 24 drop/touchpad | the OLE drag/drop port (IDropTarget/IDataObject/IDropSource over the real DoDragDrop modal loop, FileDropEvent routing + hitbox/mouse dispatch through the real element tree, RevokeDragDrop exactly once, generation-checked stale-callback refusal) + the whole direct_manipulation.rs module with simulated-gesture dispatch and the honest no-touchpad record; the orchestrator's input-routing watchdog hardening | 16 tests (11 gpui + 5 dropspec) |
| 27 inspector | the live element-state inspector + debug diagnostics: the DiagnosticCapability switch (cfg/feature mapping, release profile reproduced), long ids with per-frame instances, the tree build/publication with value-only snapshots, picking over the real hit test, the debug frame overlay port, the leak/cycle detector over the real entity map; the orchestrator-wired dispatch seam + integration test | 30 tests (25 inspectorspec + 5 gpui) |
| 22 a11y/UIA | the pinned AccessKit provider service + the Go A11yProvider (the snapshot publication, WM_GETOBJECT routing, the listener + built-in action dispatch) | a11yspec 9 + the real Narrator evidence |
| 28 capture | the Windows Graphics Capture producer on the renderer's device + the capture model with the full pixel-verified draw loop | capturespec 11 (real monitor capture) |
| 29 device loss | the bounded-retry device rebuild + the recovery controller | devicelossspec 10 (injected loss, end-to-end) |
| 32 render-to-image | the pinned test-support path + the headless profile trace | headlessspec 9 (the pin's own test patterns) |
| 30 shutdown | the cross-service close/cancellation/shutdown corpus + two Go fixes (the retirement dispatch, the element teardown) | shutdownspec 11 + TRACES.md |
| 33 final audit | the ledger reconciliation (6911/6911), the visual calibration (noise 0, defects detected), the full corpus | the committed audit + calibration tools |
| 19 SVG | the user-approved second DLL rebuild: the SVG service in slot 8 (the pinned resvg/usvg 0.48.1 through the public SvgRenderer paths, the three sizing modes with the smooth 2x, the 8192 clamp, BGRA, the alpha-mask entry, the NeedsFontAssets font surface) + the Go client + the SvgRenderer model (exactly-once font orchestration) + the tinted-mask Svg element | 6 native + 14 svgspec tests |

**Current native artifact (rev 11, the wave-4 fix-up):** DLL 18,861,056 bytes, sha256 `37ee6877023dda0635544555639c9a68100e44d5c3790aad8613e4c938ed712e`, native revision 11, capability mask 0xFFF (bootstrap/layout/renderer/scene/text/glyph-raster/glyph-atlas/image/svg/**a11y/capture**), static CRT, OS-only imports; the reserved slot array holds 16 entries (slot 8 = SVG, slot 9 = a11y, slot 10 = capture). The rev-11 fixes: the scene surface export gate accepts the tag-1 capture payload (the 64-bit frame handle packs across the two reserved words), ERR_RECOVERING is -13 (the rev-10 -12 collided with rendererErrScene), the device rebuild takes the old resources before recreating (the in-place rebuild now succeeds), and the a11y action mask is a tag-presence set. Reference harness: `reference/target/debug/gpui-reference-harness.exe`. Fixture kinds v1: layout-effects, layout-metrics, scene-painting, paths-filters, text-geometry, glyph-raster, authoring-counter, **image-codecs (fx-0008, landed)** (fx-0001..0008 all byte-deterministic and reproduced bit-exactly; the distinct **AtlasKey::Svg variant landed** too — both recorded follow-ups are DONE).

## Final state (2026-10-11, after batch 12 — THE BACKLOG IS COMPLETE)

**Resolved: 34/34 (all tickets 01-34).** Ticket 33's audit: the capability-ledger reconciliation is COMPLETE (6911/6911 rows map to executed evidence — the committed conformance/cmd/auditreconcile), the visual calibration ran with the committed conformance/cmd/visualcalib (the measured repeat-run noise is ZERO, the material injected defects are all detected, the exact gates run at threshold 0 justified, and the ~98% intent stays intent — never an SSIM percentage), and the complete corpus on the final rev-11 artifact is green apart from the documented environmental classes. Remaining work is recorded in evidence/ticket33-full-core-audit.json (the release publication the user decides — no LICENSE file added; the visual-noise campaign over non-deterministic content; the mixed-COM heap-corruption root cause; the cursor gate's reboot re-validation). ENVIRONMENTAL NOTES: the post-fullscreen shell state leaves the desktop layer winning mouse hit tests system-wide until a reboot (the ticket26 cursor gate fails honestly in it and passed when the shell was healthy); the live-capture pixel gate requires serial execution (-p 1 — the monitor content is shared across parallel test binaries); Narrator (launched during the ticket-22 evidence) is a protected process — the user can toggle it off with Win+Ctrl+Enter.

## Historical wave state (2026-10-06, predecessor chat)

Wave 1: all four subagents aborted on connection errors (~18-20 min in); the orchestrator resolved 13 in-session and salvaged 15's tree. Per the no-auto-retry rule the remaining slices (15, 25, 31) were taken in-session; wave 2 (this chat) re-delegated 25/31/21/20 under the same pattern with the user's fresh authorization — all five workers have now run past the wave-1 abort window without connection failures.

## Verification state (2026-10-11, at the batch-7 commit)

`CGO_ENABLED=0 go test ./... -count=1` — the batch gate: 29 packages ok with only the documented environmental cursor class (ticket26 hover gates, fails under the machine's interactive use, passes isolated) and the consumerspec provenance check, which evidence/ticket19-svg-fonts.json resolves (the drift detector working as designed: the new SHA must appear in the evidence records). `go vet ./...` clean, gofmt clean, `go build ./...` green. fx-0001..fx-0007 all pass unchanged after the artifact rotation to revision 9 (the bit-exact property held through both rotations now); the rotated-artifact regression set (layoutspec/scenespec/glyphspec/textspec/imagespec/native) green. clipspec tests DO touch the real system clipboard by design; svgspec + internal/native touch the real rebuilt SVG service (rev 9).

## Key conventions and gotchas

- Envelope JSON keys snake_case (Rust serde defaults); effect ops kebab-case tagged; trace f32 values `"f32:XXXXXXXX"` (8 uppercase hex); CompareTraces exact (first mismatch names event seq/field).
- Reference window scale factor 2.0; rem = 16 logical px; measurement snapping `ceil(max(0,x)*scale)`; rounding midpoint-toward-zero (util.rs).
- Recorded traces embed machine-dependent facts (Segoe UI etc. via pinned Parley store; FontId canonical bits) — deterministic on THIS machine; evidence records the environment.
- OLE STA required on the foreground host; winhost tests open REAL windows — must FAIL not skip if window creation fails.
- Ledger: `go run ./conformance/cmd/ledgergen -scan reference/out/api-scan.json -out conformance/ledger/generated -profiles-dir reference/out/profiles` (schema @2).
- Rust (only with approval): `cd reference && RUSTFLAGS="-C target-feature=+crt-static" cargo build --release -p gpui-go-native --target x86_64-pc-windows-msvc -j 4` then `go run ./reference/native/tools/measure -dll reference/target/x86_64-pc-windows-msvc/release/gpui_go_native.dll` (refreshes manifest + embedded DLL). Update the cumulative assertions additively — the full set learned across the two rotations: internal/native/abi.go (nativeRevision + the reserved array if the slot table grows + the layout comment), layout_test.go + glyph_test.go + scenespec + glyphspec (the capability mask), native_test.go (the record sizes/offsets when the ABI table grows), consumerspec/evidence.go (the SHA/bytes/revision constants — and the new SHA must appear in an evidence JSON for the provenance check). Harness builds fast in debug.
- CE pin `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a` (reference/ce-source, zero patches). Zed `a84689073d296dfd39987bc7dd478e43ef76d83a` comparison only. ~98% visual closeness is intent, not a threshold; calibration is ticket33.

## Historical planning notes

See [PROJECT.md](../PROJECT.md) and the sections below: Wayfinder map, specification, conformance contract/inventory, 34-ticket plan.
