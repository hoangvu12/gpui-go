package consumerspec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// Import verification constants.
const (
	// importDescriptorSize is sizeof(IMAGE_IMPORT_DESCRIPTOR): 20 bytes,
	// terminated by an all-zero descriptor.
	importDescriptorSize = 20
	// delayDescriptorSize is sizeof(IMAGE_DELAYLOAD_DESCRIPTOR): 32 bytes
	// with RVA fields (dlattrRva), terminated by an all-zero descriptor.
	delayDescriptorSize = 32
	// dlattrRva marks delay descriptors whose fields are RVAs (the modern
	// MSVC form; 0 means virtual addresses needing the image base).
	dlattrRva = 1

	// ordinalFlag64/ordinalFlag32 are the IMAGE_ORDINAL_FLAG thunks.
	ordinalFlag64 = uint64(1) << 63
	ordinalFlag32 = uint64(1) << 31

	maxImportDescriptors = 4096
	maxThunkEntries      = 65536
)

// loadLibraryAPIs are the dynamic-loading entry points the Windows loader
// family exposes; an artifact importing them resolves modules at runtime,
// which static import tables cannot enumerate (the "dynamic" import class
// of the conformance contract).
var loadLibraryAPIs = map[string]bool{
	"LoadLibraryA":             true,
	"LoadLibraryW":             true,
	"LoadLibraryExA":           true,
	"LoadLibraryExW":           true,
	"LoadPackagedLibrary":      true,
	"GetProcAddress":           true,
	"LoadModule":               true,
	"FreeLibrary":              true,
	"FreeLibraryAndExitThread": true,
}

// crtRedistributables are the DLL base names (lowercase, no path) that a
// separately installed VC++ redistributable or UCRT copy provides. A
// static-CRT artifact imports NONE of them through any import class: the
// conclusion must come from the actual import tables, never from the build
// flags (conformance contract: "do not infer absence of a redistributable
// requirement from a build flag alone").
var crtRedistributables = map[string]bool{
	"vcruntime140.dll":           true,
	"vcruntime140d.dll":          true,
	"vcruntime140_1.dll":         true,
	"ucrtbase.dll":               true,
	"msvcp140.dll":               true,
	"msvcp140d.dll":              true,
	"msvcp140_1.dll":             true,
	"msvcp140_2.dll":             true,
	"msvcp140_atomic_wait.dll":   true,
	"msvcp140_1_codecvt_ids.dll": true,
}

// isCRTDLLName reports whether a DLL name is a CRT redistributable
// (vcruntime*/msvcp*/ucrtbase/api-ms-win-crt-*/msvcr<version>.dll).
func isCRTDLLName(name string) bool {
	base := strings.ToLower(strings.TrimSuffix(name, "\x00"))
	base = base[strings.LastIndexByte(base, '\\')+1:]
	base = base[strings.LastIndexByte(base, '/')+1:]
	if crtRedistributables[base] {
		return true
	}
	if strings.HasPrefix(base, "api-ms-win-crt-") {
		return true
	}
	// Old-style versioned CRTs (msvcr90.dll, msvcr120.dll, ...). msvcrt.dll
	// itself is an OS component in System32 and stays allowed.
	if len(base) >= len("msvcr00.dll") && strings.HasPrefix(base, "msvcr") {
		tail := strings.TrimSuffix(base[len("msvcr"):], ".dll")
		if tail != "" && strings.Trim(tail, "0123456789") == "" {
			return true
		}
	}
	return false
}

// isCRTString reports whether a CRT redistributable DLL name appears in s
// (case-insensitive), for the dynamic-load string scan.
func isCRTString(s string) (string, bool) {
	lower := strings.ToLower(s)
	for name := range crtRedistributables {
		if strings.Contains(lower, name) {
			return name, true
		}
	}
	for prefix := range map[string]bool{"api-ms-win-crt-": true, "vcruntime": true, "msvcp140": true} {
		if strings.Contains(lower, prefix) {
			return prefix, true
		}
	}
	return "", false
}

// ImportDLL is one imported DLL of an image.
type ImportDLL struct {
	// Name is the DLL name as recorded in the descriptor.
	Name string
	// Functions are the by-name imports.
	Functions []string
	// Ordinals are the by-ordinal imports.
	Ordinals []uint32
	// Delay marks a delay-load import (resolved on first use, not at load).
	Delay bool
}

// ImportReport is the full import verification of one PE image.
type ImportReport struct {
	// Machine is the parsed COFF machine name.
	Machine string
	// NormalDLLs are the static import-table entries.
	NormalDLLs []ImportDLL
	// DelayDLLs are the delay-load import entries.
	DelayDLLs []ImportDLL
	// LoadLibraryAPIs lists imported dynamic-loading entry points
	// (LoadLibrary*/GetProcAddress family) — the indicators of runtime
	// resolution that static tables cannot enumerate.
	LoadLibraryAPIs []string
	// CRTDLLs lists any CRT redistributable found in the normal or delay
	// import tables (empty for a static-CRT artifact).
	CRTDLLs []string
	// DynamicCRTStrings lists CRT DLL names found as plain strings in the
	// image (a heuristic probe for dynamically LoadLibrary-ed CRTs; false
	// positives are possible, misses are not excluded by construction).
	DynamicCRTStrings []string
	// ImageBase, recorded for VA-mode delay descriptors.
	ImageBase uint64
}

// StaticCRT reports the ACTUAL static-CRT outcome: no CRT redistributable
// appears in any import class and no CRT DLL name appears as a dynamic-load
// string. Derived exclusively from the parsed image bytes.
func (r *ImportReport) StaticCRT() bool {
	return len(r.CRTDLLs) == 0 && len(r.DynamicCRTStrings) == 0
}

// ParseImports parses the normal and delay import tables of a PE image with
// pure stdlib (no debug/pe: the delay-load table is beyond it) and derives
// the static-CRT conclusion. This is the "clean-machine normal/delay/dynamic
// imports and actual static-CRT outcome" check of the delivery ticket.
func ParseImports(image []byte) (*ImportReport, error) {
	f, err := ParsePE(image)
	if err != nil {
		return nil, err
	}
	r := &ImportReport{Machine: fmt.Sprintf("%#x", f.Machine), ImageBase: f.ImageBase}

	// Normal imports.
	if rva, size := f.dataDir(dirImport); rva != 0 {
		if size > 0 {
			dlls, err := parseImportDescriptors(f, rva, false)
			if err != nil {
				return nil, fmt.Errorf("normal import table: %w", err)
			}
			r.NormalDLLs = dlls
		}
	}

	// Delay imports.
	if rva, size := f.dataDir(dirDelayLoad); rva != 0 {
		if size > 0 {
			dlls, err := parseDelayImports(f, rva)
			if err != nil {
				return nil, fmt.Errorf("delay import table: %w", err)
			}
			r.DelayDLLs = dlls
		}
	}

	// Dynamic-loading entry points: any import of the LoadLibrary family.
	for _, dll := range append(append([]ImportDLL(nil), r.NormalDLLs...), r.DelayDLLs...) {
		for _, fn := range dll.Functions {
			if loadLibraryAPIs[fn] {
				r.LoadLibraryAPIs = append(r.LoadLibraryAPIs, fn)
			}
		}
	}

	// CRT conclusion from every import class.
	for _, dll := range append(append([]ImportDLL(nil), r.NormalDLLs...), r.DelayDLLs...) {
		if isCRTDLLName(dll.Name) {
			r.CRTDLLs = append(r.CRTDLLs, dll.Name)
		}
	}
	// Dynamic-load string scan: the whole image for CRT DLL names.
	if hit, ok := isCRTString(string(image)); ok {
		r.DynamicCRTStrings = append(r.DynamicCRTStrings, hit)
	}

	return r, nil
}

// parseImportDescriptors walks the import descriptor array at rva.
func parseImportDescriptors(f *PEFile, rva uint32, delay bool) ([]ImportDLL, error) {
	base, ok := f.rvaToOffset(rva)
	if !ok {
		return nil, fmt.Errorf("import directory RVA %#x is not mapped", rva)
	}
	var out []ImportDLL
	for n := 0; ; n++ {
		if n >= maxImportDescriptors {
			return nil, errors.New("import descriptor table does not terminate")
		}
		d := base + n*importDescriptorSize
		if d+importDescriptorSize > len(f.Data) {
			return nil, errors.New("import descriptor out of bounds")
		}
		oft := binary.LittleEndian.Uint32(f.Data[d:])
		nameRVA := binary.LittleEndian.Uint32(f.Data[d+12:])
		firstThunk := binary.LittleEndian.Uint32(f.Data[d+16:])
		if oft == 0 && nameRVA == 0 && firstThunk == 0 {
			break
		}
		name, err := f.readCStringAtRVA(nameRVA)
		if err != nil {
			return nil, err
		}
		thunkRVA := oft
		if thunkRVA == 0 {
			thunkRVA = firstThunk
		}
		if thunkRVA == 0 {
			out = append(out, ImportDLL{Name: name, Delay: delay})
			continue
		}
		functions, ordinals, err := f.parseThunkTable(thunkRVA)
		if err != nil {
			return nil, fmt.Errorf("thunks of %s: %w", name, err)
		}
		out = append(out, ImportDLL{Name: name, Functions: functions, Ordinals: ordinals, Delay: delay})
	}
	return out, nil
}

// parseDelayImports walks the delay-load descriptor array at rva.
func parseDelayImports(f *PEFile, rva uint32) ([]ImportDLL, error) {
	base, ok := f.rvaToOffset(rva)
	if !ok {
		return nil, fmt.Errorf("delay import directory RVA %#x is not mapped", rva)
	}
	var out []ImportDLL
	for n := 0; ; n++ {
		if n >= maxImportDescriptors {
			return nil, errors.New("delay descriptor table does not terminate")
		}
		d := base + n*delayDescriptorSize
		if d+delayDescriptorSize > len(f.Data) {
			return nil, errors.New("delay descriptor out of bounds")
		}
		attrs := binary.LittleEndian.Uint32(f.Data[d:])
		nameField := uint64(binary.LittleEndian.Uint32(f.Data[d+4:]))
		intField := uint64(binary.LittleEndian.Uint32(f.Data[d+16:]))
		iatField := uint64(binary.LittleEndian.Uint32(f.Data[d+12:]))
		if attrs == 0 && nameField == 0 && intField == 0 && iatField == 0 {
			break
		}
		// dlattrRva: fields are RVAs; otherwise virtual addresses.
		nameRVA := uint32(nameField)
		intRVA := uint32(intField)
		if attrs&dlattrRva == 0 {
			if nameField < f.ImageBase || intField < f.ImageBase {
				return nil, fmt.Errorf("delay descriptor %d carries virtual addresses below the image base", n)
			}
			nameRVA = uint32(nameField - f.ImageBase)
			intRVA = uint32(intField - f.ImageBase)
		}
		name, err := f.readCStringAtRVA(nameRVA)
		if err != nil {
			return nil, err
		}
		thunkRVA := intRVA
		if thunkRVA == 0 {
			thunkRVA = uint32(iatField) // INT absent: the IAT still names the imports
			if attrs&dlattrRva == 0 {
				if iatField < f.ImageBase {
					return nil, fmt.Errorf("delay descriptor %d IAT below the image base", n)
				}
				thunkRVA = uint32(iatField - f.ImageBase)
			}
		}
		var functions []string
		var ordinals []uint32
		if thunkRVA != 0 {
			functions, ordinals, err = f.parseThunkTable(thunkRVA)
			if err != nil {
				return nil, fmt.Errorf("delay thunks of %s: %w", name, err)
			}
		}
		out = append(out, ImportDLL{Name: name, Functions: functions, Ordinals: ordinals, Delay: true})
	}
	return out, nil
}

// readCStringAtRVA reads a NUL-terminated string at an RVA.
func (f *PEFile) readCStringAtRVA(rva uint32) (string, error) {
	off, ok := f.rvaToOffset(rva)
	if !ok {
		return "", fmt.Errorf("string RVA %#x is not mapped", rva)
	}
	end := bytes.IndexByte(f.Data[off:], 0)
	if end < 0 {
		return "", fmt.Errorf("string at RVA %#x is not NUL-terminated", rva)
	}
	if end > 4096 {
		return "", fmt.Errorf("string at RVA %#x implausibly long", rva)
	}
	return string(f.Data[off : off+end]), nil
}

// parseThunkTable walks one thunk array (original-first-thunk style: name
// RVAs or ordinals). PE32+ thunks are 8 bytes, PE32 thunks 4 bytes.
func (f *PEFile) parseThunkTable(rva uint32) ([]string, []uint32, error) {
	base, ok := f.rvaToOffset(rva)
	if !ok {
		return nil, nil, fmt.Errorf("thunk table RVA %#x is not mapped", rva)
	}
	var functions []string
	var ordinals []uint32
	width := 8
	flag := ordinalFlag64
	if f.OptMagic == optMagicPE32 {
		width = 4
		flag = ordinalFlag32
	}
	for n := 0; ; n++ {
		if n >= maxThunkEntries {
			return nil, nil, errors.New("thunk table does not terminate")
		}
		off := base + n*width
		if off+width > len(f.Data) {
			return nil, nil, errors.New("thunk entry out of bounds")
		}
		var v uint64
		switch width {
		case 8:
			v = binary.LittleEndian.Uint64(f.Data[off:])
		case 4:
			v = uint64(binary.LittleEndian.Uint32(f.Data[off:]))
		}
		if v == 0 {
			break
		}
		if v&flag != 0 {
			ordinals = append(ordinals, uint32(v&0xffff))
			continue
		}
		name, err := f.readCStringAtRVA(uint32(v) + 2) // IMAGE_IMPORT_BY_NAME: Hint(2) + Name
		if err != nil {
			return nil, nil, err
		}
		functions = append(functions, name)
	}
	return functions, ordinals, nil
}
