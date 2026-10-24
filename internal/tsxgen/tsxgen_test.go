package tsxgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// The drift tests (ticket 63): the committed gpui-script.d.ts must
// declare exactly the surface the engine accepts, in both directions,
// without tsc — the comparison is Go, against the engine's own code
// and behavior.
//
//	engine → types: every table literal, family sample and candidate
//	                class the engine accepts must be in the Class type
//	types → engine: every class the types accept must pass
//	                gpui.CheckClasses (the check Styled.Class runs)
//
// A planted gap — a class, style key, element, prop, event field or
// global removed from the .d.ts — fails the parity test, and a test
// plants each kind to prove the comparison catches it.

// moduleRoot walks up from the working directory to go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}

// committedDTS reads the generated file the app embeds.
func committedDTS(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(moduleRoot(t), "gpui", "script", "types", "gpui-script.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// engineSurface extracts the surface once.
func engineSurface(t *testing.T) *Surface {
	t.Helper()
	s, err := Extract(moduleRoot(t))
	if err != nil {
		t.Fatalf("extracting the engine surface: %v", err)
	}
	return s
}

// TestDTSIsCurrent pins the committed file to the engine: regenerating
// must be byte-identical. Any engine change (a new style key, class
// table entry, event field or global) without regenerating fails here
// with the fix in the message.
func TestDTSIsCurrent(t *testing.T) {
	want := engineSurface(t).DTS()
	got := committedDTS(t)
	if want != got {
		t.Fatalf("gpui/script/types/gpui-script.d.ts is stale: the engine's surface changed\n" +
			"regenerate with: go generate ./gpui/script/types")
	}
}

// TestDTSDeclaresTheEngineSurface is the parity test: the committed
// .d.ts, parsed back, must declare exactly the extracted surface —
// same elements, props, events, event fields, style properties with
// their value types, globals with their signatures, and class aliases
// with the Class type. Both directions (missing and extra).
func TestDTSDeclaresTheEngineSurface(t *testing.T) {
	engine := engineSurface(t)
	parsed, err := ParseDTS(committedDTS(t))
	if err != nil {
		t.Fatalf("parsing the committed .d.ts: %v", err)
	}
	diffs := Compare(engine, parsed)
	for _, d := range diffs {
		t.Errorf("drift: %s", d)
	}
}

// TestPlantedGapsFailTheParityCheck proves the comparison catches a
// gap: each planted edit (a removed class, style key, element, prop,
// event field and global, and a changed style type) must make the
// parity test fail.
func TestPlantedGapsFailTheParityCheck(t *testing.T) {
	engine := engineSurface(t)
	dts := committedDTS(t)
	plant := func(name, old, new string) {
		t.Helper()
		if !strings.Contains(dts, old) {
			t.Fatalf("planting %s: %q not found in the .d.ts", name, old)
		}
		planted, err := ParseDTS(strings.Replace(dts, old, new, 1))
		if err != nil {
			t.Fatalf("planting %s: the .d.ts no longer parses: %v", name, err)
		}
		if diffs := Compare(engine, planted); len(diffs) == 0 {
			t.Errorf("planting %s: the parity test did not catch the gap", name)
		}
	}
	plant("a removed class", "\t\"flex\" |\n", "")
	plant("a removed style key", "\tfontSize?: number | `${number}px`;\n", "")
	plant("a changed style type", "\tdisplay?: \"flex\" | \"block\";\n", "\tdisplay?: \"flex\";\n")
	plant("a removed element", "\t\ttext: HostProps;\n", "")
	plant("a removed prop", "\tonMouseDown?: (event: MouseEvent) => void;\n", "")
	plant("a removed event field", "\tmetaKey: boolean;\n", "")
	plant("a removed global", "declare function btoa(data: string): string;\n\n", "")
	plant("a removed class alias arm", "\t\"max-w-none\" |\n", "")
}

// TestClassTypesMatchTheEngine checks the class surface against the
// engine's behavior (gpui.CheckClasses, the check Styled.Class runs —
// the same check the host runs on className at load):
//
//   - everything the types accept, the engine accepts (the .d.ts's
//     samples, with variant and important compositions);
//   - everything the engine accepts that the candidates probe finds
//     (the Tailwind utility namespace, independent of the generator's
//     tables) is matched by the Class type.
func TestClassTypesMatchTheEngine(t *testing.T) {
	parsed, err := ParseDTS(committedDTS(t))
	if err != nil {
		t.Fatalf("parsing the committed .d.ts: %v", err)
	}
	// The types must accept what the engine accepts.
	for _, class := range parsed.ClassSamples() {
		if err := gpui.CheckClasses(class); err != nil {
			t.Errorf("the types accept %q, the engine does not: %v", class, err)
		}
	}
	// The engine's own tables (method classes, the palette, the
	// scales) must all be matched by the Class type.
	engine := engineSurface(t)
	for _, class := range engineTableClasses(engine) {
		if !parsed.ClassMatches(class) {
			t.Errorf("the engine accepts %q, the Class type does not", class)
		}
	}
	// The candidates probe: the Tailwind utility namespace, enumerated
	// independently of the generator's tables, so a family the engine
	// gained is caught.
	accepted, rejected := 0, 0
	for _, class := range classCandidates() {
		err := gpui.CheckClasses(class)
		if err == nil {
			accepted++
			if !parsed.ClassMatches(class) {
				t.Errorf("the engine accepts %q, the Class type does not", class)
			}
			continue
		}
		rejected++
	}
	if accepted < 100 {
		t.Fatalf("the candidate probe accepted only %d classes; the list is broken", accepted)
	}
	t.Logf("candidates: %d accepted (all matched by the Class type), %d rejected by the engine", accepted, rejected)
}

// engineTableClasses lists the literal classes the engine's parsed
// tables accept (method classes, special classes, scroll classes, the
// palette under the color prefixes, the named scales and the
// variants) — everything the extraction read from the tables, as
// standalone classes.
func engineTableClasses(s *Surface) []string {
	aliases := s.Aliases()
	var out []string
	// The method, special, scroll, blur, max-w and filter literals
	// are CoreClass's own literal arms.
	out = append(out, aliases["CoreClass"].Literals...)
	// The palette under each color prefix (with the opacity modifier).
	palette := aliases["PaletteColor"].Literals
	colorPrefixes := aliases["ColorPrefix"].Literals
	for _, p := range colorPrefixes {
		for _, c := range palette {
			out = append(out, p+c, p+c+"/50")
		}
	}
	return dedupe(out)
}

// classCandidates enumerates the Tailwind v3 utility namespace
// independently of the generator's tables: the prefix families with
// their value forms, negative and prefixed forms, and the bare
// utilities. Everything here the ENGINE accepts must be matched by the
// Class type; anything it rejects is skipped (the probe measures the
// engine, not Tailwind).
func classCandidates() []string {
	var out []string
	seen := map[string]bool{}
	add := func(classes ...string) {
		for _, c := range classes {
			if c != "" && !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	// The prefix families.
	for _, prefix := range tailwindPrefixes {
		for _, value := range tailwindValues {
			add(prefix + "-" + value)
		}
		if strings.HasSuffix(prefix, "-") {
			add(prefix+"0", prefix+"50")
			continue
		}
		add(prefix)
	}
	// Negative forms (margins, insets, and the filters that take them).
	for _, prefix := range []string{"m", "mx", "my", "mt", "mr", "mb", "ml", "inset", "inset-x", "inset-y",
		"top", "right", "bottom", "left", "translate-x", "translate-y", "rotate", "skew-x", "skew-y",
		"hue-rotate", "brightness", "contrast", "saturate", "grayscale", "sepia", "invert", "bg-linear", "order", "z"} {
		for _, value := range tailwindValues {
			add("-" + prefix + "-" + value)
		}
	}
	// The bare utilities (literal forms, spanning Tailwind's families).
	add(tailwindLiterals...)
	// A few composed forms.
	add("hover:flex", "active:bg-slate-800", "focus-visible:text-white", "group-hover/menu:flex",
		"group-active:flex", "focus-within:bg-red-500", "!p-4", "p-4!", "hover:!p-4", "hover:p-4!",
		"!flex", "flex!", "group", "group/menu", "overflow-auto", "overflow-x-scroll")
	return out
}

// tailwindPrefixes is Tailwind v3's utility prefix namespace (the
// families that take a value), independent of the engine's tables.
var tailwindPrefixes = []string{
	// box, spacing and sizing
	"p", "px", "py", "pt", "pr", "pb", "pl", "m", "mx", "my", "mt", "mr", "mb", "ml",
	"w", "h", "size", "min-w", "min-h", "max-w", "max-h", "gap", "gap-x", "gap-y", "basis",
	"inset", "inset-x", "inset-y", "top", "right", "bottom", "left", "indent",
	// borders, rings and radii
	"border", "border-t", "border-r", "border-b", "border-l", "border-x", "border-y",
	"rounded", "rounded-t", "rounded-r", "rounded-b", "rounded-l", "rounded-tl", "rounded-tr",
	"rounded-br", "rounded-bl", "ring", "inset-ring", "ring-offset", "divide-x", "divide-y",
	// typography
	"text", "font", "leading", "tracking", "line-clamp", "decoration", "underline-offset",
	"list", "whitespace",
	// colors, backgrounds and gradients
	"bg", "bg-linear", "bg-gradient", "from", "via", "to", "shadow", "drop-shadow",
	// opacity and filters
	"opacity", "blur", "backdrop-blur", "brightness", "contrast", "saturate", "hue-rotate",
	"grayscale", "sepia", "invert", "backdrop-saturate", "backdrop-brightness", "backdrop-contrast",
	"backdrop-grayscale", "backdrop-opacity", "backdrop-invert", "backdrop-sepia", "backdrop-hue-rotate",
	// grid and layout
	"grid-cols", "grid-rows", "col-span", "row-span", "col-start", "col-end", "row-start", "row-end",
	"auto-cols", "auto-rows", "space-x", "space-y", "order", "z", "aspect",
	// effects, transitions and animation
	"scale", "scale-x", "scale-y", "rotate", "translate-x", "translate-y", "skew-x", "skew-y",
	"duration", "delay", "ease", "transition",
	// interactivity
	"cursor", "accent", "caret", "scroll-m", "scroll-p", "snap", "scroll-behavior",
	// svg
	"fill", "stroke",
}

// tailwindValues is the value space the candidate probe combines with
// the prefixes (scale numbers, keywords, fractions, bracketed values
// and colors).
var tailwindValues = []string{
	"0", "1", "2", "3", "4", "8", "12", "13", "16", "96", "112", "128", "9999",
	"0.5", "0.75", "1.5", "2.5", "3.5", "4.5", "10.5",
	"px", "full", "auto", "none",
	"1/2", "1/3", "2/3", "1/4", "3/4", "1/5", "2/5", "4/5", "1/6", "5/6", "1/12", "5/12",
	"[12px]", "[1.5rem]", "[50%]", "[0]", "[45deg]", "[0.35]", "[10]",
	"slate-800", "slate-50", "red-500", "white", "black", "transparent", "current", "inherit",
	"[#0f172a]", "[#fff]",
	"xs", "sm", "md", "lg", "xl", "2xl", "3xl", "7xl",
	"tight", "snug", "normal", "relaxed", "loose", "sans", "serif", "mono",
	"45", "45deg", "90deg", "100", "105", "150", "200", "0.35", "35",
	"start", "end", "center", "between", "around", "evenly", "baseline", "stretch",
	"row", "col", "wrap", "nowrap", "reverse", "hidden", "visible", "clip", "scroll",
	"to-r", "to-tl", "solid", "dashed", "video", "square", "tabular-nums", "none",
}

// tailwindLiterals is a spread of Tailwind v3's bare utilities (the
// forms without a value), independent of the engine's tables.
var tailwindLiterals = []string{
	"container", "block", "inline-block", "inline", "flex", "inline-flex", "grid", "inline-grid",
	"table", "inline-table", "flow-root", "contents", "list-item", "hidden",
	"static", "fixed", "absolute", "relative", "sticky",
	"box-border", "box-content", "isolate", "isolation",
	"flex-1", "flex-auto", "flex-initial", "flex-none", "flex-grow", "flex-shrink", "grow", "shrink",
	"flex-row", "flex-row-reverse", "flex-col", "flex-col-reverse", "flex-wrap", "flex-wrap-reverse",
	"flex-nowrap", "basis-auto", "basis-full",
	"grid-flow-row", "grid-flow-col", "grid-flow-dense", "grid-flow-row-dense", "grid-flow-col-dense",
	"items-start", "items-end", "items-center", "items-baseline", "items-stretch",
	"justify-start", "justify-end", "justify-center", "justify-between", "justify-around", "justify-evenly",
	"content-start", "content-end", "content-center", "content-between", "content-around", "content-evenly",
	"self-auto", "self-start", "self-end", "self-center", "self-stretch",
	"place-content-center", "place-items-center", "place-self-center",
	"justify-items-start", "justify-self-start",
	"antialiased", "subpixel-antialiased", "italic", "not-italic",
	"uppercase", "lowercase", "capitalize", "normal-case", "underline", "overline", "line-through",
	"no-underline", "truncate", "text-wrap", "text-nowrap", "text-balance", "text-pretty",
	"whitespace-normal", "whitespace-nowrap", "whitespace-pre", "break-words", "break-all",
	"font-sans", "font-serif", "font-mono", "font-bold", "font-medium", "text-left", "text-center",
	"text-right", "text-justify", "text-sm", "text-base", "text-lg", "text-xl", "text-2xl",
	"align-baseline", "align-top", "align-middle", "align-bottom",
	"leading-none", "leading-tight", "leading-snug", "leading-normal", "leading-relaxed", "leading-loose",
	"tracking-tighter", "tracking-tight", "tracking-normal", "tracking-wide", "tracking-wider",
	"tracking-widest", "whitespace-pre-line", "whitespace-pre-wrap",
	"list-none", "list-disc", "list-decimal", "list-inside", "list-outside",
	"bg-linear", "bg-linear-to-t", "bg-linear-to-tr", "bg-linear-to-r", "bg-linear-to-br",
	"bg-linear-to-b", "bg-linear-to-bl", "bg-linear-to-l", "bg-linear-to-tl",
	"bg-gradient-to-r", "bg-radial", "bg-conic", "bg-none", "bg-cover", "bg-contain", "bg-center",
	"bg-fixed", "bg-local", "bg-scroll", "bg-clip-border", "bg-clip-text",
	"border", "border-t", "border-r", "border-b", "border-l", "border-x", "border-y",
	"border-solid", "border-dashed", "border-dotted", "border-double", "border-none", "border-hidden",
	"divide-y", "divide-x", "divide-solid",
	"rounded", "rounded-t", "rounded-r", "rounded-b", "rounded-l", "rounded-tl", "rounded-tr",
	"rounded-br", "rounded-bl", "rounded-none", "rounded-sm", "rounded-md", "rounded-lg", "rounded-xl",
	"rounded-2xl", "rounded-3xl", "rounded-full", "rounded-s", "rounded-e", "rounded-ss", "rounded-se",
	"ring", "ring-inset", "ring-offset-2", "inset-ring", "shadow", "shadow-sm", "shadow-md", "shadow-lg",
	"shadow-xl", "shadow-2xl", "shadow-inner", "shadow-none",
	"opacity-0", "opacity-50", "opacity-100",
	"blur", "blur-none", "blur-sm", "blur-md", "blur-lg", "blur-xl", "blur-2xl", "blur-3xl",
	"backdrop-blur", "backdrop-blur-sm", "grayscale", "grayscale-0", "invert", "invert-0",
	"sepia", "sepia-0", "brightness-0", "brightness-100", "saturate-0", "saturate-100",
	"hue-rotate-0", "hue-rotate-90", "drop-shadow", "drop-shadow-md", "filter", "filter-none",
	"transition", "transition-all", "transition-colors", "duration-300", "ease-in", "ease-out",
	"ease-linear", "ease-in-out", "delay-100", "animate-none", "animate-spin", "animate-ping",
	"scale-75", "scale-100", "rotate-45", "translate-x-4", "translate-y-4", "skew-x-12", "skew-y-12",
	"transform", "transform-gpu", "origin-center", "origin-top", "origin-top-right",
	"cursor-auto", "cursor-default", "cursor-pointer", "cursor-text", "cursor-move", "cursor-grab",
	"cursor-grabbing", "cursor-not-allowed", "cursor-crosshair", "cursor-wait", "cursor-help",
	"cursor-n-resize", "cursor-progress", "pointer-events-auto", "pointer-events-none",
	"resize", "resize-x", "resize-y", "resize-none", "select-none", "select-text", "select-all",
	"scroll-auto", "scroll-smooth", "snap-start", "snap-end", "snap-center", "snap-align-none",
	"touch-auto", "touch-none", "touch-pan-x", "overscroll-auto", "overscroll-contain",
	"will-change-auto", "will-change-transform", "sr-only", "not-sr-only",
	"z-0", "z-10", "z-50", "order-1", "order-first", "order-last", "order-none",
	"aspect-auto", "aspect-square", "aspect-video",
	"overflow-auto", "overflow-hidden", "overflow-clip", "overflow-visible", "overflow-scroll",
	"overflow-x-auto", "overflow-x-hidden", "overflow-x-clip", "overflow-x-visible", "overflow-x-scroll",
	"overflow-y-auto", "overflow-y-hidden", "overflow-y-clip", "overflow-y-visible", "overflow-y-scroll",
	"overscroll-x-auto", "overscroll-y-auto",
	"break-normal", "break-keep", "hyphens-none", "hyphens-auto",
	"columns-1", "columns-2", "columns-3", "break-after-auto", "break-before-page", "break-inside-avoid",
	"float-left", "float-right", "float-none", "clear-left", "clear-both", "clear-none",
	"object-contain", "object-cover", "object-fill", "object-none", "object-scale-down",
	"object-bottom", "object-center", "object-left", "object-left-bottom", "object-left-top",
	"object-right", "object-right-bottom", "object-right-top", "object-top",
	"fill-current", "stroke-current", "stroke-0", "stroke-1", "stroke-2",
	"normal-nums", "tabular-nums", "proportional-nums", "lining-nums", "oldstyle-nums",
	"slashed-zero", "ordinal", "diagonal-fractions", "stacked-fractions",
	"place-content-between", "table-auto", "table-fixed", "border-collapse", "border-separate",
	"caption-top", "caption-bottom",
	"mix-blend-normal", "mix-blend-multiply", "bg-blend-normal",
	"text-decoration-none", "decoration-solid", "decoration-wavy",
	"appearance-none", "accent-auto", "caret-transparent",
}

// TestParseRoundTrip proves ParseDTS reads back exactly what DTS
// renders — the parser and the renderer agree on the surface.
func TestParseRoundTrip(t *testing.T) {
	engine := engineSurface(t)
	parsed, err := ParseDTS(engine.DTS())
	if err != nil {
		t.Fatalf("parsing the rendered .d.ts: %v", err)
	}
	for _, d := range Compare(engine, parsed) {
		t.Errorf("round trip: %s", d)
	}
}
