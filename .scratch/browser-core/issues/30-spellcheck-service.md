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

- [ ] Service test: known misspellings are reported for a sample string.
- [ ] The UI thread is never blocked by a check (timing test).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
