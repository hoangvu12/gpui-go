# Authoring signature fixture

This isolated module has the planned root import `gpui-go` and package name `gpui`.
Its API operations are stubs that panic; it is not a runtime prototype or a library to import.

`example/counter.go` contains the counter, reusable label, subscription and two-window
snippets extracted from the authoring example, with only a package/import/host wrapper.
`consumer` and `shared` provide independent external-package callers, custom elements,
managed views, restricted parents and shared payloads. The `authoring` directory is a
byte-identical copy of the implemented helper; its existing tests execute here.

`cases.json` pairs each intentional type error with a minimally corrected control.
Each negative compiles separately under `testdata`, outside ordinary `./...` discovery.
The verifier requires the control to pass, the negative to fail, and the diagnostic to
match the relevant type constraint. This prevents an unrelated syntax/import failure
from being mistaken for a successful negative check.

Run `powershell -File .scratch/signature-validation/verify.ps1` from the project root.
It uses the installed Go executable, disables automatic toolchain/module downloads,
runs root and fixture tests/vet, checks helper-copy hashes, and records complete outputs
and fixture hashes in the evidence directory.

The fixture verifies signatures and inference only. Compile acceptance of `Child(nil)`
or self-retention is deliberate: runtime contracts must reject the corresponding misuse.
No stub operation is executed to claim events, lifetime rules or native behavior work.
