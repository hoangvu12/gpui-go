package native

import (
	"fmt"
	"strconv"
)

// Sentinel errors. Every failure mode named by the distribution contract is a
// distinct value so consumers (and tests) can distinguish identity failures
// from OS/runtime failures with errors.Is.
var (
	// ErrUnsupportedTarget: this build of the loader cannot run on the
	// current GOOS/GOARCH (the artifact is Windows AMD64 only).
	ErrUnsupportedTarget = fmt.Errorf("native: unsupported host for the gpui-go native artifact (want windows/amd64)")

	// ErrNoArtifact: no embedded artifact is compiled in and no bundle was
	// configured (measurement builds with -tags gpui_native_noembed).
	ErrNoArtifact = fmt.Errorf("native: no embedded native artifact and no bundle configured")

	// ErrBundlePath: the bundle override is not a usable absolute directory.
	ErrBundlePath = fmt.Errorf("native: invalid bundle directory")

	// ErrWrongHash: artifact bytes do not match the manifest's SHA-256 (or
	// the manifest's recorded size).
	ErrWrongHash = fmt.Errorf("native: artifact hash mismatch")

	// ErrWrongArchitecture: the PE machine type is not IMAGE_FILE_MACHINE_AMD64.
	ErrWrongArchitecture = fmt.Errorf("native: artifact machine type is not AMD64")

	// ErrABIMismatch: bootstrap-table identity mismatch (record layout,
	// magic, ABI version, CE pin or a null required function slot).
	ErrABIMismatch = fmt.Errorf("native: ABI identity mismatch")

	// ErrMissingCapability: the artifact lacks a required capability bit.
	ErrMissingCapability = fmt.Errorf("native: artifact missing required capability")

	// ErrMissingExport: a required export (e.g. gpui_go_abi) is absent.
	ErrMissingExport = fmt.Errorf("native: required export missing from artifact")

	// ErrBundleManifest: a bundle-local manifest tried to authorize a
	// different identity than the release's embedded authoritative manifest.
	ErrBundleManifest = fmt.Errorf("native: bundle manifest does not match the embedded authoritative manifest")

	// ErrCacheUnavailable: the per-user cache root cannot be created or used
	// (read-only location, blocked path, ...). Never falls back to a temp dir.
	ErrCacheUnavailable = fmt.Errorf("native: per-user artifact cache unavailable")

	// ErrCacheCorrupt: a cache file failed verification and could not be
	// replaced (possibly mapped by another process).
	ErrCacheCorrupt = fmt.Errorf("native: cache artifact corrupt and irreplaceable")

	// ErrOSLoad: the OS loader rejected the artifact (LoadLibraryExW
	// failure). Distinct from every identity check above; carries the
	// syscall errno (e.g. ERROR_MOD_NOT_FOUND, ERROR_ACCESS_DENIED).
	ErrOSLoad = fmt.Errorf("native: OS load failure")

	// ErrBadStatus: a native call returned a non-zero status code.
	ErrBadStatus = fmt.Errorf("native: native call failed")

	// ErrClosed: the library handle was already released.
	ErrClosed = fmt.Errorf("native: library released")

	// ErrDoubleRelease: Close was called more than once.
	ErrDoubleRelease = fmt.Errorf("native: library already released once")
)

// HashError reports an artifact that does not match its manifest hash/size.
type HashError struct {
	Kind     string // "manifest-declared sha256", "manifest-declared bytes", ...
	Path     string
	Expected string
	Actual   string
}

func (e *HashError) Error() string {
	return ErrWrongHash.Error() + ": " + e.Path + ": " + e.Kind + ": expected " + e.Expected + ", got " + e.Actual
}

func (e *HashError) Unwrap() error { return ErrWrongHash }

// ArchitectureError reports a wrong PE machine type (or unparseable PE).
type ArchitectureError struct {
	Path     string
	Expected string
	Actual   string
	Reason   string
}

func (e *ArchitectureError) Error() string {
	if e.Reason != "" {
		return ErrWrongArchitecture.Error() + ": " + e.Path + ": " + e.Reason
	}
	return ErrWrongArchitecture.Error() + ": " + e.Path + ": expected machine " + e.Expected + ", got " + e.Actual
}

func (e *ArchitectureError) Unwrap() error { return ErrWrongArchitecture }

// ABIError reports a bootstrap-table identity mismatch.
type ABIError struct {
	Path     string
	Field    string
	Expected string
	Actual   string
}

func (e *ABIError) Error() string {
	return ErrABIMismatch.Error() + ": " + e.Path + ": field " + e.Field + ": expected " + e.Expected + ", got " + e.Actual
}

func (e *ABIError) Unwrap() error { return ErrABIMismatch }

// CapabilityError reports a missing required capability bit.
type CapabilityError struct {
	Path     string
	Required uint64
	Actual   uint64
	Detail   string
}

func (e *CapabilityError) Error() string {
	if e.Detail != "" {
		return ErrMissingCapability.Error() + ": " + e.Path + ": " + e.Detail
	}
	return ErrMissingCapability.Error() + ": " + e.Path + ": required mask " +
		"0x" + strconv.FormatUint(e.Required, 16) + ", artifact has 0x" + strconv.FormatUint(e.Actual, 16)
}

func (e *CapabilityError) Unwrap() error { return ErrMissingCapability }

// ExportError reports a missing required export.
type ExportError struct {
	Path string
	Name string
	Err  error
}

func (e *ExportError) Error() string {
	return ErrMissingExport.Error() + ": " + e.Path + ": " + e.Name + ": " + errText(e.Err)
}

func (e *ExportError) Unwrap() error { return ErrMissingExport }

// CacheError reports an unusable per-user cache (ErrCacheUnavailable).
type CacheError struct {
	Op   string
	Path string
	Err  error
}

func (e *CacheError) Error() string {
	return ErrCacheUnavailable.Error() + ": " + e.Op + " " + e.Path + ": " + errText(e.Err)
}

func (e *CacheError) Unwrap() error { return ErrCacheUnavailable }

// CorruptCacheError reports a cache file that failed verification and could
// not be replaced (ErrCacheCorrupt).
type CorruptCacheError struct {
	Path string
	Op   string
	Err  error
}

func (e *CorruptCacheError) Error() string {
	return ErrCacheCorrupt.Error() + ": " + e.Op + " " + e.Path + ": " + errText(e.Err)
}

func (e *CorruptCacheError) Unwrap() error { return ErrCacheCorrupt }

// OSLoadError wraps a LoadLibraryExW failure (ErrOSLoad); Err is the raw
// syscall errno, e.g. ERROR_MOD_NOT_FOUND (126) or ERROR_ACCESS_DENIED (5).
type OSLoadError struct {
	Path string
	Err  error
}

func (e *OSLoadError) Error() string {
	return ErrOSLoad.Error() + ": " + e.Path + ": " + errText(e.Err)
}

func (e *OSLoadError) Unwrap() error { return ErrOSLoad }

// StatusError reports a non-zero native status code.
type StatusError struct {
	Code   int32
	Name   string
	Detail string
}

func (e *StatusError) Error() string {
	detail := e.Detail
	if detail != "" {
		detail = ": " + detail
	}
	return ErrBadStatus.Error() + ": status " + strconv.Itoa(int(e.Code)) + " (" + e.Name + ")" + detail
}

func (e *StatusError) Unwrap() error { return ErrBadStatus }

func errText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
