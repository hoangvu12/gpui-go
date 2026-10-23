package main

import (
	"reflect"
	"testing"
)

func TestScanRustTestsIgnoresCommentsAndLiterals(t *testing.T) {
	src := `//! The layout algebra runs as plain ` + "`#[test]`" + `.
/// #[test] in a doc comment
/* #[test] /* nested */ fn not_a_test() {} */
const S: &str = "#[test] fn in_a_string() {}";
const R: &str = r#"#[gpui::test] fn in_raw() { "}" }"#;

#[cfg(test)]
mod tests {
    #[test]
    fn first<'a>() {
        let c = '{';
        let s = "}";
        let r = r"}}";
        let _x: &'a str = "";
    }

    #[gpui::test(iterations = 3)]
    async fn second(cx: &mut TestAppContext) {
        if true { }
    }
}
`
	got := scanRustTests([]byte(src))
	want := []rustTest{
		{Name: "first", Attr: 9, FnLine: 10, EndLine: 15},
		{Name: "second", Attr: 17, FnLine: 18, EndLine: 20},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestWiringOnly(t *testing.T) {
	if !wiringOnly([]byte("//! docs\nmod a;\npub(super) mod b;\npub use a::{X,\n    Y};\n")) {
		t.Fatal("a mod/use-only file is wiring")
	}
	if wiringOnly([]byte("mod a;\npub fn f() {}\n")) {
		t.Fatal("a file with code is not wiring")
	}
}

func TestCitationForms(t *testing.T) {
	src := `package p

// Ported from U:history.rs:134-285 (10/10).

// U:text/inline.rs:2232-2260 and 2262-2290
func TestA() {}

// whose decisions close it (U:alert_dialog.rs:130-139, 180-190;
// U:dialog.rs:571-600). Deviation: no clipboard.
func TestB() {}

// Not applicable here: the cached-view test (U:input/base/state.rs:
// 4957-5019, no AnyView::cached) and more.
`
	g, err := parseGo("p_test.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	cs, funcs := scanCitations(g)
	type row struct {
		Path     string
		Ranges   [][2]int
		Context  string
		Deviated bool
		NA       bool
	}
	var got []row
	for _, c := range cs {
		got = append(got, row{c.Path, c.Ranges, c.Context, c.Deviated, c.NotApplicable})
	}
	want := []row{
		{"history.rs", [][2]int{{134, 285}}, "", false, false},
		{"text/inline.rs", [][2]int{{2232, 2260}, {2262, 2290}}, "TestA", false, false},
		{"alert_dialog.rs", [][2]int{{130, 139}, {180, 190}}, "TestB", true, false},
		{"dialog.rs", [][2]int{{571, 600}}, "TestB", true, false},
		{"input/base/state.rs", [][2]int{{4957, 5019}}, "", false, true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if len(funcs) != 2 || funcs[1].Name != "TestB" || !funcs[1].Deviated || !funcs[0].Cites["history.rs"] {
		t.Fatalf("funcs %+v", funcs)
	}
}

func TestResolvePrefersTestFuncsAndNarrowSpans(t *testing.T) {
	rt := rustTest{Name: "t", Attr: 100, FnLine: 101, EndLine: 120}
	cs := []citation{
		{File: "h.go", Path: "x.rs", Ranges: [][2]int{{1, 900}}},                                     // free comment
		{File: "h.go", Path: "x.rs", Ranges: [][2]int{{100, 110}}, Context: "helper"},                // helper
		{File: "a.go", Path: "x.rs", Ranges: [][2]int{{90, 130}}, Context: "TestWide"},               // test, wide
		{File: "b.go", Path: "x.rs", Ranges: [][2]int{{100, 120}}, Context: "TestT", Deviated: true}, // test, narrow
		{File: "c.go", Path: "x.rs", Ranges: [][2]int{{200, 300}}, Context: "TestOther"},             // no overlap
	}
	te := resolveTest("x.rs", rt, cs)
	if te.Status != statusDeviated || !reflect.DeepEqual(te.GoTests, []string{"b.go:TestT"}) {
		t.Fatalf("%+v", te)
	}
	if te := resolveTest("x.rs", rt, cs[4:]); te.Status != "" {
		t.Fatalf("a non-overlapping citation credited the test: %+v", te)
	}
}
