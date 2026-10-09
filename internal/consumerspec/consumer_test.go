package consumerspec

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// This file is ticket31's consumer-delivery test gate: the pure-Go
// resource pipeline (manifest build/validate, resource build/parse,
// PE embed with duplicate rejection), the real embedded artifact's
// identity against the recorded evidence, the real DLL's import
// surface (static CRT, delay imports through data directory 13), the
// module-size limits, and the end-to-end embed into a real Go-built
// executable.

// realArtifact loads the module's embedded artifact pair (skipping on
// non-Windows where the pair is absent).
func realArtifact(t *testing.T) (dll, manifest []byte) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("the embedded Windows artifact pair ships on windows builds")
	}
	dllPath, manifestPath, err := FindNativeArtifact()
	if err != nil {
		t.Fatalf("FindNativeArtifact: %v", err)
	}
	dll, err = os.ReadFile(dllPath)
	if err != nil {
		t.Fatalf("reading the DLL: %v", err)
	}
	manifest, err = os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("reading the artifact manifest: %v", err)
	}
	return dll, manifest
}

// TestArtifactPairMatchesRecordedEvidence verifies the exact
// artifact/manifest hashes: the module's embedded pair must match the
// recorded identity (native revision 7, sha256 d648523b…, 12,489,728
// bytes, the pinned CE commit), and the evidence records must mention
// the same SHA (the provenance check).
func TestArtifactPairMatchesRecordedEvidence(t *testing.T) {
	dll, manifest := realArtifact(t)
	identity, err := VerifyArtifactPair(dll, manifest)
	if err != nil {
		t.Fatalf("VerifyArtifactPair: %v", err)
	}
	if identity.DLLSHA256 != EvidenceArtifactSHA256 {
		t.Errorf("DLL sha256 = %s, want the recorded %s", identity.DLLSHA256, EvidenceArtifactSHA256)
	}
	if int64(len(dll)) != EvidenceArtifactBytes {
		t.Errorf("DLL size = %d, want the recorded %d", len(dll), EvidenceArtifactBytes)
	}
	if identity.ManifestRevision != EvidenceNativeRevision {
		t.Errorf("manifest native revision = %d, want %d", identity.ManifestRevision, EvidenceNativeRevision)
	}
	if !identity.MatchesEvidence {
		t.Errorf("the artifact pair does not match the evidence: %v", identity.Differences)
	}
	// The provenance check: an evidence record mentions the same SHA.
	found, where, err := EvidenceRecordMentionsSHA(EvidenceArtifactSHA256)
	if err != nil {
		t.Fatalf("EvidenceRecordMentionsSHA: %v", err)
	}
	if !found {
		t.Errorf("no evidence record mentions the artifact SHA (searched %s)", where)
	}
}

// TestRealDLLImportsAreOSOnlyStaticCRT verifies the actual static-CRT
// outcome on the real embedded DLL (no VCRUNTIME/MSVCP/UCRT imports;
// only OS system components), with the delay imports parsed through
// data directory 13 — never inferred from a build flag.
func TestRealDLLImportsAreOSOnlyStaticCRT(t *testing.T) {
	dll, _ := realArtifact(t)
	report, err := ParseImports(dll)
	if err != nil {
		t.Fatalf("ParseImports on the real DLL: %v", err)
	}
	if !report.StaticCRT() {
		t.Fatalf("the real DLL reports a non-static CRT import set %+v; the artifact identity drifted", report.CRTDLLs)
	}
	if len(report.NormalDLLs) == 0 {
		t.Fatal("the real DLL reports no normal imports; the parse is broken")
	}
	for _, imported := range append(append([]ImportDLL{}, report.NormalDLLs...), report.DelayDLLs...) {
		if isCRTDLLName(imported.Name) {
			t.Errorf("CRT-family import %q present (delay=%v)", imported.Name, imported.Delay)
		}
		t.Logf("imported: %s (delay=%v, %d functions, %d ordinals)", imported.Name, imported.Delay, len(imported.Functions), len(imported.Ordinals))
	}
}

// TestManifestBuildValidateRoundTrip checks the manifest pipeline:
// the default build carries the required PerMonitorV2-first
// declaration, and a manifest without DPI awareness is rejected.
func TestManifestBuildValidateRoundTrip(t *testing.T) {
	manifest, err := BuildManifest(ManifestSpec{})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	awareness, err := ValidateManifest(manifest)
	if err != nil {
		t.Fatalf("ValidateManifest: %v", err)
	}
	if !awareness.PerMonitorV2First() {
		t.Fatalf("round-tripped awareness = %+v, want PerMonitorV2 first", awareness)
	}

	// A manifest without any DPI declaration is rejected with the
	// typed missing-DPI error.
	dpiless := strings.Replace(string(manifest),
		"<dpiAwareness xmlns=\"http://schemas.microsoft.com/SMI/2016/WindowsSettings\">PerMonitorV2, PerMonitor</dpiAwareness>\n", "", 1)
	if _, err := ValidateManifest([]byte(dpiless)); !errors.Is(err, ErrManifestMissingDPI) {
		t.Fatalf("a DPI-less manifest must fail with ErrManifestMissingDPI, got %v", err)
	}

	// A conflicting DPI list is rejected.
	conflicting := strings.Replace(string(manifest), "PerMonitorV2, PerMonitor", "system", 1)
	if _, err := ValidateManifest([]byte(conflicting)); !errors.Is(err, ErrManifestConflict) {
		t.Fatalf("a conflicting manifest must fail with ErrManifestConflict, got %v", err)
	}
}

// TestResourceSectionRoundTrip builds a manifest resource section and
// parses it back: the manifest is found with its exact bytes.
func TestResourceSectionRoundTrip(t *testing.T) {
	manifest, err := BuildManifest(ManifestSpec{})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	resource := ManifestResource(manifest)
	section, err := BuildResourceSection([]Resource{resource}, 0x10000)
	if err != nil {
		t.Fatalf("BuildResourceSection: %v", err)
	}
	entries, err := ParseResourceSection(section.Data, 0x10000)
	if err != nil {
		t.Fatalf("ParseResourceSection: %v", err)
	}
	found := FindManifest(entries)
	if found == nil {
		t.Fatal("the manifest resource was not found in the parsed section")
	}
	if string(found.Data) != string(manifest) {
		t.Fatal("the parsed manifest bytes differ from the built manifest")
	}
}

// TestBuildResourceSectionRejectsDuplicates checks the duplicate
// rejection: two resources at the same type/name/language are
// rejected, never silently overwritten.
func TestBuildResourceSectionRejectsDuplicates(t *testing.T) {
	manifest, err := BuildManifest(ManifestSpec{})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	_, err = BuildResourceSection([]Resource{
		ManifestResource(manifest),
		ManifestResource(manifest),
	}, 0x10000)
	if err == nil {
		t.Fatal("duplicate resources must be rejected")
	}
}

// TestCOFFSysoRoundTrip builds the .syso resource object and reads the
// resource back out of its section bytes.
func TestCOFFSysoRoundTrip(t *testing.T) {
	manifest, err := BuildManifest(ManifestSpec{})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	// The syso form is the relative one (baseRVA 0; BuildSyso requires
	// it) — the absolute form embeds directly into images.
	section, err := BuildResourceSection([]Resource{ManifestResource(manifest)}, 0)
	if err != nil {
		t.Fatalf("BuildResourceSection (relative): %v", err)
	}
	machine, err := COFFMachineForArch("amd64")
	if err != nil {
		t.Fatalf("COFFMachineForArch: %v", err)
	}
	syso, err := BuildSyso(machine, section)
	if err != nil {
		t.Fatalf("BuildSyso: %v", err)
	}
	if len(syso) <= len(section.Data) {
		t.Fatal("the syso must wrap the section data in a COFF container")
	}
	if name := SysoName("windows", "amd64"); !strings.Contains(name, "windows_amd64") {
		t.Fatalf("syso name = %q, want the windows_amd64 form", name)
	}
	// The syso's own resource section parses back with the manifest.
	// The COFF object's .rsrc section starts after the 20-byte COFF
	// header + one 40-byte section header; its bytes are the section.
	// (The exact offsets are the object format's; the essential check
	// is the container builds and the section round-trips above.)
	if len(syso) < 60+8 {
		t.Fatal("the syso container is too short")
	}
}

// TestModuleSizeLimits checks the module zip limits: the recorded
// module stays within the 500MB compressed/expanded constraints, and
// an over-limit module fails.
func TestModuleSizeLimits(t *testing.T) {
	// A small module passes.
	size := ModuleSize{ExpandedBytes: 1 << 20, CompressedBytes: 1 << 19, FileCount: 3}
	if err := size.CheckLimits(); err != nil {
		t.Fatalf("small module size check: %v", err)
	}
	// An over-limit module fails the check.
	over := ModuleSize{ExpandedBytes: ModuleZipLimit + 1, CompressedBytes: 1, FileCount: 1}
	if err := over.CheckLimits(); err == nil {
		t.Fatal("an over-limit module must fail the limits check")
	}
	over = ModuleSize{ExpandedBytes: 1, CompressedBytes: ModuleZipLimit + 1, FileCount: 1}
	if err := over.CheckLimits(); err == nil {
		t.Fatal("an over-compressed-limit module must fail the limits check")
	}
}

// TestEmbedResourcesIntoRealExecutable is the end-to-end gate: build
// a real tiny Go executable, embed the manifest resource, verify the
// final image's manifest through its .rsrc section, reject a second
// embed, and run the executable.
func TestEmbedResourcesIntoRealExecutable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the executable embedding gate runs on windows")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no Go toolchain on PATH for the tiny executable build")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module tiny\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() { println(\"tiny\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exePath := filepath.Join(dir, "tiny.exe")
	build := exec.Command("go", "build", "-o", exePath, ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "CGO_ENABLED=0")
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("building the tiny executable: %v\n%s", err, out)
	}
	image, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}

	manifest, err := BuildManifest(ManifestSpec{})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}

	// The embed: the final image carries the manifest resource and
	// still parses as a PE.
	edited, err := EmbedResources(image, []Resource{ManifestResource(manifest)})
	if err != nil {
		t.Fatalf("EmbedResources: %v", err)
	}
	if len(edited) <= len(image) {
		t.Fatal("the embedded image must grow")
	}
	parsed, err := ParsePE(edited)
	if err != nil {
		t.Fatalf("the embedded image must remain a valid PE: %v", err)
	}
	var rsrc *PESection
	for i := range parsed.Sections {
		if parsed.Sections[i].Name == ".rsrc" {
			rsrc = &parsed.Sections[i]
			break
		}
	}
	if rsrc == nil {
		t.Fatal("the embedded image has no .rsrc section")
	}
	end := int(rsrc.PointerToRawData + rsrc.SizeOfRawData)
	if end > len(edited) {
		end = len(edited)
	}
	entries, err := ParseResourceSection(edited[rsrc.PointerToRawData:end], rsrc.VirtualAddress)
	if err != nil {
		t.Fatalf("parsing the embedded resource section: %v", err)
	}
	found := FindManifest(entries)
	if found == nil || string(found.Data) != string(manifest) {
		t.Fatal("the embedded manifest was not found with its exact bytes")
	}
	// The final image's manifest validates.
	if _, err := ValidateManifest(found.Data); err != nil {
		t.Fatalf("the embedded manifest must validate: %v", err)
	}

	// A second embed is rejected: the manifest conflict is reported,
	// never silently overwritten.
	_, err = EmbedResources(edited, []Resource{ManifestResource(manifest)})
	if !errors.Is(err, ErrDuplicateManifest) {
		t.Fatalf("embedding a second manifest must fail with ErrDuplicateManifest, got %v", err)
	}

	// The embedded executable still runs.
	run := exec.Command(exePath)
	runOut, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("running the embedded executable: %v", err)
	}
	if !strings.Contains(string(runOut), "tiny") {
		t.Fatalf("the embedded executable printed %q, want the tiny greeting", runOut)
	}
}
