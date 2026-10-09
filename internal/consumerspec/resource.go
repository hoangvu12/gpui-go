package consumerspec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

// Win32 resource-directory constants (winnt.h / rsrc.h).
const (
	// rtManifest is the RT_MANIFEST resource type.
	rtManifest = 24

	// ManifestResourceID is the manifest resource identifier the Windows
	// loader reads from an executable (resource ID 1; ID 2 is reserved for
	// DLL manifests and side-by-side assemblies).
	ManifestResourceID = 1

	// ManifestLanguage is the manifest resource language (en-US, the
	// convention used by the pinned go-winres/rsrc mechanism).
	ManifestLanguage = 0x0409

	// resourceFlagDirectory is the high bit of a directory entry's
	// OffsetToData field marking a subdirectory (the low 31 bits are then
	// an offset relative to the resource section base).
	resourceFlagDirectory = 1 << 31

	// resourceFlagNameString is the high bit of the Name field marking a
	// named entry (not used by this package; every entry is id-based).
	resourceFlagNameString = 1 << 31

	// resourceDirHeaderSize is sizeof(IMAGE_RESOURCE_DIRECTORY): 16 bytes.
	resourceDirHeaderSize = 16
	// resourceDirEntrySize is sizeof(IMAGE_RESOURCE_DIRECTORY_ENTRY): 8 bytes.
	resourceDirEntrySize = 8
	// resourceDataEntrySize is sizeof(IMAGE_RESOURCE_DATA_ENTRY): 16 bytes.
	resourceDataEntrySize = 16

	// maxResourceEntries bounds the parsed tree (corruption guard).
	maxResourceEntries = 4096
)

// Resource is one id-addressed entry of a Win32 resource section.
type Resource struct {
	// Type is the level-1 id (RT_MANIFEST = 24).
	Type uint32
	// Name is the level-2 id (manifest resource ID 1).
	Name uint32
	// Language is the level-3 id (e.g. 0x0409).
	Language uint32
	// Data is the resource payload (manifest XML bytes).
	Data []byte
}

// ManifestResource builds the RT_MANIFEST resource for the given manifest
// bytes: type 24, resource ID 1, language en-US.
func ManifestResource(manifestXML []byte) Resource {
	return Resource{
		Type:     rtManifest,
		Name:     ManifestResourceID,
		Language: ManifestLanguage,
		Data:     append([]byte(nil), manifestXML...),
	}
}

// ResourceSection is the serialized Win32 resource section data.
type ResourceSection struct {
	// Data is the section payload: the directory tree, the data entries
	// and the resource blobs.
	Data []byte
	// RelocationOffsets are the byte offsets inside Data holding 32-bit
	// values the final image must patch to absolute RVAs (the leaf
	// directory entries' OffsetToData fields and the data entries'
	// OffsetToData fields). Non-empty only in the relative (syso) form.
	RelocationOffsets []uint32
	// Absolute reports whether Data carries absolute image RVAs (true,
	// direct executable embedding) or section-relative leaf offsets
	// (false, the .syso route).
	Absolute bool
}

// BuildResourceSection serializes resources into one .rsrc section payload.
//
// In ABSOLUTE mode (baseRVA != 0) every leaf pointer carries baseRVA+offset,
// the form a finished PE needs. In RELATIVE mode (baseRVA == 0) every leaf
// pointer carries the section-relative offset and its site is recorded in
// RelocationOffsets: the COFF object then relocates those fields so the
// Go linker can write the final section RVA plus the stored addend (see
// coff.go; cmd/link's addpersrc computes exactly VA+addend at each site).
//
// Directory pointers (pointing at subdirectories) are always
// section-relative in both modes, matching the PE resource format: only
// leaf pointers are absolute image RVAs.
func BuildResourceSection(resources []Resource, baseRVA uint32) (*ResourceSection, error) {
	if len(resources) == 0 {
		return nil, errors.New("consumerspec: no resources to serialize")
	}

	// Sort the tree: type, then name, then language; reject duplicates.
	sorted := make([]Resource, len(resources))
	copy(sorted, resources)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Language < b.Language
	})
	for i := 1; i < len(sorted); i++ {
		a, b := sorted[i-1], sorted[i]
		if a.Type == b.Type && a.Name == b.Name && a.Language == b.Language {
			return nil, fmt.Errorf("consumerspec: duplicate resource (type %d, name %d, language %#x)", a.Type, a.Name, a.Language)
		}
	}

	// Group the tree: types -> names -> languages.
	type nameNode struct {
		id    uint32
		langs []Resource
	}
	type typeNode struct {
		id    uint32
		names []nameNode
	}
	var types []typeNode
	for _, r := range sorted {
		var tn *typeNode
		if n := len(types); n > 0 && types[n-1].id == r.Type {
			tn = &types[n-1]
		} else {
			types = append(types, typeNode{id: r.Type})
			tn = &types[len(types)-1]
		}
		var nn *nameNode
		if n := len(tn.names); n > 0 && tn.names[n-1].id == r.Name {
			nn = &tn.names[n-1]
		} else {
			tn.names = append(tn.names, nameNode{id: r.Name})
			nn = &tn.names[len(tn.names)-1]
		}
		nn.langs = append(nn.langs, r)
	}

	// Layout: level-1 directory, then the level-2 directories, then the
	// level-3 directories, then the data entries, then the 4-byte-aligned
	// blobs — all in the sorted traversal order.
	size := resourceDirHeaderSize + resourceDirEntrySize*len(types)
	for i := range types {
		size += resourceDirHeaderSize + resourceDirEntrySize*len(types[i].names)
		for j := range types[i].names {
			size += resourceDirHeaderSize + resourceDirEntrySize*len(types[i].names[j].langs)
		}
	}
	nEntries := len(sorted)
	size += resourceDataEntrySize * nEntries
	for _, r := range sorted {
		size += (len(r.Data) + 3) &^ 3
	}
	buf := make([]byte, size)

	var off int
	typeLevel2 := make([]int, len(types))
	off = resourceDirHeaderSize + resourceDirEntrySize*len(types)
	for i := range types {
		typeLevel2[i] = off
		off += resourceDirHeaderSize + resourceDirEntrySize*len(types[i].names)
	}
	nameLevel3 := make([][]int, len(types))
	for i := range types {
		nameLevel3[i] = make([]int, len(types[i].names))
		for j := range types[i].names {
			nameLevel3[i][j] = off
			off += resourceDirHeaderSize + resourceDirEntrySize*len(types[i].names[j].langs)
		}
	}
	entryOff := make([]int, nEntries)
	for i := range entryOff {
		entryOff[i] = off
		off += resourceDataEntrySize
	}
	blobOff := make([]int, nEntries)
	for i, r := range sorted {
		blobOff[i] = off
		off += (len(r.Data) + 3) &^ 3
	}

	// leaf serializes one leaf pointer: an absolute RVA in absolute mode,
	// the section-relative offset (and a recorded relocation site) in
	// relative mode.
	var relocs []uint32
	leaf := func(v uint32) uint32 {
		if baseRVA != 0 {
			return baseRVA + v
		}
		return v
	}

	// writeDir writes one directory header at o with n id entries.
	writeDir := func(o, n int) {
		binary.LittleEndian.PutUint32(buf[o:], 0)            // Characteristics
		binary.LittleEndian.PutUint32(buf[o+4:], 0)          // TimeDateStamp
		binary.LittleEndian.PutUint16(buf[o+8:], 0)          // MajorVersion
		binary.LittleEndian.PutUint16(buf[o+10:], 0)         // MinorVersion
		binary.LittleEndian.PutUint16(buf[o+12:], 0)         // NumberOfNamedEntries
		binary.LittleEndian.PutUint16(buf[o+14:], uint16(n)) // NumberOfIdEntries
	}

	// Level 1: types.
	writeDir(0, len(types))
	for i, tn := range types {
		e := resourceDirHeaderSize + resourceDirEntrySize*i
		binary.LittleEndian.PutUint32(buf[e:], tn.id)
		binary.LittleEndian.PutUint32(buf[e+4:], uint32(typeLevel2[i])|resourceFlagDirectory)
	}
	// Level 2: names.
	for i, tn := range types {
		writeDir(typeLevel2[i], len(tn.names))
		for j, nn := range tn.names {
			e := typeLevel2[i] + resourceDirHeaderSize + resourceDirEntrySize*j
			binary.LittleEndian.PutUint32(buf[e:], nn.id)
			binary.LittleEndian.PutUint32(buf[e+4:], uint32(nameLevel3[i][j])|resourceFlagDirectory)
		}
	}
	// Level 3: languages -> leaf pointers into the data entries; data
	// entries -> leaf pointers to the blobs.
	ei := 0
	for i, tn := range types {
		for j, nn := range tn.names {
			writeDir(nameLevel3[i][j], len(nn.langs))
			for k, res := range nn.langs {
				e := nameLevel3[i][j] + resourceDirHeaderSize + resourceDirEntrySize*k
				binary.LittleEndian.PutUint32(buf[e:], res.Language)
				binary.LittleEndian.PutUint32(buf[e+4:], leaf(uint32(entryOff[ei])))
				if baseRVA == 0 {
					relocs = append(relocs, uint32(e+4))
				}
				de := entryOff[ei]
				binary.LittleEndian.PutUint32(buf[de:], leaf(uint32(blobOff[ei])))
				if baseRVA == 0 {
					relocs = append(relocs, uint32(de))
				}
				binary.LittleEndian.PutUint32(buf[de+4:], uint32(len(res.Data)))
				binary.LittleEndian.PutUint32(buf[de+8:], 0)  // CodePage
				binary.LittleEndian.PutUint32(buf[de+12:], 0) // Reserved
				copy(buf[blobOff[ei]:], res.Data)
				ei++
			}
		}
	}

	return &ResourceSection{
		Data:              buf,
		RelocationOffsets: relocs,
		Absolute:          baseRVA != 0,
	}, nil
}

// ResourceEntry is one parsed leaf of a resource section.
type ResourceEntry struct {
	// Type, Name, Language are the ids addressing the entry.
	Type, Name, Language uint32
	// Data is a copy of the resource payload.
	Data []byte
}

// ParseResourceSection walks the resource directory tree in data.
// sectionRVA is the virtual address of the resource section in the image
// (0 for the relative/syso form, where leaf pointers are section-relative
// offsets rather than absolute RVAs). Named (string) entries are reported
// as an error: this package only writes and consumes id-addressed trees.
func ParseResourceSection(data []byte, sectionRVA uint32) ([]ResourceEntry, error) {
	if len(data) < resourceDirHeaderSize {
		return nil, errors.New("consumerspec: resource section truncated")
	}
	var entries []ResourceEntry
	var walk func(off, depth int, typ, name uint32) error
	walk = func(off, depth int, typ, name uint32) error {
		if off < 0 || off+resourceDirHeaderSize > len(data) {
			return errors.New("consumerspec: resource directory header out of bounds")
		}
		if depth > 3 {
			return errors.New("consumerspec: resource directory deeper than the three id levels")
		}
		named := binary.LittleEndian.Uint16(data[off+12:])
		ids := binary.LittleEndian.Uint16(data[off+14:])
		if int(named) != 0 {
			return errors.New("consumerspec: named (string) resource entries are not supported")
		}
		n := int(ids)
		if off+resourceDirHeaderSize+resourceDirEntrySize*n > len(data) {
			return errors.New("consumerspec: resource directory entries out of bounds")
		}
		if len(entries)+n > maxResourceEntries {
			return errors.New("consumerspec: resource section declares too many entries (corrupt)")
		}
		for i := 0; i < n; i++ {
			e := off + resourceDirHeaderSize + resourceDirEntrySize*i
			id := binary.LittleEndian.Uint32(data[e:])
			if id&resourceFlagNameString != 0 {
				return errors.New("consumerspec: named (string) resource entries are not supported")
			}
			offset := binary.LittleEndian.Uint32(data[e+4:])
			if offset&resourceFlagDirectory != 0 {
				sub := int(offset &^ resourceFlagDirectory)
				switch depth {
				case 1:
					if err := walk(sub, depth+1, id, 0); err != nil {
						return err
					}
				case 2:
					if err := walk(sub, depth+1, typ, id); err != nil {
						return err
					}
				default:
					return errors.New("consumerspec: resource directory deeper than the three id levels")
				}
				continue
			}
			// Leaf: a data entry. The offset is an absolute RVA when a
			// section RVA is given, else a section-relative offset.
			rva := offset
			if sectionRVA != 0 {
				if rva < sectionRVA {
					return fmt.Errorf("consumerspec: resource data entry RVA %#x precedes the resource section RVA %#x", rva, sectionRVA)
				}
				rva -= sectionRVA
			}
			de := int(rva)
			if de < 0 || de+resourceDataEntrySize > len(data) {
				return fmt.Errorf("consumerspec: resource data entry at %#x out of bounds", rva)
			}
			dataRVA := binary.LittleEndian.Uint32(data[de:])
			size := binary.LittleEndian.Uint32(data[de+4:])
			blob := dataRVA
			if sectionRVA != 0 {
				if blob < sectionRVA {
					return fmt.Errorf("consumerspec: resource blob RVA %#x precedes the resource section RVA %#x", blob, sectionRVA)
				}
				blob -= sectionRVA
			}
			if int(blob)+int(size) > len(data) {
				return fmt.Errorf("consumerspec: resource blob (%d bytes at %#x) out of bounds", size, blob)
			}
			entries = append(entries, ResourceEntry{
				Type:     typ,
				Name:     name,
				Language: id,
				Data:     append([]byte(nil), data[blob:blob+size]...),
			})
		}
		return nil
	}
	if err := walk(0, 1, 0, 0); err != nil {
		return nil, err
	}
	return entries, nil
}

// FindManifest returns the first RT_MANIFEST entry of a parsed tree, or nil.
func FindManifest(entries []ResourceEntry) *ResourceEntry {
	for i := range entries {
		if entries[i].Type == rtManifest {
			return &entries[i]
		}
	}
	return nil
}
