// Command measure records ticket02 native-artifact provenance and prepares
// the embedded Go artifacts.
//
// It reads the built cdylib, computes the exact identity measurements
// (SHA-256, gzip-9 size/hash, PE machine, full import table), determines the
// CRT outcome from what the PE imports actually show, and writes:
//
//   - the provenance record `reference/out/native-bootstrap.json`;
//   - the embedded manifest `internal/native/artifacts/manifest.json`;
//   - a copy of the DLL at `internal/native/artifacts/gpui_go_native.dll`.
//
// Maintainer tooling only: it is never compiled into consumer builds.
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	ceCommit     = "254b5dbd47cbb5acbcc5bbdcbb322a339276c88a"
	abiVersion   = 1
	targetTriple = "x86_64-pc-windows-msvc"

	// Ticket06: the native layout service landed in reserved slot 1 with
	// capability bit 1 and native revision 2. Ticket07: the native renderer
	// service landed in reserved slot 2 with capability bit 2 and native
	// revision 3. Ticket08: the native scene kernel service landed in
	// reserved slot 3 with capability bit 3 and native revision 4.
	// Ticket09: the text geometry service landed in reserved slot 4 with
	// capability bit 4 and native revision 5. Ticket10: the glyph raster
	// service (reserved slot 5, bit 5) and the texture atlas service
	// (reserved slot 6, bit 6) landed with native revision 6. Ticket16
	// (path primitives + the pinned PathBuilder tessellation in the
	// scene service, the scene drawing pipeline in the renderer service)
	// landed with native revision 7 and capability bit 7. Ticket17 (the
	// image codec service in reserved slot 7, the pinned image-crate
	// decode graph) landed with native revision 8 and capability bit 8.
	// Keep these in sync with reference/native/src/lib.rs
	// (GPUI_GO_NATIVE_REVISION and the capabilities module).
	nativeRevision = 8

	// bit 0 bootstrap, bit 1 layout-taffy-0-13-0, bit 2 renderer-d3d11,
	// bit 3 scene-kernel-v1, bit 4 text-parley-0-11-1, bit 5
	// glyph-raster-dwrite, bit 6 glyph-atlas-d3d11, bit 7
	// scene-draw-paths (ticket16), bit 8 image-codecs-image-0-25
	// (ticket17).
	capabilitiesMask = 511
)

// The pinned Taffy engine of the layout service (docs/layout-contract.md).
// The CE workspace lock pins checksum
// c034e05f6ee85a12daa63863c2245797715075c70649947aa0da54f3f2ab1d0f for
// 0.13.0; the resolved graph is recorded from the workspace lock below.
const taffyPinnedVersion = "0.13.0"

// The windows crate family of the renderer service (ticket07), mirroring
// the pinned gpui_windows workspace dependency (windows 0.62.2). The
// feature list is the minimal set reference/native/Cargo.toml enables.
const windowsPinnedVersion = "0.62.2"

// The pinned Parley text stack of the text service (ticket09). The
// windows platform contract pins Parley 0.11.1 / Fontique 0.11.1 /
// HarfRust 0.12.0 / Skrifa 0.44.0 / Swash 0.2.10 (the CE workspace lock
// resolution); gpui-ce and gpui_ce_parley are pinned source-path crates
// from the ce-source checkout, recorded with their local paths.
var textPinnedVersions = map[string]string{
	"parley":   "0.11.1",
	"fontique": "0.11.1",
	"harfrust": "0.12.0",
	"skrifa":   "0.44.0",
	"swash":    "0.2.10",
}

var textPathCrates = []string{"gpui-ce", "gpui_ce_parley"}

var windowsFeatures = []string{
	"Win32_Foundation",
	"Win32_Graphics_Direct3D",
	"Win32_Graphics_Direct3D11",
	"Win32_Graphics_DirectComposition",
	"Win32_Graphics_DirectWrite",
	"Win32_Graphics_Dxgi",
	"Win32_Graphics_Dxgi_Common",
	"Win32_System_LibraryLoader",
	"Win32_UI_WindowsAndMessaging",
}

// The pinned glyph/atlas stack of the raster and atlas services
// (ticket10): etagere is the pinned atlas packing algorithm
// (crates/gpui_windows/src/directx_atlas.rs; the renderer contract pins
// 0.2.15), windows-numerics supplies the Vector2 the rasterizer feeds
// into TranslateColorGlyphRun (the pin declares "0.3"), and anyhow is
// the pinned error type of the ported rasterizer.
var glyphPinnedVersions = map[string]string{
	"etagere":          "0.2.15",
	"windows-numerics": "0.3.1",
}

// taffyDefaultFeatures is taffy 0.13.0's default feature set (its Cargo.toml
// [features] default list), retained per the layout contract.
var taffyDefaultFeatures = []string{
	"std", "taffy_tree", "flexbox", "grid", "block_layout", "float_layout",
	"calc", "content_size", "detailed_layout_info",
}

type dllMeasure struct {
	Path       string `json:"path"`
	Bytes      int64  `json:"bytes"`
	SHA256     string `json:"sha256"`
	GzipBytes  int64  `json:"gzip_bytes"`
	GzipSHA256 string `json:"gzip_sha256"`
	GzipMethod string `json:"gzip_method"`
}

type peImport struct {
	DLL     string   `json:"dll"`
	Symbols []string `json:"symbols"`
}

type peMeasure struct {
	Machine         string     `json:"machine"`
	MachineCode     uint16     `json:"machine_code"`
	Characteristics uint16     `json:"characteristics"`
	Subsystem       string     `json:"subsystem"`
	Imports         []peImport `json:"imports"`
}

type provenance struct {
	Schema              string         `json:"schema"`
	BuiltAt             string         `json:"built_at"`
	Rustc               string         `json:"rustc"`
	Cargo               string         `json:"cargo"`
	Target              string         `json:"target"`
	CRTStaticRequested  bool           `json:"crt_static_requested"`
	CRTStaticOutcome    string         `json:"crt_static_outcome"`
	Rustflags           string         `json:"rustflags"`
	DLL                 dllMeasure     `json:"dll"`
	PE                  peMeasure      `json:"pe"`
	ABI                 abiMeasure     `json:"abi"`
	Taffy               taffyMeasure   `json:"taffy"`
	Windows             windowsMeasure `json:"windows"`
	Text                textMeasure    `json:"text"`
	Glyph               glyphMeasure   `json:"glyph"`
	Image               imageMeasure   `json:"image"`
	CargoLockSHA256     string         `json:"cargo_lock_sha256"`
	ReproducibilityNote string         `json:"reproducibility_note"`
}

type abiMeasure struct {
	ABIVersion       uint32   `json:"abi_version"`
	NativeRevision   uint32   `json:"native_revision"`
	CECommit         string   `json:"ce_commit"`
	CapabilitiesMask uint64   `json:"capabilities_mask"`
	Capabilities     []string `json:"capabilities"`
}

// taffyMeasure records the resolved Taffy engine of the layout service: the
// exact version, the crates.io checksum from the workspace lock, whether the
// =0.13.0 pin is satisfied, the default feature set retained by the build,
// and the effective feature/dependency graph cargo resolves for the native
// crate. The layout contract requires the resolved feature graph to be
// recorded, not just the lockfile.
type taffyMeasure struct {
	Version      string   `json:"version"`
	Checksum     string   `json:"checksum"`
	PinSatisfied bool     `json:"version_pin_satisfied"`
	Features     []string `json:"default_features"`
	FeatureGraph string   `json:"feature_graph"`
	Deps         []string `json:"dependencies"`
}

// windowsMeasure records the resolved windows crate family of the
// renderer service (ticket07): the exact version and crates.io checksum
// from the workspace lock, whether the 0.62.2 pin is satisfied, the
// minimal feature set the native crate enables, and the effective
// feature/dependency graph cargo resolves on the Windows target. The
// renderer contract requires the resolved graph and the feature manifest,
// not just the version label.
type windowsMeasure struct {
	Version      string   `json:"version"`
	Checksum     string   `json:"checksum"`
	PinSatisfied bool     `json:"version_pin_satisfied"`
	Features     []string `json:"features"`
	FeatureGraph string   `json:"feature_graph"`
	Deps         []string `json:"dependencies"`
}

// textMeasure records the pinned Parley text stack of the text service
// (ticket09): the exact versions and crates.io checksums of parley/
// fontique/harfrust/skrifa/swash from the workspace lock, whether the
// windows platform contract pins are satisfied, the resolved path-pin
// crates (gpui-ce/gpui_ce_parley from the ce-source checkout), and the
// effective feature/dependency graph cargo resolves for the native
// crate on the Windows target.
type textMeasure struct {
	Parley       registryMeasure `json:"parley"`
	Fontique     registryMeasure `json:"fontique"`
	Harfrust     registryMeasure `json:"harfrust"`
	Skrifa       registryMeasure `json:"skrifa"`
	Swash        registryMeasure `json:"swash"`
	GpuiCe       pathMeasure     `json:"gpui_ce"`
	GpuiCeParley pathMeasure     `json:"gpui_ce_parley"`
	FeatureGraph string          `json:"feature_graph"`
}

// registryMeasure is one registry crate of the text stack.
type registryMeasure struct {
	Version      string `json:"version"`
	Checksum     string `json:"checksum"`
	PinSatisfied bool   `json:"version_pin_satisfied"`
}

// pathMeasure is one pinned source-path crate of the text stack.
type pathMeasure struct {
	// Version is the pinned crate's declared version.
	Version string `json:"version"`
	// Path is the local ce-source checkout path (the pin).
	Path string `json:"path"`
	// Deps are the crate's resolved direct dependencies from cargo tree.
	Deps []string `json:"dependencies"`
}

// glyphMeasure records the resolved glyph/atlas stack of the raster and
// atlas services (ticket10): the exact versions and crates.io checksums
// of etagere and windows-numerics from the workspace lock, whether the
// renderer-contract pins are satisfied, and the resolved feature graph.
type glyphMeasure struct {
	Etagere         registryMeasure `json:"etagere"`
	WindowsNumerics registryMeasure `json:"windows_numerics"`
	Anyhow          registryMeasure `json:"anyhow"`
	FeatureGraph    string          `json:"feature_graph"`
}

// imageMeasure records the resolved image crate of the image codec
// service (ticket17): the CE workspace's `image = "0.25.1"`
// default-features resolution through the workspace lock, plus the
// feature subtree the native crate activates.
type imageMeasure struct {
	Image        registryMeasure `json:"image"`
	FeatureGraph string          `json:"feature_graph"`
}

type manifestOut struct {
	Schema             string         `json:"schema"`
	Name               string         `json:"name"`
	SHA256             string         `json:"sha256"`
	Bytes              int64          `json:"bytes"`
	GzipBytes          int64          `json:"gzip_bytes"`
	GzipSHA256         string         `json:"gzip_sha256"`
	CECommit           string         `json:"ce_commit"`
	ABIVersion         uint32         `json:"abi_version"`
	NativeRevision     uint32         `json:"native_revision"`
	CapabilitiesMask   uint64         `json:"capabilities_mask"`
	Capabilities       []string       `json:"capabilities"`
	Taffy              taffyMeasure   `json:"taffy"`
	Windows            windowsMeasure `json:"windows"`
	Text               textMeasure    `json:"text"`
	Glyph              glyphMeasure   `json:"glyph"`
	Image              imageMeasure   `json:"image"`
	Target             string         `json:"target"`
	Machine            string         `json:"machine"`
	BuiltAt            string         `json:"built_at"`
	Rustc              string         `json:"rustc"`
	CRTStaticRequested bool           `json:"crt_static_requested"`
	CRTStaticOutcome   string         `json:"crt_static_outcome"`
	Imports            []string       `json:"imports"`
	Provenance         string         `json:"provenance"`
}

func main() {
	dllPath := flag.String("dll", "", "path to the built gpui_go_native.dll")
	outPath := flag.String("out", "reference/out/native-bootstrap.json", "provenance JSON output path")
	manifestPath := flag.String("manifest", "internal/native/artifacts/manifest.json", "embedded manifest output path")
	artifactDir := flag.String("artifact-dir", "internal/native/artifacts", "directory receiving the DLL copy for embedding")
	rustflags := flag.String("rustflags", "-C target-feature=+crt-static", "RUSTFLAGS used for the build")
	lockPath := flag.String("lock", "reference/Cargo.lock", "workspace Cargo.lock to hash")
	flag.Parse()
	if *dllPath == "" {
		fmt.Fprintln(os.Stderr, "usage: measure -dll <path> [-out ...] [-manifest ...] [-artifact-dir ...]")
		os.Exit(2)
	}

	dllBytes, err := os.ReadFile(*dllPath)
	must(err, "reading DLL")

	// SHA-256 of the exact DLL bytes.
	sum := sha256.Sum256(dllBytes)
	shaHex := hex.EncodeToString(sum[:])

	// gzip-9 via the Go stdlib (deterministic within one toolchain; not
	// guaranteed byte-identical to GNU gzip).
	var gz bytes.Buffer
	zw, err := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	must(err, "gzip writer")
	_, err = zw.Write(dllBytes)
	must(err, "gzip write")
	must(zw.Close(), "gzip close")
	gzSum := sha256.Sum256(gz.Bytes())
	gzHex := hex.EncodeToString(gzSum[:])

	// PE facts.
	peM, err := measurePE(dllBytes)
	must(err, "PE parse")

	// CRT outcome from what the imports actually show: dynamic CRT manifests
	// as VCRUNTIME*.dll / ucrtbase / api-ms-win-crt-* imports.
	outcome := crtOutcome(peM)

	rustc := strings.TrimSpace(cmdOutput("rustc", "--version"))
	cargo := strings.TrimSpace(cmdOutput("cargo", "--version"))
	lockSum, lockErr := fileSHA256(*lockPath)
	lockHex := ""
	if lockErr == nil {
		lockHex = lockSum
	}

	// Resolved Taffy engine of the layout service, recorded from the
	// workspace lock and cargo tree (the layout contract requires the
	// resolved feature graph, not just the lockfile).
	taffy := measureTaffy(*lockPath)

	// Resolved windows crate family of the renderer service, recorded the
	// same way (the renderer contract requires the resolved feature graph
	// and the feature manifest).
	windows := measureWindows(*lockPath)

	// Resolved Parley text stack of the text service (ticket09): the
	// platform contract requires the lockfile versions and the resolved
	// feature graph.
	text := measureText(*lockPath)

	// Resolved glyph/atlas stack of the raster and atlas services
	// (ticket10): the renderer contract requires the etagere pin and the
	// resolved feature graph.
	glyph := measureGlyph(*lockPath)

	// The image codec service's resolved image crate (ticket17).
	image := measureImage(*lockPath)

	prov := provenance{
		Schema:             "gpui-go/native-bootstrap@2",
		BuiltAt:            time.Now().UTC().Format(time.RFC3339),
		Rustc:              rustc,
		Cargo:              cargo,
		Target:             targetTriple,
		CRTStaticRequested: true,
		CRTStaticOutcome:   outcome,
		Rustflags:          *rustflags,
		DLL: dllMeasure{
			Path:       filepath.ToSlash(*dllPath),
			Bytes:      int64(len(dllBytes)),
			SHA256:     shaHex,
			GzipBytes:  int64(gz.Len()),
			GzipSHA256: gzHex,
			GzipMethod: "Go stdlib compress/gzip, level 9 (BestCompression)",
		},
		PE:              *peM,
		ABI:             abiMeasure{ABIVersion: abiVersion, NativeRevision: nativeRevision, CECommit: ceCommit, CapabilitiesMask: capabilitiesMask, Capabilities: []string{"bootstrap-buffer-roundtrip", "layout-taffy-0-13-0", "renderer-d3d11", "scene-kernel-v1", "text-parley-0-11-1", "glyph-raster-dwrite", "glyph-atlas-d3d11", "scene-draw-paths", "image-codecs-image-0-25"}},
		Taffy:           taffy,
		Windows:         windows,
		Text:            text,
		Glyph:           glyph,
		Image:           image,
		CargoLockSHA256: lockHex,
		ReproducibilityNote: "Single build; byte reproducibility not claimed and not expected: the distribution " +
			"contract states \"Two builds have not yet demonstrated byte reproducibility\", and the MSVC link embeds a " +
			"timestamp, so rebuilding the identical source produces a new artifact identity (observed during this " +
			"ticket: two builds of the same source differed). Expected flow: rebuild -> new artifact identity -> " +
			"rerun the measure tool to refresh the manifest and embedded pair before any Go build.",
	}

	// Provenance JSON.
	provJSON, err := json.MarshalIndent(&prov, "", "  ")
	must(err, "marshal provenance")
	must(os.MkdirAll(filepath.Dir(*outPath), 0o755), "mkdir out")
	must(os.WriteFile(*outPath, append(provJSON, '\n'), 0o644), "writing provenance")

	// Embedded manifest.
	imports := make([]string, 0, len(peM.Imports))
	for _, imp := range peM.Imports {
		imports = append(imports, fmt.Sprintf("%s (%d imports)", imp.DLL, len(imp.Symbols)))
	}
	man := manifestOut{
		Schema:             "gpui-go/native-manifest@2",
		Name:               "gpui_go_native.dll",
		SHA256:             shaHex,
		Bytes:              int64(len(dllBytes)),
		GzipBytes:          int64(gz.Len()),
		GzipSHA256:         gzHex,
		CECommit:           ceCommit,
		ABIVersion:         abiVersion,
		NativeRevision:     nativeRevision,
		CapabilitiesMask:   capabilitiesMask,
		Capabilities:       []string{"bootstrap-buffer-roundtrip", "layout-taffy-0-13-0", "renderer-d3d11", "scene-kernel-v1", "text-parley-0-11-1", "glyph-raster-dwrite", "glyph-atlas-d3d11", "scene-draw-paths", "image-codecs-image-0-25"},
		Taffy:              taffy,
		Windows:            windows,
		Text:               text,
		Glyph:              glyph,
		Image:              image,
		Target:             targetTriple,
		Machine:            "amd64",
		BuiltAt:            prov.BuiltAt,
		Rustc:              rustc,
		CRTStaticRequested: true,
		CRTStaticOutcome:   outcome,
		Imports:            imports,
		Provenance:         filepath.ToSlash(*outPath),
	}
	manJSON, err := json.MarshalIndent(&man, "", "  ")
	must(err, "marshal manifest")
	must(os.MkdirAll(filepath.Dir(*manifestPath), 0o755), "mkdir artifacts")
	must(os.WriteFile(*manifestPath, append(manJSON, '\n'), 0o644), "writing manifest")
	must(os.WriteFile(filepath.Join(*artifactDir, "gpui_go_native.dll"), dllBytes, 0o644), "copying DLL")

	fmt.Printf("measured %s\n", *dllPath)
	fmt.Printf("  bytes:        %d\n", len(dllBytes))
	fmt.Printf("  sha256:       %s\n", shaHex)
	fmt.Printf("  gzip-9 bytes: %d (sha256 %s)\n", gz.Len(), gzHex)
	fmt.Printf("  machine:      %s\n", peM.Machine)
	fmt.Printf("  crt outcome:  %s\n", outcome)
	fmt.Printf("wrote %s, %s, %s\n", *outPath, *manifestPath, filepath.Join(*artifactDir, "gpui_go_native.dll"))
}

// measureTaffy reads the resolved taffy entry (version + checksum) from the
// workspace lock, records whether the =0.13.0 pin is satisfied, and captures
// the effective feature/dependency graph cargo resolves for the native
// crate on the Windows target.
// measureWindows reads the resolved windows crate entry (version +
// checksum) from the workspace lock, records whether the 0.62.2 pin is
// satisfied, and captures the effective feature/dependency graph cargo
// resolves for the native crate's windows dependency on the Windows
// target.
func measureWindows(lockPath string) windowsMeasure {
	m := windowsMeasure{
		Version:  "unresolved",
		Checksum: "",
		Features: windowsFeatures,
	}
	if lockBytes, err := os.ReadFile(lockPath); err == nil {
		lines := strings.Split(string(lockBytes), "\n")
		for i, line := range lines {
			if strings.TrimSpace(line) != "name = \"windows\"" {
				continue
			}
			// The workspace lock carries several windows crate versions
			// (other members); the renderer service resolves 0.62.2 — take
			// that entry, not the first.
			version := ""
			checksum := ""
			for _, follow := range lines[i:min(i+4, len(lines))] {
				follow = strings.TrimSpace(follow)
				if strings.HasPrefix(follow, "version = ") {
					version = strings.Trim(strings.TrimPrefix(follow, "version = "), "\"")
				}
				if strings.HasPrefix(follow, "checksum = ") {
					checksum = strings.Trim(strings.TrimPrefix(follow, "checksum = "), "\"")
				}
			}
			if version == windowsPinnedVersion || m.Version == "unresolved" {
				m.Version = version
				m.Checksum = checksum
			}
			if version == windowsPinnedVersion {
				break
			}
		}
	}
	m.PinSatisfied = m.Version == windowsPinnedVersion
	// cargo tree must run inside the reference workspace (where the native
	// crate and its lock live), whatever the tool's working directory is.
	workspaceDir := filepath.Dir(lockPath)
	m.FeatureGraph = cmdOutputIn(workspaceDir, "cargo", "tree", "-p", "gpui-go-native",
		"--target", targetTriple, "--edges", "features", "-i", "windows")
	// The dependency subtree windows activates for the native crate.
	m.Deps = windowsSubtree(m.FeatureGraph)
	return m
}

// windowsSubtree extracts the windows crate dependency lines from a cargo
// tree inverse feature-graph dump.
func windowsSubtree(tree string) []string {
	var deps []string
	for _, line := range strings.Split(tree, "\n") {
		trimmed := strings.TrimLeft(line, " |\u2500\u2502\u251c\u2514- ")
		if strings.HasPrefix(trimmed, "windows ") || strings.HasPrefix(trimmed, "windows-core ") ||
			strings.HasPrefix(trimmed, "windows-link ") || strings.HasPrefix(trimmed, "windows-strings ") ||
			strings.HasPrefix(trimmed, "windows-result ") || strings.HasPrefix(trimmed, "windows-numerics ") {
			deps = append(deps, strings.TrimSpace(trimmed))
		}
	}
	sort.Strings(deps)
	return deps
}

// measureText records the resolved Parley text stack of the text service
// (ticket09): per-crate registry versions/checksums from the workspace
// lock, the pinned path crates' declared versions and resolved direct
// dependencies, and the effective feature/dependency graph cargo
// resolves for the native crate on the Windows target.
func measureText(lockPath string) textMeasure {
	m := textMeasure{
		FeatureGraph: cmdOutputIn(filepath.Dir(lockPath), "cargo", "tree", "-p", "gpui-go-native",
			"--target", targetTriple, "--edges", "features"),
	}
	if lockBytes, err := os.ReadFile(lockPath); err == nil {
		lines := strings.Split(string(lockBytes), "\n")
		for crate := range textPinnedVersions {
			version, checksum := lockEntry(lines, crate)
			m.setRegistry(crate, registryMeasure{
				Version:      version,
				Checksum:     checksum,
				PinSatisfied: version == textPinnedVersions[crate],
			})
		}
	}
	m.GpuiCe = pathMeasure{
		Version: "0.2.2",
		Path:    "reference/ce-source/crates/gpui",
		Deps:    crateSubtree(m.FeatureGraph, "gpui-ce v"),
	}
	m.GpuiCeParley = pathMeasure{
		Version: "0.1.0",
		Path:    "reference/ce-source/crates/gpui_ce_parley",
		Deps:    crateSubtree(m.FeatureGraph, "gpui_ce_parley v"),
	}
	return m
}

// setRegistry assigns one registry crate slot of the text measure.
func (m *textMeasure) setRegistry(crate string, record registryMeasure) {
	switch crate {
	case "parley":
		m.Parley = record
	case "fontique":
		m.Fontique = record
	case "harfrust":
		m.Harfrust = record
	case "skrifa":
		m.Skrifa = record
	case "swash":
		m.Swash = record
	}
}

// measureGlyph records the resolved glyph/atlas stack of the raster and
// atlas services (ticket10): the lockfile versions and checksums of
// etagere/windows-numerics/anyhow and the effective feature graph cargo
// resolves for the native crate on the Windows target.
func measureGlyph(lockPath string) glyphMeasure {
	m := glyphMeasure{
		FeatureGraph: cmdOutputIn(filepath.Dir(lockPath), "cargo", "tree", "-p", "gpui-go-native",
			"--target", targetTriple, "--edges", "features"),
	}
	if lockBytes, err := os.ReadFile(lockPath); err == nil {
		lines := strings.Split(string(lockBytes), "\n")
		for crate := range glyphPinnedVersions {
			// The workspace lock carries multiple windows-numerics entries
			// (0.2.0 from other members, 0.3.x from the pin's gpui_windows);
			// take the entry matching the native crate's resolved version,
			// not the first.
			version, checksum := lockEntryPreferPin(lines, crate, glyphPinnedVersions[crate])
			record := registryMeasure{
				Version:      version,
				Checksum:     checksum,
				PinSatisfied: version == glyphPinnedVersions[crate],
			}
			switch crate {
			case "etagere":
				m.Etagere = record
			case "windows-numerics":
				m.WindowsNumerics = record
			}
		}
		version, checksum := lockEntry(lines, "anyhow")
		m.Anyhow = registryMeasure{Version: version, Checksum: checksum, PinSatisfied: version != ""}
	}
	return m
}

// measureImage records the image codec service's resolved image crate
// (ticket17): the CE workspace's `image = "0.25.1"` default-features
// resolution through the workspace lock, plus the feature subtree the
// native crate activates.
func measureImage(lockPath string) imageMeasure {
	m := imageMeasure{}
	if lockBytes, err := os.ReadFile(lockPath); err == nil {
		lines := strings.Split(string(lockBytes), "\n")
		version, checksum := lockEntry(lines, "image")
		m.Image = registryMeasure{
			Version:  version,
			Checksum: checksum,
			// The workspace declares "0.25.1"; the lock resolves a
			// 0.25.x compatible release — same major.minor family as
			// the pinned checkout builds with.
			PinSatisfied: strings.HasPrefix(version, "0.25."),
		}
	}
	m.FeatureGraph = cmdOutputIn(filepath.Dir(lockPath), "cargo", "tree", "-p", "gpui-go-native",
		"--target", targetTriple, "--edges", "features", "-i", "image")
	return m
}

// lockEntryPreferPin returns the version and checksum of the named
// package, preferring the entry matching the pinned version when several
// entries exist.
func lockEntryPreferPin(lines []string, name, pinned string) (version string, checksum string) {
	version, checksum = lockEntry(lines, name)
	for i, line := range lines {
		if strings.TrimSpace(line) != "name = \""+name+"\"" {
			continue
		}
		entryVersion, entryChecksum := "", ""
		for _, follow := range lines[i:min(i+4, len(lines))] {
			follow = strings.TrimSpace(follow)
			if strings.HasPrefix(follow, "version = ") {
				entryVersion = strings.Trim(strings.TrimPrefix(follow, "version = "), "\"")
			}
			if strings.HasPrefix(follow, "checksum = ") {
				entryChecksum = strings.Trim(strings.TrimPrefix(follow, "checksum = "), "\"")
			}
		}
		if entryVersion == pinned {
			return entryVersion, entryChecksum
		}
	}
	return version, checksum
}

// lockEntry returns the version and checksum of the named package from a
// Cargo.lock's lines (the first matching package entry).
func lockEntry(lines []string, name string) (version string, checksum string) {
	for i, line := range lines {
		if strings.TrimSpace(line) != "name = \""+name+"\"" {
			continue
		}
		for _, follow := range lines[i:min(i+4, len(lines))] {
			follow = strings.TrimSpace(follow)
			if strings.HasPrefix(follow, "version = ") {
				version = strings.Trim(strings.TrimPrefix(follow, "version = "), "\"")
			}
			if strings.HasPrefix(follow, "checksum = ") {
				checksum = strings.Trim(strings.TrimPrefix(follow, "checksum = "), "\"")
			}
		}
		break
	}
	return version, checksum
}

// crateSubtree extracts the dependency lines mentioning one crate from a
// cargo tree feature-graph dump.
func crateSubtree(tree string, prefix string) []string {
	var deps []string
	for _, line := range strings.Split(tree, "\n") {
		trimmed := strings.TrimLeft(line, " |─│├└- ")
		if strings.HasPrefix(trimmed, prefix) {
			deps = append(deps, strings.TrimSpace(trimmed))
		}
	}
	sort.Strings(deps)
	return deps
}

func measureTaffy(lockPath string) taffyMeasure {
	m := taffyMeasure{
		Version:  "unresolved",
		Checksum: "",
		Features: taffyDefaultFeatures,
	}
	if lockBytes, err := os.ReadFile(lockPath); err == nil {
		lines := strings.Split(string(lockBytes), "\n")
		for i, line := range lines {
			if strings.TrimSpace(line) != "name = \"taffy\"" {
				continue
			}
			for _, follow := range lines[i:min(i+4, len(lines))] {
				follow = strings.TrimSpace(follow)
				if strings.HasPrefix(follow, "version = ") {
					m.Version = strings.Trim(strings.TrimPrefix(follow, "version = "), "\"")
				}
				if strings.HasPrefix(follow, "checksum = ") {
					m.Checksum = strings.Trim(strings.TrimPrefix(follow, "checksum = "), "\"")
				}
			}
			break
		}
	}
	m.PinSatisfied = m.Version == taffyPinnedVersion
	// cargo tree must run inside the reference workspace (where the native
	// crate and its lock live), whatever the tool's working directory is.
	workspaceDir := filepath.Dir(lockPath)
	m.FeatureGraph = cmdOutputIn(workspaceDir, "cargo", "tree", "-p", "gpui-go-native",
		"--target", targetTriple, "--edges", "features")
	// The dependency subtree taffy activates for the native crate.
	m.Deps = taffySubtree(m.FeatureGraph)
	return m
}

// taffySubtree extracts the taffy dependency lines from a cargo tree
// feature-graph dump.
func taffySubtree(tree string) []string {
	var deps []string
	for _, line := range strings.Split(tree, "\n") {
		trimmed := strings.TrimLeft(line, " |\u2500\u2502\u251c\u2514- ")
		if strings.HasPrefix(trimmed, "taffy ") || strings.HasPrefix(trimmed, "slotmap ") ||
			strings.HasPrefix(trimmed, "smallvec ") || strings.HasPrefix(trimmed, "arrayvec ") {
			deps = append(deps, strings.TrimSpace(trimmed))
		}
	}
	sort.Strings(deps)
	return deps
}

func measurePE(b []byte) (*peMeasure, error) {
	f, err := pe.NewFile(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return nil, fmt.Errorf("measure: expected AMD64 image, got machine %#x", f.Machine)
	}
	m := &peMeasure{
		Machine:         "amd64",
		MachineCode:     f.Machine,
		Characteristics: f.Characteristics,
		Subsystem:       "windows-gui",
	}
	syms, err := f.ImportedSymbols()
	if err != nil {
		return nil, fmt.Errorf("measure: parsing imports: %w", err)
	}
	byDLL := map[string][]string{}
	for _, s := range syms {
		// debug/pe returns entries as "SymbolName:DLLName".
		i := strings.LastIndex(s, ":")
		if i < 0 {
			byDLL[s] = append(byDLL[s], "?")
			continue
		}
		dll, name := s[i+1:], s[:i]
		byDLL[dll] = append(byDLL[dll], name)
	}
	dlls := make([]string, 0, len(byDLL))
	for d := range byDLL {
		dlls = append(dlls, d)
	}
	sort.Strings(dlls)
	for _, d := range dlls {
		names := append([]string(nil), byDLL[d]...)
		sort.Strings(names)
		m.Imports = append(m.Imports, peImport{DLL: d, Symbols: names})
	}
	// Note: Go's debug/pe reads the standard import directory only; delay
	// imports are not parsed by the stdlib. This artifact has none expected.
	return m, nil
}

func crtOutcome(m *peMeasure) string {
	dynamic := []string{}
	for _, imp := range m.Imports {
		name := strings.ToLower(imp.DLL)
		if strings.HasPrefix(name, "vcruntime") || strings.HasPrefix(name, "msvcp") ||
			name == "ucrtbase.dll" || strings.HasPrefix(name, "api-ms-win-crt-") {
			dynamic = append(dynamic, imp.DLL)
		}
	}
	if len(dynamic) == 0 {
		return "static: no VCRUNTIME/MSVCP/UCRT/api-ms-win-crt imports present; only OS system components are imported"
	}
	return "dynamic: PE imports " + strings.Join(dynamic, ", ") + " (a separately installed VC++ redistributable would be required)"
}

func cmdOutput(name string, args ...string) string {
	return cmdOutputIn("", name, args...)
}

// cmdOutputIn runs a command in dir ("" = current directory) and returns its
// trimmed stdout, or an "unavailable" marker with the error.
func cmdOutputIn(dir string, name string, args ...string) string {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return fmt.Sprintf("unavailable: %v", err)
	}
	return string(out)
}

func fileSHA256(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}

func must(err error, what string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "measure: %s: %v\n", what, err)
		os.Exit(1)
	}
}
