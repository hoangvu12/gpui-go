# 13 - Key code and location, with Shift kept

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 01
Triage: ready-for-agent

**What to build:** Key events carry the physical code and location, Shift is kept on the keystroke, and keymaps match either form, so Ctrl+Shift+] fires as written.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Event-log fixtures of key/code for shifted punctuation match Chrome on a US layout.
- [ ] A binding written as ctrl-shift-] and one written as ctrl-} both fire for the same keystroke.
- [ ] Existing keymap tests pass.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
