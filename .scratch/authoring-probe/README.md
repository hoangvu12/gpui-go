# Isolated authoring validation

This fixture exists because the installed Go 1.26.5 compiler cannot build a module declaring Go 1.27. The system toolchain upgrade was policy-blocked. Root `go.mod` remains the authoritative project language floor.

`authoring/` contains byte-identical copies of the real package's `.go` files, used solely to test this mechanism on the available older compiler. They are not an alternative implementation; make edits in the root `authoring/` package. Resynchronize and verify hashes before using this fixture after a source change.

`generic_binding_test.go` preserves the initial language experiment. Test and vet were run with `GOTOOLCHAIN=local` and `GOWORK=off`. This neither installs a toolchain nor establishes Go 1.27 validation.
