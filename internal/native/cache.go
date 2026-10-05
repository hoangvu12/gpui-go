package native

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// Cache layout (distribution contract: the normal location is below
// os.UserCacheDir()/gpui-go/native/windows-amd64/<full-sha256>/, and the DLL
// basename also includes its full hash):
//
//	<cacheRoot>/windows-amd64/<sha256-hex>/gpui_go_native-<sha256-hex>.dll  verified final artifact
//	<cacheRoot>/windows-amd64/<sha256-hex>/gpui_go_native-<sha>.dll.staging-<pid>-<rand>  in-flight staging file
//	<cacheRoot>/publication.lock  cross-process publication lock
//
// <cacheRoot> defaults to os.UserCacheDir()/gpui-go/native (i.e.
// %LocalAppData%/gpui-go/native on Windows) and can be overridden through
// Options.CacheRoot (tests); an explicit verified bundle bypasses the cache
// entirely.
const (
	// artifactBaseName is the bundle/file name of the artifact
	// (gpui_go_native.dll). The cache copy additionally embeds the full hash
	// in its basename; see cacheArtifactName.
	artifactBaseName = "gpui_go_native.dll"

	// cachePlatformDir pins the artifact family to the recorded target.
	cachePlatformDir = "windows-amd64"

	publicationLockName = "publication.lock"
)

// cacheArtifactName returns the cache basename of the artifact: the manifest
// name with the full SHA-256 inserted before the extension, e.g.
// gpui_go_native-<sha256hex>.dll. A different artifact hash therefore never
// collides with (or overwrites) another identity's file.
func cacheArtifactName(m *Manifest) string {
	ext := filepath.Ext(m.Name)
	stem := strings.TrimSuffix(m.Name, ext)
	return stem + "-" + m.SHA256 + ext
}

// stagingPrefixFor is the staging-file prefix for a manifest's artifact.
func stagingPrefixFor(m *Manifest) string {
	return cacheArtifactName(m) + ".staging-"
}

// defaultCacheRoot returns the per-user cache root. The artifact directory
// itself is <root>/windows-amd64/<full-sha256-hex>.
func defaultCacheRoot() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("native: resolving user cache dir: %w", err)
	}
	return filepath.Join(base, "gpui-go", "native"), nil
}

// publicationLock is a cross-process exclusive byte-range lock over a lock
// file in the cache root, acquired with LockFileEx (blocking, exclusive,
// range [0,1)) on Windows. Every framework loader — in this or any other
// process — holds this lock while inspecting, publishing or cleaning a cache
// artifact directory. The platform-specific constructor is
// acquirePublicationLock (win.go / win_other.go).
type publicationLock struct {
	handle uintptr
}

// verifiedFile is an open read-only handle on the selected artifact file,
// held with FILE_SHARE_READ only: while it is retained, no other opener can
// write or delete the file (distribution contract: "Open the selected DLL
// for read with sharing that denies write/delete, hash that open file, and
// retain the verified-file handle across loading; retain the protection for
// the module's process lifetime"). The platform-specific constructor is
// openVerifiedFile (win.go / win_other.go).
type verifiedFile struct {
	path   string
	handle uintptr
}

// publications counts real publications performed in this process. Test
// instrumentation only (asserted by the concurrent-publication test).
var publications atomic.Int64

// pubMu serializes publication inside this process; the cross-process
// publication lock (LockFileEx over publication.lock) serializes it across
// processes. Both are held together while a cache directory is inspected,
// written or cleaned.
var pubMu sync.Mutex

// materialize publishes the artifact bytes into the per-user cache and
// returns the absolute path of the verified final file together with the
// retained verified handle on that file. It is the whole publication protocol
// of the distribution contract:
//
//  1. create the per-hash artifact directory;
//  2. take the cross-process publication lock;
//  3. inspect any existing final file through a write/delete-denying open:
//     if its full SHA-256 matches, keep that handle as the verified handle
//     and reuse the file (no rewrite of a possibly mapped DLL);
//  4. if the existing final file is corrupt, return a typed
//     ErrCacheCorrupt error WITHOUT overwriting it — the file could be
//     mapped by another process. Recovery is an explicit verified bundle
//     override (Options.BundleDir / GPUI_GO_NATIVE_BUNDLE) or manual cache
//     cleanup; crash recovery proper is handled by step 7;
//  5. otherwise write a uniquely named staging file in the same directory,
//     fsync, close, then rename over the final name (never trusting rename
//     atomicity alone);
//  6. re-open the final file with the verified sharing mode, hash it through
//     that open handle and keep the handle;
//  7. remove stale staging files left by killed writers (safe under the
//     lock: no live writer can be mid-write while we hold it).
func materialize(cacheRoot string, m *Manifest, dllBytes []byte) (string, *verifiedFile, error) {
	artifactDir := filepath.Join(cacheRoot, cachePlatformDir, m.SHA256)
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		return "", nil, &CacheError{Op: "create artifact dir", Path: artifactDir, Err: err}
	}

	pubMu.Lock()
	defer pubMu.Unlock()

	lock, err := acquirePublicationLock(filepath.Join(cacheRoot, publicationLockName))
	if err != nil {
		return "", nil, &CacheError{Op: "acquire publication lock", Path: filepath.Join(cacheRoot, publicationLockName), Err: err}
	}
	defer lock.release()

	final := filepath.Join(artifactDir, cacheArtifactName(m))

	// Reject reparse redirects and locations outside the configured root.
	if err := checkFinalPath(cacheRoot, artifactDir, final); err != nil {
		return "", nil, err
	}

	// Steps 3/4: inspect an existing final file through the verified open.
	if vf, data, err := inspectFinal(final); err == nil {
		if sha256Hex(data) == m.SHA256 && int64(len(data)) == m.Bytes {
			removeStaleStaging(artifactDir, m)
			return final, vf, nil
		}
		// Corrupt final file: typed error, never overwritten. It could be
		// mapped by another process; a verified bundle override is the
		// recovery path.
		vf.close()
		return "", nil, &CorruptCacheError{
			Path: final,
			Op:   "verify existing",
			Err: fmt.Errorf("cached file fails hash verification (sha256 %s, want %s); not overwritten because it may be mapped by another process; recover with a verified bundle override or by clearing the cache directory",
				sha256Hex(data), m.SHA256),
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		// errors.Is unwraps the fmt-wrapped syscall errno; os.IsNotExist does
		// not follow arbitrary %w chains.
		return "", nil, &CorruptCacheError{Path: final, Op: "inspect", Err: err}
	}

	// Step 5: staging file in the same directory (same volume: rename cannot
	// cross drives), fsync, close, rename.
	staging, err := writeStaging(artifactDir, m, dllBytes)
	if err != nil {
		return "", nil, &CacheError{Op: "write staging", Path: staging, Err: err}
	}
	if err := os.Rename(staging, final); err != nil {
		os.Remove(staging)
		return "", nil, &CacheError{Op: "publish (rename)", Path: final, Err: err}
	}

	// Step 6: always verify the final file through the retained-handle open
	// before use; the handle IS the verification (it denies write/delete, so
	// the hashed bytes cannot change underneath the loader).
	vf, data, err := inspectFinal(final)
	if err != nil {
		return "", nil, &CorruptCacheError{Path: final, Op: "verify", Err: err}
	}
	if sha256Hex(data) != m.SHA256 || int64(len(data)) != m.Bytes {
		vf.close()
		return "", nil, &CorruptCacheError{Path: final, Op: "verify", Err: fmt.Errorf("published file hashes to %s, want %s", sha256Hex(data), m.SHA256)}
	}

	// Step 7: killed-writer recovery — under the lock, any staging file left
	// belongs to a dead writer.
	removeStaleStaging(artifactDir, m)

	publications.Add(1)
	return final, vf, nil
}

// inspectFinal opens the final file with the verified sharing mode (denying
// write/delete) and reads its full content through that handle. The returned
// verifiedFile stays open: on a hash match it becomes the loader's retained
// verified handle; callers close it themselves on any mismatch path.
func inspectFinal(final string) (*verifiedFile, []byte, error) {
	vf, err := openVerifiedFile(final)
	if err != nil {
		return nil, nil, err
	}
	data, err := vf.readAll()
	if err != nil {
		vf.close()
		return nil, nil, err
	}
	return vf, data, nil
}

// checkFinalPath rejects reparse points on the artifact directory and the
// final file, and requires the final path to resolve inside the cache root.
func checkFinalPath(cacheRoot, artifactDir, final string) error {
	for _, p := range []string{cacheRoot, artifactDir, final} {
		if p == "" {
			continue
		}
		reparse, err := isReparsePoint(p)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return &CacheError{Op: "attribute check", Path: p, Err: err}
		}
		if reparse {
			return &CacheError{Op: "reject reparse redirect", Path: p, Err: fmt.Errorf("path carries a reparse point")}
		}
	}
	rel, err := filepath.Rel(cacheRoot, final)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return &CacheError{Op: "reject out-of-cache path", Path: final, Err: fmt.Errorf("final path escapes cache root %s", cacheRoot)}
	}
	return nil
}

// isReparsePoint reports whether path itself is a reparse point (symlink,
// junction or other redirect). os.Lstat does not follow the link, so a
// redirected final file is detected before it is hashed or loaded. Together
// with the in-directory check above it implements the contract's "resolve the
// final path and reject unexpected reparse redirects".
func isReparsePoint(path string) (bool, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0, nil
}

// writeStaging writes the artifact bytes to a unique staging name in dir,
// fsyncs and closes. It returns the staging path (also on error, for logs).
func writeStaging(dir string, m *Manifest, dllBytes []byte) (string, error) {
	rnd := make([]byte, 8)
	if _, err := rand.Read(rnd); err != nil {
		return "", err
	}
	staging := filepath.Join(dir, fmt.Sprintf("%s%d-%s", stagingPrefixFor(m), os.Getpid(), hex.EncodeToString(rnd)))
	f, err := os.OpenFile(staging, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return staging, err
	}
	if _, err := f.Write(dllBytes); err != nil {
		f.Close()
		os.Remove(staging)
		return staging, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(staging)
		return staging, err
	}
	return staging, f.Close()
}

// removeStaleStaging deletes staging files left by killed writers. Callers
// must hold the publication lock: no live writer can be mid-write then.
func removeStaleStaging(dir string, m *Manifest) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := stagingPrefixFor(m)
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
