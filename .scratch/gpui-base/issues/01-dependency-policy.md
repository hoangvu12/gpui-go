# Decide the third-party dependency policy for Base

Parent: ../map.md
Type: grilling
Labels: wayfinder:grilling
Triage: ready-for-human
Status: open
Assignee: unassigned
Blocked by: none

## Question

Base's Rust crate uses markdown-rs, html5ever, ropey, unicode-segmentation, chrono, regex, sum-tree, lsp-types and more; gpui-go's go.mod has zero requirements. Per dependency: add a Go module (golang.org/x or third-party) or hand-port?

## Scope

- The one-look table in [deps-recommendation](../research/deps-recommendation.md).
- The open calls: markdown (goldmark + mdast adapter vs hand-port markdown-rs), HTML (x/net/html module vs vendoring its BSD-3 `html` package into `internal/`), segmentation (clipperhouse/uax29 vs hand-port UAX #29 with generated tables), whether golang.org/x is a separate policy class.

## Acceptance criteria

- [ ] The user's choice per dependency is recorded under `## Answer`; dependent tickets (15, 24, 27, 30) are updated accordingly.

## Research

- [deps-recommendation.md](../research/deps-recommendation.md)
- [slice-06-text-selection.md](../research/slice-06-text-selection.md)
- [slice-05-input-editor.md](../research/slice-05-input-editor.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
