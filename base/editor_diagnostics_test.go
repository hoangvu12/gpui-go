package base

// Ported test of U:input/editor/diagnostics.rs (1 of 1) and Go-own tests
// of the lsp_types conversion.

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/base/rope"
)

// U:input/editor/diagnostics.rs:268-318
func TestDiagnostic(t *testing.T) {
	text := rope.FromStr("Hello, 你好warld!\nThis is a test.\nGoodbye, world!")
	diagnostics := NewDiagnosticSet(text)

	diagnostics.Push(NewDiagnostic(NewInputPosition(0, 7), NewInputPosition(0, 17), "Spelling mistake").
		WithSeverity(DiagnosticSeverityWarning))
	diagnostics.Push(NewDiagnostic(NewInputPosition(2, 9), NewInputPosition(2, 14), "Syntax error").
		WithSeverity(DiagnosticSeverityError))

	if diagnostics.Len() != 2 {
		t.Fatalf("len = %d, want 2", diagnostics.Len())
	}
	items := slices.Collect(diagnostics.Iter())
	if items[0].Message != "Spelling mistake" || items[0].ByteRange != NewInputSelection(7, 19) {
		t.Fatalf("items[0] = %q %v", items[0].Message, items[0].ByteRange)
	}
	if items[1].Message != "Syntax error" || items[1].ByteRange != NewInputSelection(45, 50) {
		t.Fatalf("items[1] = %q %v", items[1].Message, items[1].ByteRange)
	}

	if got := slices.Collect(diagnostics.Range(NewInputSelection(6, 48))); len(got) != 2 {
		t.Fatalf("range(6..48) = %d items, want 2", len(got))
	}

	if item, ok := diagnostics.ForOffset(10); !ok || item.Message != "Spelling mistake" {
		t.Fatalf("for_offset(10) = %q, %v", item.Message, ok)
	}
	if _, ok := diagnostics.ForOffset(30); ok {
		t.Fatal("for_offset(30) must be none")
	}
	if item, ok := diagnostics.ForOffset(46); !ok || item.Message != "Syntax error" {
		t.Fatalf("for_offset(46) = %q, %v", item.Message, ok)
	}

	diagnostics.Push(NewDiagnostic(NewInputPosition(1, 5), NewInputPosition(1, 7), "Info message").
		WithSeverity(DiagnosticSeverityInfo))
	if diagnostics.Len() != 3 {
		t.Fatalf("len = %d, want 3", diagnostics.Len())
	}

	diagnostics.Clear()
	if diagnostics.Len() != 0 || !diagnostics.IsEmpty() {
		t.Fatalf("len after clear = %d", diagnostics.Len())
	}
}

// Go-own: From<lsp_types::Diagnostic> defaults the severity to Info and
// renders a numeric code (U:input/editor/diagnostics.rs:54-95).
func TestDiagnosticFromLsp(t *testing.T) {
	source := "lint"
	d := DiagnosticFromLsp(LspDiagnostic{
		Range:   NewLspRange(NewInputPosition(0, 1), NewInputPosition(0, 2)),
		Code:    new(LspNumberOrStringNumber(42)),
		Source:  &source,
		Message: "m",
	})
	if d.Severity != DiagnosticSeverityInfo || d.Code == nil || *d.Code != "42" || *d.Source != "lint" {
		t.Fatalf("converted = %+v", d)
	}
	hint := LspDiagnosticSeverityHint
	if got := DiagnosticFromLsp(LspDiagnostic{Severity: &hint}).Severity; got != DiagnosticSeverityHint {
		t.Fatalf("hint severity = %v", got)
	}
	if got := DiagnosticSeverityFromLsp(LspDiagnosticSeverity(9)); got != DiagnosticSeverityInfo {
		t.Fatalf("unknown severity = %v, want Info", got)
	}
	code := DiagnosticFromLsp(LspDiagnostic{Code: new(LspNumberOrStringString("E1"))}).Code
	if code == nil || *code != "E1" {
		t.Fatalf("string code = %v", code)
	}
}
