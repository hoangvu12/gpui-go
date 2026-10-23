# Synthesis: a browser-like core, without the components

Parent: ../map.md
Type: research
Labels: wayfinder:research
Status: resolved
Assignee: orchestrator 5 (synthesis worker)
Blocked by: 01, 02, 03, 04, 05, 06, 08

## Question

Combine 01-06, 08, research/app-gaps/ and gpuix (github.com/remorses/gpuix: a React/Solid binding over GPUI with browser-like host elements and components in userland; the user pointed to it 2026-10-23) into one ranked proposal for the user:
- which browser behaviours the core should adopt, and where they differ from GPUI
- what goes in the core and what stays in components (the Base repo)
- the order of work (Go first, then grouped native rebuilds)

Output: research/browser-parity/README.md.

## Comments

## Answer

Answer: research/browser-parity/README.md. It contains 23 quick Go wins (Q1-Q23), 3 native rebuild groups (R15: bold rule, gradients, shadow clip-out, ABI growth; R16: the compositor; R17+: measure first) and user decisions D0-D16. The orchestrator verified D6 and D8 against the pin.
