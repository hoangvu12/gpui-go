package consumerspec

import (
	"encoding/binary"
	"fmt"
)

// COFF object constants (winnt.h) for the .syso resource carrier.
const (
	// coffMachineAMD64 / coffMachineI386 are the accepted machines.
	coffMachineAMD64 = 0x8664
	coffMachineI386  = 0x014c

	// coffRelocAMD64Addr32 / coffRelocI386DIR32 are the relocation types
	// the Go linker maps to objabi.R_ADDR for the machine (see
	// cmd/link/internal/loadpe/ldpe.go: IMAGE_REL_AMD64_ADDR32 = 0x0002,
	// IMAGE_REL_I386_DIR32 = 0x0006).
	coffRelocAMD64Addr32 = 0x0002
	coffRelocI386DIR32   = 0x0006

	// coffSymClassStatic marks the section symbol (cmd/link's issect:
	// storage class STATIC, type 0, name starting with '.'); the
	// relocation addend then includes the symbol value (0), i.e. the
	// 32-bit value stored at each relocation site.
	coffSymClassStatic = 3

	// section characteristics for .rsrc: initialized data, read-only —
	// the exact combination cmd/link maps to SRODATA.
	coffSectionCharacteristics = 0x40000040 // CNT_INITIALIZED_DATA | MEM_READ

	// coffSymbolSize is sizeof(COFF symbol record): 18 bytes.
	coffSymbolSize = 18
	// coffRelocSize is sizeof(COFF relocation record): 10 bytes.
	coffRelocSize = 10
)

// COFFMachineForArch maps a Go architecture name to the COFF machine code
// the architecture-specific .syso must carry ("amd64", "386"; everything
// else is an unsupported target for the initial artifact family).
func COFFMachineForArch(goarch string) (uint16, error) {
	switch goarch {
	case "amd64":
		return coffMachineAMD64, nil
	case "386":
		return coffMachineI386, nil
	default:
		return 0, fmt.Errorf("consumerspec: unsupported consumer architecture %q (the initial artifact family targets windows amd64; 386 object files are accepted but unverified)", goarch)
	}
}

// SysoName returns the architecture-specific .syso base name for a Go
// os/arch pair: "rsrc_windows_amd64.syso". Ordinary `go build` links every
// .syso of the consumer main package directory into the executable, so the
// name documents the target while the COFF machine inside is what the
// linker actually matches.
func SysoName(goos, goarch string) string {
	return fmt.Sprintf("rsrc_%s_%s.syso", goos, goarch)
}

// BuildSyso produces the complete COFF object file for one resource
// section. The layout follows the pinned go-winres mechanism (itself the
// rsrc approach), reimplemented with the standard library only:
//
//	COFF file header      20 bytes: machine, 1 section, timestamp 0
//	                      (reproducible output), 1 symbol, no optional
//	                      header
//	section header        40 bytes: ".rsrc", SizeOfRawData,
//	                      PointerToRelocations, characteristics
//	                      CNT_INITIALIZED_DATA|MEM_READ
//	section data          the resource directory (leaf pointers hold
//	                      section-relative offsets)
//	relocations          10 bytes each, type ADDR32/DIR32 against the
//	                      section symbol; the Go linker rewrites each
//	                      site to finalRVA + storedOffset
//	symbol               ".rsrc", value 0, section 1, storage class
//	                      STATIC (the section symbol cmd/link resolves)
//	string table          4 bytes (empty)
//
// section must be the relative-form ResourceSection (BuildResourceSection
// with baseRVA 0): its RelocationOffsets drive the relocation records.
func BuildSyso(machine uint16, section *ResourceSection) ([]byte, error) {
	if section == nil || len(section.Data) == 0 {
		return nil, fmt.Errorf("consumerspec: no resource section data to wrap in a .syso")
	}
	if section.Absolute {
		return nil, fmt.Errorf("consumerspec: BuildSyso needs the relative (baseRVA 0) resource form")
	}

	var relocType uint16
	switch machine {
	case coffMachineAMD64:
		relocType = coffRelocAMD64Addr32
	case coffMachineI386:
		relocType = coffRelocI386DIR32
	default:
		return nil, fmt.Errorf("consumerspec: unsupported COFF machine %#x", machine)
	}

	// Layout: header(20) + section header(40) + data (8-aligned) +
	// relocations + symbol(18) + string table(4).
	const headerSize = 20 + 40
	dataOff := (headerSize + 7) &^ 7
	relocOff := dataOff + len(section.Data)
	symOff := relocOff + coffRelocSize*len(section.RelocationOffsets)
	total := symOff + coffSymbolSize + 4

	buf := make([]byte, total)

	// COFF file header.
	binary.LittleEndian.PutUint16(buf[0:], machine)
	binary.LittleEndian.PutUint16(buf[2:], 1) // NumberOfSections
	binary.LittleEndian.PutUint32(buf[4:], 0) // TimeDateStamp: reproducible
	binary.LittleEndian.PutUint32(buf[8:], uint32(symOff))
	binary.LittleEndian.PutUint32(buf[12:], 1) // NumberOfSymbols
	binary.LittleEndian.PutUint16(buf[16:], 0) // SizeOfOptionalHeader
	binary.LittleEndian.PutUint16(buf[18:], 0) // Characteristics

	// Section header.
	sh := buf[20:60]
	copy(sh[0:8], ".rsrc\x00\x00\x00")
	binary.LittleEndian.PutUint32(sh[8:], 0)                          // VirtualSize (object)
	binary.LittleEndian.PutUint32(sh[12:], 0)                         // VirtualAddress (object)
	binary.LittleEndian.PutUint32(sh[16:], uint32(len(section.Data))) // SizeOfRawData
	binary.LittleEndian.PutUint32(sh[20:], uint32(dataOff))           // PointerToRawData
	binary.LittleEndian.PutUint32(sh[24:], uint32(relocOff))          // PointerToRelocations
	binary.LittleEndian.PutUint32(sh[28:], 0)                         // PointerToLineNumbers
	binary.LittleEndian.PutUint16(sh[32:], uint16(len(section.RelocationOffsets)))
	binary.LittleEndian.PutUint16(sh[34:], 0) // NumberOfLinenumbers
	binary.LittleEndian.PutUint32(sh[36:], coffSectionCharacteristics)

	// Section data.
	copy(buf[dataOff:], section.Data)

	// Relocations: every leaf-pointer site, pointing at symbol index 0
	// (the .rsrc section symbol).
	for i, site := range section.RelocationOffsets {
		r := relocOff + coffRelocSize*i
		binary.LittleEndian.PutUint32(buf[r:], site)
		binary.LittleEndian.PutUint32(buf[r+4:], 0) // symbol table index 0
		binary.LittleEndian.PutUint16(buf[r+8:], relocType)
	}

	// Symbol: the ".rsrc" section symbol (storage class STATIC, type 0,
	// value 0). issect() in cmd/link identifies it by exactly these
	// properties, so every relocation addend is the value stored at the
	// site plus 0.
	sym := buf[symOff : symOff+coffSymbolSize]
	copy(sym[0:8], ".rsrc\x00\x00\x00")
	binary.LittleEndian.PutUint32(sym[8:], 0)  // Value
	binary.LittleEndian.PutUint16(sym[12:], 1) // SectionNumber
	binary.LittleEndian.PutUint16(sym[14:], 0) // Type
	sym[16] = coffSymClassStatic               // StorageClass
	sym[17] = 0                                // NumberOfAuxSymbols

	// String table: length field only (4 bytes, "length includes itself").
	binary.LittleEndian.PutUint32(buf[total-4:], 4)

	return buf, nil
}
