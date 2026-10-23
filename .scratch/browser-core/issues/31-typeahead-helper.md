# 31 - TypeAhead helper

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 6 (Claude Code); worker chat 8cdf9fd8
Blocked by: none
Triage: ready-for-agent

**What to build:** Lists, trees and menus can share a TypeAhead helper that matches typed prefixes with Chrome's timeout and repeat-letter cycling.

**Blocked by:** None - can start immediately

**Status:** ready-for-agent

- [x] Unit tests for prefix matching, timeout reset and repeated-letter cycling.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 6: verified on core/31-47-small before merge. Re-ran vet, gofmt, `go build ./...` and `go test ./gpui/... ./base -count=1` (ok). All 16 new tests pass with none skipped. The half-rate test and the hidden-window test passed 10 times in a row (`-count=10`, 239 Hz display). The Chromium citations (type_ahead.cc timeout and cycling, external_begin_frame_source_win.cc OnVSync and SetPreferredInterval) match the source.

- 2026-11-30, worker (core/31-47-small): implemented in the gpui core, free of Base (grep: `base/` has no typeahead — the research's finding stands). The three acceptance behaviours are unit tests, as the ticket's checkbox asks — the browser-parity harness fixture applies when a list/tree/menu component (Base) adopts the helper, not to the pure helper.

## Answer

Implemented on `core/31-47-small`. All Go; no native rebuild. New files `gpui/typeahead.go`, `gpui/typeahead_fold_generated.go`, `gpui/typeahead_test.go` — the package `gpui` core, no Base dependency.

**The helper** (`gpui/typeahead.go`) is a port of Blink's `TypeAhead` (`third_party/blink/renderer/core/html/forms/type_ahead.cc`, read from the local Chromium sparse checkout):

- `TypeAheadDataSource` (`gpui/typeahead.go:73-84`) — the three-method option contract of `type_ahead.h:47-49` (IndexOfSelectedOption/OptionCount/OptionAtIndex). The caller owns the selection and moves it to every returned index, exactly the HTMLSelectElement contract.
- `TypeAheadMode` (`gpui/typeahead.go:87-102`) — the `ModeFlag` enum of `type_ahead.h:58-62`: `TypeAheadMatchPrefix`, `TypeAheadCycleFirstChar`, `TypeAheadMatchIndex`, combinable with `|`.
- `TypeAhead.Key(ch, mode) int` (`gpui/typeahead.go:129-197`) — `HandleEvent` (`type_ahead.cc:66-126`): the 1-second `kTypeAheadTimeout` (`type_ahead.cc:42`) resets the buffer once the gap between keys EXCEEDS it (`:69-74`; exactly one second keeps the session — pinned by a boundary test); a key stamped before the last one is rejected outright (`:70-71`, session state untouched); the search starts AFTER the selected option and wraps (`:107-114`), except a multi-character buffer anchors at the selection (`:98-101`); repeats of the same letter search on that single character from after the selection — cycling through every option starting with it (`kCycleFirstChar`, `:91-95`); with `kMatchIndex` a numeric buffer falls back to the 1-based option index (`:118-122`, `StringToIntLoose` at `string_to_number.cc:353-355`).
- `TypeAhead.Active()` (`gpui/typeahead.go:199-206`) — `HasActiveSession` (`type_ahead.cc:128-133`, delta <= timeout, the boundary included). `TypeAhead.Reset()` (`:208-216`) — `ResetSession` (`:135-138`). The `std::optional<TimeTicks> last_type_time_` of `type_ahead.h:71` became a `hasSession` flag + timestamp so a clock reading the zero time still distinguishes "no session" (Chromium's test clock base is `TimeTicks()` + 500 ms, `type_ahead_test.cc:45-47`).
- `TypeAheadShouldHandleKey(ch, mods)` (`gpui/typeahead.go:220-228`) — `ShouldHandleKeyboardEvent` (`type_ahead.cc:59-63`): printable character, no ctrl/alt/meta; shift passes (Chromium checks neither shift nor a function key; `IsPrintableChar` = `u_isprint`, `unicode.h:137-139` — Go's `unicode.IsPrint` is the port).
- **The injectable clock**: `NewTypeAheadWithClock` (`gpui/typeahead.go:115-127`) injects `func() time.Time`; `NewTypeAhead` binds `time.Now`.

**Case and accent folding** — Chromium matches with `StartsWithIgnoringCaseAndAccents` (`string_impl.cc:1164-1179`), an ICU primary-strength search ("PuPpY" starts with "pup", "Straße" with "STRASS", `wtf_string_test.cc:711-723`). The port (`typeAheadFold`, `gpui/typeahead.go:278-300`) approximates primary strength without pulling in collation tables: simple case folding, a generated table of the precomposed Latin ranges dropping their accents and expanding the ß/æ/œ/ø/ſ contractions (`gpui/typeahead_fold_generated.go:14-275`, 260 entries, NFKD + mark-strip over U+00C0..U+024F and U+1E00..U+1EFF), and ignorable combining marks. Known limitation, documented on `typeAheadFold` (`gpui/typeahead.go:265-277`): outside the precomposed Latin ranges the fold is case-only — full Unicode primary-strength equivalence (other scripts' accents, all contractions) would need ICU's tables. `typeAheadStripLeadingSpace` (`:232-243`) is `StripLeadingWhiteSpace` (`type_ahead.cc:44-57`, space or no-break space).

**Tests** (`gpui/typeahead_test.go`, all green with `go test ./gpui -run TestTypeAhead -count=1`):

- `:58-107` `TestTypeAheadPrefixMatches` — prefix matching: search starts after the selection and wraps, a multi-character prefix anchors at the selection, no-match returns -1.
- `:180-206` `TestTypeAheadTimeoutResetsBuffer` — timeout reset: kept at exactly 1 s, reset past it by 1 ns, out-of-order keys rejected (`:283-303`).
- `:208-239` `TestTypeAheadRepeatedLetterCycles` — repeat-letter cycling: 'a' 'a' 'a' selects 1, 0, 1 and 'b' 'b' 'b' selects 2, 3, 2 (Chromium's own test source items "aa"/"ab"/"ba"/"bb", `type_ahead_test.cc:31-42`); a different letter ends the cycle.
- `:109-162` `TestTypeAheadCaseAndAccentFold` + `:411-434` `TestTypeAheadFold` — case/accent-insensitive matching both directions, the ß full fold (`wtf_string_test.cc:720-723`), leading-whitespace stripping (`:164-178`).
- `:241-261` `TestTypeAheadActiveSession` (ports Chromium's `HasActiveSession` tests, `type_ahead_test.cc:54-75`), `:263-281` `TestTypeAheadReset`, `:305-344` `TestTypeAheadMatchIndex`, `:346-358` `TestTypeAheadModeWithoutPrefix`, `:360-376` `TestTypeAheadNoOptions`, `:378-409` `TestTypeAheadShouldHandleKey`.

Gates: `go vet ./...`, `gofmt -l gpui base` (clean), `go build ./...`, `go test ./gpui/... ./base -count=1` (full run in the ticket-47 commit message, same branch). No visible windows, no real-input mode.
