//go:build windows

package gpui

// The Windows spell checker (browser-core ticket 30): the
// windowsSpellChecker seam behind SpellcheckService, ported from
// Chromium's windows_spell_checker::BackgroundHelper
// (components/spellcheck/browser/windows_spell_checker.cc):
//
//   - the thread's apartment: CoInitializeEx(NULL,
//     COINIT_APARTMENTTHREADED) — the dedicated COM STA thread
//     Chromium's CreateCOMSTATaskRunner provides
//     (windows_spell_checker_unittest.cc:51-52) and
//     CreateSpellCheckerFactory asserts
//     (base::win::AssertComApartmentType(ComApartmentType::STA),
//     windows_spell_checker.cc:160);
//   - the factory: CoCreateInstance(SpellCheckerFactory,
//     CLSCTX_INPROC_SERVER | CLSCTX_LOCAL_SERVER,
//     windows_spell_checker.cc:165-166);
//   - the per-language checkers: ISpellCheckerFactory::IsSupported then
//     CreateSpellChecker (windows_spell_checker.cc:345-360, 180-200),
//     cached per language like Chromium's spell_checker_map_;
//   - checks: ISpellChecker::ComprehensiveCheck plus the
//     IEnumSpellingError walk reading get_StartIndex / get_Length /
//     get_CorrectiveAction, keeping only the GET_SUGGESTIONS and
//     REPLACE actions (windows_spell_checker.cc:228-250);
//   - suggestions: ISpellChecker::Suggest and the IEnumString walk
//     (windows_spell_checker.cc:296-315), freeing the CoTaskMem strings;
//   - dictionary changes: ISpellChecker::Add / Ignore
//     (windows_spell_checker.cc:317-338).
//
// Every method runs on the service's worker thread only (the
// SpellChecker contract); the COM interface pointers never leave it.
//
// The vtable slots, IIDs and CLSID come from the installed Windows SDK
// (10.0.26100.0) headers, verified like the dialog bindings:
// um/spellcheck.h (ISpellChecker, IEnumSpellingError, ISpellingError,
// ISpellCheckerFactory) and um/spellcheck_i.c / um/objidl.h
// (IEnumString, 00000101-0000-0000-C000-000000000046).

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// CoTaskMemFree (dialogs_windows.go's procCoTaskMemFree) frees the
// suggestion strings the enumerator hands out.
var (
	procCoInitializeEx = modOle32.NewProc("CoInitializeEx")
	procCoUninitialize = modOle32.NewProc("CoUninitialize")
)

// coinitApartmentThreaded is COINIT_APARTMENTTHREADED (objbase.h).
const coinitApartmentThreaded = uintptr(0x2)

// clsctxInprocOrLocalServer is CLSCTX_INPROC_SERVER |
// CLSCTX_LOCAL_SERVER (wtypes.h) — Chromium's spell-check factory
// context (windows_spell_checker.cc:166), deliberately not the
// dialogs' CLSCTX_ALL.
const clsctxInprocOrLocalServer = uintptr(0x1 | 0x4)

// GUIDs from spellcheck.h / spellcheck_i.c of the installed SDK.
var (
	clsidSpellCheckerFactory = guid{0x7AB36653, 0x1796, 0x484B, [8]byte{0xBD, 0xFA, 0xE7, 0x4F, 0x1D, 0xB7, 0xC1, 0xDC}}
	iidISpellCheckerFactory  = guid{0x8E018A9D, 0x2415, 0x4677, [8]byte{0xBF, 0x08, 0x79, 0x4E, 0xA6, 0x1F, 0x94, 0xBB}}
	iidISpellChecker         = guid{0xB6FD0B71, 0xE2BC, 0x4653, [8]byte{0x8D, 0x05, 0xF1, 0x97, 0xE4, 0x12, 0x77, 0x0B}}
)

// Vtable slots (after IUnknown's QueryInterface/AddRef/Release at 0-2):
// ISpellCheckerFactory: get_SupportedLanguages 3, IsSupported 4,
// CreateSpellChecker 5. ISpellChecker: Check 4, Suggest 5, Add 6,
// Ignore 7, ComprehensiveCheck 16. IEnumSpellingError: Next 3.
// ISpellingError: get_StartIndex 3, get_Length 4, get_CorrectiveAction
// 5. IEnumString: Next 3.
const (
	vtblFactoryIsSupported        = 4
	vtblFactoryCreateSpellChecker = 5
	vtblCheckerCheck              = 4
	vtblCheckerSuggest            = 5
	vtblCheckerAdd                = 6
	vtblCheckerIgnore             = 7
	vtblCheckerComprehensiveCheck = 16
	vtblEnumSpellingErrorNext     = 3
	vtblSpellingErrorStartIndex   = 3
	vtblSpellingErrorLength       = 4
	vtblSpellingErrorCorrective   = 5
	vtblEnumStringNext            = 3
)

// CORRECTIVE_ACTION values that report a misspelling
// (spellcheck.h CORRECTIVE_ACTION; the filter is Chromium's,
// windows_spell_checker.cc:241-243).
const (
	correctiveActionGetSuggestions = int32(1)
	correctiveActionReplace        = int32(2)
)

// windowsSpellChecker is the SpellChecker over the Windows spell
// checker COM API. Its zero value is usable; Prepare initializes the
// apartment and the factory on the calling (service worker) thread.
type windowsSpellChecker struct {
	coInit  bool
	factory *comIFace
	// checkers caches one ISpellChecker per BCP-47 language tag
	// (Chromium's spell_checker_map_, windows_spell_checker.cc:154-157).
	checkers map[string]*comIFace
}

// newPlatformSpellChecker returns the platform-default checking seam
// (App.Spellcheck's lazy construction).
func newPlatformSpellChecker() SpellChecker {
	return &windowsSpellChecker{}
}

// Prepare implements SpellChecker: CoInitializeEx the thread as a COM
// STA and CoCreate the factory.
func (c *windowsSpellChecker) Prepare() error {
	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	switch uint32(hr) {
	case uint32(sOK), uint32(sFalse):
		c.coInit = true
	default:
		return fmt.Errorf("gpui: CoInitializeEx(Spellcheck STA) failed with HRESULT %s (an existing incompatible COM apartment is an error)", hresultString(hr))
	}
	var factory *comIFace
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidSpellCheckerFactory)),
		0,
		clsctxInprocOrLocalServer,
		uintptr(unsafe.Pointer(&iidISpellCheckerFactory)),
		uintptr(unsafe.Pointer(&factory)),
	)
	if int32(hr) < 0 {
		return fmt.Errorf("gpui: CoCreateInstance(SpellCheckerFactory) failed with HRESULT %s", hresultString(hr))
	}
	if factory == nil {
		return errors.New("gpui: CoCreateInstance(SpellCheckerFactory) returned a null interface")
	}
	c.factory = factory
	c.checkers = make(map[string]*comIFace)
	return nil
}

// Release implements SpellChecker: release the cached checkers and the
// factory, then balance CoInitializeEx.
func (c *windowsSpellChecker) Release() {
	for lang, checker := range c.checkers {
		comRelease(checker)
		delete(c.checkers, lang)
	}
	if c.factory != nil {
		comRelease(c.factory)
		c.factory = nil
	}
	if c.coInit {
		c.coInit = false
		_, _, _ = procCoUninitialize.Call()
	}
}

// checkerFor returns the cached ISpellChecker for lang, creating it
// through the factory when the language is supported
// (CreateSpellChecker + IsSupported,
// windows_spell_checker.cc:180-200, 345-360).
func (c *windowsSpellChecker) checkerFor(lang string) (*comIFace, error) {
	if checker, ok := c.checkers[lang]; ok {
		return checker, nil
	}
	if c.factory == nil {
		return nil, errors.New("gpui: the spell checker factory was not initialized")
	}
	langPtr, err := syscall.UTF16PtrFromString(lang)
	if err != nil {
		return nil, fmt.Errorf("gpui: invalid language tag %q: %w", lang, err)
	}
	var supported int32
	if _, err := comCall(c.factory, vtblFactoryIsSupported,
		uintptr(unsafe.Pointer(langPtr)), uintptr(unsafe.Pointer(&supported))); err != nil {
		return nil, err
	}
	if supported == 0 {
		return nil, fmt.Errorf("%w: %q", ErrSpellcheckLanguageUnsupported, lang)
	}
	var checker *comIFace
	if _, err := comCall(c.factory, vtblFactoryCreateSpellChecker,
		uintptr(unsafe.Pointer(langPtr)), uintptr(unsafe.Pointer(&checker))); err != nil {
		return nil, err
	}
	if checker == nil {
		return nil, fmt.Errorf("gpui: CreateSpellChecker(%q) returned a null interface", lang)
	}
	c.checkers[lang] = checker
	return checker, nil
}

// Check implements SpellChecker: one ComprehensiveCheck pass
// (windows_spell_checker.cc:213-281 reduced to the single-language
// case) reporting the GET_SUGGESTIONS and REPLACE ranges.
func (c *windowsSpellChecker) Check(text, lang string) ([]Misspelling, error) {
	checker, err := c.checkerFor(lang)
	if err != nil {
		return nil, err
	}
	units, err := syscall.UTF16FromString(text)
	if err != nil {
		return nil, fmt.Errorf("gpui: the checked text is not encodable as UTF-16: %w", err)
	}
	var enum *comIFace
	if _, err := comCall(checker, vtblCheckerComprehensiveCheck,
		uintptr(unsafe.Pointer(&units[0])), uintptr(unsafe.Pointer(&enum))); err != nil {
		return nil, err
	}
	if enum == nil {
		return nil, nil
	}
	defer comRelease(enum)
	var misspellings []Misspelling
	for {
		var spellingError *comIFace
		hr, _, _ := syscall.SyscallN(enum.vtbl[vtblEnumSpellingErrorNext],
			uintptr(unsafe.Pointer(enum)), uintptr(unsafe.Pointer(&spellingError)))
		if int32(hr) < 0 {
			return nil, fmt.Errorf("gpui: IEnumSpellingError::Next failed with HRESULT %s", hresultString(hr))
		}
		// S_FALSE (no more errors) or a null error ends the walk.
		if hr != uintptr(sOK) || spellingError == nil {
			break
		}
		var startIndex, length uint32
		var action int32
		_, startErr := comCall(spellingError, vtblSpellingErrorStartIndex, uintptr(unsafe.Pointer(&startIndex)))
		_, lengthErr := comCall(spellingError, vtblSpellingErrorLength, uintptr(unsafe.Pointer(&length)))
		_, actionErr := comCall(spellingError, vtblSpellingErrorCorrective, uintptr(unsafe.Pointer(&action)))
		comRelease(spellingError)
		if err := errors.Join(startErr, lengthErr, actionErr); err != nil {
			return nil, err
		}
		if action != correctiveActionGetSuggestions && action != correctiveActionReplace {
			continue
		}
		misspellings = append(misspellings, Misspelling{
			Start: int(startIndex),
			End:   int(startIndex + length),
		})
	}
	return misspellings, nil
}

// Suggest implements SpellChecker: ISpellChecker::Suggest and the
// IEnumString walk (windows_spell_checker.cc:296-315), freeing every
// CoTaskMem string the enumerator hands out.
func (c *windowsSpellChecker) Suggest(word, lang string) ([]string, error) {
	checker, err := c.checkerFor(lang)
	if err != nil {
		return nil, err
	}
	units, err := syscall.UTF16FromString(word)
	if err != nil {
		return nil, fmt.Errorf("gpui: the suggested word is not encodable as UTF-16: %w", err)
	}
	var enum *comIFace
	if _, err := comCall(checker, vtblCheckerSuggest,
		uintptr(unsafe.Pointer(&units[0])), uintptr(unsafe.Pointer(&enum))); err != nil {
		return nil, err
	}
	if enum == nil {
		return nil, nil
	}
	defer comRelease(enum)
	var suggestions []string
	for {
		var strPtr *uint16
		var fetched uint32
		hr, _, _ := syscall.SyscallN(enum.vtbl[vtblEnumStringNext],
			uintptr(unsafe.Pointer(enum)), 1,
			uintptr(unsafe.Pointer(&strPtr)), uintptr(unsafe.Pointer(&fetched)))
		if int32(hr) < 0 {
			return nil, fmt.Errorf("gpui: IEnumString::Next failed with HRESULT %s", hresultString(hr))
		}
		if hr != uintptr(sOK) || strPtr == nil {
			break
		}
		suggestions = append(suggestions, syscall.UTF16ToString(unsafe.Slice(strPtr, utf16Len(strPtr))))
		_, _, _ = procCoTaskMemFree.Call(uintptr(unsafe.Pointer(strPtr)))
	}
	return suggestions, nil
}

// Add implements SpellChecker: ISpellChecker::Add
// (windows_spell_checker.cc:317-321).
func (c *windowsSpellChecker) Add(word, lang string) error {
	return c.wordOp(word, lang, vtblCheckerAdd)
}

// Ignore implements SpellChecker: ISpellChecker::Ignore
// (windows_spell_checker.cc:331-338).
func (c *windowsSpellChecker) Ignore(word, lang string) error {
	return c.wordOp(word, lang, vtblCheckerIgnore)
}

// wordOp runs one single-word dictionary operation on the language's
// checker.
func (c *windowsSpellChecker) wordOp(word, lang string, slot int) error {
	checker, err := c.checkerFor(lang)
	if err != nil {
		return err
	}
	wordPtr, err := syscall.UTF16PtrFromString(word)
	if err != nil {
		return fmt.Errorf("gpui: the word is not encodable as UTF-16: %w", err)
	}
	_, err = comCall(checker, slot, uintptr(unsafe.Pointer(wordPtr)))
	return err
}

// spellcheckPostToHost posts one completion to a live host's message
// loop (real applications). It reports whether the closure was
// accepted (false: no host, the loop exited, or a non-Windows build —
// the caller queues the completion instead).
func spellcheckPostToHost(h *Host, f func()) bool {
	if h == nil || !h.hostStarted() || hostLoopExited(h) {
		return false
	}
	return h.Post(f)
}
