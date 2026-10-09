package consumerspec

import (
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ---------------------------------------------------------------------------
// Pinned evidence identity of the shipped native artifact
// ---------------------------------------------------------------------------

// The artifact identity recorded for the current native build (native
// revision 7). These constants are the evidence values the delivery checks
// compare the module's embedded artifact against; they cite:
//
//   - evidence/ticket16-paths-filters.json (the most recent recorded
//     identity: "12,489,728 bytes; sha256 d648523b…; static CRT
//     (OS-only imports)", native revision 7), and
//   - evidence/ticket13-focus-keyboard.json (same artifact, revision 7).
//
// When the maintainer artifact rotates, these constants, the embedded
// manifest pair and the evidence records move together: the tests fail
// loudly instead of accepting a silent identity drift.
const (
	// EvidenceArtifactSHA256 is the recorded SHA-256 of the DLL.
	EvidenceArtifactSHA256 = "d648523b7fe1173a8810a27de77ff8ecbfebaa53eab95d69a6335c4a042b87dd"
	// EvidenceArtifactBytes is the recorded DLL size.
	EvidenceArtifactBytes = 12489728
	// EvidenceNativeRevision is the recorded native bridge revision.
	EvidenceNativeRevision = 7
	// EvidenceCECommit is the pinned gpui-CE commit of the artifact.
	EvidenceCECommit = "254b5dbd47cbb5acbcc5bbdcbb322a339276c88a"
	// EvidenceCRTOutcome is the recorded static-CRT outcome string.
	EvidenceCRTOutcome = "static: no VCRUNTIME/MSVCP/UCRT/api-ms-win-crt imports present; only OS system components are imported"
)

// ArtifactIdentity is the measured identity of an artifact pair.
type ArtifactIdentity struct {
	// DLLSHA256 is the measured full SHA-256 of the DLL bytes.
	DLLSHA256 string
	// DLLBytes is the measured DLL size.
	DLLBytes int64
	// ManifestSHA256, ManifestBytes, ManifestRevision, ManifestCECommit
	// and ManifestCRT come from the manifest record shipped beside the DLL.
	ManifestSHA256   string
	ManifestBytes    int64
	ManifestRevision uint32
	ManifestCECommit string
	ManifestCRT      string
	// ManifestImports lists the manifest's recorded imported DLLs.
	ManifestImports []string
	// MatchesEvidence reports agreement with the pinned evidence identity.
	MatchesEvidence bool
	// Differences lists every mismatch (empty when MatchesEvidence).
	Differences []string
}

// artifactManifestFields is the identity subset of the manifest record the
// internal/native package embeds (the authoritative parser lives there;
// this reader only extracts the identity fields the delivery checks need).
type artifactManifestFields struct {
	Name             string   `json:"name"`
	SHA256           string   `json:"sha256"`
	Bytes            int64    `json:"bytes"`
	CECommit         string   `json:"ce_commit"`
	NativeRevision   uint32   `json:"native_revision"`
	CRTStaticOutcome string   `json:"crt_static_outcome"`
	Imports          []string `json:"imports"`
}

// VerifyArtifactPair checks one DLL/manifest pair:
//
//  1. the DLL hashes to its manifest-declared SHA-256 and size (the same
//     check the loader performs before LoadLibraryExW);
//  2. the pair matches the pinned evidence identity (hash, size, native
//     revision, CE pin, CRT outcome).
//
// The identity comparison answers "does the module ship exactly the
// artifact the evidence records?", the delivery ticket's
// "exact artifact/manifest hashes" check.
func VerifyArtifactPair(dll, manifestJSON []byte) (*ArtifactIdentity, error) {
	sum := sha256.Sum256(dll)
	dllSHA := hex.EncodeToString(sum[:])

	var m artifactManifestFields
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		return nil, fmt.Errorf("consumerspec: parsing artifact manifest: %w", err)
	}
	if len(m.SHA256) != 64 {
		return nil, fmt.Errorf("consumerspec: artifact manifest sha256 field malformed: %q", m.SHA256)
	}

	id := &ArtifactIdentity{
		DLLSHA256:        dllSHA,
		DLLBytes:         int64(len(dll)),
		ManifestSHA256:   m.SHA256,
		ManifestBytes:    m.Bytes,
		ManifestRevision: m.NativeRevision,
		ManifestCECommit: m.CECommit,
		ManifestCRT:      m.CRTStaticOutcome,
		ManifestImports:  m.Imports,
	}

	// 1. Pair self-consistency.
	if dllSHA != m.SHA256 {
		id.Differences = append(id.Differences, fmt.Sprintf("dll sha256 %s does not match the manifest's %s", dllSHA, m.SHA256))
	}
	if int64(len(dll)) != m.Bytes {
		id.Differences = append(id.Differences, fmt.Sprintf("dll size %d does not match the manifest's %d", len(dll), m.Bytes))
	}
	// 2. Pinned evidence identity.
	if dllSHA != EvidenceArtifactSHA256 {
		id.Differences = append(id.Differences, fmt.Sprintf("dll sha256 %s does not match the pinned evidence identity %s", dllSHA, EvidenceArtifactSHA256))
	}
	if int64(len(dll)) != EvidenceArtifactBytes {
		id.Differences = append(id.Differences, fmt.Sprintf("dll size %d does not match the pinned evidence size %d", len(dll), EvidenceArtifactBytes))
	}
	if m.NativeRevision != EvidenceNativeRevision {
		id.Differences = append(id.Differences, fmt.Sprintf("native revision %d does not match the pinned evidence revision %d", m.NativeRevision, EvidenceNativeRevision))
	}
	if m.CECommit != EvidenceCECommit {
		id.Differences = append(id.Differences, fmt.Sprintf("ce commit %q does not match the pinned evidence commit %q", m.CECommit, EvidenceCECommit))
	}
	if m.CRTStaticOutcome != EvidenceCRTOutcome {
		id.Differences = append(id.Differences, fmt.Sprintf("crt outcome %q does not match the recorded evidence outcome", m.CRTStaticOutcome))
	}
	id.MatchesEvidence = len(id.Differences) == 0
	return id, nil
}

// ErrArtifactNotFound reports that the module's shipped artifact pair could
// not be located.
var ErrArtifactNotFound = errors.New("consumerspec: native artifact pair not found in the module")

// FindNativeArtifact locates the module's shipped artifact pair
// (internal/native/artifacts/gpui_go_native.dll + manifest.json). It
// resolves the module root from this package's source location (works in
// tests and `go run` on a dev checkout) and then from the current
// directory's ancestors.
func FindNativeArtifact() (dllPath, manifestPath string, err error) {
	for _, root := range moduleRootCandidates() {
		dir := filepath.Join(root, "internal", "native", "artifacts")
		dll := filepath.Join(dir, "gpui_go_native.dll")
		if st, statErr := os.Stat(dll); statErr == nil && !st.IsDir() && st.Size() > 0 {
			return dll, filepath.Join(dir, "manifest.json"), nil
		}
	}
	return "", "", ErrArtifactNotFound
}

// moduleRootCandidates lists candidate module roots, nearest first.
func moduleRootCandidates() []string {
	var out []string
	if _, file, _, ok := runtime.Caller(0); ok {
		out = append(out, filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")))
	}
	// CWD-based fallback for installed/invoked-from-elsewhere tooling.
	for wd, level := ".", 0; level < 5; level++ {
		abs, err := filepath.Abs(wd)
		if err != nil {
			break
		}
		out = append(out, abs)
		wd = filepath.Join(wd, "..")
	}
	return out
}

// EvidenceRecordsDir returns the evidence directory of the module (empty
// string when not found) for the evidence-record cross-check.
func EvidenceRecordsDir() string {
	for _, root := range moduleRootCandidates() {
		dir := filepath.Join(root, "evidence")
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir
		}
	}
	return ""
}

// EvidenceRecordMentionsSHA reports whether any evidence JSON record
// mentions the given artifact SHA-256 (the linkage between the pinned
// constants and the recorded evidence files).
func EvidenceRecordMentionsSHA(sha string) (bool, string, error) {
	dir := EvidenceRecordsDir()
	if dir == "" {
		return false, "", errors.New("consumerspec: evidence directory not found")
	}
	needle := []byte(sha)
	var found string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || found != "" {
			return nil
		}
		if !strings.HasSuffix(path, ".json") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if bytes.Contains(data, needle) {
			found = path
		}
		return nil
	})
	if err != nil {
		return false, "", err
	}
	return found != "", found, nil
}

// ---------------------------------------------------------------------------
// Module zip size limits
// ---------------------------------------------------------------------------

// ModuleZipLimit is Go's module-zip size ceiling applied to BOTH the
// compressed and the expanded archive (go.dev/ref/mod#zip-files).
const ModuleZipLimit int64 = 500 * 1024 * 1024

// ModuleSize measures the module content as the go module zip carries it.
//
// The zip is an approximation documented here: it walks the module root and
// counts every regular file below the module root EXCEPT the .git
// repository, vendor directories, and the gitignored maintainer trees
// reference/target and reference/ce-source (build outputs and the re-
// fetchable pinned source checkout — the module never ships them). The
// compressed size sums each file's raw DEFLATE stream (zip's method 8,
// compression level 9), which is what a module zip would store. Both totals
// are compared against ModuleZipLimit; a module below the limit under this
// wider-than-zip rule is below it under every stricter interpretation.
type ModuleSize struct {
	// ExpandedBytes is the sum of the file sizes.
	ExpandedBytes int64
	// CompressedBytes is the sum of the per-file DEFLATE streams.
	CompressedBytes int64
	// FileCount is the number of counted files.
	FileCount int
	// ExcludedDirs lists the directories skipped (documented above).
	ExcludedDirs []string
}

// CheckLimits reports an error naming the violated bound, or nil.
func (m *ModuleSize) CheckLimits() error {
	if m.ExpandedBytes >= ModuleZipLimit {
		return fmt.Errorf("consumerspec: module expanded size %d bytes exceeds the %d-byte module zip limit", m.ExpandedBytes, ModuleZipLimit)
	}
	if m.CompressedBytes >= ModuleZipLimit {
		return fmt.Errorf("consumerspec: module compressed size %d bytes exceeds the %d-byte module zip limit", m.CompressedBytes, ModuleZipLimit)
	}
	return nil
}

// MeasureModuleSize walks root and measures the module content.
func MeasureModuleSize(root string) (*ModuleSize, error) {
	m := &ModuleSize{ExcludedDirs: []string{".git", "vendor", filepath.Join("reference", "target"), filepath.Join("reference", "ce-source")}}
	excluded := map[string]bool{}
	for _, d := range m.ExcludedDirs {
		excluded[filepath.ToSlash(filepath.Clean(d))] = true
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if info != nil && info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil || rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if info.IsDir() {
			if excluded[relSlash] {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		m.FileCount++
		m.ExpandedBytes += int64(len(data))
		m.CompressedBytes += deflateSize(data)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// deflateSize returns the size of data's DEFLATE stream at level 9.
func deflateSize(data []byte) int64 {
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		return int64(len(data))
	}
	if _, err := w.Write(data); err != nil {
		return int64(len(data))
	}
	if err := w.Close(); err != nil {
		return int64(len(data))
	}
	_ = io.Discard
	return int64(buf.Len())
}
