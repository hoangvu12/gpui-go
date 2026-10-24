# 30 - Spellcheck service

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: none
Triage: ready-for-agent

**What to build:** Text inputs can opt into spellchecking through a core service backed by the Windows spell checker, running off the UI thread, with misspellings drawn as wavy underlines.

**Blocked by:** None - can start immediately

**Status:** ready-for-agent

- [x] Service test: known misspellings are reported for a sample string.
- [x] The UI thread is never blocked by a check (timing test).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise. A browser-parity fixture is not applicable here (the ticket notes Chrome's spelling markers need Chrome's own dictionaries); the checks are the service/spec tests below.

## Comments

## Answer

**API and opt-in.** The core service is `gpui.SpellcheckService` (gpui/spellcheck.go:181), constructed with `NewSpellcheckService(app, checker)` (gpui/spellcheck.go:223) over an injectable `gpui.SpellChecker` seam (gpui/spellcheck.go:91: Prepare/Check/Suggest/Add/Ignore/Release, one thread only). The surface: `Check(key, text, lang, deliver)` (gpui/spellcheck.go:248) delivering a `SpellcheckResult{Text, Misspellings, Err}` whose `Misspelling{Start, End}` are UTF-16 code-unit ranges (the checker's native unit, converted to document byte ranges by the input); `Suggest(key, word, lang, deliver)` (gpui/spellcheck.go:261); fire-and-forget `Add`/`Ignore` (gpui/spellcheck.go:274, 280); `FlushDeliveries` (gpui/spellcheck.go:320) for hostless test apps; `Close` (gpui/spellcheck.go:336). The App surface mirrors HTTPClient: `App.Spellcheck()` lazily builds the platform default (gpui/spellcheck.go:521), `App.SetSpellcheck` replaces/retires (gpui/spellcheck.go:535). On Windows the default is the `ISpellCheckerFactory`/`ISpellChecker` COM checker (gpui/spellcheck_windows.go:103 `windowsSpellChecker`); other platforms a failing stub (gpui/spellcheck_other.go).

The opt-in is per input and OFF by default (an explicit decision: Chrome's UA default enables spellcheck on most text fields, but a component library follows the explicit opt-in so no app starts native background work — or pays dictionary latency — by surprise): `InputState.Spellcheck(true)` / `SpellcheckLanguage(tag)` (base/input_spellcheck.go:53, 60; empty tag = "en-US", base/input_spellcheck.go:43), with `SpellcheckRanges()` reading the delivered misspelled byte ranges (base/input_spellcheck.go:75). Masked inputs never check (password fields must not leak their text; tested).

**Threading design.** The COM checker lives on its own background goroutine pinned with `runtime.LockOSThread` and `CoInitializeEx(COINIT_APARTMENTTHREADED)` (gpui/spellcheck.go:366 `run`; gpui/spellcheck_windows.go:119 `Prepare`), because COM objects are thread-affine — the Go equivalent of Chromium's dedicated COM STA sequence (`base::ThreadPool::CreateCOMSTATaskRunner`, windows_spell_checker_unittest.cc:51-52; BackgroundHelper's "All invocations of these methods must be posted to the same COM SingleThreadTaskRunner", windows_spell_checker.cc:40-48). Operations queue through a mutex-guarded queue (queueing never blocks the caller — Check measured < 1ms); the worker is NOT a scheduler task, so the deterministic foreground drain never waits on it. Completions post back to the foreground: through `Host.Post` (the Win32 message loop) in a real app, or a queue a test app drains with FlushDeliveries (gpui/spellcheck.go:466 `post`) — the port of Chromium's `PostTaskAndReplyWithResult` reply to the caller's sequence (windows_spell_checker.cc:512-524). A newer request for the same key supersedes an older one: queued stragglers are skipped before the checker runs and in-flight results drop at delivery (gpui/spellcheck.go:224 `queueOp` generations + gpui/spellcheck.go:484 `complete`). Panics inside the seam are contained into errors so the worker never dies (gpui/spellcheck.go:453 `guarded`). The service retires with the application (`App.Shutdown`, gpui/task_ownership.go:508) and with `App.SetSpellcheck` replacements.

**What changed.**

- gpui/spellcheck.go (new): the service, request/supersession machinery, delivery, the App surface; `App.spellcheck` field (gpui/app.go:179).
- gpui/spellcheck_windows.go (new): the COM checker — CoInitializeEx STA + `CoCreateInstance(SpellCheckerFactory, CLSCTX_INPROC_SERVER|CLSCTX_LOCAL_SERVER)` (Chromium windows_spell_checker.cc:158-184), per-language checkers via `IsSupported`/`CreateSpellChecker` (:180-200, 345-360) cached per tag, `ComprehensiveCheck` + the `IEnumSpellingError` walk keeping GET_SUGGESTIONS/REPLACE actions (:228-250), `Suggest` + `IEnumString` with CoTaskMemFree (:296-315), `Add`/`Ignore` (:317-338). Vtable slots and GUIDs verified against the installed SDK (10.0.26100.0) um/spellcheck.h / spellcheck_i.c / objidl.h, reusing the dialog COM helpers (comIFace/comCall/comRelease).
- gpui/spellcheck_other.go (new): the non-Windows stub.
- gpui/task_ownership.go:508: Shutdown retires the service (imageCodec precedent).
- base/input_spellcheck.go (new): the input hook — opt-in setters, the request the element queues (queueSpellcheckCheck, base/input_spellcheck.go:84, keyed by entity id with a weak-handle delivery that drops late results of released inputs), `applySpellcheckResult` (base/input_spellcheck.go:110, drops results whose text moved on and clears markers on failed checks), the red wavy marker style `spellcheckHighlightStyle` (base/input_spellcheck.go:189).
- base/input_state.go:474: four state fields; the request gate starts at "never requested" (base/input_state.go:742) so a DefaultValue-set document (revision 0) still queues its first check.
- base/input_element.go: the hook is ONE call in the element's prepaint (base/input_element.go:960, `e.requestSpellcheck`) — chosen over the state's four text-mutation sites because two of them run before the state is an entity; and the misspellings compose into `highlightLines` like the diagnostics (base/input_element.go:660, 744), with a full-cover base in the no-highlighter path so the style list still partitions the text (base/input_element.go:667).

**Tests** (internal/spellspec + base/input_spellcheck_test.go; commands and measurements):

- Acceptance 1 — `go test -run TestSpellcheckServiceReportsKnownMisspellingsForSampleString ./internal/spellspec/`: the fake reports {4..9} "quikc" and {20..27} "jumpped" for "The quikc brown fox jumpped over the fence", delivered on the foreground with the checked text; a clean text reports none.
- Acceptance 2 — `go test -run TestSpellcheckCheckNeverBlocksTheUIThread ./internal/spellspec/` (3 sub-runs, run 3× with -count=3): a fake checker sleeping 500ms on the worker; the UI thread loops RunUntilParked + DrawWindowFrame + FlushDeliveries. Measured: 32k-78k UI beats per 500ms run, max beat gap 11-35ms, bound 250ms (a UI thread blocked by the check would show one ≈500ms gap); Check itself returned in < 1ms. Stable across 9 runs.
- Real Windows checker, en-US — `go test -run TestWindowsSpellCheckerRealEnUS ./internal/spellspec/`: via `App.Spellcheck()`'s real COM service, "Helllo there. This sentense has mispellings." reports 3 misspellings (first {0..6} "Helllo") and Suggest("absense") = [absence]; skips with a reason when en-US is not installed (`ErrSpellcheckLanguageUnsupported`).
- Real host path — `go test -run TestSpellcheckServiceDeliversThroughHostMessageLoop ./internal/spellspec/`: a real Win32 host + App.Run; the check is queued from the host thread and the completion rides Host.Post through the message loop; the checker ran on its own pinned OS thread (≠ host thread, constant across calls).
- Supersession, prepare failure, suggest/add/ignore, close/retire, lazy/set App surface, deliveries-never-run-inline-on-the-worker: TestSpellcheckService* in internal/spellspec/spellcheck_service_test.go.
- The input hook — `go test -run TestInputSpellcheck ./base/`: off by default (no checks, no underline records), the misspelled word draws exactly ONE wavy red underline whose width tracks the shaped word, stale results supersede while typing, masked inputs skip, failed checks clear markers, UTF-16→byte conversion (non-ASCII prefix).

Full required set once at the end: `go vet` (gpui, base, internal/spellspec), `gofmt -l` (touched files), `go build ./...`, then `go test -p 1 -count=1 ./gpui/ ./internal/parity/... ./base ./internal/richtextspec ./internal/authorspec ./internal/inspectorspec ./examples/zterm ./internal/spellspec` — all ok (gpui 28.0s, base 2.5s, spellspec 2.2s, parity+fixtures 14s, the rest < 1s each).

**Chromium citations.** components/spellcheck/browser/windows_spell_checker.cc — :40-48 BackgroundHelper's same-COM-thread rule, :158-184 factory creation (STA assert, CLSCTX_INPROC_SERVER|CLSCTX_LOCAL_SERVER), :180-200/:345-360 per-language checker creation via IsSupported, :213-281 (esp. :228-250) RequestTextCheckForAllLanguages (ComprehensiveCheck + the error walk + the GET_SUGGESTIONS/REPLACE filter), :296-315 suggestions, :317-321/:331-338 Add/Ignore, :512-524 RequestTextCheck posting to the background runner and replying on the caller's sequence; windows_spell_checker_unittest.cc:51-52 CreateCOMSTATaskRunner, :150 the "absense"→"absence" test word. Marker painting: third_party/blink/renderer/core/layout/layout_theme.cc:413-416 PlatformSpellingMarkerUnderlineColor = red(255,0,0); third_party/blink/renderer/core/paint/text_decoration_info.cc:467-481 spelling/grammar errors draw the wavy stroke, :136-146 MakeSpellingGrammarWave.

**Limitations.** One language per check (the input checks its configured tag; Chromium's multi-language agreement across checkers is a follow-up); no suggestion/ignore UI (the component layer, per the research split); the check request fires at prepaint when the document revision moved (no Blink-style hot/cold idle chunking — inputs are small); the code-editor path composes markers but fold coordinate subtleties are inherited from the diagnostics path; non-Windows builds fail every operation by design.
