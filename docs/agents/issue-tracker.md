# Issue tracker: local Markdown

The user selected local Markdown. Maps, issues and specs live under `.scratch/<effort>/`. This configuration does not create a remote repository or publish external issues.

## Files

- Wayfinder map: `.scratch/<effort>/map.md`.
- Child decision tickets and later implementation tickets: `.scratch/<effort>/issues/NN-<slug>.md`, one file per ticket, numbered from `01` within the effort.
- Specification: `.scratch/<effort>/spec.md`.
- Append conversation history under `## Comments`; preserve prior entries.

When a skill says publish, create or update the corresponding local file. When it says fetch, read that file. Refer to maps and tickets by linked titles in user-facing text.

## Wayfinding operations

The map has `Labels: wayfinder:map` and the Destination, Notes, Decisions so far, Not yet specified, and Out of scope sections. Discover open tickets from child files instead of duplicating them in the map body.

Each child has a title and this metadata:

```text
Parent: ../map.md
Type: grilling
Labels: wayfinder:grilling
Status: open
Assignee: unassigned
Blocked by: none
```

`Type` is `research`, `prototype`, `grilling`, or `task`; the label uses the matching `wayfinder:` prefix. The body starts with `## Question`.

- **Blocking:** list ticket numbers in `Blocked by: NN, NN`. Create tickets before wiring dependencies. File metadata represents blocking because this tracker has no native dependency service.
- **Frontier:** open, unassigned children whose blockers are all `resolved`, ordered by ticket number. `open` is the initial state.
- **Claim:** save `Status: claimed` and `Assignee: <session or developer name>` before working. The assignee records the claim; concurrent sessions skip it.
- **Resolve:** append the resolution under `## Answer`, set `Status: resolved`, then add a one-line gist and named link to the map's Decisions so far. Detailed decisions live in the ticket.
- **Out of scope:** append the reason, set `Status: closed-out-of-scope`, and link it from the map's Out of scope section. Revisit dependents explicitly; this state does not silently satisfy a blocker.

Follow the invoked Wayfinder skill's session boundaries and obtain human answers for human-in-the-loop tickets. Create the initial map after discussing its destination and frontier.

## Triage

For incoming issues, record the triage role in `Triage:` using [the label mapping](triage-labels.md). Keep it separate from the ticket's `Status:` lifecycle. Generated implementation tickets do not need incoming-issue triage.
