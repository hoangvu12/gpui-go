// Command gpuiresource is the ticket31 pure-Go consumer resource command:
// it produces verified PerMonitorV2 executable resources with no resource
// compiler, no Rust/C toolchain and no network, using only the Go standard
// library (the mechanism the distribution contract pins through the
// go-winres approach, reimplemented in internal/consumerspec).
//
// Subcommands:
//
//	gpuiresource syso        write the consumer main package's
//	                          architecture-specific rsrc_windows_<arch>.syso
//	                          that ordinary `go build` links in
//	gpuiresource embed       embed the manifest resource into an already
//	                          built .exe by pure-Go PE editing (duplicate
//	                          manifests are rejected, never overwritten)
//	gpuiresource inspect     read and verify the manifest of a final
//	                          executable (the "inspect the final
//	                          executable's manifest" gate)
//	gpuiresource verify-imports
//	                          verify a PE image's normal/delay/dynamic
//	                          imports and the actual static-CRT outcome
//	gpuiresource verify-artifact
//	                          verify the module's shipped artifact pair
//	                          against the pinned evidence identity and the
//	                          module zip size limits
//
// Exit codes: 0 success, 1 verification failure or manifest conflict, 2
// usage error.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gpui-go/internal/consumerspec"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var code int
	switch os.Args[1] {
	case "syso":
		code = runSyso(os.Args[2:])
	case "embed":
		code = runEmbed(os.Args[2:])
	case "inspect":
		code = runInspect(os.Args[2:])
	case "verify-imports":
		code = runVerifyImports(os.Args[2:])
	case "verify-artifact":
		code = runVerifyArtifact(os.Args[2:])
	case "help", "-h", "-help", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "gpuiresource: unknown subcommand %q\n\n", os.Args[1])
		usage()
		code = 2
	}
	os.Exit(code)
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: gpuiresource <subcommand> [flags]

subcommands:
  syso [-dir DIR] [-out FILE] [-arch amd64|386]
       [-name NAME] [-version V] [-description TEXT] [-level asInvoker|highestAvailable|requireAdministrator]
       [-manifest FILE]
       Write rsrc_windows_<arch>.syso (default: DIR/rsrc_windows_<runtime arch>.syso,
       DIR defaults to the current directory). A -manifest file is preserved
       verbatim after its PerMonitorV2 declaration validates; without one the
       command generates the manifest from the customization flags.

  embed -exe FILE [-o OUTPUT] [-manifest FILE] [same customization flags]
       Embed the RT_MANIFEST resource into a built executable with pure-Go
       PE editing. An executable that already carries a manifest resource
       fails (reported conflict, never overwritten). Default output: the
       input path.

  inspect -exe FILE
       Read the manifest resource out of a final executable and verify its
       PerMonitorV2 DPI declaration.

  verify-imports [-pe FILE | -module]
       Parse the PE import tables (normal, delay, dynamic LoadLibrary
       entry points) and report the actual static-CRT outcome.
       -module verifies the module's shipped native DLL.

  verify-artifact [-module | -dll FILE -manifest FILE]
       Check the artifact pair's hash/size self-consistency, the pinned
       evidence identity, the manifest/DLL import-set agreement and the
       module zip size limits.
`)
}

// manifestFlags carries the manifest customization flags shared by syso and
// embed; they are explicit application resource settings the command
// preserves verbatim.
type manifestFlags struct {
	fs *flag.FlagSet

	manifest    string
	name        string
	version     string
	description string
	level       string
}

func newManifestFlags(fs *flag.FlagSet) *manifestFlags {
	m := &manifestFlags{fs: fs}
	fs.StringVar(&m.manifest, "manifest", "", "custom manifest XML file (preserved verbatim; its DPI declaration must be PerMonitorV2-first)")
	fs.StringVar(&m.name, "name", "", "assemblyIdentity name (default "+consumerspec.DefaultManifestName+")")
	fs.StringVar(&m.version, "version", "", "assemblyIdentity version (default "+consumerspec.DefaultManifestVersion+")")
	fs.StringVar(&m.description, "description", "", "manifest description")
	fs.StringVar(&m.level, "level", "", "requestedExecutionLevel: asInvoker, highestAvailable or requireAdministrator")
	return m
}

// buildManifest returns the manifest bytes: a validated custom manifest
// (content preserved) or the generated template carrying the flags.
func (m *manifestFlags) buildManifest() ([]byte, error) {
	if m.manifest != "" {
		data, err := os.ReadFile(m.manifest)
		if err != nil {
			return nil, fmt.Errorf("reading -manifest %s: %w", m.manifest, err)
		}
		dpi, err := consumerspec.ValidateManifest(data)
		if err != nil {
			return nil, fmt.Errorf("manifest %s rejected (customizations are preserved, conflicts are reported): %w", m.manifest, err)
		}
		fmt.Printf("custom manifest %s validated: dpiAwareness %q (PerMonitorV2 first)\n",
			m.manifest, strings.Join(dpi.Values, ", "))
		return data, nil
	}
	return consumerspec.BuildManifest(consumerspec.ManifestSpec{
		Name:           m.name,
		Version:        m.version,
		Description:    m.description,
		ExecutionLevel: m.level,
	})
}

// runSyso writes the architecture-specific .syso for go build.
func runSyso(args []string) int {
	fs := flag.NewFlagSet("syso", flag.ExitOnError)
	dir := fs.String("dir", ".", "consumer main package directory receiving the .syso")
	out := fs.String("out", "", "explicit output path (overrides -dir and the generated name)")
	arch := fs.String("arch", runtime.GOARCH, "consumer GOARCH (amd64, 386)")
	mf := newManifestFlags(fs)
	fs.Parse(args)
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "gpuiresource syso: unexpected positional arguments")
		return 2
	}

	manifest, err := mf.buildManifest()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gpuiresource:", err)
		return 1
	}
	// Verify the produced manifest round-trips through validation before it
	// is shipped into an executable.
	if _, err := consumerspec.ValidateManifest(manifest); err != nil {
		fmt.Fprintln(os.Stderr, "gpuiresource: generated manifest failed validation:", err)
		return 1
	}

	machine, err := consumerspec.COFFMachineForArch(*arch)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gpuiresource:", err)
		return 1
	}
	section, err := consumerspec.BuildResourceSection([]consumerspec.Resource{consumerspec.ManifestResource(manifest)}, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gpuiresource:", err)
		return 1
	}
	syso, err := consumerspec.BuildSyso(machine, section)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gpuiresource:", err)
		return 1
	}

	target := *out
	if target == "" {
		target = filepath.Join(*dir, consumerspec.SysoName("windows", *arch))
	}
	if err := os.WriteFile(target, syso, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "gpuiresource: writing", target, ":", err)
		return 1
	}
	fmt.Printf("wrote %s (%d bytes): COFF machine %#x, .rsrc section %d bytes, %d ADDR32 relocations, RT_MANIFEST id %d lang %#x\n",
		target, len(syso), machine, len(section.Data), len(section.RelocationOffsets),
		consumerspec.ManifestResourceID, consumerspec.ManifestLanguage)
	fmt.Printf("manifest: %d bytes, PerMonitorV2 declared first\n", len(manifest))
	fmt.Println("ordinary `go build` (CGO_ENABLED=0) links the .syso; no resource compiler is needed")
	return 0
}

// runEmbed embeds the manifest into an existing executable.
func runEmbed(args []string) int {
	fs := flag.NewFlagSet("embed", flag.ExitOnError)
	exe := fs.String("exe", "", "executable to embed into (required)")
	output := fs.String("o", "", "output path (default: the -exe input path)")
	mf := newManifestFlags(fs)
	fs.Parse(args)
	if *exe == "" {
		fmt.Fprintln(os.Stderr, "gpuiresource embed: -exe is required")
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "gpuiresource embed: unexpected positional arguments")
		return 2
	}

	manifest, err := mf.buildManifest()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gpuiresource:", err)
		return 1
	}

	image, err := os.ReadFile(*exe)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gpuiresource: reading", *exe, ":", err)
		return 1
	}
	before := len(image)
	edited, err := consumerspec.EmbedResources(image, []consumerspec.Resource{consumerspec.ManifestResource(manifest)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "gpuiresource: embedding into", *exe, ":", err)
		if errors.Is(err, consumerspec.ErrDuplicateManifest) {
			fmt.Fprintln(os.Stderr, "gpuiresource: the executable already carries a manifest resource; the conflict is reported, nothing was overwritten")
		}
		return 1
	}

	target := *output
	if target == "" {
		target = *exe
	}
	if err := os.WriteFile(target, edited, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "gpuiresource: writing", target, ":", err)
		return 1
	}
	fmt.Printf("embedded RT_MANIFEST into %s (%d -> %d bytes)\n", target, before, len(edited))

	// Immediately verify the final executable's own manifest: the
	// distribution contract gates on the final image, never the input XML.
	code := inspectPath(target)
	if code != 0 {
		return code
	}
	fmt.Println("embed: PASS")
	return 0
}

// runInspect prints and verifies the manifest of a final executable.
func runInspect(args []string) int {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	exe := fs.String("exe", "", "executable to inspect (required)")
	fs.Parse(args)
	if *exe == "" {
		fmt.Fprintln(os.Stderr, "gpuiresource inspect: -exe is required")
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "gpuiresource inspect: unexpected positional arguments")
		return 2
	}
	return inspectPath(*exe)
}

// inspectPath reads and validates one executable's manifest resource.
func inspectPath(path string) int {
	image, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gpuiresource: reading", path, ":", err)
		return 1
	}
	f, err := consumerspec.ParsePE(image)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gpuiresource: parsing", path, ":", err)
		return 1
	}
	fmt.Printf("machine: %s, sections: %d, size: %d bytes\n", f.MachineName(), f.NumberOfSection, len(image))

	entry, err := consumerspec.InspectManifest(image)
	if err != nil {
		fmt.Println("manifest: none —", err)
		return 1
	}
	fmt.Printf("RT_MANIFEST: id %d, language %#x, %d bytes\n", entry.Name, entry.Language, len(entry.Data))

	dpi, err := consumerspec.ValidateManifest(entry.Data)
	if err != nil {
		fmt.Println("manifest validation: FAIL —", err)
		return 1
	}
	fmt.Printf("dpiAwareness: %q\n", strings.Join(dpi.Values, ", "))
	if !dpi.PerMonitorV2First() {
		fmt.Println("manifest validation: FAIL — PerMonitorV2 is not the first declared awareness")
		return 1
	}
	fmt.Println("manifest validation: PASS (PerMonitorV2 first)")
	fmt.Println(string(entry.Data))
	return 0
}

// runVerifyImports verifies a PE image's import tables.
func runVerifyImports(args []string) int {
	fs := flag.NewFlagSet("verify-imports", flag.ExitOnError)
	pe := fs.String("pe", "", "PE image (DLL or exe) to verify")
	module := fs.Bool("module", false, "verify the module's shipped native artifact")
	fs.Parse(args)
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "gpuiresource verify-imports: unexpected positional arguments")
		return 2
	}

	var image []byte
	var label string
	switch {
	case *module:
		dll, _, err := consumerspec.FindNativeArtifact()
		if err != nil {
			fmt.Fprintln(os.Stderr, "gpuiresource:", err)
			return 1
		}
		image, err = os.ReadFile(dll)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gpuiresource: reading", dll, ":", err)
			return 1
		}
		label = dll
	case *pe != "":
		data, err := os.ReadFile(*pe)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gpuiresource: reading", *pe, ":", err)
			return 1
		}
		image, label = data, *pe
	default:
		fmt.Fprintln(os.Stderr, "gpuiresource verify-imports: pass -pe FILE or -module")
		return 2
	}

	report, err := consumerspec.ParseImports(image)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gpuiresource: parsing imports of", label, ":", err)
		return 1
	}
	printImportReport(label, report)

	ok := true
	if !report.StaticCRT() {
		fmt.Println("static CRT: FAIL — CRT redistributables found:", strings.Join(append(report.CRTDLLs, report.DynamicCRTStrings...), ", "))
		ok = false
	} else {
		fmt.Println("static CRT: PASS (conclusion from the parsed import tables, not from build flags)")
	}
	if !ok {
		return 1
	}
	fmt.Println("verify-imports: PASS")
	return 0
}

// printImportReport prints the three import classes.
func printImportReport(label string, r *consumerspec.ImportReport) {
	fmt.Printf("imports of %s\n", label)
	fmt.Printf("  machine: %s\n", r.Machine)
	printDLLs := func(title string, dlls []consumerspec.ImportDLL) {
		if len(dlls) == 0 {
			fmt.Printf("  %s: none\n", title)
			return
		}
		fmt.Printf("  %s (%d):\n", title, len(dlls))
		for _, d := range dlls {
			if d.Delay {
				fmt.Printf("    %s [delay-load]", d.Name)
			} else {
				fmt.Printf("    %s", d.Name)
			}
			fmt.Printf(" — %d by-name, %d by-ordinal imports\n", len(d.Functions), len(d.Ordinals))
		}
	}
	printDLLs("normal imports", r.NormalDLLs)
	printDLLs("delay imports", r.DelayDLLs)
	if len(r.LoadLibraryAPIs) == 0 {
		fmt.Println("  dynamic loads: no LoadLibrary-family imports")
	} else {
		sort.Strings(r.LoadLibraryAPIs)
		fmt.Printf("  dynamic loads: %s (imported; runtime-resolved modules are not enumerable statically)\n",
			strings.Join(r.LoadLibraryAPIs, ", "))
	}
}

// runVerifyArtifact verifies the module's artifact pair and size limits.
func runVerifyArtifact(args []string) int {
	fs := flag.NewFlagSet("verify-artifact", flag.ExitOnError)
	module := fs.Bool("module", true, "verify the module's shipped artifact pair (default)")
	dll := fs.String("dll", "", "explicit DLL path")
	manifest := fs.String("manifest", "", "explicit manifest path (with -dll)")
	fs.Parse(args)
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "gpuiresource verify-artifact: unexpected positional arguments")
		return 2
	}

	dllPath, manifestPath := *dll, *manifest
	if *module || dllPath == "" {
		found, foundManifest, err := consumerspec.FindNativeArtifact()
		if err != nil {
			fmt.Fprintln(os.Stderr, "gpuiresource:", err)
			return 1
		}
		dllPath, manifestPath = found, foundManifest
	}
	dllBytes, err := os.ReadFile(dllPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gpuiresource: reading", dllPath, ":", err)
		return 1
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gpuiresource: reading", manifestPath, ":", err)
		return 1
	}

	id, err := consumerspec.VerifyArtifactPair(dllBytes, manifestBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gpuiresource:", err)
		return 1
	}
	fmt.Printf("artifact: %s\n", dllPath)
	fmt.Printf("  dll sha256:   %s (%d bytes)\n", id.DLLSHA256, id.DLLBytes)
	fmt.Printf("  manifest:     sha256 %s, %d bytes, native revision %d\n", id.ManifestSHA256, id.ManifestBytes, id.ManifestRevision)
	fmt.Printf("  ce pin:       %s\n", id.ManifestCECommit)
	fmt.Printf("  crt outcome:  %s\n", id.ManifestCRT)
	ok := true
	if !id.MatchesEvidence {
		fmt.Println("  evidence identity: FAIL")
		for _, d := range id.Differences {
			fmt.Println("    -", d)
		}
		ok = false
	} else {
		fmt.Println("  evidence identity: PASS (exact artifact/manifest hash equality with the pinned evidence values)")
	}

	// Cross-check the manifest's recorded import list against the actual
	// import tables of the DLL.
	importReport, err := consumerspec.ParseImports(dllBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gpuiresource: parsing imports:", err)
		return 1
	}
	actual := map[string]bool{}
	for _, d := range importReport.NormalDLLs {
		actual[strings.ToLower(d.Name)] = true
	}
	for _, d := range importReport.DelayDLLs {
		actual[strings.ToLower(d.Name)] = true
	}
	declared := map[string]bool{}
	var declaredList []string
	for _, imp := range id.ManifestImports {
		name := strings.ToLower(strings.TrimSpace(imp))
		if idx := strings.IndexByte(name, ' '); idx > 0 {
			name = name[:idx] // entries read "KERNEL32.dll (31 imports)"
		}
		declared[name] = true
		declaredList = append(declaredList, name)
	}
	missing := difference(declared, actual)
	extra := difference(actual, declared)
	if len(missing) == 0 && len(extra) == 0 {
		fmt.Printf("  import agreement: PASS (manifest declares %d imports; the parsed tables agree)\n", len(declaredList))
	} else {
		fmt.Println("  import agreement: FAIL")
		for _, m := range missing {
			fmt.Println("    - manifest declares", m, "but the import tables do not")
		}
		for _, e := range extra {
			fmt.Println("    - import tables carry", e, "but the manifest does not")
		}
		ok = false
	}
	if !importReport.StaticCRT() {
		fmt.Println("  static CRT: FAIL —", strings.Join(append(importReport.CRTDLLs, importReport.DynamicCRTStrings...), ", "))
		ok = false
	} else {
		fmt.Println("  static CRT: PASS (verified from the import tables)")
	}

	// Evidence-record linkage: at least one evidence JSON mentions the hash.
	if mentioned, where, err := consumerspec.EvidenceRecordMentionsSHA(consumerspec.EvidenceArtifactSHA256); err == nil {
		if mentioned {
			fmt.Printf("  evidence record: PASS (%s mentions the pinned sha256)\n", filepath.ToSlash(filepath.Base(where)))
		} else {
			fmt.Println("  evidence record: FAIL — no evidence JSON mentions the pinned sha256")
			ok = false
		}
	}

	// Module zip size limits.
	root := consumerspec.EvidenceRecordsDir()
	if root != "" {
		root = filepath.Clean(filepath.Join(root, ".."))
		if size, err := consumerspec.MeasureModuleSize(root); err == nil {
			fmt.Printf("  module size:   %d files, %d bytes expanded, %d bytes compressed (zip method 8, level 9)\n",
				size.FileCount, size.ExpandedBytes, size.CompressedBytes)
			fmt.Printf("                 exclusions: %s\n", strings.Join(size.ExcludedDirs, ", "))
			if err := size.CheckLimits(); err != nil {
				fmt.Println("  module size: FAIL —", err)
				ok = false
			} else {
				fmt.Printf("  module size:   PASS (both below the %d MiB module zip limit)\n", consumerspec.ModuleZipLimit/(1024*1024))
			}
		}
	}

	if !ok {
		fmt.Println("verify-artifact: FAIL")
		return 1
	}
	fmt.Println("verify-artifact: PASS")
	return 0
}

// difference returns the keys of a that are absent from b, sorted.
func difference(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
