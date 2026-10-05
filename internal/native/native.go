// Package native loads the gpui-go native artifact on Windows AMD64 from a
// pure-Go, CGO_ENABLED=0 consumer.
//
// It implements the ticket02 slice of the module and native distribution
// contract (docs/distribution-contract.md):
//
//   - the exact prebuilt DLL and its authoritative manifest are embedded in
//     this package (internal/native/artifacts) and materialized on first use
//     into the per-user cache below
//     os.UserCacheDir()/gpui-go/native/windows-amd64/<full-sha256>/, with
//     the full hash also in the DLL basename
//     (gpui_go_native-<sha256hex>.dll);
//   - an explicit, verified, absolute bundle directory can replace the
//     embedded bytes (Options.BundleDir or GPUI_GO_NATIVE_BUNDLE) for offline
//     installation; a bundle-local manifest can never authorize a different
//     identity than the embedded authoritative manifest;
//   - publication is protected by a cross-process LockFileEx lock; staging
//     files are fsynced, renamed, and the final file's full SHA-256 is always
//     verified through the retained handle before use; a corrupt FINAL file
//     is a typed error and is never overwritten (it may be mapped by another
//     process — the verified bundle override is the recovery path), while
//     crashed writers are recovered by ignoring/cleaning uniquely named
//     staging files;
//   - the selected DLL is opened for read with sharing that DENIES write and
//     delete (FILE_SHARE_READ only), hashed through that same open handle,
//     and the verified handle is retained across loading for the module's
//     process lifetime;
//   - identity is checked before any call: manifest hash == DLL SHA-256, PE
//     machine == IMAGE_FILE_MACHINE_AMD64, bootstrap-table magic, ABI
//     version, CE pin and required capability bits;
//   - loading uses LoadLibraryExW with the absolute verified path and
//     LOAD_LIBRARY_SEARCH_SYSTEM32 only — dependencies resolve from System32
//     (the artifact imports only OS system components);
//     LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR stays reserved for future verified
//     private companion DLLs per the contract; no application-dir, CWD or
//     PATH search ever happens;
//   - the DLL stays loaded for process lifetime: there is no FreeLibrary and
//     no hot unload;
//   - no network fetch, no compiler fallback, no shared temp dir.
//
// All failures are typed (see errors.go): wrong hash, wrong architecture, ABI
// mismatch, missing capability, missing export, OS load failure, cache
// problems and native status codes are distinguishable sentinel values.
package native

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"unsafe"
)

// Options configures Load.
type Options struct {
	// BundleDir selects an explicit bundle directory override: an absolute
	// path to a directory containing gpui_go_native.dll and manifest.json.
	// The bundle's DLL must hash to the embedded authoritative manifest's
	// SHA-256; identity checks are never relaxed. When empty, the
	// GPUI_GO_NATIVE_BUNDLE environment variable is consulted; when that is
	// also empty, the embedded artifact is used.
	BundleDir string

	// CacheRoot overrides the per-user cache root (default:
	// os.UserCacheDir()/gpui-go/native). Intended for tests and for
	// deployment-controlled cache locations; it must be absolute.
	CacheRoot string
}

// Identity is the verified identity record of a loaded library.
type Identity struct {
	// Name is the artifact file name ("gpui_go_native.dll").
	Name string
	// SHA256 is the verified full SHA-256 of the loaded artifact bytes.
	SHA256 string
	// Bytes is the artifact size in bytes.
	Bytes int64
	// GzipBytes is the measured gzip-9 size of the artifact (provenance).
	GzipBytes int64
	// CECommit is the pinned gpui-CE commit of the artifact family.
	CECommit string
	// ABIVersion is the private ABI schema major version.
	ABIVersion uint32
	// NativeRevision is the native bridge source revision.
	NativeRevision uint32
	// Capabilities is the compiled capability bitmask.
	Capabilities uint64
	// CapabilityNames lists the assigned capability bit names.
	CapabilityNames []string
	// Machine is the verified PE machine ("amd64").
	Machine string
	// Target is the build target triple of the artifact.
	Target string
	// Source is where the loaded file came from: "cache" or "bundle".
	Source string
	// Path is the absolute path the DLL was loaded from.
	Path string
	// BuiltAt, Rustc, CRTStatic record build provenance.
	BuiltAt   string
	Rustc     string
	CRTStatic string
	// Imports lists the imported DLL summary from the manifest.
	Imports []string
	// Generation is the process-unique generation token stamped at load.
	Generation uint64
}

// generation counter stamps every loaded library handle (the
// "generation-stamped handle" of the bootstrap slice).
var generationCounter atomic.Uint64

// Library is a loaded, verified native artifact handle. It is safe for
// concurrent use. The underlying OS module stays resident for the process
// lifetime by design (no FreeLibrary, no hot unload); Close performs the
// logical, exactly-once release of the handle and releases the retained
// verified file handle at that point — in practice a shutdown-time
// operation. While the library is alive, the verified handle keeps denying
// write and delete access to the artifact file (distribution contract:
// "retain the verified-file handle across loading; retain the protection for
// the module's process lifetime").
type Library struct {
	identity  Identity
	module    uintptr
	table     abiTable
	probeAddr uintptr
	verified  *verifiedFile
	released  atomic.Bool
}

// Load loads and verifies the native artifact according to opt.
//
// The source selection order is: explicit Options.BundleDir, then the
// GPUI_GO_NATIVE_BUNDLE environment variable, then the embedded default. The
// embedded default is materialized into the per-user cache and loaded from
// there; a bundle is loaded directly from its verified directory.
func Load(opt Options) (*Library, error) {
	if err := hostSupported(); err != nil {
		return nil, err
	}

	bundleDir := opt.BundleDir
	if bundleDir == "" {
		bundleDir = os.Getenv("GPUI_GO_NATIVE_BUNDLE")
	}
	if bundleDir != "" {
		return loadFromBundle(bundleDir)
	}
	return loadEmbedded(opt.CacheRoot)
}

// loadFromBundle implements the verified absolute bundle override.
func loadFromBundle(bundleDir string) (*Library, error) {
	if !filepath.IsAbs(bundleDir) {
		return nil, fmt.Errorf("%w: %q is not an absolute path", ErrBundlePath, bundleDir)
	}
	info, err := os.Stat(bundleDir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: %q is not a usable directory: %v", ErrBundlePath, bundleDir, err)
	}

	dllPath := filepath.Join(bundleDir, artifactBaseName)
	manifestPath := filepath.Join(bundleDir, "manifest.json")

	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("%w: reading bundle manifest: %v", ErrBundlePath, err)
	}
	bundleManifest, err := parseManifest(manifestBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBundlePath, err)
	}

	// Open the selected DLL with sharing that denies write and delete, and
	// read/hash it through that same retained handle (distribution contract).
	vf, err := openVerifiedFile(dllPath)
	if err != nil {
		return nil, fmt.Errorf("%w: opening bundle artifact: %v", ErrBundlePath, err)
	}
	handedOver := false
	defer func() {
		if !handedOver {
			vf.close()
		}
	}()
	dllBytes, err := vf.readAll()
	if err != nil {
		return nil, fmt.Errorf("%w: reading bundle artifact: %v", ErrBundlePath, err)
	}

	// Self-consistency first: the bundle manifest must describe the bundle's
	// actual bytes (hash, size) and the machine must be AMD64.
	if err := verifyArtifactBytes(bundleManifest, dllBytes, dllPath); err != nil {
		return nil, err
	}

	// Authority: the embedded manifest of this Go release is authoritative; a
	// bundle manifest can never authorize a replacement hash or identity.
	auth, err := authoritativeManifest()
	if err != nil {
		return nil, err
	}
	if err := bundleManifest.matchesAuthority(auth); err != nil {
		return nil, err
	}

	// The bundle DLL must also byte-match the authoritative hash.
	if sha256Hex(dllBytes) != auth.SHA256 {
		return nil, &HashError{Kind: "embedded authoritative sha256", Path: dllPath, Expected: auth.SHA256, Actual: sha256Hex(dllBytes)}
	}

	handedOver = true
	return newLibrary(auth, bundleManifest, dllPath, "bundle", vf)
}

// loadEmbedded implements the default embedded delivery.
func loadEmbedded(cacheRootOverride string) (*Library, error) {
	if len(embeddedDLL) == 0 || len(embeddedManifest) == 0 {
		return nil, ErrNoArtifact
	}
	auth, err := authoritativeManifest()
	if err != nil {
		return nil, err
	}
	// The embedded bytes must match the embedded manifest. This is guaranteed
	// by the build, but verifying costs one hash and catches a corrupted
	// embedding (e.g. a text-mode rewrite of the artifact).
	if err := verifyArtifactBytes(auth, embeddedDLL, artifactBaseName+" (embedded)"); err != nil {
		return nil, err
	}

	cacheRoot := cacheRootOverride
	if cacheRoot == "" {
		cacheRoot, err = defaultCacheRoot()
		if err != nil {
			return nil, &CacheError{Op: "resolve default root", Path: "", Err: err}
		}
	} else if !filepath.IsAbs(cacheRoot) {
		return nil, fmt.Errorf("%w: cache root %q is not absolute", ErrCacheUnavailable, cacheRoot)
	}

	finalPath, vf, err := materialize(cacheRoot, auth, embeddedDLL)
	if err != nil {
		return nil, err
	}
	// materialize verified the final file's full SHA-256 through the open
	// verified handle and returns that handle; while it is retained the file
	// cannot be written or deleted, so no re-read is needed before loading.

	return newLibrary(auth, auth, finalPath, "cache", vf)
}

// authoritativeManifest parses the embedded authoritative manifest.
func authoritativeManifest() (*Manifest, error) {
	m, err := parseManifest(embeddedManifest)
	if err != nil {
		return nil, fmt.Errorf("native: embedded manifest invalid: %w", err)
	}
	return m, nil
}

// newLibrary loads the OS module, resolves and validates the bootstrap table
// and stamps the generation token. All identity checks precede any call. It
// takes ownership of vf, the open verified file handle: on success the
// Library retains it (denying write/delete on the artifact file); on failure
// it is closed before returning.
func newLibrary(auth, display *Manifest, path, source string, vf *verifiedFile) (*Library, error) {
	module, err := loadLibraryExRestricted(path)
	if err != nil {
		vf.close()
		return nil, err
	}

	table, err := loadTableFromModule(module, path)
	if err != nil {
		vf.close()
		return nil, err
	}
	if err := validateAbiTable(table, auth, path); err != nil {
		vf.close()
		return nil, err
	}

	lib := &Library{
		module:   module,
		table:    *table,
		verified: vf,
		identity: Identity{
			Name:            display.Name,
			SHA256:          auth.SHA256,
			Bytes:           auth.Bytes,
			GzipBytes:       display.GzipBytes,
			CECommit:        auth.CECommit,
			ABIVersion:      table.abiVersion,
			NativeRevision:  table.nativeRevision,
			Capabilities:    table.capabilities,
			CapabilityNames: display.Capabilities,
			Machine:         display.Machine,
			Target:          display.Target,
			Source:          source,
			Path:            path,
			BuiltAt:         display.BuiltAt,
			Rustc:           display.Rustc,
			CRTStatic:       display.CRTStaticOutcome,
			Imports:         display.Imports,
			Generation:      generationCounter.Add(1),
		},
	}

	// The panic probe is a test-only diagnostic export; it is optional.
	if addr, err := getProcAddr(module, "gpui_go_panic_probe"); err == nil {
		lib.probeAddr = addr
	}
	return lib, nil
}

// Identity returns the verified identity of the loaded artifact.
func (l *Library) Identity() Identity { return l.identity }

// RoundTrip performs the checked bootstrap buffer round trip: the native code
// validates the request, computes the FNV-1a 64 checksum of the request bytes
// and echoes them back. The response buffer is Go-owned; the DLL retains
// nothing. data must be at most 4096 bytes.
func (l *Library) RoundTrip(data []byte) (echo []byte, checksum uint64, err error) {
	if l.released.Load() {
		return nil, 0, ErrClosed
	}
	if len(data) > maxBufferLen {
		return nil, 0, &StatusError{
			Code:   statusLengthOverMax,
			Name:   statusName(statusLengthOverMax),
			Detail: fmt.Sprintf("input length %d exceeds the %d-byte bootstrap bound (rejected before the call)", len(data), maxBufferLen),
		}
	}
	echo = make([]byte, len(data))

	req := bufferRequest{
		magic:      abiMagic,
		abiVersion: abiVersion,
		length:     uint32(len(data)),
	}
	if len(data) > 0 {
		req.data = uintptr(unsafe.Pointer(&data[0]))
	}
	res := bufferResponse{}
	if len(echo) > 0 {
		res.data = uintptr(unsafe.Pointer(&echo[0]))
	}

	code, err := callRoundTripRaw(l.table.bufferRoundTrip, &req, &res)
	if err != nil {
		return nil, 0, err
	}
	// Keep every buffer referenced by the request/response records alive
	// until the foreign call has returned.
	runtime.KeepAlive(data)
	runtime.KeepAlive(echo)
	runtime.KeepAlive(&req)
	runtime.KeepAlive(&res)

	if code != statusOK {
		return nil, 0, &StatusError{Code: code, Name: statusName(code), Detail: fmt.Sprintf("native status echoed in response: %d", res.status)}
	}
	if res.status != statusOK {
		return nil, 0, &StatusError{Code: res.status, Name: statusName(res.status)}
	}
	if res.echoedLen != uint32(len(data)) {
		return nil, 0, fmt.Errorf("%w: echoed length %d, want %d", ErrBadStatus, res.echoedLen, len(data))
	}
	wantChecksum := fnv1a64(data)
	if res.checksum != wantChecksum {
		return nil, 0, fmt.Errorf("%w: native checksum %#x, want %#x", ErrBadStatus, res.checksum, wantChecksum)
	}
	return echo, res.checksum, nil
}

// PanicProbe invokes the artifact's deliberate panic probe and verifies the
// panic was contained as status 7 before returning across the ABI. It exists
// so conformance tests (and this package's own tests) can observe real panic
// containment; application code never needs it.
func (l *Library) PanicProbe() error {
	if l.released.Load() {
		return ErrClosed
	}
	if l.probeAddr == 0 {
		return fmt.Errorf("%w: panic probe export not present", ErrMissingExport)
	}
	code, err := callNoArgExport(l.probeAddr)
	if err != nil {
		return err
	}
	if code != statusPanic {
		return &StatusError{Code: code, Name: statusName(code), Detail: "panic probe must report containment (status 7)"}
	}
	return nil
}

// Close logically releases the library handle exactly once and releases the
// retained verified file handle — in practice a shutdown-time operation (the
// distribution contract keeps the protection for the module's process
// lifetime; a long-lived application simply keeps the Library open until
// exit). The OS module stays loaded for the process lifetime by design (no
// FreeLibrary, no hot unload). A second Close reports ErrDoubleRelease;
// calls after Close report ErrClosed.
func (l *Library) Close() error {
	if !l.released.CompareAndSwap(false, true) {
		return ErrDoubleRelease
	}
	l.verified.close()
	return nil
}

// fnv1a64 is the Go-side reference of the native checksum (FNV-1a, 64-bit).
func fnv1a64(b []byte) uint64 {
	const (
		offsetBasis = 0xcbf29ce484222325
		prime       = 0x100000001b3
	)
	h := uint64(offsetBasis)
	for _, c := range b {
		h ^= uint64(c)
		h *= prime
	}
	return h
}
