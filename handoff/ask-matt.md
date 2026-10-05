# Ask Matt: research handoff

Working directory: `C:\Users\ADMIN\Desktop\nguyenvu\gpui-go`.

For the latest checkpoint and exact continuation step, read [resume after compaction](resume.md). This file retains the routing/setup background.

## What the user asked for

Current implementation exception: the user subsequently instructed "research the best method then do it" for fluent custom-component authoring. The [selected generic helper](../authoring/README.md) is implemented and tested on the installed Go 1.26.5 compiler using an isolated, identical-source fixture. The root module targets Go 1.27; its toolchain installation remains blocked and that build is unverified. Continue the active Go authoring ticket for remaining API decisions. Broader GPUI implementation remains outside the current scope.

Latest scope clarification: first make the Go port work 1:1 with the Rust GPUI core. Base is a separate library to address later. Core parity is the destination; do not substitute an application-driven subset or a component showcase as the first-release target. See [PROJECT](../PROJECT.md) for the current scope. Earlier Base-first recommendations below and in the research are historical proposals.

The user subsequently selected Windows as the first usable target, with other platforms later, and allowed native dependencies where they make the port easier or better. They accepted GPUI-CE commit `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a` as the pinned baseline after the [latest-version check](../research/08-upstream-version-check.md).

GPUI-CE works well, but Rust build time and disk use are painful. The user wants a faithful Go port of GPUI syntax and programming model, with a corresponding GPUI Base/component ecosystem later. They rejected treating a different Go toolkit's interface as the destination. They requested deep research using web, Exa and GitHub CLI source reads and asked that findings be saved here for Ask Matt. The original research-only boundary now has the bounded authoring exception described above.

## Start here

1. Read [PROJECT](../PROJECT.md) for confirmed intent and [CONTEXT](../CONTEXT.md) for terminology.
2. Read [synthesis](../research/00-synthesis.md) for conclusions and corrections to the earlier investigation.
3. Read [decision brief](../research/06-decision-brief.md) for candidate questions and dependencies.
4. Open the indexed technical report only when the discussion reaches that topic. Source snapshots and provenance are under [evidence](../evidence/).

## Ask Matt routing result

The applicable router is installed at [ask-matt/SKILL.md](C:/Users/ADMIN/.agents/skills/ask-matt/SKILL.md). It was read together with its phase-boundaries reference. The best-fit proposed next flow is **`/wayfinder`**: a faithful framework plus component ecosystem has unresolved, interacting decisions spanning multiple sessions. A smaller initial scope could instead start with `/grill-with-docs` and reach a spec in one session.

This report does not begin the interview or resolve its human decisions. The first subject should be fidelity and the first usable product scope. The research already supplies source facts; interview the user about preferences and tradeoffs.

## Setup status

Setup is now configured following the user's choices: local Markdown tracking, default triage labels, and both `AGENTS.md` and `CLAUDE.md`. Shared instructions live in `AGENTS.md`; tracker and domain conventions live under `docs/agents/`. The user explicitly requested both entry files, overriding the setup skill's default of choosing one. No remote repository or external issue was created.

The [Wayfinder map](../.scratch/gpui-core/map.md) is charted with nine decision tickets and their blocking relationships. The parity-policy and development-criteria decisions are resolved; seven remain open. Continue using the map and its frontier; the earlier research decision brief is historical input, not the live tracker.

Installed flow files:

- [Setup](C:/Users/ADMIN/.agents/skills/setup-matt-pocock-skills/SKILL.md)
- [Wayfinder](C:/Users/ADMIN/.agents/skills/wayfinder/SKILL.md)
- [Grill with docs](C:/Users/ADMIN/.agents/skills/grill-with-docs/SKILL.md)
- [To spec](C:/Users/ADMIN/.agents/skills/to-spec/SKILL.md)
- [To tickets](C:/Users/ADMIN/.agents/skills/to-tickets/SKILL.md)

After scope and architecture decisions are resolved, follow the router into `/to-spec`, then `/to-tickets`. Broader prototype/code work needs a later instruction extending the current bounded authoring authorization. A proposed experiment in a report is not that instruction.

## Preserve these distinctions

- Syntax fidelity, behavioral fidelity, visual parity, and ecosystem coverage are separate targets.
- The user's current Windows PC is the accepted first usable target; broader Windows support and other operating systems are deferred.
- Go application builds without Rust are distinct from a runtime with no Rust/native code.
- Go 1.27 supports generic methods; installed Go 1.26.5 does not support that new syntax. Generic interface methods remain unsupported.
- Original GPUI Kit uses a pinned Zed snapshot; the GPUI-CE component fork provides another baseline. Do not assume interchangeability.
- Source presence and upstream test names were inspected; benchmarks and native behavior tests were not run.
- Research recommendations remain proposals until accepted. The copied earlier report predates the clarified faithful-port scope.
