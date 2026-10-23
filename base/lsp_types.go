package base

// Hand-written lsp-types 0.97.0 ("proposed" feature, the version Base's
// Cargo.lock pins) for the data Base exchanges with its LSP providers
// (ticket 01: recreate in-repo; research deps-03 §3). Cited as
// L:<file>:<line> = lsp-types-0.97.0/src.
//
// Only the fields Base reads or writes are kept; a struct whose Base use
// is opaque (CodeAction) keeps its one required field. There is no
// transport in Base, so there are no JSON tags.
//
// Go shapes:
//
//   - Every type takes the Lsp prefix: lsp_types is a public dependency of
//     Base's API (users build Hover, CompletionItem, ... values), and the
//     bare names (Range, Color, Hover, Location) would collide in this
//     package.
//   - lsp_types::Position is InputPosition (input_rope_ext.go), as Base
//     re-exports it (U:input/mod.rs:113).
//   - lsp_types::Uri (a fluent_uri::Uri newtype) is its string form.
//   - Option<T> is a pointer, or a nil slice for Option<Vec<T>> (ticket 19
//     convention). serde_json::Value is json.RawMessage.
//   - The untagged unions (NumberOrString, CompletionTextEdit,
//     CompletionResponse, HoverContents, MarkedString,
//     InlineCompletionResponse) are structs whose set pointer selects the
//     variant; each has constructors for its variants.
//   - The i32 newtype enums (DiagnosticSeverity, DiagnosticTag,
//     CompletionTriggerKind, InlineCompletionTriggerKind) are named int32
//     types with their constants.

import "encoding/json"

// LspRange is lsp_types::Range (L:lib.rs:260): a zero-based,
// end-exclusive range of positions.
type LspRange struct {
	Start InputPosition
	End   InputPosition
}

// NewLspRange is Range::new (L:lib.rs:268).
func NewLspRange(start, end InputPosition) LspRange { return LspRange{Start: start, End: end} }

// LspLocation is lsp_types::Location (L:lib.rs:275).
type LspLocation struct {
	URI   string
	Range LspRange
}

// LspLocationLink is lsp_types::LocationLink (L:lib.rs:289): a link
// from an origin range to a target location.
type LspLocationLink struct {
	// OriginSelectionRange is the span of the origin of this link; nil
	// uses the word range at the mouse position.
	OriginSelectionRange *LspRange
	// TargetURI is the target resource identifier.
	TargetURI string
	// TargetRange is the full target range, e.g. a function's body.
	TargetRange LspRange
	// TargetSelectionRange is the range to select and reveal, e.g. the
	// function name; contained in TargetRange.
	TargetSelectionRange LspRange
}

// LspShowDocumentParams is lsp_types::ShowDocumentParams
// (L:window.rs:138), the window/showDocument request.
type LspShowDocumentParams struct {
	// URI is the uri to show.
	URI string
	// External shows the resource in an external program (a browser).
	External *bool
	// TakeFocus asks the client to take focus.
	TakeFocus *bool
	// Selection is the range to select when the document is a text file.
	Selection *LspRange
}

// LspTextEdit is lsp_types::TextEdit (L:lib.rs:524).
type LspTextEdit struct {
	Range   LspRange
	NewText string
}

// LspNumberOrString is lsp_types::NumberOrString (L:lib.rs:205):
// Number when Number is set, else String.
type LspNumberOrString struct {
	Number *int32
	String string
}

// LspNumberOrStringNumber is NumberOrString::Number.
func LspNumberOrStringNumber(n int32) LspNumberOrString { return LspNumberOrString{Number: &n} }

// LspNumberOrStringString is NumberOrString::String.
func LspNumberOrStringString(s string) LspNumberOrString { return LspNumberOrString{String: s} }

// LspDiagnosticSeverity is lsp_types::DiagnosticSeverity
// (L:lib.rs:447).
type LspDiagnosticSeverity int32

// The LSP diagnostic severities (L:lib.rs:450-457).
const (
	LspDiagnosticSeverityError       LspDiagnosticSeverity = 1
	LspDiagnosticSeverityWarning     LspDiagnosticSeverity = 2
	LspDiagnosticSeverityInformation LspDiagnosticSeverity = 3
	LspDiagnosticSeverityHint        LspDiagnosticSeverity = 4
)

// LspDiagnosticTag is lsp_types::DiagnosticTag (L:lib.rs:476).
type LspDiagnosticTag int32

// The LSP diagnostic tags (L:lib.rs:478-487).
const (
	// LspDiagnosticTagUnnecessary is unused or unnecessary code.
	LspDiagnosticTagUnnecessary LspDiagnosticTag = 1
	// LspDiagnosticTagDeprecated is deprecated or obsolete code.
	LspDiagnosticTagDeprecated LspDiagnosticTag = 2
)

// LspCodeDescription is lsp_types::CodeDescription (L:lib.rs:402).
type LspCodeDescription struct {
	Href string
}

// LspDiagnosticRelatedInformation is lsp_types::DiagnosticRelatedInformation
// (L:lib.rs:465).
type LspDiagnosticRelatedInformation struct {
	Location LspLocation
	Message  string
}

// LspDiagnostic is lsp_types::Diagnostic (L:lib.rs:356), converted
// into Base's Diagnostic by DiagnosticFromLsp.
type LspDiagnostic struct {
	Range              LspRange
	Severity           *LspDiagnosticSeverity
	Code               *LspNumberOrString
	CodeDescription    *LspCodeDescription
	Source             *string
	Message            string
	RelatedInformation []LspDiagnosticRelatedInformation
	Tags               []LspDiagnosticTag
	Data               json.RawMessage
}

// LspCompletionTriggerKind is lsp_types::CompletionTriggerKind
// (L:completion.rs:395).
type LspCompletionTriggerKind int32

// The completion trigger kinds (L:completion.rs:396-404).
const (
	LspCompletionTriggerKindInvoked                         LspCompletionTriggerKind = 1
	LspCompletionTriggerKindTriggerCharacter                LspCompletionTriggerKind = 2
	LspCompletionTriggerKindTriggerForIncompleteCompletions LspCompletionTriggerKind = 3
)

// LspCompletionContext is lsp_types::CompletionContext
// (L:completion.rs:382).
type LspCompletionContext struct {
	TriggerKind      LspCompletionTriggerKind
	TriggerCharacter *string
}

// LspInsertReplaceEdit is lsp_types::InsertReplaceEdit
// (L:completion.rs:248).
type LspInsertReplaceEdit struct {
	NewText string
	Insert  LspRange
	Replace LspRange
}

// LspCompletionTextEdit is lsp_types::CompletionTextEdit
// (L:completion.rs:261): Edit or InsertAndReplace, whichever is set.
type LspCompletionTextEdit struct {
	Edit             *LspTextEdit
	InsertAndReplace *LspInsertReplaceEdit
}

// LspCompletionItem is lsp_types::CompletionItem (L:completion.rs:419)
// with the fields Base reads (U:input/editor/lsp/overlay.rs:166-197).
type LspCompletionItem struct {
	// Label is the text shown in the menu, and the text inserted when
	// there is no InsertText or TextEdit.
	Label string
	// InsertText is inserted at the cursor when TextEdit is nil.
	InsertText *string
	// TextEdit replaces its range with its new text.
	TextEdit *LspCompletionTextEdit
}

// LspCompletionList is lsp_types::CompletionList (L:completion.rs:408)
// with the field Base reads.
type LspCompletionList struct {
	Items []LspCompletionItem
}

// LspCompletionResponse is lsp_types::CompletionResponse
// (L:completion.rs:345): List when it is set, else Array.
type LspCompletionResponse struct {
	Array []LspCompletionItem
	List  *LspCompletionList
}

// LspCompletionResponseArray is CompletionResponse::Array.
func LspCompletionResponseArray(items []LspCompletionItem) LspCompletionResponse {
	return LspCompletionResponse{Array: items}
}

// LspCompletionResponseList is CompletionResponse::List.
func LspCompletionResponseList(list LspCompletionList) LspCompletionResponse {
	return LspCompletionResponse{List: &list}
}

// items is the response's items, either variant (U:input/editor/lsp/
// completions.rs:187-190).
func (r LspCompletionResponse) items() []LspCompletionItem {
	if r.List != nil {
		return r.List.Items
	}
	return r.Array
}

// LspInlineCompletionTriggerKind is lsp_types::InlineCompletionTriggerKind
// (L:inline_completion.rs:62).
type LspInlineCompletionTriggerKind int32

// The inline completion trigger kinds (L:inline_completion.rs:63-72).
const (
	LspInlineCompletionTriggerKindInvoked   LspInlineCompletionTriggerKind = 1
	LspInlineCompletionTriggerKindAutomatic LspInlineCompletionTriggerKind = 2
)

// LspInlineCompletionContext is lsp_types::InlineCompletionContext
// (L:inline_completion.rs:93). Base always sends no selected
// completion info (U:input/editor/lsp/completions.rs:297-300), so that
// field is not kept.
type LspInlineCompletionContext struct {
	TriggerKind LspInlineCompletionTriggerKind
}

// LspInlineCompletionItem is lsp_types::InlineCompletionItem
// (L:inline_completion.rs:135) with the field Base reads.
type LspInlineCompletionItem struct {
	// InsertText is the text inserted at the cursor, shown as ghost text
	// until accepted.
	InsertText string
}

// LspInlineCompletionList is lsp_types::InlineCompletionList
// (L:inline_completion.rs:124).
type LspInlineCompletionList struct {
	Items []LspInlineCompletionItem
}

// LspInlineCompletionResponse is lsp_types::InlineCompletionResponse
// (L:inline_completion.rs:115): List when it is set, else Array.
type LspInlineCompletionResponse struct {
	Array []LspInlineCompletionItem
	List  *LspInlineCompletionList
}

// items is the response's items, either variant (U:input/editor/lsp/
// completions.rs:322-325).
func (r LspInlineCompletionResponse) items() []LspInlineCompletionItem {
	if r.List != nil {
		return r.List.Items
	}
	return r.Array
}

// LspMarkupKind is lsp_types::MarkupKind (L:lib.rs:2592).
type LspMarkupKind uint8

// The markup kinds.
const (
	LspMarkupKindPlainText LspMarkupKind = iota
	LspMarkupKindMarkdown
)

// LspMarkupContent is lsp_types::MarkupContent (L:lib.rs:2623).
type LspMarkupContent struct {
	Kind  LspMarkupKind
	Value string
}

// LspMarkedString is lsp_types::MarkedString (L:lib.rs:2466-2476):
// LanguageString{Language, Value} when Language is set, else String(Value).
type LspMarkedString struct {
	Language *string
	Value    string
}

// LspMarkedStringString is MarkedString::String.
func LspMarkedStringString(value string) LspMarkedString { return LspMarkedString{Value: value} }

// LspMarkedStringLanguageString is MarkedString::LanguageString.
func LspMarkedStringLanguageString(language, value string) LspMarkedString {
	return LspMarkedString{Language: &language, Value: value}
}

// LspHoverContents is lsp_types::HoverContents (L:hover.rs:82): the
// set field is the variant (Scalar, Array, Markup).
type LspHoverContents struct {
	Scalar *LspMarkedString
	Array  []LspMarkedString
	Markup *LspMarkupContent
}

// LspHoverContentsScalar is HoverContents::Scalar.
func LspHoverContentsScalar(s LspMarkedString) LspHoverContents {
	return LspHoverContents{Scalar: &s}
}

// LspHoverContentsArray is HoverContents::Array.
func LspHoverContentsArray(items []LspMarkedString) LspHoverContents {
	return LspHoverContents{Array: items}
}

// LspHoverContentsMarkup is HoverContents::Markup.
func LspHoverContentsMarkup(m LspMarkupContent) LspHoverContents {
	return LspHoverContents{Markup: &m}
}

// LspHover is lsp_types::Hover (L:hover.rs:70).
type LspHover struct {
	Contents LspHoverContents
	// Range is the hovered range; nil uses the word at the offset.
	Range *LspRange
}

// LspCodeAction is lsp_types::CodeAction (L:code_action.rs:238).
// Base passes it through to its provider unread; Title is the one field
// the protocol requires.
type LspCodeAction struct {
	Title string
}

// LspColor is lsp_types::Color (L:color.rs:74): sRGB components in
// [0, 1].
type LspColor struct {
	Red   float32
	Green float32
	Blue  float32
	Alpha float32
}

// LspColorInformation is lsp_types::ColorInformation (L:color.rs:65).
type LspColorInformation struct {
	Range LspRange
	Color LspColor
}

// LspSemanticToken is lsp_types::SemanticToken (L:semantic_tokens.rs:147):
// one delta-encoded token.
type LspSemanticToken struct {
	DeltaLine            uint32
	DeltaStart           uint32
	Length               uint32
	TokenType            uint32
	TokenModifiersBitset uint32
}

// LspSemanticTokens is lsp_types::SemanticTokens (L:semantic_tokens.rs:234)
// with the field Base reads.
type LspSemanticTokens struct {
	Data []LspSemanticToken
}

// LspSemanticTokensLegend is lsp_types::SemanticTokensLegend
// (L:semantic_tokens.rs:137) with the field Base reads. A
// SemanticTokenType is its name, e.g. "keyword" (SemanticTokenType::as_str).
type LspSemanticTokensLegend struct {
	TokenTypes []string
}
