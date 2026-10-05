package native

import (
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// verifyArtifactBytes checks a candidate artifact against its manifest, in the
// contract's order, before anything is loaded:
//
//  1. the manifest's declared SHA-256 equals the full SHA-256 of the bytes;
//  2. the manifest's declared byte count equals the actual length;
//  3. the PE machine type is IMAGE_FILE_MACHINE_AMD64 and the image parses.
//
// The authority check (manifest == the release's embedded manifest) is
// separate; see Manifest.matchesAuthority.
func verifyArtifactBytes(m *Manifest, dllBytes []byte, path string) error {
	if err := checkHashAndSize(m, dllBytes, path); err != nil {
		return err
	}
	return checkPEMachine(dllBytes, path)
}

// checkHashAndSize implements check 1 and 2.
func checkHashAndSize(m *Manifest, dllBytes []byte, path string) error {
	sum := sha256.Sum256(dllBytes)
	actual := hex.EncodeToString(sum[:])
	if actual != strings.ToLower(m.SHA256) {
		return &HashError{
			Kind:     "manifest-declared sha256",
			Path:     path,
			Expected: m.SHA256,
			Actual:   actual,
		}
	}
	if m.Bytes != int64(len(dllBytes)) {
		return &HashError{
			Kind:     "manifest-declared bytes",
			Path:     path,
			Expected: fmt.Sprintf("%d", m.Bytes),
			Actual:   fmt.Sprintf("%d", len(dllBytes)),
		}
	}
	return nil
}

// checkPEMachine implements check 3. A PE that cannot be parsed at all is an
// architecture-family failure: the loader refuses to interpret unknown
// binaries, whatever their hash.
func checkPEMachine(dllBytes []byte, path string) error {
	f, err := pe.NewFile(bytes.NewReader(dllBytes))
	if err != nil {
		return &ArchitectureError{Path: path, Reason: "PE parse failure: " + err.Error()}
	}
	defer f.Close()
	if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return &ArchitectureError{
			Path:     path,
			Expected: "amd64 (0x8664)",
			Actual:   machineName(f.Machine),
		}
	}
	return nil
}

// machineName renders a COFF machine code for error messages.
func machineName(machine uint16) string {
	switch machine {
	case pe.IMAGE_FILE_MACHINE_I386:
		return "i386 (0x14c)"
	case pe.IMAGE_FILE_MACHINE_ARM64:
		return "arm64 (0xaa64)"
	case pe.IMAGE_FILE_MACHINE_ARM:
		return "arm (0x1c0)"
	case pe.IMAGE_FILE_MACHINE_ARMNT:
		return "armnt (0x1c4)"
	case pe.IMAGE_FILE_MACHINE_IA64:
		return "ia64 (0x200)"
	case pe.IMAGE_FILE_MACHINE_UNKNOWN:
		return "unknown (0x0)"
	default:
		return fmt.Sprintf("machine %#x", machine)
	}
}

// validateAbiTable validates the loaded bootstrap table against the manifest
// after the layout self-checks pass:
//
//   - record size/alignment agree with the Go mirrors (layoutSelfChecks);
//   - magic is the "GPGO" constant;
//   - table abi_version == manifest abi_version (2 in either direction is an
//     ABI mismatch);
//   - table ce_commit == manifest ce_commit (the CE pin);
//   - all required capability bits are present (unknown extra bits are fine);
//   - the required function slot (buffer_round_trip) is non-null.
//
// table must point at a Go-owned copy of the DLL's table (see loadTable).
func validateAbiTable(t *abiTable, m *Manifest, path string) error {
	if err := t.layoutSelfChecks(); err != nil {
		if ae, ok := err.(*ABIError); ok {
			ae.Path = path
		}
		return err
	}
	if t.magic != abiMagic {
		return &ABIError{Path: path, Field: "magic", Expected: fmt.Sprintf("%#x", abiMagic), Actual: fmt.Sprintf("%#x", t.magic)}
	}
	if t.abiVersion != m.ABIVersion {
		return &ABIError{Path: path, Field: "abi_version", Expected: strconv.FormatUint(uint64(m.ABIVersion), 10), Actual: strconv.FormatUint(uint64(t.abiVersion), 10)}
	}
	if commit := trimZerosToString(t.ceCommit[:]); commit != m.CECommit {
		return &ABIError{Path: path, Field: "ce_commit", Expected: m.CECommit, Actual: commit}
	}
	if t.capabilities&requiredCapabilities != requiredCapabilities {
		return &CapabilityError{Path: path, Required: requiredCapabilities, Actual: t.capabilities}
	}
	if t.bufferRoundTrip == 0 {
		return &CapabilityError{Path: path, Detail: "buffer_round_trip function slot is null"}
	}
	return nil
}

func trimZerosToString(b []byte) string {
	n := len(b)
	for n > 0 && b[n-1] == 0 {
		n--
	}
	return string(b[:n])
}

// sha256Hex hashes data and returns the lowercase hex digest.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
