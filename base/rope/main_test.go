package rope

import (
	"os"
	"testing"
)

// TestMain shrinks the tree constants to ropey's test values
// (ropey/src/tree/mod.rs:15-21: MAX_CHILDREN 5, MIN_CHILDREN 2,
// MAX_TEXT_SIZE 15, MIN_TEXT_SIZE 7) so small test texts build deep trees.
func TestMain(m *testing.M) {
	maxChildren, minChildren, maxText, minText = 5, 2, 15, 7
	os.Exit(m.Run())
}
