package ledger

import (
	"fmt"
	"strings"
)

// The cfg evaluator parses Rust cfg expressions of the shapes produced by the
// api-scan, e.g.
//
//	feature = "test-support"
//	any (test , feature = "test-support" , feature = "bench-support")
//	all (target_os = "macos" , any (test , feature = "test-support"))
//	not (feature = "x11")
//	target_os = "windows"
//	test
//
// Evaluation fixes the compile target of the Windows reference profiles:
// target_os "windows" and target_family "windows" are true, every other
// target_os/target_family value is false; the test, bench, doc and
// debug_assertions flags are false (the recorded reference profiles are
// release graphs; debug/inspector/test behavior is recorded separately per
// docs/conformance-inventory.md); unix-style flags are false. Anything the
// evaluator cannot parse or does not know is an error: callers treat
// evaluation failure conservatively (symbol present everywhere) and record a
// note, and the test suite asserts zero failures against the real artifact.

// The fixed evaluation target of the recorded reference profiles.
const (
	cfgTargetOS     = "windows"
	cfgTargetFamily = "windows"
	cfgTargetArch   = "x86_64"
)

// EvaluateCfg evaluates one cfg expression under the given active feature
// set. features maps feature name to enabled; the feature name "default" is
// not a cfg-testable feature (cargo features are opt-in) and evaluates to
// true, with the caller recording a note.
func EvaluateCfg(expr string, features map[string]bool) (bool, error) {
	toks, err := tokenizeCfg(expr)
	if err != nil {
		return false, err
	}
	if len(toks) == 0 {
		return false, fmt.Errorf("empty cfg expression")
	}
	p := &cfgParser{toks: toks, features: features}
	v, err := p.expr()
	if err != nil {
		return false, err
	}
	if p.pos != len(p.toks) {
		return false, fmt.Errorf("unexpected trailing cfg token %q", p.toks[p.pos].text)
	}
	return v, nil
}

// cfgToken is one lexical token of a cfg expression.
type cfgToken struct {
	kind string // "ident", "string", "punct"
	text string
}

// tokenizeCfg splits a cfg expression into identifiers (including hyphens and
// underscores, as feature names carry them), double-quoted strings, and the
// punctuation "(", ")", "," and "=". Whitespace is skipped, including the
// space the scan writes between a predicate head and its parenthesis
// ("any (test , ...)").
func tokenizeCfg(s string) ([]cfgToken, error) {
	var toks []cfgToken
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			i++
		case r == '(' || r == ')' || r == ',' || r == '=':
			toks = append(toks, cfgToken{kind: "punct", text: string(r)})
			i++
		case r == '"':
			j := i + 1
			var sb strings.Builder
			for j < len(runes) && runes[j] != '"' {
				sb.WriteRune(runes[j])
				j++
			}
			if j >= len(runes) {
				return nil, fmt.Errorf("unterminated string in cfg %q", s)
			}
			toks = append(toks, cfgToken{kind: "string", text: sb.String()})
			i = j + 1
		case isCfgIdentRune(r):
			j := i
			var sb strings.Builder
			for j < len(runes) && isCfgIdentRune(runes[j]) {
				sb.WriteRune(runes[j])
				j++
			}
			toks = append(toks, cfgToken{kind: "ident", text: sb.String()})
			i = j
		default:
			return nil, fmt.Errorf("unexpected rune %q in cfg %q", string(r), s)
		}
	}
	return toks, nil
}

func isCfgIdentRune(r rune) bool {
	return r == '_' || r == '-' ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// cfgParser is a recursive-descent parser over the cfg token stream.
type cfgParser struct {
	toks     []cfgToken
	pos      int
	features map[string]bool
}

func (p *cfgParser) peek() (cfgToken, bool) {
	if p.pos >= len(p.toks) {
		return cfgToken{}, false
	}
	return p.toks[p.pos], true
}

func (p *cfgParser) next() (cfgToken, error) {
	if p.pos >= len(p.toks) {
		return cfgToken{}, fmt.Errorf("unexpected end of cfg expression")
	}
	t := p.toks[p.pos]
	p.pos++
	return t, nil
}

func (p *cfgParser) expectPunct(text string) error {
	t, err := p.next()
	if err != nil {
		return err
	}
	if t.kind != "punct" || t.text != text {
		return fmt.Errorf("expected %q in cfg, got %q", text, t.text)
	}
	return nil
}

// expr parses one expression: any/all/not forms, name = "value" predicates or
// bare flags.
func (p *cfgParser) expr() (bool, error) {
	t, err := p.next()
	if err != nil {
		return false, err
	}
	if t.kind != "ident" {
		return false, fmt.Errorf("expected identifier in cfg, got %q", t.text)
	}
	switch t.text {
	case "any", "all":
		if err := p.expectPunct("("); err != nil {
			return false, err
		}
		result := t.text == "all" // neutral element: any → false, all → true
		n := 0
		for {
			v, err := p.expr()
			if err != nil {
				return false, err
			}
			n++
			if t.text == "any" {
				result = result || v
			} else {
				result = result && v
			}
			sep, ok := p.peek()
			if !ok {
				return false, fmt.Errorf("unterminated %s(...) in cfg", t.text)
			}
			switch {
			case sep.kind == "punct" && sep.text == ",":
				p.pos++
				continue
			case sep.kind == "punct" && sep.text == ")":
				p.pos++
			default:
				return false, fmt.Errorf("expected ',' or ')' in %s(...), got %q", t.text, sep.text)
			}
			break
		}
		if n == 0 {
			return false, fmt.Errorf("empty %s(...) in cfg", t.text)
		}
		return result, nil
	case "not":
		if err := p.expectPunct("("); err != nil {
			return false, err
		}
		v, err := p.expr()
		if err != nil {
			return false, err
		}
		if err := p.expectPunct(")"); err != nil {
			return false, err
		}
		return !v, nil
	default:
		if sep, ok := p.peek(); ok && sep.kind == "punct" && sep.text == "=" {
			p.pos++
			val, err := p.next()
			if err != nil {
				return false, err
			}
			if val.kind != "string" {
				return false, fmt.Errorf("expected string after %q =, got %q", t.text, val.text)
			}
			return evalCfgPredicate(t.text, val.text, p.features)
		}
		return evalCfgFlag(t.text)
	}
}

func evalCfgPredicate(name, value string, features map[string]bool) (bool, error) {
	switch name {
	case "feature":
		if value == "default" {
			// "default" is not a cfg-testable feature name; treated as
			// always true with a caller-visible note.
			return true, nil
		}
		return features[value], nil
	case "target_os":
		return value == cfgTargetOS, nil
	case "target_family":
		return value == cfgTargetFamily, nil
	case "target_arch":
		return value == cfgTargetArch, nil
	case "target_env":
		return value == "msvc", nil
	case "target_pointer_width":
		return value == "64", nil
	default:
		return false, fmt.Errorf("unknown cfg predicate %q", name)
	}
}

func evalCfgFlag(name string) (bool, error) {
	switch name {
	case "test", "bench", "doc", "debug_assertions":
		// Release reference profiles; debug/test behavior is a separately
		// recorded profile.
		return false, nil
	case "unix", "macos", "linux", "freebsd", "android", "ios", "wasm":
		return false, nil
	case "windows":
		return true, nil
	default:
		return false, fmt.Errorf("unknown cfg flag %q", name)
	}
}

// CfgFeatureNames extracts the feature names compared by feature = "..."
// predicates anywhere in the expression. It is lenient: on a parse failure it
// falls back to a plain scan, so owner rules never depend on full
// evaluability.
func CfgFeatureNames(expr string) []string {
	var names []string
	if toks, err := tokenizeCfg(expr); err == nil {
		for i := 0; i+2 < len(toks); i++ {
			if toks[i].kind == "ident" && toks[i].text == "feature" &&
				toks[i+1].kind == "punct" && toks[i+1].text == "=" &&
				toks[i+2].kind == "string" {
				names = append(names, toks[i+2].text)
				i += 2
			}
		}
	}
	if fb := cfgFeatureNamesFallback(expr); len(fb) > len(names) {
		return fb
	}
	return names
}

func cfgFeatureNamesFallback(expr string) []string {
	var names []string
	rest := expr
	for {
		i := strings.Index(rest, "feature")
		if i < 0 {
			break
		}
		tail := strings.TrimLeft(rest[i+len("feature"):], " \t")
		if strings.HasPrefix(tail, "=") {
			tail = strings.TrimLeft(tail[1:], " \t")
			if strings.HasPrefix(tail, `"`) {
				if j := strings.Index(tail[1:], `"`); j >= 0 {
					names = append(names, tail[1:1+j])
					rest = tail[1+j:]
					continue
				}
			}
		}
		rest = rest[i+len("feature"):]
	}
	return names
}

// CfgHasBareToken reports whether the expression contains the bare cfg flag
// token (e.g. "test" as a flag, never inside a quoted string). Lenient with a
// fallback on parse failure.
func CfgHasBareToken(expr, token string) bool {
	if toks, err := tokenizeCfg(expr); err == nil {
		for _, t := range toks {
			if t.kind == "ident" && t.text == token {
				return true
			}
		}
		return false
	}
	for _, seg := range splitOutsideStrings(expr) {
		for _, w := range strings.FieldsFunc(seg, func(r rune) bool {
			return !isCfgIdentRune(r)
		}) {
			if w == token {
				return true
			}
		}
	}
	return false
}

func splitOutsideStrings(expr string) []string {
	parts := strings.Split(expr, `"`)
	out := make([]string, 0, len(parts))
	for i, p := range parts {
		if i%2 == 0 {
			out = append(out, p)
		}
	}
	return out
}

// The five recorded conformance profiles. Each profile's CargoFeatures is the
// effective feature set of its resolved cargo graph; the cfg evaluator tests
// rows against exactly that set. The profile ids are stable and double as the
// ledger's Profile values.

// Profile id constants in canonical order.
const (
	ProfileNativeDefault           = "native-default"
	ProfileTestSupport             = "test-support"
	ProfileInspectorCapture        = "inspector-capture"
	ProfileCustomGPUWgpu           = "custom-gpu-wgpu"
	ProfileHotpatchProfilerStacker = "hotpatch-profiler-stacker"
)

// DefaultProfileOrder is the canonical profile order used for deterministic
// Profile slices and report tables.
var DefaultProfileOrder = []string{
	ProfileNativeDefault,
	ProfileTestSupport,
	ProfileInspectorCapture,
	ProfileCustomGPUWgpu,
	ProfileHotpatchProfilerStacker,
}

// GraphFileBase maps profile id to the cargo tree artifact basename under the
// profiles directory.
var graphFileBase = map[string]string{
	ProfileNativeDefault:           "native-default-windows.txt",
	ProfileTestSupport:             "test-support-windows.txt",
	ProfileInspectorCapture:        "inspector-capture-windows.txt",
	ProfileCustomGPUWgpu:           "custom-gpu-wgpu.txt",
	ProfileHotpatchProfilerStacker: "hotpatch-profiler-stacker.txt",
}

// profileFeatureSets are the effective feature sets of the recorded graphs.
var profileFeatureSets = map[string][]string{
	ProfileNativeDefault: {"default", "wayland", "x11", "windows-manifest"},
	ProfileTestSupport: {"default", "wayland", "x11", "windows-manifest",
		"test-support", "leak-detection"},
	ProfileInspectorCapture: {"default", "wayland", "x11", "windows-manifest",
		"inspector", "screen-capture"},
	ProfileCustomGPUWgpu: {"wgpu-surfaces", "custom-gpu"},
	ProfileHotpatchProfilerStacker: {"default", "wayland", "x11", "windows-manifest",
		"hot-patching", "profiler", "stacker"},
}

var profileDescriptions = map[string]string{
	ProfileNativeDefault:           "Native Windows default effective feature graph.",
	ProfileTestSupport:             "Default plus test-support and leak-detection; the profile the recorded reference fixtures run in.",
	ProfileInspectorCapture:        "Default plus inspector and screen-capture.",
	ProfileCustomGPUWgpu:           "No default features, wgpu-surfaces plus custom-gpu.",
	ProfileHotpatchProfilerStacker: "Default plus hot-patching, profiler and stacker.",
}

// DefaultProfiles returns the five recorded profile records with graph files
// resolved under profilesDir. The CargoFeatures field of each record is the
// feature set used for cfg evaluation.
func DefaultProfiles(profilesDir string) map[string]ProfileRecord {
	profiles := make(map[string]ProfileRecord, len(DefaultProfileOrder))
	for _, id := range DefaultProfileOrder {
		graph := graphFileBase[id]
		if profilesDir != "" {
			graph = joinPath(profilesDir, graphFileBase[id])
		}
		features := profileFeatureSets[id]
		// Copy to keep the records independent.
		fs := make([]string, len(features))
		copy(fs, features)
		profiles[id] = ProfileRecord{
			Name:          id,
			Description:   profileDescriptions[id],
			CargoFeatures: fs,
			GraphFile:     graph,
		}
	}
	return profiles
}

// joinPath joins a directory and a relative path with forward slashes (the
// artifact paths are recorded in a portable form).
func joinPath(dir, name string) string {
	if dir == "" {
		return name
	}
	if strings.HasSuffix(dir, "/") || strings.HasSuffix(dir, "\\") {
		return dir + name
	}
	return dir + "/" + name
}

// FeatureSet converts a profile's CargoFeatures into the evaluator's feature
// map.
func FeatureSet(r ProfileRecord) map[string]bool {
	set := make(map[string]bool, len(r.CargoFeatures))
	for _, f := range r.CargoFeatures {
		set[f] = true
	}
	return set
}

// ProfileResolution is the outcome of resolving one row against the recorded
// profiles.
type ProfileResolution struct {
	// Profiles lists profile ids (in canonical order) whose feature sets
	// satisfy every cfg condition on a Windows target.
	Profiles []string
	// FailedCfg holds cfg strings that could not be evaluated.
	FailedCfg []string
	// DefaultFeature is true when a cfg mentions feature = "default", which
	// is not a cfg-testable feature name and is treated as always true.
	DefaultFeature bool
}

// ResolveProfiles evaluates one row's cfg conditions (AND across cfg
// strings) against each profile. A row with no cfg exists in every profile.
// If any cfg string fails to evaluate, the row is conservatively present in
// all profiles and the failure is reported for a note.
func ResolveProfiles(cfg []string, profiles map[string]ProfileRecord) ProfileResolution {
	res := ProfileResolution{Profiles: []string{}}
	ordered := ProfileOrder(profiles)
	if len(ordered) == 0 {
		return res
	}
	for _, expr := range cfg {
		if expr == "" {
			continue
		}
		for _, name := range CfgFeatureNames(expr) {
			if name == "default" {
				res.DefaultFeature = true
			}
		}
	}
	featureSets := make(map[string]map[string]bool, len(ordered))
	for _, id := range ordered {
		featureSets[id] = FeatureSet(profiles[id])
	}
	for _, expr := range cfg {
		if expr == "" {
			continue
		}
		if _, err := EvaluateCfg(expr, featureSets[ordered[0]]); err != nil {
			// Conservative: present everywhere; note records the failure.
			res.Profiles = append([]string{}, ordered...)
			res.FailedCfg = append(res.FailedCfg, expr)
			return res
		}
	}
	for _, id := range ordered {
		present := true
		for _, expr := range cfg {
			if expr == "" {
				continue
			}
			v, err := EvaluateCfg(expr, featureSets[id])
			if err != nil {
				// Unreachable given the pre-check above.
				res.Profiles = append([]string{}, ordered...)
				res.FailedCfg = append(res.FailedCfg, expr)
				return res
			}
			if !v {
				present = false
				break
			}
		}
		if present {
			res.Profiles = append(res.Profiles, id)
		}
	}
	return res
}

// ProfileOrder returns the canonical profile order for the given profile set;
// unknown profile ids sort after the canonical ones by id.
func ProfileOrder(profiles map[string]ProfileRecord) []string {
	ordered := make([]string, 0, len(profiles))
	seen := make(map[string]bool, len(profiles))
	for _, id := range DefaultProfileOrder {
		if _, ok := profiles[id]; ok {
			ordered = append(ordered, id)
			seen[id] = true
		}
	}
	rest := make([]string, 0, len(profiles))
	for id := range profiles {
		if !seen[id] {
			rest = append(rest, id)
		}
	}
	sortStrings(rest)
	return append(ordered, rest...)
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
