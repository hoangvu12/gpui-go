# Ecosystem source evidence

Retrieved 2026-10-04 through GitHub CLI `gh api` Contents and Git Trees endpoints. These are selected upstream reference files, not gpui-go implementation, installed dependencies, or complete repositories. No source was built or executed.

- `gpui-kit/`: longbridge/gpui-kit at `4c7f1350331562436df868c55ac33bebc4c6406c`.
- `gpui-ce-component/`: gpui-ce/gpui-component at `c08206932417f863062d2ab70cc2854c6ca04238`.
- `yororen-ui/`: MeowLynxSea/yororen-ui at `346502ac654b77fdaff3be2d7444fca8783acfc9`.
- `manifest.json`: per-file source URL, pinned commit, byte count, SHA-256, and retrieval method.
- `gpui-kit-tree.json`: complete path inventory returned by Git Trees; most listed files were not downloaded or read.
- `*-commit.json`: commit API records for the two additionally discovered repositories.

Contents responses were Base64-decoded, interpreted as UTF-8, and written with .NET File.WriteAllText. The source files were not edited. License notices in the snapshots apply to the corresponding upstream material; this directory does not license that material anew.

Read-only `git diff --no-index` comparisons of three shared Base files found:

| File | Result |
|---|---|
| `popover.rs` | Identical |
| `virtual_list.rs` | Identical |
| `button.rs` | Difference in test syntax: CE calls `crate::ElementExt::on_prepaint` explicitly for the outer Button; Kit uses the method chain |

This bounded comparison establishes neither complete compatibility nor absence of drift elsewhere. Research conclusions and limitations are recorded in [the ecosystem report](../../research/02-base-and-ecosystem.md).
