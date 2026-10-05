# GPUI-CE upstream version check

Checked 2026-10-04 using live GitHub CLI API calls, the crates.io API, and docs.rs. After this check, the user accepted the pinned main revision below as the parity baseline.

## Published crate

The crates.io API reports `max_version`, `max_stable_version`, and `newest_version` as **0.2.2**. It was published on **2026-08-28 at 23:15:07 UTC** and is not yanked. The returned older versions `0.3.2` and `0.3.3` were published on 2025-12-27 and are yanked; their higher version numbers should not be mistaken for the current recommended registry release. [Registry API](https://crates.io/api/v1/crates/gpui-ce), [crate documentation](https://docs.rs/crate/gpui-ce/0.2.2).

## Current repository main

The live `commits/main` response identifies **254b5dbd47cbb5acbcc5bbdcbb322a339276c88a**, dated **2026-10-03 at 15:16:48 UTC**, with the message `Implement character palette support on Windows and Linux (#308)`. This is the same revision inspected in the existing core research. [Pinned commit](https://github.com/gpui-ce/gpui-ce/commit/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a).

The GitHub Releases API returned an empty list. GitHub release objects and crates.io publication are different channels; an empty Releases page does not mean no crate has been published. [GitHub releases](https://github.com/gpui-ce/gpui-ce/releases).

## Accepted reference

The user accepted `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a` as the compatibility reference. The registry release 0.2.2 remains a distinct artifact, not the chosen target. Pinning keeps conformance work stable; following subsequent main commits is a separate update policy to resolve in the Wayfinder map.

Read-only commands used: `gh api repos/gpui-ce/gpui-ce`, `gh api repos/gpui-ce/gpui-ce/commits/main`, and `gh api repos/gpui-ce/gpui-ce/releases --paginate`; crates.io metadata was retrieved with PowerShell `Invoke-RestMethod`. No compiler, package installation, or build was invoked.
