# Hard tabs render the way GPUI's users see them

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: 41

## Question

A hard tab paints as the missing-glyph box under the pin (Parley), while Base's 0.2.2 line (DirectWrite) drew blank space at a tab stop (ticket 41). The user asked to follow how GPUI and its users handle it. Research the pin, the 0.2.2 line, Zed's editor (tab map) and the gpui-component input, then implement the closest faithful behaviour.

## Comments

- 2026-10-23 — Created and claimed by the orchestrator at the user's request ("see how gpui or sth do it").
