// Package wincred is ticket25's pure-Go Windows Credential Manager
// adapter (advapi32 CredWriteW/CredReadW/CredDeleteW/CredFree), ported
// from the pinned CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - reference/ce-source/crates/gpui_windows/src/platform.rs
//     write_credentials/read_credentials/delete_credentials
//     (platform.rs:829-926): the blob-size precheck, the CREDENTIALW
//     field values (CRED_TYPE_GENERIC, CRED_PERSIST_LOCAL_MACHINE,
//     LastWritten from GetSystemTimeAsFileTime), ERROR_NOT_FOUND mapped
//     to an absence result, CredFree after CredReadW, and propagated
//     CredDeleteW failures.
//   - reference/ce-source/crates/gpui_windows/src/util.rs
//     windows_credentials_target_name (util.rs:85): the "zed:url={}"
//     target mapping.
//
// Security policy (ticket25): this package only touches entries named by
// the caller. It never enumerates credentials (no CredEnumerateW
// anywhere) and callers of its tests must only round-trip
// caller-supplied temporary entries.
package wincred

import "fmt"

// MaxCredentialBlobSize is CRED_MAX_CREDENTIAL_BLOB_SIZE (5*512, wincred.h):
// the Windows Credential Manager limit the pinned reference enforces before
// calling CredWriteW.
const MaxCredentialBlobSize = 2560

// Entry is one read credential: the username string and the raw blob bytes.
type Entry struct {
	// Username is the UserName field of the CREDENTIALW record.
	Username string
	// Password is the CredentialBlob bytes, exactly as stored.
	Password []byte
}

// BlobSizeError reports a password blob larger than the Credential
// Manager limit. The pinned reference rejects it before CredWriteW with
// this message because CredWriteW itself answers with the opaque RPC
// error 0x800706F7 "The stub received bad data"
// (crates/gpui_windows/src/platform.rs:830-837).
type BlobSizeError struct {
	// URL is the credential key the caller attempted to write.
	URL string
	// Size is the rejected blob length in bytes.
	Size int
}

// Error renders the reference's message.
func (e *BlobSizeError) Error() string {
	return fmt.Sprintf("credential for %s is %d bytes, which exceeds the Windows Credential Manager limit of %d bytes",
		e.URL, e.Size, MaxCredentialBlobSize)
}

// TargetName maps a credential key to the Windows Credential Manager
// target name, mirroring windows_credentials_target_name
// (crates/gpui_windows/src/util.rs:85: format!("zed:url={}", url)).
func TargetName(url string) string {
	return "zed:url=" + url
}
