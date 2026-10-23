package base

// Port of U:input/editor/diagnostics.rs: Diagnostic, its severity, and the
// DiagnosticSet the code editor keeps over its text, plus the diagnostic
// underline style of U:input/base/element.rs:29-47.
//
// Go adaptations: SharedString fields are strings, Option<SharedString> a
// *string. DiagnosticSet keeps its entries in the ported sum tree
// (base/sumtree, ticket 15) with the upstream summary and seek target.
// DiagnosticRelatedInformation / CodeDescription / DiagnosticTag are the
// lsp_types re-exports (U:input/editor/diagnostics.rs:12-15), here
// LspDiagnosticRelatedInformation, LspCodeDescription and
// LspDiagnosticTag; RelatedInformation is the same alias.

import (
	"bytes"
	"encoding/json"
	"iter"
	"slices"
	"strconv"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/base/sumtree"
	"github.com/hoangvu12/gpui-go/gpui"
)

// DiagnosticRelatedInformation is the lsp_types re-export
// (U:input/editor/diagnostics.rs:12).
type DiagnosticRelatedInformation = LspDiagnosticRelatedInformation

// RelatedInformation is the same re-export
// (U:input/editor/diagnostics.rs:14).
type RelatedInformation = LspDiagnosticRelatedInformation

// DiagnosticTag is the lsp_types re-export
// (U:input/editor/diagnostics.rs:15).
type DiagnosticTag = LspDiagnosticTag

// DiagnosticSeverity is a diagnostic's severity
// (U:input/editor/diagnostics.rs:76-83). The zero value is Hint, the
// upstream default.
type DiagnosticSeverity uint8

const (
	DiagnosticSeverityHint DiagnosticSeverity = iota
	DiagnosticSeverityError
	DiagnosticSeverityWarning
	DiagnosticSeverityInfo
)

// DiagnosticSeverityFromLsp is From<lsp_types::DiagnosticSeverity>: an
// unknown value is Info (U:input/editor/diagnostics.rs:85-95).
func DiagnosticSeverityFromLsp(value LspDiagnosticSeverity) DiagnosticSeverity {
	switch value {
	case LspDiagnosticSeverityError:
		return DiagnosticSeverityError
	case LspDiagnosticSeverityWarning:
		return DiagnosticSeverityWarning
	case LspDiagnosticSeverityInformation:
		return DiagnosticSeverityInfo
	case LspDiagnosticSeverityHint:
		return DiagnosticSeverityHint
	}
	return DiagnosticSeverityInfo
}

// Diagnostic is a message about a range of the text
// (U:input/editor/diagnostics.rs:17-52).
type Diagnostic struct {
	// Range is the position range the message applies to.
	Range LspRange
	// Severity is the diagnostic's severity.
	Severity DiagnosticSeverity
	// Code is the diagnostic's code; nil when omitted.
	Code            *string
	CodeDescription *LspCodeDescription
	// Source describes where the diagnostic comes from, e.g. 'typescript'.
	Source *string
	// Message is the diagnostic's message.
	Message string
	// RelatedInformation marks related locations, e.g. colliding names.
	RelatedInformation []DiagnosticRelatedInformation
	// Tags is additional metadata.
	Tags []DiagnosticTag
	// Data is preserved between textDocument/publishDiagnostics and
	// textDocument/codeAction.
	Data json.RawMessage
}

// NewDiagnostic is Diagnostic::new (U:input/editor/diagnostics.rs:98-104).
func NewDiagnostic(start, end InputPosition, message string) Diagnostic {
	return Diagnostic{Range: LspRange{Start: start, End: end}, Message: message}
}

// WithSeverity is U:input/editor/diagnostics.rs:106-109.
func (d Diagnostic) WithSeverity(severity DiagnosticSeverity) Diagnostic {
	d.Severity = severity
	return d
}

// WithCode is U:input/editor/diagnostics.rs:111-114.
func (d Diagnostic) WithCode(code string) Diagnostic {
	d.Code = &code
	return d
}

// WithSource is U:input/editor/diagnostics.rs:116-119.
func (d Diagnostic) WithSource(source string) Diagnostic {
	d.Source = &source
	return d
}

// DiagnosticFromLsp is From<lsp_types::Diagnostic>: an omitted severity
// is Info, a numeric code is its decimal string
// (U:input/editor/diagnostics.rs:54-74).
func DiagnosticFromLsp(value LspDiagnostic) Diagnostic {
	d := Diagnostic{
		Range:              value.Range,
		Severity:           DiagnosticSeverityInfo,
		CodeDescription:    value.CodeDescription,
		Source:             value.Source,
		Message:            value.Message,
		RelatedInformation: value.RelatedInformation,
		Tags:               value.Tags,
		Data:               value.Data,
	}
	if value.Severity != nil {
		d.Severity = DiagnosticSeverityFromLsp(*value.Severity)
	}
	if value.Code != nil {
		code := value.Code.String
		if value.Code.Number != nil {
			code = strconv.FormatInt(int64(*value.Code.Number), 10)
		}
		d.Code = &code
	}
	return d
}

// Equal is the derived PartialEq (U:input/editor/diagnostics.rs:17).
func (d Diagnostic) Equal(other Diagnostic) bool {
	return d.Range == other.Range && d.Severity == other.Severity &&
		equalStringPtr(d.Code, other.Code) && equalCodeDescription(d.CodeDescription, other.CodeDescription) &&
		equalStringPtr(d.Source, other.Source) && d.Message == other.Message &&
		slices.Equal(d.RelatedInformation, other.RelatedInformation) &&
		slices.Equal(d.Tags, other.Tags) && bytes.Equal(d.Data, other.Data)
}

func equalStringPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func equalCodeDescription(a, b *LspCodeDescription) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// DiagnosticEntry is a diagnostic with its byte range in the text
// (U:input/editor/diagnostics.rs:122-135). The Rust Deref to Diagnostic is
// the embedded field.
type DiagnosticEntry struct {
	// ByteRange is the byte range of the diagnostic in the rope (Rust's
	// `range`, renamed because the embedded Diagnostic has a Range).
	ByteRange InputSelection
	Diagnostic
}

// Equal is the derived PartialEq (U:input/editor/diagnostics.rs:122).
func (e DiagnosticEntry) Equal(other DiagnosticEntry) bool {
	return e.ByteRange == other.ByteRange && e.Diagnostic.Equal(other.Diagnostic)
}

// DiagnosticSummary is the sum-tree summary: the entry count and the byte
// range of the last entry (U:input/editor/diagnostics.rs:137-170).
type DiagnosticSummary struct {
	count int
	start int
	end   int
}

// AddSummary is U:input/editor/diagnostics.rs:165-169.
func (s DiagnosticSummary) AddSummary(other DiagnosticSummary) DiagnosticSummary {
	return DiagnosticSummary{count: s.count + other.count, start: other.start, end: other.end}
}

// Summary is U:input/editor/diagnostics.rs:144-153.
func (e DiagnosticEntry) Summary() DiagnosticSummary {
	return DiagnosticSummary{count: 1, start: e.ByteRange.Start, end: e.ByteRange.End}
}

// diagnosticOffset seeks by byte offset
// (U:input/editor/diagnostics.rs:172-183).
type diagnosticOffset int

func (o diagnosticOffset) Cmp(other DiagnosticSummary) int {
	switch {
	case int(o) < other.start:
		return -1
	case int(o) > other.end:
		return 1
	}
	return 0
}

// DiagnosticSet is the diagnostics of one text
// (U:input/editor/diagnostics.rs:185-262).
type DiagnosticSet struct {
	text        rope.Rope
	diagnostics sumtree.SumTree[DiagnosticEntry, DiagnosticSummary]
}

// NewDiagnosticSet is DiagnosticSet::new (U:input/editor/diagnostics.rs:
// 192-197).
func NewDiagnosticSet(text rope.Rope) *DiagnosticSet {
	return &DiagnosticSet{text: text}
}

// Reset takes the new text and drops every diagnostic
// (U:input/editor/diagnostics.rs:199-202).
func (s *DiagnosticSet) Reset(text rope.Rope) {
	s.text = text
	s.Clear()
}

// Push adds a diagnostic, converting its positions to byte offsets of the
// set's text (U:input/editor/diagnostics.rs:204-216).
func (s *DiagnosticSet) Push(diagnostic Diagnostic) {
	ext := RopeExtOf(&s.text)
	start := ext.PositionToOffset(diagnostic.Range.Start)
	end := ext.PositionToOffset(diagnostic.Range.End)
	s.diagnostics.Push(DiagnosticEntry{ByteRange: NewInputSelection(start, end), Diagnostic: diagnostic})
}

// PushLsp pushes an lsp_types::Diagnostic: push's `impl Into<Diagnostic>`
// (U:input/editor/diagnostics.rs:204).
func (s *DiagnosticSet) PushLsp(diagnostic LspDiagnostic) { s.Push(DiagnosticFromLsp(diagnostic)) }

// Extend pushes every diagnostic (U:input/editor/diagnostics.rs:218-226).
func (s *DiagnosticSet) Extend(diagnostics []Diagnostic) {
	for _, d := range diagnostics {
		s.Push(d)
	}
}

// Len is the number of diagnostics (U:input/editor/diagnostics.rs:228-230).
func (s *DiagnosticSet) Len() int { return s.diagnostics.Summary().count }

// Clear drops every diagnostic (U:input/editor/diagnostics.rs:232-234).
func (s *DiagnosticSet) Clear() {
	s.diagnostics = sumtree.New[DiagnosticEntry, DiagnosticSummary]()
}

// IsEmpty is U:input/editor/diagnostics.rs:236-238.
func (s *DiagnosticSet) IsEmpty() bool { return s.diagnostics.IsEmpty() }

// Range yields the entries from the first one the start does not pass
// while they start before the end (U:input/editor/diagnostics.rs:
// 240-252).
func (s *DiagnosticSet) Range(rng InputSelection) iter.Seq[DiagnosticEntry] {
	return func(yield func(DiagnosticEntry) bool) {
		cursor := sumtree.NewCursor[DiagnosticSummary](s.diagnostics)
		cursor.Seek(diagnosticOffset(rng.Start), sumtree.Left)
		for {
			entry, ok := cursor.Item()
			if !ok || entry.ByteRange.Start >= rng.End {
				return
			}
			cursor.Next()
			if !yield(entry) {
				return
			}
		}
	}
}

// ForOffset is the diagnostic at offset
// (U:input/editor/diagnostics.rs:254-256).
func (s *DiagnosticSet) ForOffset(offset int) (DiagnosticEntry, bool) {
	for entry := range s.Range(NewInputSelection(offset, offset+1)) {
		return entry, true
	}
	return DiagnosticEntry{}, false
}

// Iter yields every entry in push order
// (U:input/editor/diagnostics.rs:258-261).
func (s *DiagnosticSet) Iter() iter.Seq[DiagnosticEntry] { return s.diagnostics.Iter() }

// diagnostics is LayoutMode::diagnostics / diagnostics_mut: the code
// editor's set, nil on another layout (U:input/base/mode.rs:327-340).
func (m *layoutMode) diagnosticSet() *DiagnosticSet {
	if m.kind != layoutCodeEditor {
		return nil
	}
	return m.diagnostics
}

// resetDiagnostics is `if let Some(d) = self.mode.diagnostics_mut() {
// d.reset(&self.text) }`, run after every text change
// (U:input/base/state.rs:1220-1222, 3740-3742, 4087-4089, 4205-4207).
func (s *InputBaseState[M]) resetDiagnostics() {
	if d := s.mode.diagnosticSet(); d != nil {
		d.Reset(s.text)
	}
}

// Diagnostics is the code editor's diagnostic set; nil on an input or
// textarea (U:input/base/state.rs:864-867).
func (s *InputBaseState[M]) Diagnostics() *DiagnosticSet { return s.mode.diagnosticSet() }

// DiagnosticsMut is U:input/base/state.rs:869-872. Go has no shared
// reference, so it is the same pointer as Diagnostics.
func (s *InputBaseState[M]) DiagnosticsMut() *DiagnosticSet { return s.mode.diagnosticSet() }

// DiagnosticPopover is the diagnostic the pointer hovers, for the
// application to render; nil when none (U:input/base/state.rs:434-435,
// 558-560).
func (s *InputBaseState[M]) DiagnosticPopover() *DiagnosticEntry { return s.diagnosticPopover }

// diagnosticHighlightStyle is the wavy underline of a severity
// (U:input/base/element.rs:29-47).
func diagnosticHighlightStyle(severity DiagnosticSeverity, colors DiagnosticColors) gpui.HighlightStyle {
	var color gpui.Hsla
	switch severity {
	case DiagnosticSeverityError:
		color = colors.Error
	case DiagnosticSeverityWarning:
		color = colors.Warning
	case DiagnosticSeverityInfo:
		color = colors.Info
	default:
		color = colors.Hint
	}
	return gpui.HighlightStyle{Underline: &gpui.UnderlineStyle{Color: &color, Thickness: 1, Wavy: true}}
}
