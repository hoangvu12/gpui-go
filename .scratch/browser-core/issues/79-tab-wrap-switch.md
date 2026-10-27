# 79 - App-level Tab wrap switch

Parent: ../map.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 13 (worker: core/79 worktree gpui-go-wt-core-79)
Blocked by: none
Triage: ready-for-agent

**What to build:** An app-level switch that makes Tab past the last focus stop wrap to the first (and Shift+Tab before the first wrap to the last). It is OFF by default, so the default stays Chrome's: focus passes out and the window blurs (ticket 14's boundary rule, `Window.FocusNext/FocusPrev`).

- [x] With the switch off, Tab past the last stop blurs as today (the existing focusspec/parity tests are unchanged).
- [x] With it on, Tab past the last stop focuses the first and Shift+Tab before the first focuses the last, respecting inert subtrees, `TabGroup`s and modal focus traps.
- [x] Documented next to the Tab navigation docs.

User decision (2026-10-27): add the switch, off by default (asked as open question 1 since 2026-10-26; the user accepted the recommendation).

## Comments

- 2026-10-27, orchestrator 13: merged at 7907afb (worker commit 3415208, pi glm-5.3, chat 823c50f6). Verified: reviewed the diff (the wrap arms reuse the existing `firstStop`/`lastStop`, so Inert/TabGroup/modal scoping comes from the registered stops; off by default, zero value); the broad set + portfixture on the branch: 82 ok, only the known flakes (portfixture TestFx0004; clipspec TestClipboardPowerShellNativeInterop, which passed alone on the branch and failed the same way on main at the time, so clipboard contention); gpui-go-base `go test ./...` via a temp `-modfile` in %TEMP% ok; both codemod checks 0; gofmt clean on the touched files. Chromium citation checked: `focus_controller.cc` at 0d4dcd4f, the pass-to-Chrome arm and the "Chrome doesn't want focus, so we should wrap focus" fallback just after it.

## Answer

- **API:** `App.TabWrap() bool` / `App.SetTabWrap(bool)` (gpui/app.go, next to the `SmoothScrolling` pair) — the app-level switch, OFF by default (the zero value, so no constructor wiring was touched); `TestApp.WithTabWrap(bool)` (gpui/testapp.go) is the test-side opt-in, the `WithSmoothScrolling` twin. Only the focus walk consults the flag, so a change needs no window refresh.
- **Behaviour:** `Window.FocusNext`/`FocusPrev` (gpui/focus.go) keep ticket 14's boundary as the default — `TabStopMap.Next`/`Prev` report false at the cycle's ends and the caller blurs (the pass-out). With the switch on, the false answer wraps instead: `firstStop()` forward, `lastStop()` backward, still a `focusOriginKeyboard` focus (the focus-visible ring shows, like every Tab move).
- **Wrap rules:** an Inert subtree never registers a tab stop (ticket 14's prepaint suppression, div.go `registerDivElementDispatch`), and the wrap target scans the ordered slice — so the wrap can never land in one; `TabGroup`s keep the forward walk's order (`firstStop`/`lastStop` scan the same (path, index) order `Next` walks); a modal dialog's inert outside leaves only the dialog's stops registered, so the wrap stays inside the trap — and the trap's edge behaviour with the switch OFF is unchanged (`TestShowModalTabNeverLeavesTheDialog` still pins the pass-out blur). With nothing focusable there is no wrap target: the wrap falls through to the blur, a no-op — nothing happens. A focused excluded element (negative tabindex) at the tree's end reaches the boundary through its tree-order arm and wraps forward like any last stop; backward from it keeps ticket 14's tree-order predecessor.
- **Chromium:** the default boundary kept is `third_party/blink/renderer/core/page/focus_controller.cc:2216-2223` — `AdvanceFocusInDocumentOrder`'s "pass focus to Chrome" arm (`CanTakeFocus`/`TakeFocus`, `ClearFocusedElement`, the nulled starting point), the blur's mirror. Chromium itself keeps a wrap fallback when the UI refuses the focus (`:2227-2236`, "Chrome doesn't want focus, so we should wrap focus" — the document-scope re-search), which is the shape the switch opts into; the port makes it an application choice because a real Chrome window passes focus out (headless records the body). Source: `git -C %TEMP%\research-src\chromium show HEAD:third_party/blink/renderer/core/page/focus_controller.cc`.
- **Tests:** `internal/focusspec/focus_wrap_test.go` (new, seven tests) — the off-default pass-out (`TestTabWrapOffByDefaultKeepsThePassOut`), the forward wrap (`…WrapsForwardToTheFirst`), the backward wrap (`…WrapsBackwardToTheLast`), the inert bookends (`…SkipsInertSubtreesAtTheEdges` — the corpus's inert head carries tabindex 1 and its tail 5, so a broken suppression would steal the wrap targets), the positive-index/TabGroup order over two laps (`…KeepsPositiveIndexAndTabGroupOrder`), the excluded element at the tree's end (`…FromANegativeTabIndexAtTheEnd`), and the nothing-focusable no-op (`…WithNothingFocusableDoesNothing`); `gpui/modal_test.go` `TestShowModalTabWrapStaysInsideTheDialog` — the wrap cycles d2 → d (the dialog's own slot) → d1 inside the dialog, never the outside anchor. No parity fixture changed: Chrome never records a wrap (it passes out at the boundary), so the switch is Go-test-pinned.
- **Docs:** the public API doc comments (gpui/app.go `TabWrap`/`SetTabWrap`, gpui/testapp.go `WithTabWrap`, and the wrap-arm notes on `FocusNext`/`FocusPrev` in gpui/focus.go) and a ticket-79 paragraph next to ticket 14's Tab navigation docs in docs/browser-parity-harness.md.
- **Validation:** `go test -p 1 -count=1` over the full final package list (gpui, parity, focusspec, the specs, tsxgen, script, examples, cmd) passes; `go run ./cmd/displaycodemod -check ./...` and `go run ./cmd/positioncodemod -check ./...` both report 0 changes; gofmt/vet clean; gpui-go-base `go test ./...` passes against this worktree through a temporary `-modfile` replace (no base file changed).
- **Gaps:** no parity fixture for the wrap (unrecordable from Chrome — the headless records pin the body at the boundary, exactly what the default keeps); a single-stop cycle with the wrap on keeps focus on that stop (`focusWithOrigin` no-ops on the same identity) instead of blurring — the trap intuition for a one-element dialog; `SetTabWrap` triggers no refresh (nothing rendered depends on the flag).
