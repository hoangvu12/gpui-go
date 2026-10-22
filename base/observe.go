package base

// Element observation for UI queries (U:observe.rs).
//
// Upstream, TestSupportExt::test_support returns the element itself in
// normal builds and wraps it in test_support::Observed only under the
// `test-support` cargo feature (U:observe.rs:4-9, 27-36). The Go port has
// no feature flags; it keeps the normal-build behavior: TestSupport
// returns the element unchanged, so production render code calling it
// keeps the native type and identity.
//
// Deviation: the feature-gated Observed element and its snapshot
// registry (U:test_support.rs, consumed only by the Kit crate's testing
// API, which is out of scope) are not ported; see the ticket record.

import "github.com/hoangvu12/gpui-go/gpui"

// TestSupport registers an existing element for UI queries
// (U:observe.rs:20-38 TestSupportExt::test_support). In this port it is
// the identity: the element keeps its exact native type and identity.
// Call it before TrackFocus, like upstream.
func TestSupport(element *gpui.DivElement) *gpui.DivElement {
	return element
}
