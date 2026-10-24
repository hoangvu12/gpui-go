package tsxgen

import (
	"fmt"
	"sort"
	"strings"
)

// Compare reports how two surfaces differ (the drift tests' verdict:
// empty means the .d.ts declares exactly the engine's surface). The
// comparison is set-wise per section and exact per type — the rendered
// canonical text is the surface.
func Compare(engine, dts *Surface) []string {
	var diffs []string
	diffs = append(diffs, cmpSet("elements", engine.Elements, dts.Elements)...)
	diffs = append(diffs, cmpSet("props", engine.Props, dts.Props)...)
	diffs = append(diffs, cmpSet("events", engine.Events, dts.Events)...)
	diffs = append(diffs, cmpFields("event object fields", engine.Event, dts.Event)...)
	diffs = append(diffs, cmpStyles("style properties", engine.Styles, dts.Styles)...)
	diffs = append(diffs, cmpGlobals("globals", engine.Globals, dts.Globals)...)
	diffs = append(diffs, cmpClasses(engine, dts)...)
	return diffs
}

// cmpSet compares two name sets.
func cmpSet(what string, want, got []string) []string {
	var diffs []string
	for _, w := range want {
		if !contains(got, w) {
			diffs = append(diffs, fmt.Sprintf("%s: the .d.ts lacks %q (the engine accepts it)", what, w))
		}
	}
	for _, g := range got {
		if !contains(want, g) {
			diffs = append(diffs, fmt.Sprintf("%s: the .d.ts declares %q, which the engine does not accept", what, g))
		}
	}
	return diffs
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// cmpFields compares named fields with their types.
func cmpFields(what string, want, got []Field) []string {
	var diffs []string
	gotByName := map[string]Field{}
	for _, f := range got {
		gotByName[f.Name] = f
	}
	for _, w := range want {
		g, ok := gotByName[w.Name]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("%s: the .d.ts lacks %q (the engine provides it)", what, w.Name))
			continue
		}
		if w.Type.Render() != g.Type.Render() {
			diffs = append(diffs, fmt.Sprintf("%s: %q is %s in the .d.ts, %s in the engine",
				what, w.Name, g.Type.Render(), w.Type.Render()))
		}
	}
	for name := range gotByName {
		if !containsField(want, name) {
			diffs = append(diffs, fmt.Sprintf("%s: the .d.ts declares %q, which the engine does not provide", what, name))
		}
	}
	return diffs
}

func containsField(fs []Field, name string) bool {
	for _, f := range fs {
		if f.Name == name {
			return true
		}
	}
	return false
}

// cmpStyles compares the style properties with their value types.
func cmpStyles(what string, want, got []StyleProp) []string {
	var diffs []string
	gotByName := map[string]StyleProp{}
	for _, s := range got {
		gotByName[s.Name] = s
	}
	for _, w := range want {
		g, ok := gotByName[w.Name]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("%s: the .d.ts lacks %q (the engine accepts it)", what, w.Name))
			continue
		}
		if w.Type.Render() != g.Type.Render() {
			diffs = append(diffs, fmt.Sprintf("%s: %q is %s in the .d.ts, %s in the engine",
				what, w.Name, g.Type.Render(), w.Type.Render()))
		}
	}
	for _, g := range got {
		if _, ok := gotByName[g.Name]; !ok {
			continue
		}
		if !containsStyle(want, g.Name) {
			diffs = append(diffs, fmt.Sprintf("%s: the .d.ts declares %q, which the engine does not accept", what, g.Name))
		}
	}
	return diffs
}

func containsStyle(ss []StyleProp, name string) bool {
	for _, s := range ss {
		if s.Name == name {
			return true
		}
	}
	return false
}

// cmpGlobals compares the globals with their signatures (whitespace-
// normalized: the byte test pins the exact formatting).
func cmpGlobals(what string, want, got []Global) []string {
	var diffs []string
	gotByName := map[string]Global{}
	for _, g := range got {
		gotByName[g.Name] = g
	}
	for _, w := range want {
		g, ok := gotByName[w.Name]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("%s: the .d.ts lacks %q (the host provides it)", what, w.Name))
			continue
		}
		if normalizeSig(w.Sig) != normalizeSig(g.Sig) {
			diffs = append(diffs, fmt.Sprintf("%s: %q is declared differently in the .d.ts:\n\t.d.ts:   %s\n\tengine: %s",
				what, w.Name, normalizeSig(g.Sig), normalizeSig(w.Sig)))
		}
	}
	for _, g := range got {
		if _, ok := gotByName[g.Name]; !ok {
			continue
		}
		if !containsGlobal(want, g.Name) {
			diffs = append(diffs, fmt.Sprintf("%s: the .d.ts declares %q, which the host does not provide", what, g.Name))
		}
	}
	return diffs
}

func containsGlobal(gs []Global, name string) bool {
	for _, g := range gs {
		if g.Name == name {
			return true
		}
	}
	return false
}

func normalizeSig(sig string) string {
	return strings.Join(strings.Fields(sig), " ")
}

// cmpClasses compares the class surface: the same aliases (by name and
// body) and the same Class type.
func cmpClasses(engine, dts *Surface) []string {
	var diffs []string
	engineAliases := map[string]TSType{}
	for _, a := range engine.Classes.Aliases {
		engineAliases[a.Name] = a.Type
	}
	for _, a := range dts.Classes.Aliases {
		want, ok := engineAliases[a.Name]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("classes: the .d.ts declares alias %q, which the engine surface does not have", a.Name))
			continue
		}
		if want.Render() != a.Type.Render() {
			diffs = append(diffs, fmt.Sprintf("classes: alias %q differs:\n\t.d.ts:   %s\n\tengine: %s", a.Name, a.Type.Render(), want.Render()))
		}
		delete(engineAliases, a.Name)
	}
	for name := range engineAliases {
		diffs = append(diffs, fmt.Sprintf("classes: the .d.ts lacks alias %q (the engine surface has it)", name))
	}
	if engine.Classes.Class.Render() != dts.Classes.Class.Render() {
		diffs = append(diffs, fmt.Sprintf("classes: the Class type differs:\n\t.d.ts:   %s\n\tengine: %s",
			dts.Classes.Class.Render(), engine.Classes.Class.Render()))
	}
	return diffs
}

// Aliases returns the surface's aliases, sorted by name (test support:
// the drift tests build the matching environment from them).
func (s *Surface) Aliases() map[string]TSType {
	out := map[string]TSType{}
	for _, a := range s.Classes.Aliases {
		out[a.Name] = a.Type
	}
	return out
}

// ClassMatches reports whether the .d.ts's Class type accepts one
// class string (references resolved through the surface's aliases).
func (s *Surface) ClassMatches(class string) bool {
	return s.Classes.Class.Matches(class, s.Aliases())
}

// ClassSamples returns representative class strings the Class type
// accepts (one per literal and template arm, with variant and
// important compositions of the core samples).
func (s *Surface) ClassSamples() []string {
	core := TSType{}
	for _, a := range s.Classes.Aliases {
		if a.Name == "CoreClass" {
			core = a.Type
		}
	}
	aliases := s.Aliases()
	samples := core.Samples(aliases)
	var out []string
	out = append(out, samples...)
	// The variant compositions apply to the state-styled classes only:
	// group and the scroll classes configure interactivity, which the
	// engine rejects under a variant.
	composable := func(sample string) bool {
		return !strings.HasPrefix(sample, "group") && !strings.HasPrefix(sample, "overflow-")
	}
	for _, sample := range samples {
		if !composable(sample) {
			continue
		}
		out = append(out,
			"hover:"+sample,
			"group-hover/menu:"+sample,
			"!"+sample,
			sample+"!",
			"hover:!"+sample,
			"hover:"+sample+"!")
	}
	return dedupe(out)
}

// SortedStrings returns the strings sorted (test support).
func SortedStrings(ss []string) []string {
	out := append([]string(nil), ss...)
	sort.Strings(out)
	return out
}
