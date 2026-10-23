# 03 - Missing cursors and frames for hidden windows

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: pi implementer (worktree core/02-03-small)
Blocked by: none
Triage: ready-for-agent

**What to build:** The grab, grabbing, copy and context-menu cursor styles show the right Windows cursor, and minimized or occluded windows stop drawing frames.

**Blocked by:** None - can start immediately

**Status:** ready-for-agent

- [x] Each of the four cursor styles maps to a loaded system cursor (test of the cursor table).
- [x] A minimized or occluded window schedules no frames until it is visible again; an animation resumes correctly afterwards.
- [x] Existing frame-loop tests still pass.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

Implemented on `core/02-03-small` (commit: see the browser-core branch history). All Go; no native rebuild.

**The four cursors.** Windows ships no system cursors for grab, grabbing, copy or context-menu (that is why the reference's loader maps them to the arrow, `gpui_windows/src/util.rs:114`), and Chromium draws its own `.cur` resources for them. The port builds them at runtime:

- `gpui/desktop_cursors_windows.go` (new) — the glyph table `cursorGlyphs` (`:53-209`) with one frozen 32×32 monochrome bitmap per style (`#` black, `o` white, `.` transparent): an open hand for grab (hotspot 15,19), a closed fist for grabbing (15,15), two sheets with a plus badge for copy (1,4), an arrow with a menu panel for context-menu (0,1); `cursorPlanes` (`:264-291`) encodes the AND/XOR planes CreateCursor takes; `builtCursorHandle` (`:233-261`) creates each HCURSOR once and caches it for the process lifetime (the research's recommended alternative to bundling `.cur` assets — CreateCursor is the monochrome sibling of CreateIconIndirect and needs no GDI bitmaps).
- `gpui/desktop_windows.go:478-486` — `loadCursorHandle` routes the four styles to the built cursors; the system-resource table `cursorResourceID` (`:448-474`) is unchanged (still pinning util.rs's arms, arrow for the four).
- `gpui/desktop.go:78-99` — the four `CursorStyle` docs no longer claim the arrow fallback.
- Known limitations (documented in the file): monochrome only, 32×32 at 96dpi (no DPI scaling, unlike LR_DEFAULTSIZE system cursors), hotspots chosen by hand (arrow-tip convention for context-menu, palm center for the hands).

**Frames for hidden windows.**

- `gpui/desktop_windows.go:635-656` — `hostWindow.frameVisible()`: hidden = minimized (the WM_SIZE `SIZE_MINIMIZED` tracking plus `IsIconic` as ground truth) or DWM-cloaked (`DWMWA_CLOAKED`, `dwmwaCloaked` at `:164` — the occlusion Windows reports to an app: another virtual desktop or a shell-hidden window; a failed query fails open). DWM cloak is the only occlusion signal available to the Go host — ordinary occlusion by a window on top only surfaces through the app-owned present's `DXGI_STATUS_OCCLUDED`, which the core's present hook never sees.
- `gpui/frameloop_windows.go:123-152` — the frame pacer's posted invalidation skips hidden windows: instead of `InvalidateRect`, the window's demand is put back into the pacer's pending set, so the pacer re-checks at every vblank-paced frame slot (one `IsIconic`/DWM query per vblank while demand is hidden — the reference's vsync thread invalidated every window on every vblank, so this is strictly cheaper) and the pending wake, dirty flag or next-frame callbacks are served the moment the window is visible again. Chromium's rule: the display scheduler draws only for visible windows (`components/viz/service/display/display_scheduler.cc:602-607`).

**Tests** (all green: `go vet ./gpui/... ./base`; `gofmt -l gpui base`; `go test ./gpui/... -count=1`; `go test ./base -count=1`; `go build ./...`):

- `gpui/desktop_cursors_windows_test.go:27-56` — `TestCursorBuiltStylesLoadDistinctCursors`: the ticket's cursor-table test — each of the four styles loads a non-zero cursor, distinct from the arrow and from each other, stable across calls (the cache); the pointing-hand system cursor still loads.
- `gpui/desktop_cursors_windows_test.go:59-107` — `TestCursorBuiltGlyphArt`: the glyph table pins — 32×32 rows over the three cells, hotspots in-bounds on ink, white cells never touch transparent (no ragged edges), no blank glyph.
- `gpui/desktop_cursors_windows_test.go:109-146` — `TestCursorBuiltPlanesEncoding`: the plane encoder round-trips every cell (black AND=0/XOR=0, white AND=0/XOR=1, transparent AND=1/XOR=0, MSB-first, 4 bytes per row).
- `gpui/interact_windows_test.go:246-282` — the old arrow-fallback pin (`TestInteractCursorVariantsWithoutWindowsArm`) became `TestInteractCursorVariantsLoadBuiltCursors`: enum contiguity and the unchanged resource table still pinned, the fallback replaced by built cursors. (Was red before the fix: the four loaded the arrow.)
- `gpui/frameloop_windows_test.go:154-246` — `TestFrameLoopMinimizedWindowSkipsFramesUntilVisible`: a real window parked far offscreen (origin 20000,20000 — no visible window), minimize/restore driven by the same synthetic `WM_SIZE` messages a real minimize/restore delivers: while minimized an animation's frames are not scheduled (no draws or presents for 400ms while the demand stays pending), and after the restore every remaining animation frame is drawn and the window goes idle. (Was red before the fix: "a minimized window drew 5 frame(s)", the animation drained.)
- The existing frame-loop gates still pass: `TestFrameLoopRealWindowNotifyRepaints`, `TestFrameLoopRealWindowWithoutPresentHookDraws`, `TestFrameLatencyWaitableRealWindow` (the full waitable/DwmFlush comparison), `TestRealWindowActivationReachesObservers`, and every cross-platform frameloop test.
- No visible windows: the one new real-window test is offscreen; `GPUI_REAL_INPUT_TESTS` was never set.
