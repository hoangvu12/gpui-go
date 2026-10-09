//go:build windows

// Ticket25's real Windows Credential Manager corpus. Environment
// expectation: this machine's interactive session with a writable user
// credential store. Every test uses ONLY caller-supplied temporary
// entries under a unique per-run target name (never enumerating or
// printing unrelated credentials), cleans up exactly the entries it
// created, and records absence/limit/error results explicitly.
package wincred

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"syscall"
	"testing"
)

// newTestURL mints a unique credential key for one test run.
func newTestURL(t *testing.T) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("crypto/rand: %v", err)
	}
	return "https://gpui-go.wincred.test/" + hex.EncodeToString(b[:])
}

// cleanupEntry deletes exactly this test's entry, ignoring the
// absent-entry error so cleanup never fails the test for a missing write.
func cleanupEntry(t *testing.T, url string) {
	t.Helper()
	t.Cleanup(func() {
		if err := Store().Delete(url); err != nil && !errors.Is(err, syscall.Errno(1168)) {
			t.Errorf("cleanup delete for the test entry %s failed: %v", url, err)
		}
	})
}

func TestTargetNameMapping(t *testing.T) {
	// crates/gpui_windows/src/util.rs:85 — format!("zed:url={}", url).
	if got := TargetName("https://example.com"); got != "zed:url=https://example.com" {
		t.Fatalf("TargetName = %q, want zed:url=https://example.com", got)
	}
}

func TestReadAbsentIsAnAbsenceResult(t *testing.T) {
	url := newTestURL(t)
	cleanupEntry(t, url)

	entry, found, err := Store().Read(url)
	if err != nil {
		t.Fatalf("read of a never-written credential returned an error: %v", err)
	}
	if found {
		t.Fatalf("read of a never-written credential reported found with entry %+v", entry)
	}
}

func TestRoundTripAndCleanup(t *testing.T) {
	url := newTestURL(t)
	cleanupEntry(t, url)

	// Deliberately non-ASCII username and binary blob (including a NUL
	// byte) to prove the UTF-16 and byte-copy paths. Values are only
	// compared, never printed.
	username := "gpui-go 测试 user ✓"
	password := []byte{0x00, 0x01, 0xff, 'p', 'a', 's', 's', 0x00, 0xfe, 0x10}

	if err := Store().Write(url, username, password); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	entry, found, err := Store().Read(url)
	if err != nil {
		t.Fatalf("read after write failed: %v", err)
	}
	if !found {
		t.Fatal("read after write reported absence")
	}
	if entry.Username != username {
		t.Fatalf("username round trip mismatch (lengths %d vs %d)", len(entry.Username), len(username))
	}
	if !bytes.Equal(entry.Password, password) {
		t.Fatalf("blob round trip mismatch (%d vs %d bytes)", len(entry.Password), len(password))
	}

	if err := Store().Delete(url); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	// After cleanup the entry is absent again — the absence result.
	if _, found, err := Store().Read(url); err != nil || found {
		t.Fatalf("read after delete: found=%v err=%v (want absence with nil error)", found, err)
	}
}

func TestMaxBlobSizeBoundary(t *testing.T) {
	// The reference rejects len > CRED_MAX_CREDENTIAL_BLOB_SIZE
	// (platform.rs:833); the boundary value itself is a valid write.
	url := newTestURL(t)
	cleanupEntry(t, url)

	// 2560 bytes: allowed.
	password := bytes.Repeat([]byte{0x5a}, MaxCredentialBlobSize)
	if err := Store().Write(url, "boundary", password); err != nil {
		t.Fatalf("write of exactly %d bytes failed: %v", MaxCredentialBlobSize, err)
	}
	entry, found, err := Store().Read(url)
	if err != nil || !found {
		t.Fatalf("read of the %d-byte boundary credential: found=%v err=%v", MaxCredentialBlobSize, found, err)
	}
	if !bytes.Equal(entry.Password, password) {
		t.Fatalf("boundary blob round trip mismatch (%d bytes)", len(entry.Password))
	}
	if err := Store().Delete(url); err != nil {
		t.Fatalf("delete of the boundary credential failed: %v", err)
	}

	// 2561 bytes: rejected with the reference's clear message, and the
	// rejection happens before CredWriteW (the entry must stay absent).
	var blobErr *BlobSizeError
	err = Store().Write(url, "boundary", make([]byte, MaxCredentialBlobSize+1))
	if !errors.As(err, &blobErr) {
		t.Fatalf("write of %d bytes returned %v, want a *BlobSizeError", MaxCredentialBlobSize+1, err)
	}
	if want := "credential for " + url; !strings.HasPrefix(blobErr.Error(), want) {
		t.Fatalf("blob-size error message = %q, want prefix %q", blobErr.Error(), want)
	}
	if !strings.HasSuffix(blobErr.Error(), "bytes, which exceeds the Windows Credential Manager limit of 2560 bytes") {
		t.Fatalf("blob-size error message = %q, want the reference's limit wording", blobErr.Error())
	}
	if _, found, err := Store().Read(url); err != nil || found {
		t.Fatalf("rejected write still created an entry: found=%v err=%v", found, err)
	}
}

func TestDeleteAbsentPropagatesError(t *testing.T) {
	// The reference propagates CredDeleteW failures (platform.rs:916-924):
	// deleting a credential that does not exist is an explicit error, not
	// an absence result.
	url := newTestURL(t)
	cleanupEntry(t, url)

	if err := Store().Delete(url); err == nil {
		t.Fatal("delete of an absent credential returned nil, want the propagated CredDeleteW error")
	}
}

func TestEmptyBlobRoundTrip(t *testing.T) {
	url := newTestURL(t)
	cleanupEntry(t, url)

	if err := Store().Write(url, "empty-blob", nil); err != nil {
		t.Fatalf("write of an empty blob failed: %v", err)
	}
	entry, found, err := Store().Read(url)
	if err != nil || !found {
		t.Fatalf("read of the empty-blob credential: found=%v err=%v", found, err)
	}
	if len(entry.Password) != 0 {
		t.Fatalf("empty blob round trip returned %d bytes", len(entry.Password))
	}
}
