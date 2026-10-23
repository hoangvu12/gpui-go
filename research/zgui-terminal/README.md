# zgui's terminal example ("zterm") and how to recreate it on gpui-go

Researched 2026-10-23. Read-only. Sources:
- `zeronsh/zgui` @ `c804b803587e802cf077b1133cc05309bc95139f` (HEAD 2026-10-23), a shallow clone in `%TEMP%\zgui-term-yXn8\zgui`. Nothing was built or run. Paths marked `zgui:` are relative to that repo.
- `zeronsh/zeron` @ `dfc38967db72f59f271d5924a40e7dd466839928`, read through `gh api`. Paths marked `zeron:`.
- Zed @ `a84689073d296dfd39987bc7dd478e43ef76d83a` (this project's Zed comparison pin, PROJECT.md), read through `gh api`. Paths marked `zed:`.
- gpui-go @ `61d7ec0` (this worktree). Paths marked `go:`.

Labels: **[V]** means verified in source. **[I]** means my inference.

---

## 1. Where it is

[V] The app is **not** a separate `zgui_desktop` repo. It is an **example in the `zgui-desktop` crate** of the zgui repo:
- Entry point: `zgui: crates/zgui-desktop/examples/terminal.rs` (294 lines). Run line at `:3`: `cargo run --release -p zgui-desktop --features ghostty --example terminal`.
- UI: `zgui: crates/zgui-desktop/examples/terminal/` holds 25 Luau files (5,101 LOC) and 4 Rust modules (`session.rs` 1,426, `plugins.rs` 586, `program_status.rs` 304, `activity.rs` 260).
- Example plugins: `zgui: crates/zgui-desktop/examples/terminal-plugins/{editor,hacker-news,notes}` (1,541 LOC Luau).
- It ships as **"zterm"**. `zgui: scripts/zterm-app.sh` builds `zterm.app` with `--features ghostty,terminal-bundle` and bundle id `sh.zeron.zterm`.
- History: 8 commits, from `6048e7d` (2026-10-21, "Add a Ghostty terminal multiplexer example in Luau…") to `9a86131` (2026-10-22, "zterm: glass sidebar and tabs…").
- Tests: `zgui: crates/zgui-script/tests/terminal_example.rs` has 1,397 lines and about 30 headless UI tests (splits, tab drag, palette, plugins, layout persistence, and `resizing_the_window_moves_the_panes_without_running_script` at `:540`).

Other zeronsh repos (`gh repo list zeronsh`): zgui, zeron, zui, gpui-ce, gpui-component (fork), chat. `gh search code --owner zeronsh terminal` also finds a **GPUI-drawn terminal in `zeron`** (`zeron: crates/ui/src/terminal/*`), covered in §3.4.

## 2. What the app is

### 2.1 The terminal grid is Ghostty, not zgui [V]

This is the main finding. zgui itself draws no terminal cells.
- `zgui: crates/zgui/src/terminal.rs:16-20`: "The terminal is the platform's own view (a Ghostty surface on macOS, with the desktop host's `ghostty` feature), placed like a web page … It renders, reads the keyboard (IME included) and the mouse itself. Without a backend the node shows a labelled placeholder."
- `zgui: crates/zgui-desktop/src/native_host/ghostty.rs` (2,013 LOC):
  - `:4-11`: one Ghostty app per process; each terminal is a `ZguiTerminalView` NSView "that Ghostty renders into (Metal, its own thread)".
  - `ghostty_app_new` is at `:196` and `ghostty_surface_new` at `:1887`.
  - It loads the user's own Ghostty config (`:167-178`), then applies overrides at `:41-80`: it unbinds app shortcuts and sets `background-opacity = 0` so that zgui's glass shows through.
- `zgui: crates/zgui-ghostty-sys/`: `build.rs` (207 lines) fetches Ghostty at commit `9f0e1719dc918368367d368bfe300f59bb68b5a4` (1.3.2-dev, `:24`), patches it and builds it with **Zig 0.16**. `src/bindings.rs` has 2,026 lines of FFI.
  - `build.rs:62-63` panics on any target other than macOS: "macOS only for now".
- `zgui: crates/zgui-desktop/Cargo.toml:14-16`: the `ghostty` feature is "macOS only for now". `zgui: docs/windows.md:198`: "Ghostty terminals and `NativeContent::Platform` views are macOS only".
- So **zterm does not run on Windows**. On Windows the `ui.terminal` node shows a placeholder.

Consequences [V]:
- Ghostty provides all of the following: VT parsing, the PTY/child, the cell grid, the glyph atlas, the Metal renderer, scrollback, selection, copy/paste (Ghostty binding actions, `zgui: crates/zgui/src/terminal.rs:184-186`), the cursor and its blink, fonts and ligatures, IME (`NSTextInputClient` in ghostty.rs), mouse reporting, links, and the terminal colours (from the user's Ghostty config).
- zgui provides the chrome around the terminal.

### 2.2 PTY and sessions [V]

- **Server and client sessions.** Each pane's Ghostty surface runs the zterm binary itself with `--attach <id>` (`terminal.rs:35-43`; `session.rs:1-14`). That client relays bytes over a Unix socket to a **session daemon** (`--session-daemon`). The daemon owns the real PTYs through `libc::forkpty` (`session.rs:1330`). Shells therefore outlive the window, tmux-style.
- The daemon mirrors each screen with the **`vt100` 0.16** crate (`zgui-desktop/Cargo.toml:97-99`; `session.rs:7-8`, `:640`). It keeps 2,000 lines of scrollback (`session.rs:50`) so it can redraw a re-attaching client.
- The daemon is all Unix: `UnixListener`, `forkpty`, `SIGWINCH`, `termios`, `proc_listchildpids`.
- There is no alacritty_terminal, vte, portable-pty or ConPTY anywhere in zterm.

### 2.3 Features of the zgui-drawn shell around the terminals [V]

| Feature | Where |
|---|---|
| Tabs in the titlebar: an icon per pane, drag to reorder, lift a tab out, drop a pane on the strip to make a tab | `terminal/components/tabs.luau` (484) |
| Split panes as a tree (row/column), draggable dividers, drag a pane by its grip, zoom pane, equalize | `lib/layout.luau` (224), `frames.luau` (276), `components/pane.luau` (268), `divider.luau` |
| Pane edges as % + points, so a resize runs no script | `zgui: docs/scripting.md:445` |
| ⌘K palette: new terminal, browser or plugin panel, fuzzy filter | `components/palette.luau` (409) |
| Browser panes (WKWebView) | `components/browser.luau` (335) |
| "Agents" sidebar listing Claude Code and Codex runs by status, using OSC 7501 Program Status, OSC 9;4 progress, spinners and bell | `agents.luau`, `components/sidebar.luau`, `activity.rs`, `program_status.rs`; `zgui: crates/zgui/src/terminal.rs:51-116` |
| Terminal state surfaced: title (OSC 0/2), pwd (OSC 7), foreground process and pid, bell, progress, exit, command finished (OSC 133), notifications (OSC 9/777) | `zgui: crates/zgui/src/terminal.rs:51-173` |
| Menus and shortcuts: New Tab ⌘T, Split ⌘D / ⇧⌘D, Close ⌘W, Copy/Paste/Select All (→ Ghostty bindings), Clear ⇧⌘K, Bigger/Smaller/Actual ⌘= ⌘- ⌘0, next/prev pane/tab, ⌘1…9 | `commands.luau:66-161` |
| Layout and session persistence (`layout.json`, debounced 500 ms) | `terminal.rs:109-182` |
| Plugin system with a consent sheet and capabilities | `plugins.rs`, `components/consent.luau` |
| Theme tokens with a "terminal-glass" frosted dark theme, Geist UI font; the terminal font is Ghostty's | `theme.luau`, `glass.json`, `terminal.rs:53-60` |
| Hot reload of the Luau UI; shells keep running | `terminal.rs:4-6`, `:259-260` |

Not in zterm's own code, so either delegated to Ghostty or absent: scrollback UI, search, a settings UI, ligature control, link detection. [V for absence in zgui code; I that Ghostty's own defaults apply]

### 2.4 Benchmarks, screenshots, README [V]

- **Not used in any zgui benchmark.** There are no hits for terminal or zterm in `comparisons/`, `docs/benchmarking.md` or `site/`. The two hits in `scripts/` are unrelated wording.
- No screenshot of it is in `docs/images/`.
- `README.md` does not mention it. `docs/scripting.md:28, 151-164` documents `ui.term` / `ui.terminal` and calls the example "a multiplexer written this way".
- `docs/gpui-parity.md:101-103` keeps a "terminal emulator" out of zgui's framework-parity scope.

## 3. Mapping to gpui-go

### 3.1 What zterm needs and what gpui-go has

| Need | gpui-go today | Gap |
|---|---|---|
| PTY on Windows | None. No ConPTY / PseudoConsole code in `go:` (grep). No `golang.org/x/sys` in `go.mod`; Win32 is reached through `syscall.NewLazyDLL` (e.g. `go: gpui/ime_win32.go:39-55`). | New. See 3.2. |
| VT parser and screen model | None | New Go dependency or a port. See 3.2. |
| Cell-grid paint | `Canvas` (`go: gpui/canvas.go:27`), `Window.PaintQuad` (`go: gpui/paintapi.go:150`), `WindowTextSystem.ShapeLine` plus `ShapedLine.Paint` (`go: gpui/shaped_line.go:107, 351`), two-frame shaped-line cache (`go: gpui/text_cache.go`) | **`ShapeLine` rejects `forceWidth`** (`go: gpui/shaped_line.go:100-110`). Zed's terminal passes `Some(cell_width)` (`zed: crates/terminal_view/src/terminal_element.rs:163-167`). See 3.3. |
| Monospace font | System fonts through DirectWrite (Consolas is measured in `go: gpui/text_tabs.go:11-20`) | Fine. Ligatures follow from shaping. [I] |
| Keyboard to bytes | Keymap, actions, key dispatch (`go: gpui/keymap.go`, `key_dispatch.go`, `keyboard_win32.go`) | Port Zed's `crates/terminal/src/mappings/` (keys and mouse) |
| IME | `IMEInputHandler` (`go: gpui/ime.go:135`), `Window.HandleInput` (`go: gpui/input.go:60`), IMM32 | Implement the handler on the terminal view, as Zed's `TerminalInputHandler` does [I] |
| Clipboard | `App.ReadClipboard` / `WriteClipboard` (`go: gpui/clipboard.go:418, 434`) | None |
| Tabs | `base/tabs.go` (unstyled; no keyboard roving, `:15-17`) | The styling and tab drag behaviour need to be added |
| Splits with draggable dividers, tab groups, persistence | `base/dock*.go` (PaneTree, Split, MovePanel, persisted state) and `base/resizable*.go` | Probably enough for splits and drag-to-dock [I] |
| Palette | `base/combobox.go`, `popover.go`, `dialog.go` | Composition only |
| Scrollbar | `base/scrollbar.go`, `gpui/scroll*.go` | The terminal owns its scroll offset (display_offset), like Zed's `terminal_scrollbar.rs` |
| Cursor blink | `base/input_blink_cursor.go` | Reuse the pattern |
| Unchanged panes cost nothing | `gpui.CachedView` (`go: gpui/view_cache.go:410`) | Wrap each pane in it |
| Browser panes | None (zgui uses WKWebView/WebView2) | Out of scope for an MVP [I] |
| Glass window | `Window.SetBackgroundAppearance` (`go: gpui/window.go:516`) | Check that blur is supported |

### 3.2 Hard parts

**ConPTY with `CGO_ENABLED=0` [V API, I effort].**
- `golang.org/x/sys/windows` @ `f5cb030` exports `CreatePseudoConsole` and `ResizePseudoConsole` (`windows/syscall_windows.go:1871, 1878`) and `ClosePseudoConsole` (`:312`). It also has `ProcThreadAttributeListContainer` for `PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE`. All of it is pure Go syscalls.
- Ready-made options:
  - `github.com/charmbracelet/x/conpty` (MIT; it only requires `golang.org/x/sys`) and `x/xpty` (cross-platform)
  - `github.com/aymanbagabas/go-pty` (MIT, pushed 2026-10-13)
  - `github.com/UserExistsError/conpty` (MIT)
- To avoid the x/sys dependency, the same calls can go through the existing `syscall.NewLazyDLL("kernel32.dll")` pattern.
- Pitfalls:
  - Read the output pipe on a dedicated goroutine; a blocking `ReadFile` must never touch the UI thread.
  - Close the pseudo console before draining the pipe at exit.
  - ConPTY re-renders and rewraps on resize, so debounce resizes. Zeron debounces 80 ms (`zeron: crates/ui/src/terminal/panel.rs:13-15`).
  - Zeron needed its own ConPTY launcher with job membership because "portable-pty's Windows launcher runs the child before returning" (`zeron: crates/engine/src/terminals/windows.rs:1-3`).

**VT parsing and the screen model [V libs, I fit].**
- Pure-Go candidates (all MIT):
  - **`github.com/charmbracelet/x/vt`** @ `cb897cb`, active. A full emulator: screen, scrollback, modes, OSC, mouse, key encoding (`key.go`), and **damage tracking** (`vt/damage.go`: `CellDamage` / `RectDamage`). Its module needs Go 1.24, pulls in ultraviolet, x/ansi and uniseg, and requires `golang.org/x/sys v0.41.0` (indirect).
  - `github.com/danielgatis/go-vte`: a port of Alacritty's `vte` parser only (no grid).
  - `github.com/hinshun/vt10x`: small and stale (2023).
  - `github.com/liamg/darktile`: a whole app; stale (2023).
- [I] The GPUI-parity route is to port `alacritty_terminal`'s Term/Grid to Go (Zed uses it). That is large: Term, grid, selection, regex search and vi mode. Pragmatic order: start on `x/vt` behind a small interface shaped like `alacritty_terminal`'s (`feed(bytes)`, `cells(viewport)`, `cursor`, `selection`, `mode`, `scroll(display_offset)`), and keep the port as a later swap.

**Grid paint cost [V numbers, I projection].**
- Zed's approach (`zed: crates/terminal_view/src/terminal_element.rs`):
  - `layout_grid` (`:438`) walks visible cells once.
  - It merges adjacent same-style cells into a `BatchedTextRun` (`:95-130`) and paints each with `shape_line(..., Some(cell_width))` (`:149-175`).
  - Backgrounds become `BackgroundRegion`s merged into rects (`:271-388`, `:597-604`). The cursor is painted separately.
- Zed's PTY events are batched: the first is processed immediately, then up to 100 events or 4 ms per batch (`zed: crates/terminal/src/terminal.rs:1420-1470`). Default scrollback is 10,000 lines, maximum 100,000 (`:891-892`).
- gpui-go's measured costs (`go: .scratch/gpui-perf/map.md:20-26`): after tickets 01-07, a 300-line uncached text page costs about **31 ms/frame** and 8.4k allocs. A cached root-only redraw costs 20 ms. Present1 dirty rects are still deferred.
- [I] Roughly 0.1 ms per shaped line puts a full 50-row pane at about 5 ms per uncached frame. Four streaming panes are about 20 ms, so busy output would miss 60 Hz unless:
  - only changed panes repaint (`CachedView`), and
  - the shaped-line cache hits for unchanged rows. The cache is keyed on text plus runs (`go: gpui/text_cache.go:144-207`), so unchanged rows already hit.
- **Shaping-handle cap [V].** The cap is 256 live native shaping handles per process (`go: internal/native/text.go:107`), with LRU parking since ticket 40 (`go: gpui/text_cache.go:70-80`). With batched runs, a few panes of colourful output (several runs per row) can exceed 256 live lines per frame. Expect parking churn there; measure it early. [I]
- **`forceWidth` gap [V].** Without it, glyphs can drift off the cell grid with fallback fonts, wide (CJK/emoji) cells and box-drawing glyphs.
  - Workaround [I]: start a new run at every wide or non-ASCII cell and position each run at `col*cellWidth`.
  - Proper fix: native support for fixed advances, a native change under the pinned-DLL contract.
- [I] Better than Zed: `x/vt` damage lets the element re-shape only dirty rows.

### 3.3 Upstream Zed's design to mirror [V]

`crates/terminal` (238 KB `terminal.rs`, plus `alacritty.rs`, `mappings/`, `pty_info.rs`, `terminal_settings.rs`):
- It wraps `alacritty_terminal` (`Cargo.toml:24`) and its `tty::new` / `EventLoop`, which handles ConPTY on Windows (`alacritty.rs:11, 49, 171-188, 220`).
- `Terminal::sync` (`terminal.rs:2498`) snapshots the grid into `last_content`, which the element paints.

`crates/terminal_view` contains `terminal_element.rs` (116 KB), `terminal_view.rs`, `terminal_panel.rs`, `terminal_scrollbar.rs`, `terminal_path_like_target.rs` and `persistence.rs`. Hyperlink regex search is throttled to 100 ms (`terminal.rs:1631`). Vi mode is at `:1849`.

### 3.4 Closer reference: zeron's GPUI terminal [V]

`zeron` (same org, GPUI/zui) already draws a terminal in GPUI on Windows:
- `emulator.rs`: `alacritty_terminal 0.26` + `vte 0.15` as a pure state machine, with selection.
- `view.rs` (1,113): a custom element. It probes cell metrics from the mono font, paints background quads for non-default cells, **one `ShapedLine` per row**, and the cursor. It also has a 12 ms input coalescer and an 80 ms resize debounce.
- `panel.rs` (2,123): tabs with drag-reorder.
- `engine/src/terminals/windows.rs` (491): a ConPTY launcher.

This is the most direct Rust-GPUI-on-Windows counterpart to compare against.

## 4. Proposed plan [I; proposals until the user accepts them]

1. **MVP: one pane.**
   - A ConPTY session (x/sys or `charmbracelet/x/conpty`) runs `pwsh`/`cmd` with a goroutine reader.
   - An `x/vt` emulator sits behind a Go `TerminalBackend` interface.
   - A `Terminal` entity batches PTY events Zed-style (first event immediately, then 4 ms / 100 events) and calls `cx.Notify`.
   - A `TerminalElement` (Canvas or custom Element) paints merged background rects plus per-row batched `ShapeLine` runs at `col*cellWidth`, and a block cursor.
   - Key-to-bytes from Zed's `mappings/keys.rs`, paste (bracketed) and resize.
   - Acceptance: vim, htop-like TUIs, `git log` colours, and pasting into pwsh.
2. **Usable.**
   - Scrollback with wheel and scrollbar (display offset).
   - Mouse selection and copy (word/line), and the IME handler.
   - Cursor blink and shapes (DECSCUSR), bracketed paste, mouse reporting modes, OSC 0/2 title, OSC 7 pwd, bell.
   - Damage-driven row re-shaping and `CachedView` per pane.
3. **zterm chrome parity.**
   - Tabs in a custom titlebar with drag.
   - Split tree through `base/dock` (split right/down, divider drag, zoom, equalize, pane/tab navigation, ⌘1-9 equivalents on Ctrl).
   - A Ctrl+K palette, a theme close to `glass.json`, and layout persistence.
   - Optional: a session daemon over named pipes so shells survive window close; an agents sidebar from OSC 9;4 / 7501 / 133.
4. **Zed parity extras.** Search (regex over the grid), hyperlinks (OSC 8 plus regex), vi mode, settings, and the `forceWidth` native fix.
5. **Engine parity (optional).** Port `alacritty_terminal`'s Term/Grid to Go, or keep `x/vt` and document the difference.

## 5. Perf comparison method [I]

zterm only runs on macOS, and its grid is drawn by Ghostty's Metal renderer, so **zterm vs a gpui-go terminal compares Ghostty against gpui-go, not zgui against gpui-go**. Options:

- **A. Same-OS, framework-vs-framework (recommended).** On this Windows PC, compare the gpui-go terminal against:
  - (1) `zeron`'s GPUI terminal (alacritty_terminal + ConPTY + GPUI paint; closest analog), and
  - (2) Zed's terminal panel at the pin.
  - Optional baselines: Windows Terminal and Alacritty.
- **B. Cross-OS against zterm.** Only meaningful for the chrome (tab and split animations, sidebar), and only with gpui-go also on macOS, which is not planned. Skip it, or report it as qualitative.

Reuse zgui's methodology from `../zgui-vs-gpui-fast/zgui.md` §1.2:
- 20 s trials, discard the first 5 s, sample every 50 ms, 3 rotated repeats, report medians.
- Metrics: process CPU (user+sys), working set / private bytes, and GPU time (PresentMon or ETW). Frame pacing is p99 present interval and the count over 25 ms. Startup time is time to the first painted prompt.

Scenes, with fixed size 1280×800, font Consolas 14 and the same theme:
1. **idle**: prompt shown, cursor blinking.
2. **stream**: `cat` of a 50 MB file with ANSI colours (or a fixed `vtebench`-style byte script replayed into each terminal's PTY).
3. **tui**: a full-screen redraw at 60 Hz (a scripted `btop`-like generator).
4. **scroll**: wheel through 10k lines of scrollback.
5. **splits**: 4 panes, 2 streaming.
6. **latency**: keypress to pixel. Use `go: gpui/frameloop_latency_windows_test.go`-style instrumentation, or Typometer.

Feed identical byte streams by having each terminal run the same generator command. In gpui-go, also record the internal metrics from `go: gpui/perf_bench_test.go` (allocs/frame, native calls/frame, shaped-line cache hit rate, shaping-handle parking count).
