//go:build windows

package native

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"unsafe"

	"debug/pe"
)

// Every test pins its cache root to a temp directory so tests never touch the
// real per-user cache and always start cold. The observable default-root path
// is exercised by internal/native/cmd/demo.

// testDir creates a unique temp directory whose removal failure is ignored.
// t.TempDir would fail the whole test at cleanup: a loaded DLL stays mapped
// for the process lifetime (no FreeLibrary by design) and Windows refuses to
// delete a mapped image file. Best-effort cleanup keeps the tests honest
// without penalizing the residency policy.
func testDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gpui-go-native-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func mustLoad(t *testing.T, opt Options) *Library {
	t.Helper()
	lib, err := Load(opt)
	if err != nil {
		t.Fatalf("Load(%+v) failed: %v", opt, err)
	}
	return lib
}

func testAuthority(t *testing.T) *Manifest {
	t.Helper()
	m, err := authoritativeManifest()
	if err != nil {
		t.Fatalf("embedded manifest invalid: %v", err)
	}
	return m
}

// writeBundle writes a bundle directory from the given bytes and manifest.
func writeBundle(t *testing.T, dll []byte, m *Manifest) string {
	t.Helper()
	dir := testDir(t)
	if err := os.WriteFile(filepath.Join(dir, artifactBaseName), dll, 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// manifestForBytes clones the authoritative manifest and re-points it at the
// given bytes (correct sha256 + size): a self-consistent manifest for
// artificially crafted artifacts.
func manifestForBytes(t *testing.T, dll []byte) *Manifest {
	t.Helper()
	m := *testAuthority(t)
	m.SHA256 = sha256Hex(dll)
	m.Bytes = int64(len(dll))
	return &m
}

// (a) happy path from the embedded artifact: load, identity, round trips.
func TestLoadEmbeddedIdentityAndRoundTrip(t *testing.T) {
	auth := testAuthority(t)
	lib := mustLoad(t, Options{CacheRoot: testDir(t)})
	defer lib.Close()

	if len(embeddedDLL) == 0 {
		t.Fatal("no embedded artifact compiled in")
	}
	if sha256Hex(embeddedDLL) != auth.SHA256 {
		t.Fatalf("embedded DLL hash %s does not match embedded manifest %s", sha256Hex(embeddedDLL), auth.SHA256)
	}

	id := lib.Identity()
	if id.SHA256 != auth.SHA256 {
		t.Errorf("identity sha256 = %s, want manifest %s", id.SHA256, auth.SHA256)
	}
	if id.Bytes != auth.Bytes || int64(len(embeddedDLL)) != auth.Bytes {
		t.Errorf("identity bytes = %d, want %d", id.Bytes, auth.Bytes)
	}
	if id.ABIVersion != 1 {
		t.Errorf("identity abi version = %d, want 1", id.ABIVersion)
	}
	if id.NativeRevision != nativeRevision {
		t.Errorf("identity native revision = %d, want %d", id.NativeRevision, nativeRevision)
	}
	if id.CECommit != ceCommitHex {
		t.Errorf("identity ce commit = %q, want %q", id.CECommit, ceCommitHex)
	}
	if id.Capabilities&capBootstrapBufferRoundTrip == 0 {
		t.Errorf("identity capabilities %#x missing bootstrap bit", id.Capabilities)
	}
	if id.Machine != "amd64" {
		t.Errorf("identity machine = %q, want amd64", id.Machine)
	}
	if id.Source != "cache" {
		t.Errorf("identity source = %q, want cache", id.Source)
	}
	if !filepath.IsAbs(id.Path) {
		t.Errorf("loaded path %q is not absolute", id.Path)
	}
	// The contract cache layout: <root>/windows-amd64/<sha>/gpui_go_native-<sha>.dll.
	wantBase := "gpui_go_native-" + auth.SHA256 + ".dll"
	if filepath.Base(id.Path) != wantBase {
		t.Errorf("loaded path base = %q, want %q (hash in basename)", filepath.Base(id.Path), wantBase)
	}
	if !strings.Contains(filepath.ToSlash(id.Path), "/windows-amd64/"+auth.SHA256+"/") {
		t.Errorf("loaded path %q does not use the contract layout windows-amd64/<sha>/", id.Path)
	}
	if id.Generation == 0 {
		t.Error("identity generation not stamped")
	}

	// The cache path is keyed by the full sha256 hex (checked via id.Path
	// above); the round-trip corpus follows.
	cases := [][]byte{
		{},
		{0x41},
		bytes.Repeat([]byte("gpui-go-bootstrap!"), 15)[:15],
		bytes.Repeat([]byte{0x00, 0xFF, 0x7F}, 256),
		make([]byte, maxBufferLen),
	}
	for i, data := range cases {
		echo, checksum, err := lib.RoundTrip(data)
		if err != nil {
			t.Fatalf("round trip %d (len %d): %v", i, len(data), err)
		}
		if len(echo) != len(data) {
			t.Fatalf("round trip %d: echoed len %d, want %d", i, len(echo), len(data))
		}
		if !bytes.Equal(echo, data) {
			t.Fatalf("round trip %d: echoed bytes differ", i)
		}
		if want := fnv1a64(data); checksum != want {
			t.Fatalf("round trip %d: checksum %#x, want %#x", i, checksum, want)
		}
	}

	// Deterministic content checksum for a known vector.
	_, checksum, err := lib.RoundTrip([]byte("foobar"))
	if err != nil {
		t.Fatal(err)
	}
	if checksum != 0x85944171f73967e8 {
		t.Errorf("FNV-1a64(\"foobar\") = %#x, want 0x85944171f73967e8", checksum)
	}
}

// (b) wrong-hash manifest rejection (mutated manifest), at the verification
// stage and through the full bundle Load path.
func TestWrongHashManifestRejected(t *testing.T) {
	auth := testAuthority(t)

	mutated := *auth
	mutated.SHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	err := verifyArtifactBytes(&mutated, embeddedDLL, "mutated-manifest")
	if !errors.Is(err, ErrWrongHash) {
		t.Errorf("mutated sha256: got %v, want ErrWrongHash", err)
	}

	wrongSize := *auth
	wrongSize.Bytes = auth.Bytes + 1
	err = verifyArtifactBytes(&wrongSize, embeddedDLL, "mutated-manifest")
	if !errors.Is(err, ErrWrongHash) {
		t.Errorf("mutated bytes: got %v, want ErrWrongHash", err)
	}

	// Full path: bundle whose manifest declares a hash that does not match
	// the bundle's DLL bytes.
	m := *auth
	m.SHA256 = mutated.SHA256
	bundle := writeBundle(t, embeddedDLL, &m)
	if _, err := Load(Options{BundleDir: bundle}); !errors.Is(err, ErrWrongHash) {
		t.Errorf("bundle with wrong-hash manifest: got %v, want ErrWrongHash", err)
	}
}

// fakePE crafts a minimal, debug/pe-parseable PE image with an arbitrary COFF
// machine. Handcrafted layout (328 bytes total):
//
//	offset 0x00: DOS header area: "MZ" and e_lfanew = 0x40 (rest zero);
//	             debug/pe always reads 96 bytes here, so the blob is ≥ 96.
//	offset 0x40: PE signature "PE\0\0".
//	offset 0x44: COFF header (20 bytes): machine, 0 sections, 0 symbols,
//	             SizeOfOptionalHeader = 240, characteristics 0x2002
//	             (IMAGE_FILE_EXECUTABLE_IMAGE | IMAGE_FILE_DLL).
//	offset 0x58: PE32+ optional header (240 bytes = 112 fixed + 16 data
//	             directories): magic 0x20b, NumberOfRvaAndSizes = 16, all
//	             data directories zero, no sections.
//
// debug/pe accepts this: the machine must be in its known list (i386 and
// amd64 are), SizeOfOptionalHeader selects the PE32+ parse, and 0 sections
// means nothing else is read.
func fakePE(t *testing.T, machine uint16) []byte {
	t.Helper()
	b := make([]byte, 0x148) // 328
	b[0], b[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(b[0x3c:], 0x40)
	copy(b[0x40:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(b[0x44:], machine)
	binary.LittleEndian.PutUint16(b[0x44+16:], 240) // SizeOfOptionalHeader
	binary.LittleEndian.PutUint16(b[0x44+18:], 0x2002)
	binary.LittleEndian.PutUint16(b[0x58:], 0x20b)  // PE32+ magic
	binary.LittleEndian.PutUint32(b[0x58+108:], 16) // NumberOfRvaAndSizes

	// Sanity: debug/pe must parse it and report the crafted machine.
	f, err := pe.NewFile(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("handcrafted PE does not parse: %v", err)
	}
	if f.Machine != machine {
		f.Close()
		t.Fatalf("handcrafted PE machine = %#x, want %#x", f.Machine, machine)
	}
	f.Close()
	return b
}

// (c) wrong architecture rejection: handcrafted PE with machine != AMD64 and
// a self-consistent manifest, rejected at the verification stage and through
// the full bundle Load path (verification runs before the authority check,
// so the machine error surfaces first).
func TestWrongArchitectureRejected(t *testing.T) {
	blob := fakePE(t, pe.IMAGE_FILE_MACHINE_I386)
	m := manifestForBytes(t, blob)

	err := verifyArtifactBytes(m, blob, "fake-i386.dll")
	if !errors.Is(err, ErrWrongArchitecture) {
		t.Fatalf("verifyArtifactBytes: got %v, want ErrWrongArchitecture", err)
	}
	arch, ok := err.(*ArchitectureError)
	if !ok || arch.Actual != "i386 (0x14c)" {
		t.Errorf("architecture error detail = %+v", err)
	}

	bundle := writeBundle(t, blob, m)
	_, err = Load(Options{BundleDir: bundle})
	if !errors.Is(err, ErrWrongArchitecture) {
		t.Errorf("bundle Load with i386 PE: got %v, want ErrWrongArchitecture", err)
	}

	// An unparseable PE is also an architecture-family failure.
	garbage := append([]byte(nil), embeddedDLL...)
	for i := range garbage {
		garbage[i] ^= 0xFF
	}
	err = verifyArtifactBytes(manifestForBytes(t, garbage), garbage, "garbage.dll")
	if !errors.Is(err, ErrWrongArchitecture) {
		t.Errorf("garbage PE: got %v, want ErrWrongArchitecture", err)
	}
}

// (d) ABI version mismatch rejection (and other table identity checks),
// through validateAbiTable with the real manifest and mutated table copies.
func TestABIMismatchRejected(t *testing.T) {
	auth := testAuthority(t)
	lib := mustLoad(t, Options{CacheRoot: testDir(t)})
	defer lib.Close()
	base := lib.table // a validated copy of the real table

	tests := []struct {
		name  string
		mutCB func(*abiTable)
		mutM  func(*Manifest)
		want  error
	}{
		{
			name:  "table abi newer than manifest",
			mutCB: func(tb *abiTable) { tb.abiVersion = 2 },
			want:  ErrABIMismatch,
		},
		{
			name: "manifest abi newer than table",
			mutM: func(m *Manifest) { m.ABIVersion = 2 },
			want: ErrABIMismatch,
		},
		{
			name:  "bad magic",
			mutCB: func(tb *abiTable) { tb.magic = 0xDEADBEEF },
			want:  ErrABIMismatch,
		},
		{
			name:  "ce commit mismatch",
			mutCB: func(tb *abiTable) { tb.ceCommit[0] ^= 0x01 },
			want:  ErrABIMismatch,
		},
		{
			name:  "record size self-check mismatch",
			mutCB: func(tb *abiTable) { tb.sizeOfTable = 999 },
			want:  ErrABIMismatch,
		},
		{
			name:  "record alignment self-check mismatch",
			mutCB: func(tb *abiTable) { tb.alignOfTable = 4 },
			want:  ErrABIMismatch,
		},
		{
			name:  "buffer request size self-check mismatch",
			mutCB: func(tb *abiTable) { tb.sizeOfBufferRequest = 32 },
			want:  ErrABIMismatch,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tb := base
			m := *auth
			if tt.mutCB != nil {
				tt.mutCB(&tb)
			}
			if tt.mutM != nil {
				tt.mutM(&m)
			}
			err := validateAbiTable(&tb, &m, "test")
			if !errors.Is(err, tt.want) {
				t.Errorf("got %v, want %v", err, tt.want)
			}
		})
	}
}

// (e) missing capability rejection.
func TestMissingCapabilityRejected(t *testing.T) {
	auth := testAuthority(t)
	lib := mustLoad(t, Options{CacheRoot: testDir(t)})
	defer lib.Close()

	for name, mask := range map[string]uint64{
		"no capabilities":   0,
		"only a future bit": 1 << 1,
	} {
		tb := lib.table
		tb.capabilities = mask
		err := validateAbiTable(&tb, auth, "test")
		if !errors.Is(err, ErrMissingCapability) {
			t.Errorf("%s: got %v, want ErrMissingCapability", name, err)
		}
	}

	// A null required function slot is reported as a missing capability too.
	tb := lib.table
	tb.bufferRoundTrip = 0
	err := validateAbiTable(&tb, auth, "test")
	if !errors.Is(err, ErrMissingCapability) {
		t.Errorf("null function slot: got %v, want ErrMissingCapability", err)
	}

	// Unknown extra bits must NOT be rejected (forward compatibility).
	tb = lib.table
	tb.capabilities |= 1 << 7
	if err := validateAbiTable(&tb, auth, "test"); err != nil {
		t.Errorf("unknown extra capability bit rejected: %v", err)
	}
}

// (f) corrupt FINAL cache file (distribution contract): hash verification
// fails, Load returns a typed ErrCacheCorrupt error, and the file is NOT
// overwritten — it could be mapped by another process. The recovery path is
// the explicit verified bundle override.
func TestCorruptCacheFileFailsTypedError(t *testing.T) {
	auth := testAuthority(t)
	root := testDir(t)
	artifactDir := filepath.Join(root, "windows-amd64", auth.SHA256)
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(artifactDir, cacheArtifactName(auth))
	corrupt := []byte("killed-writer partial bytes")
	if err := os.WriteFile(final, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}

	lib, err := Load(Options{CacheRoot: root})
	if lib != nil {
		lib.Close()
		t.Fatal("Load succeeded despite a corrupt final cache file")
	}
	if !errors.Is(err, ErrCacheCorrupt) {
		t.Fatalf("got %v, want ErrCacheCorrupt", err)
	}

	// The corrupt final file must be left exactly as it was found.
	got, readErr := os.ReadFile(final)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(got, corrupt) {
		t.Error("corrupt final cache file was overwritten; the contract forbids replacing a possibly mapped file")
	}

	// The contract recovery path: an explicit, verified bundle override works
	// even with a corrupt cache in place (and does not touch the cache).
	bundle := writeBundle(t, embeddedDLL, auth)
	lib = mustLoad(t, Options{BundleDir: bundle, CacheRoot: root})
	defer lib.Close()
	echo, _, rtErr := lib.RoundTrip([]byte("bundle-recovery"))
	if rtErr != nil || string(echo) != "bundle-recovery" {
		t.Fatalf("bundle recovery round trip: echo=%q err=%v", echo, rtErr)
	}
}

// Killed-writer crash recovery: uniquely named STAGING files are ignored and
// cleaned under the publication lock; the artifact is published fresh and
// verifies.
func TestStaleStagingFilesCleanedOnLoad(t *testing.T) {
	auth := testAuthority(t)
	root := testDir(t)
	artifactDir := filepath.Join(root, "windows-amd64", auth.SHA256)
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(artifactDir, stagingPrefixFor(auth)+"4242-deadbeefcafe")
	if err := os.WriteFile(stale, []byte("stale staging bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	lib := mustLoad(t, Options{CacheRoot: root})
	defer lib.Close()

	if _, err := os.Lstat(stale); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stale staging file not cleaned: %v", err)
	}
	final := filepath.Join(artifactDir, cacheArtifactName(auth))
	data, err := os.ReadFile(final)
	if err != nil {
		t.Fatal(err)
	}
	if sha256Hex(data) != auth.SHA256 {
		t.Errorf("published file hash %s, want %s", sha256Hex(data), auth.SHA256)
	}
}

// (g) unwritable cache directory: Load fails with a clear typed error, no
// panic, no fallback to any temp directory.
//
// Windows' read-only attribute on directories does not block file creation,
// and ACL-based denial is environment-dependent, so the deterministic
// simulation blocks the cache root path with a regular file (directory
// creation then fails, exactly as a read-only location would).
func TestUnwritableCacheDirFailsCleanly(t *testing.T) {
	blocker := filepath.Join(testDir(t), "blocker")
	if err := os.WriteFile(blocker, []byte("occupied"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(blocker, "gpui-go", "native")

	lib, err := Load(Options{CacheRoot: root})
	if lib != nil {
		lib.Close()
		t.Fatal("Load succeeded with an unusable cache root")
	}
	if !errors.Is(err, ErrCacheUnavailable) {
		t.Fatalf("got %v, want ErrCacheUnavailable", err)
	}

	// Relative cache roots are rejected the same way.
	if _, err := Load(Options{CacheRoot: "relative/path"}); !errors.Is(err, ErrCacheUnavailable) {
		t.Errorf("relative cache root: got %v, want ErrCacheUnavailable", err)
	}
}

// (h) concurrent publication: N goroutines race Load with a cold cache; all
// succeed, exactly one publication happens, and no staging files remain.
func TestConcurrentPublication(t *testing.T) {
	auth := testAuthority(t)
	root := testDir(t)

	before := publications.Load()
	const n = 8
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			lib, err := Load(Options{CacheRoot: root})
			if err == nil {
				defer lib.Close()
				_, _, err = lib.RoundTrip([]byte("concurrent"))
			}
			errs[i] = err
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d failed: %v", i, err)
		}
	}

	artifactDir := filepath.Join(root, "windows-amd64", auth.SHA256)
	entries, err := os.ReadDir(artifactDir)
	if err != nil {
		t.Fatal(err)
	}
	files, staging := 0, 0
	for _, e := range entries {
		files++
		if e.Name() != cacheArtifactName(auth) {
			staging++
		}
	}
	if files != 1 || staging != 0 {
		t.Errorf("artifact dir contains %d files (%d unexpected): exactly one final DLL expected", files, staging)
	}
	if got := publications.Load() - before; got != 1 {
		t.Errorf("performed %d publications, want exactly 1", got)
	}
}

// (i) forced GC during and around calls: round trips and the panic probe stay
// stable while a goroutine hammers runtime.GC.
func TestForcedGCDuringCalls(t *testing.T) {
	lib := mustLoad(t, Options{CacheRoot: testDir(t)})
	defer lib.Close()

	done := make(chan struct{})
	var gcWG sync.WaitGroup
	gcWG.Add(1)
	go func() {
		defer gcWG.Done()
		for {
			select {
			case <-done:
				return
			default:
				runtime.GC()
			}
		}
	}()

	sizes := []int{0, 1, 7, 64, 4096}
	pattern := []byte("gc-stress-pattern")
	for i := 0; i < 300; i++ {
		data := make([]byte, sizes[i%len(sizes)])
		for j := range data {
			data[j] = pattern[j%len(pattern)]
		}
		echo, checksum, err := lib.RoundTrip(data)
		if err != nil {
			t.Fatalf("iteration %d (len %d): %v", i, len(data), err)
		}
		if !bytes.Equal(echo, data) || checksum != fnv1a64(data) {
			t.Fatalf("iteration %d: corrupted result under GC", i)
		}
		if err := lib.PanicProbe(); err != nil {
			t.Fatalf("iteration %d: panic probe: %v", i, err)
		}
	}
	close(done)
	gcWG.Wait()
}

// (j) status-code paths of the real DLL, through the low-level hook
// (callRoundTripRaw) with crafted request/response records.
func TestNativeStatusCodes(t *testing.T) {
	lib := mustLoad(t, Options{CacheRoot: testDir(t)})
	defer lib.Close()
	fn := lib.table.bufferRoundTrip
	if fn == 0 {
		t.Fatal("no round-trip function pointer")
	}

	okReq := func() bufferRequest {
		return bufferRequest{magic: abiMagic, abiVersion: abiVersion, length: 0}
	}

	t.Run("bad magic", func(t *testing.T) {
		req := okReq()
		req.magic = 0xDEADBEEF
		res := bufferResponse{status: -1}
		code, err := callRoundTripRaw(fn, &req, &res)
		if err != nil || code != statusBadMagic || res.status != statusBadMagic {
			t.Errorf("code=%d res.status=%d err=%v, want status %d everywhere", code, res.status, err, statusBadMagic)
		}
	})
	t.Run("bad abi version", func(t *testing.T) {
		req := okReq()
		req.abiVersion = 2
		res := bufferResponse{status: -1}
		code, err := callRoundTripRaw(fn, &req, &res)
		if err != nil || code != statusBadABIVersion || res.status != statusBadABIVersion {
			t.Errorf("code=%d res.status=%d err=%v, want status %d", code, res.status, err, statusBadABIVersion)
		}
	})
	t.Run("length over max", func(t *testing.T) {
		req := okReq()
		req.length = maxBufferLen + 1
		res := bufferResponse{status: -1}
		code, err := callRoundTripRaw(fn, &req, &res)
		if err != nil || code != statusLengthOverMax || res.status != statusLengthOverMax {
			t.Errorf("code=%d res.status=%d err=%v, want status %d", code, res.status, err, statusLengthOverMax)
		}
	})
	t.Run("null data with nonzero length", func(t *testing.T) {
		req := okReq()
		req.length = 4
		res := bufferResponse{status: -1}
		code, err := callRoundTripRaw(fn, &req, &res)
		if err != nil || code != statusNullData || res.status != statusNullData {
			t.Errorf("code=%d res.status=%d err=%v, want status %d", code, res.status, err, statusNullData)
		}
	})
	t.Run("null response data with nonzero length", func(t *testing.T) {
		data := []byte{1, 2, 3, 4}
		req := bufferRequest{magic: abiMagic, abiVersion: abiVersion, length: 4, data: uintptr(unsafe.Pointer(&data[0]))}
		res := bufferResponse{status: -1}
		code, err := callRoundTripRaw(fn, &req, &res)
		if err != nil || code != statusNullResponse || res.status != statusNullResponse {
			t.Errorf("code=%d res.status=%d err=%v, want status %d (null response buffer)", code, res.status, err, statusNullResponse)
		}
	})
	t.Run("null request", func(t *testing.T) {
		res := bufferResponse{status: -1}
		code, err := callRoundTripRaw(fn, nil, &res)
		if err != nil || code != statusNullRequest {
			t.Errorf("code=%d err=%v, want status %d", code, err, statusNullRequest)
		}
	})
	t.Run("null response", func(t *testing.T) {
		req := okReq()
		code, err := callRoundTripRaw(fn, &req, nil)
		if err != nil || code != statusNullResponse {
			t.Errorf("code=%d err=%v, want status %d", code, err, statusNullResponse)
		}
	})

	// The public API pre-rejects oversize input without calling the DLL.
	_, _, err := lib.RoundTrip(make([]byte, maxBufferLen+1))
	var se *StatusError
	if !errors.As(err, &se) || se.Code != statusLengthOverMax {
		t.Errorf("oversize RoundTrip: got %v, want StatusError(%d)", err, statusLengthOverMax)
	}

	// The panic probe reports containment (status 7) and the library remains
	// usable afterwards.
	if err := lib.PanicProbe(); err != nil {
		t.Errorf("panic probe: %v", err)
	}
	if echo, _, err := lib.RoundTrip([]byte("still-alive")); err != nil || string(echo) != "still-alive" {
		t.Errorf("round trip after panic probe: echo=%q err=%v", echo, err)
	}
}

// (k) bundle override: a verified copy of the embedded artifact works; a
// wrong-hash bundle and a bundle manifest that tries to authorize a
// replacement identity fail.
func TestBundleOverride(t *testing.T) {
	auth := testAuthority(t)

	t.Run("valid bundle", func(t *testing.T) {
		bundle := writeBundle(t, embeddedDLL, auth)
		lib := mustLoad(t, Options{BundleDir: bundle})
		defer lib.Close()
		id := lib.Identity()
		if id.Source != "bundle" {
			t.Errorf("source = %q, want bundle", id.Source)
		}
		if id.Path != filepath.Join(bundle, artifactBaseName) {
			t.Errorf("path = %q", id.Path)
		}
		if echo, _, err := lib.RoundTrip([]byte("from-bundle")); err != nil || string(echo) != "from-bundle" {
			t.Errorf("bundle round trip: echo=%q err=%v", echo, err)
		}
		// The cache is not used for bundle loads: no per-user write happens.
	})

	t.Run("bundle via environment variable", func(t *testing.T) {
		bundle := writeBundle(t, embeddedDLL, auth)
		t.Setenv("GPUI_GO_NATIVE_BUNDLE", bundle)
		lib := mustLoad(t, Options{})
		defer lib.Close()
		if lib.Identity().Source != "bundle" {
			t.Errorf("source = %q, want bundle", lib.Identity().Source)
		}
	})

	t.Run("wrong-hash bundle bytes", func(t *testing.T) {
		mutated := append([]byte(nil), embeddedDLL...)
		mutated[len(mutated)-1] ^= 0xFF
		bundle := writeBundle(t, mutated, auth)
		if _, err := Load(Options{BundleDir: bundle}); !errors.Is(err, ErrWrongHash) {
			t.Errorf("got %v, want ErrWrongHash", err)
		}
	})

	t.Run("mutated manifest hash", func(t *testing.T) {
		m := *auth
		m.SHA256 = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
		bundle := writeBundle(t, embeddedDLL, &m)
		if _, err := Load(Options{BundleDir: bundle}); !errors.Is(err, ErrWrongHash) {
			t.Errorf("got %v, want ErrWrongHash", err)
		}
	})

	t.Run("bundle manifest cannot authorize replacement", func(t *testing.T) {
		// A different DLL whose manifest is self-consistent: hash and size
		// match the new bytes, machine is fine, but the embedded manifest is
		// authoritative, so this must be rejected.
		replacement := append([]byte(nil), embeddedDLL...)
		replacement[len(replacement)-2] ^= 0xFF
		m := manifestForBytes(t, replacement)
		bundle := writeBundle(t, replacement, m)
		_, err := Load(Options{BundleDir: bundle})
		if !errors.Is(err, ErrBundleManifest) {
			t.Errorf("got %v, want ErrBundleManifest", err)
		}
	})

	t.Run("relative bundle path", func(t *testing.T) {
		if _, err := Load(Options{BundleDir: "relative/bundle"}); !errors.Is(err, ErrBundlePath) {
			t.Errorf("got %v, want ErrBundlePath", err)
		}
	})

	t.Run("missing files", func(t *testing.T) {
		dir := testDir(t)
		if _, err := Load(Options{BundleDir: dir}); !errors.Is(err, ErrBundlePath) {
			t.Errorf("got %v, want ErrBundlePath", err)
		}
	})
}

// Release exactly once: Close is idempotent-error, and calls after Close fail.
func TestCloseReleasesExactlyOnce(t *testing.T) {
	lib := mustLoad(t, Options{CacheRoot: testDir(t)})

	if _, _, err := lib.RoundTrip([]byte("before-close")); err != nil {
		t.Fatalf("round trip before close: %v", err)
	}
	if err := lib.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := lib.Close(); err == nil || !errors.Is(err, ErrDoubleRelease) {
		t.Errorf("second Close: got %v, want ErrDoubleRelease", err)
	}
	if _, _, err := lib.RoundTrip([]byte("after")); !errors.Is(err, ErrClosed) {
		t.Errorf("RoundTrip after Close: got %v, want ErrClosed", err)
	}
	if err := lib.PanicProbe(); !errors.Is(err, ErrClosed) {
		t.Errorf("PanicProbe after Close: got %v, want ErrClosed", err)
	}
}

// Cache path containment: a reparse point inside the cache (e.g. a symlinked
// final file) is rejected. Skipped when the environment cannot create
// symlinks.
func TestCacheRejectsReparseRedirect(t *testing.T) {
	auth := testAuthority(t)
	root := testDir(t)
	artifactDir := filepath.Join(root, "windows-amd64", auth.SHA256)
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Real artifact outside the cache, symlinked into the artifact dir under
	// the hashed cache name.
	outside := filepath.Join(testDir(t), "real.dll")
	if err := os.WriteFile(outside, embeddedDLL, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(artifactDir, cacheArtifactName(auth))
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("environment cannot create symlinks: %v", err)
	}
	_, err := Load(Options{CacheRoot: root})
	if !errors.Is(err, ErrCacheUnavailable) {
		t.Fatalf("got %v, want ErrCacheUnavailable (reparse redirect rejected)", err)
	}
}

// Verified-handle retention (distribution contract): the selected DLL is
// opened for read with sharing that DENIES write and delete; while the
// Library retains the handle, another opener cannot obtain write access and
// the file cannot be deleted. Close releases the handle (shutdown-time).
func TestVerifiedHandleDeniesWriteAndDelete(t *testing.T) {
	lib := mustLoad(t, Options{CacheRoot: testDir(t)})
	path := lib.Identity().Path

	// Another opener requesting GENERIC_WRITE is denied with a sharing
	// violation while the verified handle is retained.
	if err := openForWriteProbe(path); err == nil {
		t.Fatal("write access granted while the verified handle is retained")
	} else if en, ok := err.(syscall.Errno); !ok || uint(en) != 32 { // ERROR_SHARING_VIOLATION
		t.Errorf("write probe error = %v, want ERROR_SHARING_VIOLATION (32)", err)
	}

	// Delete/rename-over is denied too (no FILE_SHARE_DELETE).
	if err := os.Remove(path); err == nil {
		t.Fatal("cache artifact deleted while the verified handle is retained")
	}

	// The library stays fully usable with the handle in place.
	if echo, _, err := lib.RoundTrip([]byte("protected")); err != nil || string(echo) != "protected" {
		t.Errorf("round trip under handle protection: echo=%q err=%v", echo, err)
	}

	// Close releases the handle exactly once (shutdown-time release); the OS
	// module itself stays resident per the no-unload policy.
	if err := lib.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := lib.RoundTrip([]byte("closed")); !errors.Is(err, ErrClosed) {
		t.Errorf("RoundTrip after Close: got %v, want ErrClosed", err)
	}
}

// openForWriteProbe tries to open path for GENERIC_WRITE (no sharing
// offered). It returns nil when the open succeeded (handle closed again), or
// the raw syscall error otherwise.
func openForWriteProbe(path string) error {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	h, _, callErr := syscall.SyscallN(
		procCreateFileW.Addr(),
		uintptr(unsafe.Pointer(p)),
		uintptr(genericWrite),
		0, // no sharing offered
		0,
		uintptr(openExisting),
		uintptr(fileAttributeNormal),
		0,
	)
	if h == invalidHandleValue || h == 0 {
		return callErr
	}
	syscall.CloseHandle(syscall.Handle(h))
	return nil
}

// Layout mirrors: the Go structs must match the sizes the artifact reports.
func TestGoMirrorsMatchArtifactRecordSizes(t *testing.T) {
	lib := mustLoad(t, Options{CacheRoot: testDir(t)})
	defer lib.Close()
	if got := unsafe.Sizeof(abiTable{}); got != 152 {
		t.Errorf("sizeof(abiTable) = %d, want 152", got)
	}
	if got := unsafe.Sizeof(bufferRequest{}); got != 24 {
		t.Errorf("sizeof(bufferRequest) = %d, want 24", got)
	}
	if got := unsafe.Sizeof(bufferResponse{}); got != 24 {
		t.Errorf("sizeof(bufferResponse) = %d, want 24", got)
	}
	if got := unsafe.Offsetof(abiTable{}.capabilities); got != 128 {
		t.Errorf("offsetof(capabilities) = %d, want 128", got)
	}
	if got := unsafe.Offsetof(abiTable{}.bufferRoundTrip); got != 0 {
		t.Errorf("offsetof(bufferRoundTrip) = %d, want 0", got)
	}
}
