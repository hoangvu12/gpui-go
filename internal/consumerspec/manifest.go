// Package consumerspec implements the pure-Go consumer-delivery slice of
// ticket31 (.scratch/gpui-core-implementation/issues/31-consumer-delivery.md)
// against the distribution contract (docs/distribution-contract.md,
// "Application DPI resources") and the conformance contract
// (docs/conformance-contract.md, "Distribution and failure evidence").
//
// It owns, with the standard library only:
//
//   - the application manifest model: a PerMonitorV2 DPI manifest generated
//     from explicit customizations, and validation of consumer-provided
//     manifests that preserves their content and reports conflicting DPI
//     declarations instead of silently overwriting them (manifest.go);
//   - the Win32 resource-directory tree for RT_MANIFEST (resource.go),
//     serialized either with section-relative offsets plus relocation sites
//     (the .syso route) or with absolute image RVAs (direct executable
//     editing);
//   - the COFF object writer that produces the architecture-specific
//     rsrc_windows_<arch>.syso ordinary `go build` links in (coff.go) — the
//     mechanism the distribution contract pins through the go-winres
//     approach, reimplemented with zero dependencies because this module
//     is offline and stdlib-only;
//   - pure-Go PE parsing and editing that embeds the resource section into
//     a built executable, rejecting duplicate manifests (pe.go);
//   - import-table verification of a built artifact: normal imports, delay
//     imports, dynamically resolved loads and the actual static-CRT
//     outcome, never inferred from a build flag (imports.go);
//   - the pinned evidence identity of the shipped native artifact and the
//     module-size measurements (evidence.go).
//
// Consumer packages never import research/reference snapshots; everything
// here works on bytes supplied by the caller or on the module's own
// committed artifact files.
package consumerspec

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"
)

// ---------------------------------------------------------------------------
// Manifest specification
// ---------------------------------------------------------------------------

// Default manifest field values.
const (
	DefaultManifestName    = "gpui-go.consumer"
	DefaultManifestVersion = "1.0.0.0"
	DefaultExecutionLevel  = "asInvoker"
)

// RequiredDPIAwarenessFirst is the DPI awareness value every executable
// resource produced or accepted by this package must declare FIRST:
// PerMonitorV2 (the fallback list may continue with PerMonitor, which
// covers Windows versions without V2).
const RequiredDPIAwarenessFirst = "PerMonitorV2"

// ExecutionLevel values allowed in a generated manifest (requestedExecutionLevel).
var allowedExecutionLevels = map[string]bool{
	"asInvoker":            true,
	"highestAvailable":     true,
	"requireAdministrator": true,
}

// ManifestSpec describes one consumer application manifest. Zero fields fall
// back to the defaults; every field is an explicit application resource
// setting this package preserves verbatim (the generated XML never edits or
// discards consumer customizations).
type ManifestSpec struct {
	// Name is the assemblyIdentity name. Default DefaultManifestName.
	Name string
	// Version is the assemblyIdentity version. Default DefaultManifestVersion.
	Version string
	// Description is the optional <description> element.
	Description string
	// ExecutionLevel is requestedExecutionLevel. Default DefaultExecutionLevel.
	ExecutionLevel string
	// ProcessorArchitecture is the assemblyIdentity processorArchitecture
	// attribute. Default "*" (wildcard) so the same manifest serves every
	// consumer architecture.
	ProcessorArchitecture string
}

// normalize fills defaults and validates the field values.
func (s ManifestSpec) normalize() (ManifestSpec, error) {
	if strings.TrimSpace(s.Name) == "" {
		s.Name = DefaultManifestName
	}
	if strings.TrimSpace(s.Version) == "" {
		s.Version = DefaultManifestVersion
	}
	if strings.TrimSpace(s.ExecutionLevel) == "" {
		s.ExecutionLevel = DefaultExecutionLevel
	}
	if strings.TrimSpace(s.ProcessorArchitecture) == "" {
		s.ProcessorArchitecture = "*"
	}
	if !allowedExecutionLevels[s.ExecutionLevel] {
		return s, fmt.Errorf("consumerspec: execution level %q is not one of asInvoker, highestAvailable, requireAdministrator", s.ExecutionLevel)
	}
	// Attribute-position values: control characters are rejected outright;
	// everything else is XML-escaped verbatim below (an explicit application
	// setting is never edited or discarded).
	for field, value := range map[string]string{
		"name": s.Name, "description": s.Description,
		"execution level": s.ExecutionLevel, "processor architecture": s.ProcessorArchitecture,
	} {
		if strings.ContainsFunc(value, unicode.IsControl) {
			return s, fmt.Errorf("consumerspec: manifest %s contains control characters", field)
		}
	}
	return s, nil
}

// supportedOSGUIDs lists the compatibility GUIDs for Windows 7 through
// Windows 11 (Windows 11 reports through the Windows 10 GUID), preserving
// the order the Windows loader documents.
var supportedOSGUIDs = []string{
	"{35138b9a-5d96-4fbd-8e2d-a2440225f93a}", // Windows 7
	"{4a2f28e3-53b9-4441-ba9c-d69d4a4a6e38}", // Windows 8
	"{1f676c76-80e1-4239-95bb-83d0f6d0da78}", // Windows 8.1
	"{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}", // Windows 10 / 11
}

// BuildManifest renders the application manifest XML for spec: a Windows
// assembly declaring PerMonitorV2 DPI awareness first (with the PerMonitor
// fallback), the requested execution level, the supported-OS compatibility
// list and the explicit customizations. The returned bytes are UTF-8 (no
// BOM), matching the resource bytes written by the resource command.
func BuildManifest(spec ManifestSpec) ([]byte, error) {
	s, err := spec.normalize()
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">` + "\n")
	fmt.Fprintf(&b, "  <assemblyIdentity type=\"win32\" name=\"%s\" version=\"%s\" processorArchitecture=\"%s\"/>\n",
		xmlEscape(s.Name), xmlEscape(s.Version), xmlEscape(s.ProcessorArchitecture))
	if s.Description != "" {
		fmt.Fprintf(&b, "  <description>%s</description>\n", xmlEscape(s.Description))
	}
	b.WriteString("  <trustInfo xmlns=\"urn:schemas-microsoft-com:asm.v3\">\n")
	b.WriteString("    <security>\n")
	b.WriteString("      <requestedPrivileges>\n")
	fmt.Fprintf(&b, "        <requestedExecutionLevel level=\"%s\" uiAccess=\"false\"/>\n", xmlEscape(s.ExecutionLevel))
	b.WriteString("      </requestedPrivileges>\n")
	b.WriteString("    </security>\n")
	b.WriteString("  </trustInfo>\n")
	b.WriteString("  <compatibility xmlns=\"urn:schemas-microsoft-com:compatibility.v1\">\n")
	b.WriteString("    <application>\n")
	for _, guid := range supportedOSGUIDs {
		fmt.Fprintf(&b, "      <supportedOS Id=\"%s\"/>\n", guid)
	}
	b.WriteString("    </application>\n")
	b.WriteString("  </compatibility>\n")
	// The required DPI declaration: PerMonitorV2 first, PerMonitor as the
	// documented fallback for hosts without V2.
	b.WriteString("  <asmv3:application xmlns:asmv3=\"urn:schemas-microsoft-com:asm.v3\">\n")
	b.WriteString("    <asmv3:windowsSettings>\n")
	b.WriteString("      <dpiAwareness xmlns=\"http://schemas.microsoft.com/SMI/2016/WindowsSettings\">PerMonitorV2, PerMonitor</dpiAwareness>\n")
	b.WriteString("    </asmv3:windowsSettings>\n")
	b.WriteString("  </asmv3:application>\n")
	b.WriteString("</assembly>\n")
	return b.Bytes(), nil
}

// xmlEscape escapes a manifest attribute/element value.
func xmlEscape(s string) string {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// ---------------------------------------------------------------------------
// Manifest validation (consumed manifests are preserved, conflicts reported)
// ---------------------------------------------------------------------------

// Sentinel errors for manifest validation.
var (
	// ErrManifestMalformed: the XML is not a well-formed assembly manifest.
	ErrManifestMalformed = errors.New("consumerspec: manifest XML is malformed")
	// ErrManifestMissingDPI: the manifest declares no DPI awareness at all.
	ErrManifestMissingDPI = errors.New("consumerspec: manifest declares no DPI awareness")
	// ErrManifestConflict: the manifest declares an awareness that conflicts
	// with the required PerMonitorV2-first setting (or mixes the legacy
	// dpiAware element with it).
	ErrManifestConflict = errors.New("consumerspec: manifest DPI awareness conflicts with the required PerMonitorV2 declaration")
)

// DPIAwareness is the parsed DPI declaration of a manifest.
type DPIAwareness struct {
	// Values are the whitespace/comma-separated dpiAwareness list entries,
	// in declaration order ("" when the manifest has no dpiAwareness
	// element).
	Values []string
	// Legacy is the content of a legacy <dpiAware> element ("" when absent).
	Legacy string
}

// PerMonitorV2First reports whether the dpiAwareness list exists and its
// first entry is PerMonitorV2 (case-insensitive, the documented spelling).
func (d DPIAwareness) PerMonitorV2First() bool {
	if len(d.Values) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(d.Values[0]), RequiredDPIAwarenessFirst)
}

// ValidateManifest validates a consumer-provided manifest without modifying
// a byte of it. The distribution contract requires the resource command to
// "preserve explicit application resource settings and report conflicting
// manifests instead of silently overwriting them", so a manifest passes only
// when its DPI declaration is exactly the required one:
//
//   - a dpiAwareness list whose FIRST entry is PerMonitorV2 passes
//     (additional fallback entries such as PerMonitor are preserved);
//   - a dpiAwareness list with a different first entry, or a legacy
//     <dpiAware> element (which cannot express V2, and whose presence
//     alongside asmv3 settings is ambiguous), fails with
//     ErrManifestConflict;
//   - a manifest with no DPI declaration at all fails with
//     ErrManifestMissingDPI: the command never silently injects awareness
//     into a consumer-authored manifest.
//
// Malformed XML fails with ErrManifestMalformed (wrapped).
func ValidateManifest(manifest []byte) (DPIAwareness, error) {
	text, err := decodeManifestBytes(manifest)
	if err != nil {
		return DPIAwareness{}, err
	}

	var (
		dpi       DPIAwareness
		rootSeen  bool
		rootIsAsm bool
		depth     int
		capture   string // local name whose text is being captured
		captured  strings.Builder
	)
	dec := xml.NewDecoder(bytes.NewReader(text))
	dec.Strict = true
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return DPIAwareness{}, fmt.Errorf("%w: %v", ErrManifestMalformed, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			local := localName(t.Name.Local)
			if depth == 1 {
				rootSeen = true
				rootIsAsm = local == "assembly"
			}
			switch local {
			case "dpiAwareness", "dpiAware":
				capture = local
				captured.Reset()
			}
		case xml.CharData:
			if capture != "" {
				captured.Write(t)
			}
		case xml.EndElement:
			local := localName(t.Name.Local)
			if capture != "" && local == capture {
				switch capture {
				case "dpiAwareness":
					dpi.Values = splitAwarenessList(captured.String())
				case "dpiAware":
					dpi.Legacy = strings.TrimSpace(captured.String())
				}
				capture = ""
			}
			depth--
		}
	}
	if !rootSeen {
		return dpi, fmt.Errorf("%w: no root element", ErrManifestMalformed)
	}
	if !rootIsAsm {
		return dpi, fmt.Errorf("%w: root element is not <assembly>", ErrManifestMalformed)
	}

	if dpi.Legacy != "" {
		if len(dpi.Values) > 0 {
			return dpi, fmt.Errorf("%w: legacy <dpiAware>%q is declared alongside <dpiAwareness>%q; the legacy element cannot express PerMonitorV2 and must be removed",
				ErrManifestConflict, dpi.Legacy, strings.Join(dpi.Values, ", "))
		}
		return dpi, fmt.Errorf("%w: only the legacy <dpiAware>%q element is declared; it cannot express PerMonitorV2 (replace it with an asmv3 <dpiAwareness>PerMonitorV2, PerMonitor</dpiAwareness> list)",
			ErrManifestConflict, dpi.Legacy)
	}
	if len(dpi.Values) == 0 {
		return dpi, fmt.Errorf("%w: neither <dpiAwareness> nor <dpiAware> is present; add <dpiAwareness>PerMonitorV2, PerMonitor</dpiAwareness> under asmv3:windowsSettings, or drop -manifest and let the command generate one",
			ErrManifestMissingDPI)
	}
	if !dpi.PerMonitorV2First() {
		return dpi, fmt.Errorf("%w: dpiAwareness list is %q; the first entry must be PerMonitorV2",
			ErrManifestConflict, strings.Join(dpi.Values, ", "))
	}
	return dpi, nil
}

// localName normalizes a decoded local name (the decoder already removed
// namespace prefixes).
func localName(name string) string {
	return strings.TrimSpace(name)
}

// splitAwarenessList splits a dpiAwareness list on commas and whitespace.
func splitAwarenessList(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\r' || r == '\n'
	})
	var out []string
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// decodeManifestBytes decodes the manifest payload: UTF-8 (with or without
// BOM), UTF-16LE or UTF-16BE with BOM. Windows accepts all three encodings
// for RT_MANIFEST resources; go-winres writes UTF-8.
func decodeManifestBytes(data []byte) ([]byte, error) {
	switch {
	case len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF:
		return data[3:], nil
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return utf16Decode(data[2:], false), nil
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return utf16Decode(data[2:], true), nil
	default:
		return data, nil
	}
}

// utf16Decode converts UTF-16 code units to UTF-8 text.
func utf16Decode(units []byte, bigEndian bool) []byte {
	var sb strings.Builder
	for i := 0; i+1 < len(units); i += 2 {
		var u uint32
		if bigEndian {
			u = uint32(units[i])<<8 | uint32(units[i+1])
		} else {
			u = uint32(units[i+1])<<8 | uint32(units[i])
		}
		switch {
		case u >= 0xD800 && u <= 0xDBFF && i+3 < len(units):
			var lo uint32
			if bigEndian {
				lo = uint32(units[i+2])<<8 | uint32(units[i+3])
			} else {
				lo = uint32(units[i+3])<<8 | uint32(units[i+2])
			}
			if lo >= 0xDC00 && lo <= 0xDFFF {
				r := 0x10000 + (u-0xD800)<<10 + (lo - 0xDC00)
				sb.WriteRune(rune(r))
				i += 2
				continue
			}
			sb.WriteRune(0xFFFD)
		case u >= 0xD800 && u <= 0xDFFF:
			sb.WriteRune(0xFFFD)
		default:
			sb.WriteRune(rune(u))
		}
	}
	return []byte(sb.String())
}

// SortAwarenessValues returns the values in declaration order (helper for
// diagnostics; lists are validated, never reordered).
func SortAwarenessValues(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
