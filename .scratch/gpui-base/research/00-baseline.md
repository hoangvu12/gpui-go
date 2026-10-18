# Base baseline selection

Research date: 2026-10-19.

## Pinned source

- **Selected:** `gpui-ce/gpui-component` at `c08206932417f863062d2ab70cc2854c6ca04238`, crate `crates/base` (`gpui_ce_components_base` 0.2.0, lib name `gpui_base`). Hashed per-file manifest: [evidence/base/source-manifest.json](../../../evidence/base/source-manifest.json).
- **Comparison only:** `longbridge/gpui-kit` at `4c7f1350331562436df868c55ac33bebc4c6406c` (the same commit research/02 audited). Kit's HEAD has since moved (`5591a658…` on 2026-10-19); it is not re-pinned.

## Why the CE fork and not Kit

1. The gpui-go core pin `254b5dbd` declares `name = "gpui-ce"`, `version = "0.2.2"` in `crates/gpui/Cargo.toml`. The CE fork's workspace depends on exactly `gpui = { package = "gpui-ce", version = "0.2.2" }`. So the CE fork's Base was written against the same core API line we ported. Kit pins `gpui-pre = 0.3.7`, a Zed snapshot family that the core never targeted.
2. The CE fork is a rebase of Kit ("Merge pull request #4 from philocalyst/rebase-gpui-kit", 2026-10-01). `diff -rq` of `crates/base/src` reports 55 differing files. Sampled diffs are CE API adaptations, for example the explicit `crate::ElementExt::on_prepaint(...)` call in button.rs tests and `cfg(feature = "test-support")` instead of `cfg(test)` in root.rs.
3. The CE fork's HEAD still equals the pin (`git ls-remote` on 2026-10-19), so the pin is also current upstream. The audit's pin was not stale.
4. This is the same rule the core used: a fixed pin, with no silent upstream moves.

## Scope facts

- 268 files under `crates/base`, 108,213 lines of Rust under `src/`, 1,226 `#[test]`/`#[gpui::test]` functions.
- The published Base site (gpui-kit.com/base, v0.7.3) lists 39 primitives (Accordion, Alert Dialog, Avatar, Button, Calendar, Checkbox, Collapsible, Color Picker, Combobox, Date Picker, Dialog, Editor, Hover Card, Input, Link, Nav Stack, Number Input, OTP Input, Pagination, Popover, Popup, Progress, Radio, Radio Group, Resizable, Scrollbar, Select, Sheet, Slider, Switch, Table, Tabs, Textarea, Time Field, Toast, Toggle, Toggle Group, Tooltip, Tree). It also lists infrastructure guides: Text Selection, Motion, TextView, VirtualList, Dock, History, Plot. `lib.rs` additionally exports Toolbar, Questionnaire, ScrollBounce, ScrollableMask, AutoScroll, SelectableText, Measure, Root/RootPlugin, theme tokens and the StyledExt/ElementExt extension surfaces.
- WebView, Icon, TitleBar, DataTable, Sidebar, Menu, Notification, etc. live in the **styled** `crates/component` (~91k lines), which is not Base. `crates/webview` (wry) is separate again. They are out of this effort's scope; a later "Component" effort would build on Base.
- Base's Cargo dependencies beyond gpui: anyhow, aho-corasick, chrono, data-url, futures, itertools, web-time, lsp-types, markdown (markdown-rs), html5ever + markup5ever_rcdom, num-traits, palette, ropey, regex, rust_decimal (optional), schemars, serde/serde_json, smallvec, sum-tree, tracing, unicode-segmentation, syntect, smol. On Windows it also uses `windows`, only for SystemParametersInfoW (reduced motion). gpui-go's go.mod has zero third-party modules today.
