# 20 - Text selectable by default, with UserSelect

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 9 (Claude Code)
Blocked by: 19
Triage: ready-for-agent

**What to build:** Text is selectable by default with an automatic I-beam over it, and UserSelect(None) opts out (D7).

**Blocked by:** 19 (Core text selection on static text)

**Status:** ready-for-agent

- [x] Fixtures: plain text shows the I-beam and selects; a UserSelect(None) subtree doesn't.
- [x] Buttons and other interactive elements in examples opt out where Chrome's UA style would (documented).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

- **API:** `UserSelect` (auto/none/text — all/contain not modelled) cascades through the text style stack; `Styled.UserSelect(v)`, `SelectNone/SelectText/SelectAuto`, and the Tailwind classes `select-none/select-text/select-auto` (`select-all` rejected). The resolution walks the refinement stack innermost-first — none wins by inheritance, text re-enables under none, auto consults the ancestors (node.cc:2017-2041); exported `UserSelectAllowsSelection(w)` and `WithUserSelectSuppressed` (base's inline flow and the detached roots — drag preview, inspector, inline object fragments).
- **Default-on:** `installWindowTextSelection` (gpui/user_select.go:140) gives every window the selection state and gestures at the head of `drawRoots` (gpui/render.go), so the state/listeners sit outermost; the state is created lazily by the first participant (leak-free). The `TextSelectionLayer` is now a compat no-op (gpui/text_selection.go). `textElement` inserts a selection hitbox at prepaint and registers a participant at paint (gpui/div.go textSelectableByUserSelect/paintUserSelectText) — drag, double and triple click work with no opt-in; `SelectableText` honors the same opt-out.
- **I-beam:** `cursorStyleRequest.automatic` (gpui/interact.go) — the text's `setAutomaticCursorStyle(CursorIBeam, hitbox)` loses to any hovered explicit request (Chrome's cursor: auto resolution, event_handler.cc:574-764 → SelectAutoCursor:839-847 → ShouldShowIBeamForNode:563-573).
- **Fixtures:** `internal/parity/fixtures/userselect` — `userselect-drag` (plain `Div().Child(text)` selects by drag; the record's selection text/cursor match the replay's; registered in replay_test.go and cmd/parityrecord) and `userselect-none` (a select-none subtree selects nothing; the arrow over it); the cursor is logged (WindowCursorStyle) since screenshots can't see it. Wrong view: the default fixture under SelectNone (Tier C fails, boxes pass).
- **Key citations:** html.css:850/:904 (button's user-select:none), :2191/:2215 (select), :2624 (menuitem); css_properties.json5:8169-8175 (inherited); node.cc:2017-2041 (CanStartSelection walk); event_handler.cc:563-573, :839-847; selection_controller.cc:122-127, :1170 (mousedown on non-selectable starts nothing).
- **Opt-outs (Chrome's UA analogues):** base Button, Tab, Toolbar, Select trigger, Checkbox, Radio, Switch, Toggle, Pagination, and base's inline flow under user-select:none; zterm's titlebar (tabs/tools/caption buttons), pane menu, palette rows. Documents and content text stay selectable.
- **Cost:** the differential `TestUserSelectIdleFrameAllocations` (gpui/user_select_alloc_test.go) pins **21 allocs per idle text per frame** (down from 53.5: slot-array identities, one batched registration flush per frame, runs carried on the registration — no per-text entity update while idle); the probe's paired overhead stays 0.25ms/1ms.
- **Limitations:** `user-select: all/contain` not modelled; text inside an inline paragraph's OBJECT fragments doesn't auto-register (the paragraph's participant owns it); a tree edit that renumbers the paint order moves the auto participants' identities (a live selection may collapse — Chrome's node-anchored selection survives); the I-beam isn't shown over text in a replayed cached-view frame (its hitbox isn't replayed); user-select:none is a style opt-out only, not a keyboard copy suppression.
