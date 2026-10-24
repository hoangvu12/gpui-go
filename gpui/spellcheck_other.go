//go:build !windows

package gpui

import "errors"

// The non-Windows spellcheck stubs (browser-core ticket 30): the
// service, its types and the App surface are platform neutral
// (spellcheck.go); only the platform checker is Windows. The target is
// windows/amd64 (docs/windows-platform-contract.md), so other builds
// get a checker that fails every operation instead of a COM apartment.

// ErrSpellcheckUnsupportedPlatform is the operation error on builds
// without the Windows spell checker.
var ErrSpellcheckUnsupportedPlatform = errors.New("gpui: the platform spell checker exists only on Windows")

// unsupportedSpellChecker fails every operation.
type unsupportedSpellChecker struct{}

// newPlatformSpellChecker returns the platform-default checking seam
// (App.Spellcheck's lazy construction).
func newPlatformSpellChecker() SpellChecker { return unsupportedSpellChecker{} }

// Prepare implements SpellChecker.
func (unsupportedSpellChecker) Prepare() error { return ErrSpellcheckUnsupportedPlatform }

// Check implements SpellChecker.
func (unsupportedSpellChecker) Check(text, lang string) ([]Misspelling, error) {
	return nil, ErrSpellcheckUnsupportedPlatform
}

// Suggest implements SpellChecker.
func (unsupportedSpellChecker) Suggest(word, lang string) ([]string, error) {
	return nil, ErrSpellcheckUnsupportedPlatform
}

// Add implements SpellChecker.
func (unsupportedSpellChecker) Add(word, lang string) error { return ErrSpellcheckUnsupportedPlatform }

// Ignore implements SpellChecker.
func (unsupportedSpellChecker) Ignore(word, lang string) error {
	return ErrSpellcheckUnsupportedPlatform
}

// Release implements SpellChecker.
func (unsupportedSpellChecker) Release() {}

// spellcheckPostToHost has no live host loop to post to on this
// platform (the Win32 host requires a Windows build).
func spellcheckPostToHost(h *Host, f func()) bool { return false }
