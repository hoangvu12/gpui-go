// Package actionspec exercises the public action API of the gpui
// package from outside it: canonical definitions with callbacks
// supplied by an external package, named registration, JSON
// construction, boxed payloads and the dispatch entry points.
//
// The action registry is process-global and keyed by payload type, so
// the payload types declared here are distinct from every type the
// in-package tests use, even where names coincide.
//
// It is a test-only helper package: nothing exports behavior.
package actionspec
