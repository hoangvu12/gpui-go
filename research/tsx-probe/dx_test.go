package tsxprobe

// DX checks for research/browser-parity/09: which language features run
// in goja as written and after esbuild lowering, and whether runtime
// errors point back into the .tsx through the inline source map.
//
//	CGO_ENABLED=0 go test -run TestDX -count=1 -v .

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"github.com/evanw/esbuild/pkg/api"
)

// Each snippet must leave the global `ok` true.
var features = []struct{ name, src string }{
	{"let/const, arrows, template literals", "const f = (a) => `${a}`; var ok = f(1) === '1';"},
	{"destructuring, default and rest params", "const {a, ...r} = {a: 1, b: 2}; const g = (x = 1, ...y) => x + y.length; var ok = a === 1 && r.b === 2 && g() === 1;"},
	{"spread in arrays/objects", "const o = {...{a: 1}, b: 2}; var ok = [...[1, 2], 3].length === 3 && o.a === 1;"},
	{"classes, getters", "class A { get x() { return 1 } }; class B extends A {}; var ok = new B().x === 1;"},
	{"class fields (ES2022)", "class A { x = 1; static y = 2; #p = 3; p() { return this.#p } }; var ok = new A().x === 1 && A.y === 2 && new A().p() === 3;"},
	{"optional chaining, ??", "const o = {a: null}; var ok = o.a?.b === undefined && (o.a ?? 5) === 5;"},
	{"logical assignment (??=)", "let a = null; a ??= 3; var ok = a === 3;"},
	{"Map, Set, Symbol, for-of", "const m = new Map([[1, 2]]); let s = 0; for (const [k, v] of m) s += k + v; var ok = s === 3 && new Set([1, 1]).size === 1 && typeof Symbol() === 'symbol';"},
	{"generators", "function* g() { yield 1; yield 2 }; var ok = [...g()].length === 2;"},
	{"async/await + Promise", "var ok = false; (async () => { await null; ok = true })();"},
	{"BigInt", "var ok = 2n ** 64n > 0n;"},
	{"Array.prototype.flat/at, Object.fromEntries", "var ok = [[1], [2]].flat().length === 2 && [1, 2].at(-1) === 2 && Object.fromEntries([['a', 1]]).a === 1;"},
	{"String.prototype.replaceAll", "var ok = 'aa'.replaceAll('a', 'b') === 'bb';"},
	{"regex named groups, lookbehind", "var ok = /(?<y>\\d+)/.exec('a12').groups.y === '12' && /(?<=a)b/.test('ab');"},
	{"Intl.NumberFormat", "var ok = typeof Intl !== 'undefined' && new Intl.NumberFormat('en').format(1000) === '1,000';"},
	{"structuredClone", "var ok = structuredClone({a: 1}).a === 1;"},
	{"setTimeout (host API)", "var ok = typeof setTimeout === 'function';"},
	{"top-level await (module)", "await null; var ok = true;"},
}

func runIn(src string) (bool, string) {
	vm := goja.New()
	if _, err := vm.RunString(src); err != nil {
		return false, firstLine(err.Error())
	}
	// Run queued promise jobs: goja runs them when the outermost call
	// returns, so a no-op call is enough.
	_, _ = vm.RunString("void 0")
	return vm.Get("ok").ToBoolean(), ""
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

func lower(src string, target api.Target) (string, string) {
	r := api.Transform(src, api.TransformOptions{Loader: api.LoaderTS, Target: target})
	if len(r.Errors) > 0 {
		return "", r.Errors[0].Text
	}
	return string(r.Code), ""
}

func TestDX(t *testing.T) {
	type row struct {
		Feature string `json:"feature"`
		Raw     string `json:"raw"`
		ES2020  string `json:"es2020"`
		ES2017  string `json:"es2017"`
		ES2015  string `json:"es2015"`
	}
	verdict := func(src string, target api.Target) string {
		code := src
		if target != 0 {
			var err string
			if code, err = lower(src, target); err != "" {
				return "esbuild: " + err
			}
		}
		ok, err := runIn(code)
		switch {
		case err != "":
			return "goja: " + err
		case !ok:
			return "wrong result"
		}
		return "ok"
	}
	var rows []row
	for _, f := range features {
		rows = append(rows, row{f.name, verdict(f.src, 0), verdict(f.src, api.ES2020), verdict(f.src, api.ES2017), verdict(f.src, api.ES2015)})
	}

	// Runtime errors: does the stack point into view.tsx?
	src := strings.Replace(viewSrc, "onClick={() => clicks.set(clicks.get() + 1)}",
		"onClick={() => { const o: any = null; clicks.set(o.count + 1) }}", 1)
	h := NewHost()
	if err := h.Load(src, "view.tsx"); err != nil {
		t.Fatal(err)
	}
	handlerErr := h.Dispatch(h.Find("counter").OnClick)
	line := 0
	for i, l := range strings.Split(src, "\n") {
		if strings.Contains(l, "o.count") {
			line = i + 1
		}
	}

	// Describe-time error: an unsupported style property.
	bad := strings.Replace(viewSrc, `fontSize: 14 }}>`, `fontSize: 14, gridArea: "a" }}>`, 1)
	describeErr := NewHost().Load(bad, "view.tsx")

	out, _ := json.MarshalIndent(map[string]any{
		"features":               rows,
		"handler_error":          fmt.Sprint(handlerErr),
		"handler_error_tsx_line": line,
		"describe_error":         fmt.Sprint(describeErr),
	}, "", "  ")
	t.Logf("%s", out)
	if path := os.Getenv("TSX_DX_OUT"); path != "" {
		if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
