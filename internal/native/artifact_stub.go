//go:build gpui_native_noembed

package native

// Measurement-only stub for `-tags gpui_native_noembed`: produces the loader
// with no embedded default artifact so `go build` output size can be compared
// against the embedding build. Load without a bundle fails with ErrNoArtifact.
var (
	embeddedDLL      []byte
	embeddedManifest []byte
)
