package consumerspec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// PE directory indices (winnt.h, the 16-entry array order: export 0,
// import 1, resource 2, exception 3, security 4, basereloc 5, debug 6,
// architecture 7, globalptr 8, tls 9, load-config 10, bound-import 11,
// iat 12, delay-import 13, clr 14, reserved 15). Only the entries this
// package reads are named; the editor writes index 2 (resource).
const (
	dirImport    = 1
	dirResource  = 2
	dirIAT       = 12
	dirDelayLoad = 13
)

// PE structure constants.
const (
	dosSig        = "MZ"
	peSig         = "PE\x00\x00"
	peHeaderSize  = 24 // PE signature + COFF header
	coffHdrSize   = 20
	sectHdrSize   = 40
	optMagicPE32  = 0x10b
	optMagicPE32P = 0x20b
)

// Errors of the pure-Go PE editor.
var (
	// ErrDuplicateManifest: the executable already carries an RT_MANIFEST
	// resource; the distribution contract requires reporting the conflict
	// instead of silently overwriting it.
	ErrDuplicateManifest = errors.New("consumerspec: executable already contains a manifest resource; report the conflict instead of overwriting (build with a .syso, or remove one of the manifests)")
	// ErrNoHeaderRoom: the section table cannot grow inside SizeOfHeaders.
	ErrNoHeaderRoom = errors.New("consumerspec: no room in the PE headers for another section entry")
	// ErrSectionTooSmall: an existing resource section cannot host the
	// rebuilt tree in place.
	ErrSectionTooSmall = errors.New("consumerspec: existing resource section too small for the merged resource tree; use the .syso build route instead")
)

// PESection is one parsed section header.
type PESection struct {
	Name             string
	VirtualSize      uint32
	VirtualAddress   uint32
	SizeOfRawData    uint32
	PointerToRawData uint32
	Characteristics  uint32
	HeaderOffset     int // file offset of the 40-byte header
	RelocCount       uint16
}

// PEFile is a parsed PE image (pure stdlib, no debug/pe dependency: the
// editor must read AND rewrite header fields in place, which debug/pe does
// not support). The parse model follows the ticket31 probe groundwork
// (.scratch/tmp31/probe.go) extended with the fields the editor and the
// import verifier need.
type PEFile struct {
	Data []byte // the full image bytes

	Machine          uint16
	NumberOfSection  uint16
	PEOffset         int // offset of the "PE\0\0" signature
	OptOffset        int // offset of the optional header
	OptMagic         uint16
	SectionAlignment uint32
	FileAlignment    uint32
	SizeOfImage      uint32
	SizeOfHeaders    uint32
	CheckSumOffset   int
	NumRvaAndSizes   uint32
	DataDirOffset    int // offset of the 16 data directory entries
	ImageBase        uint64
	Sections         []PESection
}

// ParsePE parses the headers of a PE image. Both PE32 and PE32+ optional
// headers are accepted; malformed input is a typed error.
func ParsePE(data []byte) (*PEFile, error) {
	if len(data) < 0x40 {
		return nil, errors.New("consumerspec: file too short for a DOS header")
	}
	if string(data[0:2]) != dosSig {
		return nil, errors.New("consumerspec: missing MZ signature")
	}
	peOff := int(binary.LittleEndian.Uint32(data[0x3c:]))
	if peOff <= 0 || peOff+peHeaderSize > len(data) {
		return nil, fmt.Errorf("consumerspec: e_lfanew %d out of bounds", peOff)
	}
	if !bytes.Equal(data[peOff:peOff+4], []byte(peSig)) {
		return nil, errors.New("consumerspec: missing PE signature")
	}
	coff := peOff + 4
	f := &PEFile{
		Data:            data,
		Machine:         binary.LittleEndian.Uint16(data[coff:]),
		NumberOfSection: binary.LittleEndian.Uint16(data[coff+2:]),
		PEOffset:        peOff,
	}
	optSize := int(binary.LittleEndian.Uint16(data[coff+16:]))
	f.OptOffset = peOff + peHeaderSize
	if optSize == 0 {
		return nil, errors.New("consumerspec: image has no optional header (not an executable)")
	}
	if f.OptOffset+optSize > len(data) {
		return nil, errors.New("consumerspec: optional header out of bounds")
	}
	opt := f.OptOffset
	f.OptMagic = binary.LittleEndian.Uint16(data[opt:])
	switch f.OptMagic {
	case optMagicPE32P:
		f.ImageBase = binary.LittleEndian.Uint64(data[opt+24:])
		f.SectionAlignment = binary.LittleEndian.Uint32(data[opt+32:])
		f.FileAlignment = binary.LittleEndian.Uint32(data[opt+36:])
		f.SizeOfImage = binary.LittleEndian.Uint32(data[opt+56:])
		f.SizeOfHeaders = binary.LittleEndian.Uint32(data[opt+60:])
		f.CheckSumOffset = opt + 64
		f.NumRvaAndSizes = binary.LittleEndian.Uint32(data[opt+108:])
		f.DataDirOffset = opt + 112
	case optMagicPE32:
		f.ImageBase = uint64(binary.LittleEndian.Uint32(data[opt+28:]))
		f.SectionAlignment = binary.LittleEndian.Uint32(data[opt+32:])
		f.FileAlignment = binary.LittleEndian.Uint32(data[opt+36:])
		f.SizeOfImage = binary.LittleEndian.Uint32(data[opt+56:])
		f.SizeOfHeaders = binary.LittleEndian.Uint32(data[opt+60:])
		f.CheckSumOffset = opt + 64
		f.NumRvaAndSizes = binary.LittleEndian.Uint32(data[opt+92:])
		f.DataDirOffset = opt + 96
	default:
		return nil, fmt.Errorf("consumerspec: unknown optional header magic %#x", f.OptMagic)
	}
	if f.SectionAlignment == 0 || f.FileAlignment == 0 {
		return nil, errors.New("consumerspec: zero section/file alignment")
	}
	if int(f.NumRvaAndSizes) > 16 {
		return nil, fmt.Errorf("consumerspec: NumberOfRvaAndSizes %d exceeds 16", f.NumRvaAndSizes)
	}

	sectTable := f.OptOffset + optSize
	if sectTable+sectHdrSize*int(f.NumberOfSection) > len(data) {
		return nil, errors.New("consumerspec: section table out of bounds")
	}
	for i := 0; i < int(f.NumberOfSection); i++ {
		h := sectTable + sectHdrSize*i
		s := PESection{
			Name:             string(bytes.TrimRight(data[h:h+8], "\x00")),
			VirtualSize:      binary.LittleEndian.Uint32(data[h+8:]),
			VirtualAddress:   binary.LittleEndian.Uint32(data[h+12:]),
			SizeOfRawData:    binary.LittleEndian.Uint32(data[h+16:]),
			PointerToRawData: binary.LittleEndian.Uint32(data[h+20:]),
			RelocCount:       binary.LittleEndian.Uint16(data[h+32:]),
			Characteristics:  binary.LittleEndian.Uint32(data[h+36:]),
			HeaderOffset:     h,
		}
		f.Sections = append(f.Sections, s)
	}
	return f, nil
}

// MachineName renders the COFF machine code.
func (f *PEFile) MachineName() string {
	switch f.Machine {
	case 0x8664:
		return "amd64 (0x8664)"
	case 0x014c:
		return "i386 (0x14c)"
	case 0xaa64:
		return "arm64 (0xaa64)"
	default:
		return fmt.Sprintf("machine %#x", f.Machine)
	}
}

// dataDir returns data directory i (RVA, size); (0,0) when absent.
func (f *PEFile) dataDir(i int) (uint32, uint32) {
	if i >= int(f.NumRvaAndSizes) {
		return 0, 0
	}
	o := f.DataDirOffset + 8*i
	rva := binary.LittleEndian.Uint32(f.Data[o:])
	size := binary.LittleEndian.Uint32(f.Data[o+4:])
	return rva, size
}

// setDataDir writes data directory i.
func (f *PEFile) setDataDir(i int, rva, size uint32) error {
	if i >= 16 {
		return fmt.Errorf("consumerspec: data directory index %d out of range", i)
	}
	if i >= int(f.NumRvaAndSizes) {
		// Grow NumberOfRvaAndSizes when the array already has room in the
		// optional header (16 entries is the maximum).
		if f.DataDirOffset+8*(i+1) > f.OptOffset+int(f.optionalHeaderSize()) {
			return fmt.Errorf("consumerspec: data directory %d has no room in the optional header", i)
		}
		f.NumRvaAndSizes = uint32(i + 1)
		f.writeNumRvaAndSizes()
	}
	o := f.DataDirOffset + 8*i
	binary.LittleEndian.PutUint32(f.Data[o:], rva)
	binary.LittleEndian.PutUint32(f.Data[o+4:], size)
	return nil
}

// optionalHeaderSize returns SizeOfOptionalHeader from the COFF header.
func (f *PEFile) optionalHeaderSize() uint16 {
	return binary.LittleEndian.Uint16(f.Data[f.PEOffset+4+16:])
}

// writeNumRvaAndSizes stores NumberOfRvaAndSizes back into the image.
func (f *PEFile) writeNumRvaAndSizes() {
	switch f.OptMagic {
	case optMagicPE32P:
		binary.LittleEndian.PutUint32(f.Data[f.OptOffset+108:], f.NumRvaAndSizes)
	case optMagicPE32:
		binary.LittleEndian.PutUint32(f.Data[f.OptOffset+92:], f.NumRvaAndSizes)
	}
}

// rvaToOffset maps an image RVA to a file offset (false when outside the
// raw data of any section).
func (f *PEFile) rvaToOffset(rva uint32) (int, bool) {
	for _, s := range f.Sections {
		vsize := s.VirtualSize
		if vsize == 0 || vsize < s.SizeOfRawData {
			vsize = s.SizeOfRawData
		}
		if rva >= s.VirtualAddress && rva < s.VirtualAddress+vsize {
			off := int(s.PointerToRawData) + int(rva-s.VirtualAddress)
			if off < 0 || off >= len(f.Data) || off >= int(s.PointerToRawData)+int(s.SizeOfRawData) {
				return 0, false
			}
			return off, true
		}
	}
	return 0, false
}

// sectionData returns the raw bytes of section s.
func (f *PEFile) sectionData(s PESection) ([]byte, error) {
	start := int(s.PointerToRawData)
	if start == 0 {
		return nil, fmt.Errorf("consumerspec: section %s has no raw data", s.Name)
	}
	end := start + int(s.SizeOfRawData)
	if start < 0 || end > len(f.Data) {
		return nil, fmt.Errorf("consumerspec: section %s raw data out of bounds", s.Name)
	}
	return f.Data[start:end], nil
}

// findResourceSection locates the section hosting the resource directory.
func (f *PEFile) findResourceSection() (*PESection, bool) {
	rva, _ := f.dataDir(dirResource)
	if rva == 0 {
		return nil, false
	}
	for i := range f.Sections {
		s := &f.Sections[i]
		vsize := s.VirtualSize
		if vsize == 0 || vsize < s.SizeOfRawData {
			vsize = s.SizeOfRawData
		}
		if rva >= s.VirtualAddress && rva < s.VirtualAddress+vsize {
			return s, true
		}
	}
	return nil, false
}

// ReadResourceSection returns the resource section payload and its RVA.
func (f *PEFile) ReadResourceSection() (data []byte, sectionRVA uint32, err error) {
	rva, _ := f.dataDir(dirResource)
	if rva == 0 {
		return nil, 0, errors.New("consumerspec: image has no resource directory")
	}
	s, ok := f.findResourceSection()
	if !ok {
		return nil, 0, fmt.Errorf("consumerspec: resource directory RVA %#x is outside every section", rva)
	}
	data, err = f.sectionData(*s)
	if err != nil {
		return nil, 0, err
	}
	return data, s.VirtualAddress, nil
}

// InspectManifest reads the RT_MANIFEST resource out of a PE image: the
// "inspect the final executable's manifest" gate of the distribution
// contract (never just the input XML or a DLL resource).
func InspectManifest(exe []byte) (*ResourceEntry, error) {
	f, err := ParsePE(exe)
	if err != nil {
		return nil, err
	}
	data, rva, err := f.ReadResourceSection()
	if err != nil {
		return nil, err
	}
	entries, err := ParseResourceSection(data, rva)
	if err != nil {
		return nil, err
	}
	m := FindManifest(entries)
	if m == nil {
		return nil, errors.New("consumerspec: resource section carries no RT_MANIFEST entry")
	}
	return m, nil
}

// EmbedResources embeds resources into a parsed PE image and returns the
// edited image bytes. The rules mirror the distribution contract:
//
//   - an image that already carries an RT_MANIFEST resource fails with
//     ErrDuplicateManifest (report the conflict, never silently overwrite);
//   - an image with other resources merges the new entries into the
//     existing tree in place when they fit (ErrSectionTooSmall otherwise:
//     use the .syso route);
//   - an image without resources gets a fresh .rsrc section appended after
//     the last section, with the resource data directory, SizeOfImage and
//     the PE checksum updated.
//
// The function never rewrites or drops consumer resource settings: existing
// entries are copied verbatim into the rebuilt tree.
func EmbedResources(exe []byte, add []Resource) ([]byte, error) {
	if len(add) == 0 {
		return nil, errors.New("consumerspec: no resources to embed")
	}
	// Copy so the caller's slice is never mutated.
	data := append([]byte(nil), exe...)
	f, err := ParsePE(data)
	if err != nil {
		return nil, err
	}

	if rva, _ := f.dataDir(dirResource); rva != 0 {
		return embedIntoExisting(f, add)
	}
	return appendResourceSection(f, add)
}

// embedIntoExisting merges resources into an existing resource section.
func embedIntoExisting(f *PEFile, add []Resource) ([]byte, error) {
	sec, _ := f.findResourceSection()
	if sec == nil {
		return nil, errors.New("consumerspec: resource directory set but no section hosts it")
	}
	old, err := f.sectionData(*sec)
	if err != nil {
		return nil, err
	}
	entries, err := ParseResourceSection(old, sec.VirtualAddress)
	if err != nil {
		return nil, fmt.Errorf("consumerspec: parsing the existing resource tree: %w", err)
	}
	if FindManifest(entries) != nil {
		for _, r := range add {
			if r.Type == rtManifest {
				return nil, ErrDuplicateManifest
			}
		}
	}
	var merged []Resource
	for _, e := range entries {
		merged = append(merged, Resource{Type: e.Type, Name: e.Name, Language: e.Language, Data: e.Data})
	}
	merged = append(merged, add...)
	built, err := BuildResourceSection(merged, sec.VirtualAddress)
	if err != nil {
		return nil, err
	}
	if len(built.Data) > int(sec.SizeOfRawData) {
		return nil, fmt.Errorf("%w: rebuilt tree needs %d bytes, section has %d",
			ErrSectionTooSmall, len(built.Data), sec.SizeOfRawData)
	}
	copy(f.Data[int(sec.PointerToRawData):], built.Data)
	if sec.VirtualSize < uint32(len(built.Data)) {
		binary.LittleEndian.PutUint32(f.Data[sec.HeaderOffset+8:], uint32(len(built.Data)))
	}
	if err := f.setDataDir(dirResource, sec.VirtualAddress, uint32(len(built.Data))); err != nil {
		return nil, err
	}
	writePEChecksum(f)
	return f.Data, nil
}

// appendResourceSection appends a new .rsrc section to the image.
func appendResourceSection(f *PEFile, add []Resource) ([]byte, error) {
	for _, s := range f.Sections {
		if s.Name == ".rsrc" {
			return nil, errors.New("consumerspec: image has a .rsrc section without a resource data directory; refusing to guess its layout")
		}
	}

	// The section's virtual address follows the last section, aligned.
	var maxVEnd, maxRawEnd uint32
	for _, s := range f.Sections {
		vend := s.VirtualAddress + s.VirtualSize
		if s.SizeOfRawData > s.VirtualSize {
			vend = s.VirtualAddress + s.SizeOfRawData
		}
		if vend > maxVEnd {
			maxVEnd = vend
		}
		if end := s.PointerToRawData + s.SizeOfRawData; end > maxRawEnd {
			maxRawEnd = end
		}
	}
	align := func(v, a uint32) uint32 {
		if a == 0 {
			return v
		}
		return (v + a - 1) &^ (a - 1)
	}
	newVA := align(maxVEnd, f.SectionAlignment)
	newRawPtr := align(maxRawEnd, f.FileAlignment)
	if newRawPtr < f.SizeOfHeaders {
		newRawPtr = align(f.SizeOfHeaders, f.FileAlignment)
	}

	// Build the tree with absolute RVAs at the chosen section address.
	built, err := BuildResourceSection(add, newVA)
	if err != nil {
		return nil, err
	}
	newRawSize := uint32(len(built.Data))
	newVSize := newRawSize

	// Header room: the new section header must stay inside SizeOfHeaders.
	sectTable := f.OptOffset + int(f.optionalHeaderSize())
	newTableEnd := sectTable + sectHdrSize*(int(f.NumberOfSection)+1)
	if uint32(newTableEnd) > f.SizeOfHeaders || newTableEnd > len(f.Data) {
		return nil, fmt.Errorf("%w: new table end %d exceeds SizeOfHeaders %d",
			ErrNoHeaderRoom, newTableEnd, f.SizeOfHeaders)
	}
	// The new raw data must not overlap existing raw data (it follows the
	// last section by construction).
	if newRawPtr < maxRawEnd {
		return nil, fmt.Errorf("consumerspec: computed raw pointer %d overlaps existing data ending at %d", newRawPtr, maxRawEnd)
	}

	// Grow the file.
	total := int(newRawPtr) + len(built.Data)
	if total > len(f.Data) {
		grown := make([]byte, total)
		copy(grown, f.Data)
		f.Data = grown
	}

	// Write the new section header.
	h := sectTable + sectHdrSize*int(f.NumberOfSection)
	copy(f.Data[h:h+8], ".rsrc\x00\x00\x00")
	binary.LittleEndian.PutUint32(f.Data[h+8:], newVSize)
	binary.LittleEndian.PutUint32(f.Data[h+12:], newVA)
	binary.LittleEndian.PutUint32(f.Data[h+16:], newRawSize)
	binary.LittleEndian.PutUint32(f.Data[h+20:], newRawPtr)
	binary.LittleEndian.PutUint32(f.Data[h+24:], 0)          // PointerToRelocations
	binary.LittleEndian.PutUint32(f.Data[h+28:], 0)          // PointerToLineNumbers
	binary.LittleEndian.PutUint16(f.Data[h+32:], 0)          // NumberOfRelocations
	binary.LittleEndian.PutUint16(f.Data[h+34:], 0)          // NumberOfLinenumbers
	binary.LittleEndian.PutUint32(f.Data[h+36:], 0x40000040) // CNT_INITIALIZED_DATA | MEM_READ

	// COFF header: NumberOfSections.
	coff := f.PEOffset + 4
	binary.LittleEndian.PutUint16(f.Data[coff+2:], f.NumberOfSection+1)

	// Section data.
	copy(f.Data[int(newRawPtr):], built.Data)

	// Resource data directory.
	if err := f.setDataDir(dirResource, newVA, newRawSize); err != nil {
		return nil, err
	}

	// SizeOfImage covers the new section's virtual extent.
	newSizeOfImage := align(newVA+newVSize, f.SectionAlignment)
	switch f.OptMagic {
	case optMagicPE32P, optMagicPE32:
		binary.LittleEndian.PutUint32(f.Data[f.OptOffset+56:], newSizeOfImage)
	}
	f.SizeOfImage = newSizeOfImage

	writePEChecksum(f)
	return f.Data, nil
}

// writePEChecksum recomputes the optional-header CheckSum field over the
// whole image (the field is zeroed for the computation and then stored).
// Windows does not verify it for ordinary user-mode images, but a correct
// value keeps the edited executable consistent for tooling that does.
func writePEChecksum(f *PEFile) {
	sum := peChecksum(f.Data, f.CheckSumOffset)
	binary.LittleEndian.PutUint32(f.Data[f.CheckSumOffset:], sum)
}

// peChecksum computes the standard PE checksum: the 16-bit-word sum of the
// whole file (checksum field treated as zero), folded to 16 bits after
// every addition, plus the file length.
func peChecksum(data []byte, checksumOffset int) uint32 {
	fold := func(sum uint64) uint64 { return (sum & 0xffff) + (sum >> 16) }
	var sum uint64
	for i := 0; i+1 < len(data); i += 2 {
		w := binary.LittleEndian.Uint16(data[i : i+2])
		// Treat the 4 checksum-field bytes as zero.
		for b := 0; b < 4; b++ {
			switch checksumOffset + b {
			case i:
				w &^= 0x00ff
			case i + 1:
				w &^= 0xff00
			}
		}
		sum = fold(sum + uint64(w))
	}
	if len(data)%2 == 1 {
		sum = fold(sum + uint64(data[len(data)-1])<<8)
	}
	sum = fold(sum)
	sum += uint64(len(data))
	return uint32(sum)
}
