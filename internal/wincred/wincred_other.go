//go:build !windows

package wincred

import "errors"

// ErrUnsupportedPlatform reports the Windows Credential Manager calls on
// a non-windows build (the selected platform contract targets windows).
var ErrUnsupportedPlatform = errors.New("wincred: the Windows Credential Manager requires a windows build")

type windowsCredentialStore struct{}

// Store reports the unsupported platform.
func Store() *windowsCredentialStore { return &windowsCredentialStore{} }

// Write reports the unsupported platform.
func (windowsCredentialStore) Write(url, username string, password []byte) error {
	if len(password) > MaxCredentialBlobSize {
		return &BlobSizeError{URL: url, Size: len(password)}
	}
	return ErrUnsupportedPlatform
}

// Read reports the unsupported platform.
func (windowsCredentialStore) Read(url string) (entry Entry, found bool, err error) {
	return Entry{}, false, ErrUnsupportedPlatform
}

// Delete reports the unsupported platform.
func (windowsCredentialStore) Delete(url string) error { return ErrUnsupportedPlatform }
