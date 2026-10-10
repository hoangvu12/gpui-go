package gpui

// This file is ticket23's clipboard model and application surface,
// ported from the pinned CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/platform.rs:3279-3297 — ClipboardItem (the
//     entries vector) and ClipboardEntry:3310-3325 (String/Image/
//     ExternalPaths variants).
//   - crates/gpui/src/platform.rs:3282-3300 — ClipboardReadError
//     (Unavailable/Denied/UnsupportedContent) with the exact Display
//     strings.
//   - crates/gpui/src/platform.rs:3327-3404 — ClipboardItem::new_string,
//     new_string_with_metadata, new_string_with_json_metadata,
//     new_image, text (concatenation of all string entries with the
//     external-paths fallback), metadata (the single-string rule),
//     entries.
//   - crates/gpui/src/platform.rs:3683-3733 — ClipboardString (text +
//     Option<String> metadata) and text_hash: SeaHasher (seahash 4.1.0,
//     Cargo.lock) over Rust's Hash-for-str input.
//   - crates/gpui/src/interactive.rs:694 — ExternalPaths
//     (SmallVec<[PathBuf; 2]> with paths()).
//   - crates/gpui/src/platform.rs:339-351 — the Platform clipboard
//     surface: read_from_clipboard/write_to_clipboard, the
//     read_from_clipboard_async default (Task::ready of the sync read;
//     Windows does not override it) and the Linux/FreeBSD- and
//     macOS-cfg-gated primary selection and find pasteboard (absent on
//     Windows).
//   - crates/gpui_windows/src/platform.rs:821-827 — the Windows
//     platform delegates straight to the clipboard module
//     (gpui_windows/src/clipboard.rs), which clipboard_windows.go
//     ports; its test_clipboard corpus (~1577) is the round-trip
//     fixture this port's clipspec mirrors.
//
// The Win32 behavior lives in clipboard_windows.go (the Host methods
// over the lazy-proc syscall bindings); the non-Windows Host reports
// the unsupported-platform errors through win32host_other.go.

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ---------------------------------------------------------------------------
// Clipboard entries (crates/gpui/src/platform.rs:3310)
// ---------------------------------------------------------------------------

// ClipboardEntryKind discriminates one clipboard entry (the reference
// ClipboardEntry enum's variants).
type ClipboardEntryKind uint8

// Clipboard entry kinds.
const (
	// ClipboardEntryString is a text entry (ClipboardEntry::String).
	ClipboardEntryString ClipboardEntryKind = iota
	// ClipboardEntryImage is an image entry (ClipboardEntry::Image).
	ClipboardEntryImage
	// ClipboardEntryExternalPaths is a files entry
	// (ClipboardEntry::ExternalPaths).
	ClipboardEntryExternalPaths
)

// String renders the entry kind for diagnostics.
func (k ClipboardEntryKind) String() string {
	switch k {
	case ClipboardEntryString:
		return "string"
	case ClipboardEntryImage:
		return "image"
	case ClipboardEntryExternalPaths:
		return "external-paths"
	}
	return fmt.Sprintf("unknown(%d)", uint8(k))
}

// ClipboardString is a text entry with optional associated metadata
// (reference platform.rs:3683 ClipboardString).
//
// Go adaptation of Option<String> metadata: an empty Metadata string
// means None, following this package's established Option<SharedString>
// convention (see PathPromptOptions.Prompt). Consequence: writing
// Some("") metadata round-trips as None. The pinned corpus only uses
// non-empty JSON metadata.
type ClipboardString struct {
	// Text is the text content.
	Text string
	// Metadata is the associated metadata ("" = none).
	Metadata string
}

// NewClipboardString creates a clipboard string with no metadata
// (ClipboardString::new).
func NewClipboardString(text string) ClipboardString {
	return ClipboardString{Text: text}
}

// WithMetadata replaces the metadata (builder form of the reference's
// with_json_metadata tail: metadata set verbatim, no serialization).
func (s ClipboardString) WithMetadata(metadata string) ClipboardString {
	s.Metadata = metadata
	return s
}

// WithJSONMetadata replaces the metadata after serializing value as
// JSON (ClipboardString::with_json_metadata, which serde_json-serializes
// and unwraps). The Go adaptation surfaces the marshal error instead of
// panicking on it.
func (s ClipboardString) WithJSONMetadata(value any) (ClipboardString, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return s, fmt.Errorf("gpui: serializing clipboard metadata as JSON: %w", err)
	}
	s.Metadata = string(encoded)
	return s, nil
}

// UnmarshalMetadataJSON decodes the metadata JSON into value
// (ClipboardString::metadata_json, which returns None when metadata is
// absent OR unparseable). It reports false for both those cases; the
// reference also swallows the parse error, so does this port.
func (s ClipboardString) UnmarshalMetadataJSON(value any) bool {
	if s.Metadata == "" {
		return false
	}
	return json.Unmarshal([]byte(s.Metadata), value) == nil
}

// ExternalPaths is a file-list entry (reference interactive.rs:694:
// ExternalPaths(SmallVec<[PathBuf; 2]>)).
type ExternalPaths []string

// Paths returns the entry's paths (ExternalPaths::paths).
func (p ExternalPaths) Paths() []string { return p }

// ClipboardEntry is either a ClipboardString, an Image or a file list
// (reference ClipboardEntry). Exactly the field selected by Kind is
// meaningful.
type ClipboardEntry struct {
	// Kind selects the variant.
	Kind ClipboardEntryKind
	// String is the text entry (Kind == ClipboardEntryString).
	String ClipboardString
	// Image is the image entry (Kind == ClipboardEntryImage).
	Image Image
	// Paths is the files entry (Kind == ClipboardEntryExternalPaths).
	Paths ExternalPaths
}

// NewStringClipboardEntry wraps one clipboard string
// (From<ClipboardString> for ClipboardEntry).
func NewStringClipboardEntry(value ClipboardString) ClipboardEntry {
	return ClipboardEntry{Kind: ClipboardEntryString, String: value}
}

// NewImageClipboardEntry wraps one image (From<Image> for
// ClipboardEntry).
func NewImageClipboardEntry(value Image) ClipboardEntry {
	return ClipboardEntry{Kind: ClipboardEntryImage, Image: value}
}

// NewExternalPathsClipboardEntry wraps one file list (the read side's
// CF_HDROP entry shape).
func NewExternalPathsClipboardEntry(paths ExternalPaths) ClipboardEntry {
	return ClipboardEntry{Kind: ClipboardEntryExternalPaths, Paths: paths}
}

// ---------------------------------------------------------------------------
// ClipboardItem (crates/gpui/src/platform.rs:3279, 3327-3404)
// ---------------------------------------------------------------------------

// ClipboardItem is the content exchanged with the platform clipboard:
// one or more entries written together or read together.
type ClipboardItem struct {
	// Entries are the item's entries, in order.
	Entries []ClipboardEntry
}

// NewClipboardStringItem creates a string item with no metadata
// (ClipboardItem::new_string).
func NewClipboardStringItem(text string) ClipboardItem {
	return ClipboardItem{Entries: []ClipboardEntry{
		NewStringClipboardEntry(NewClipboardString(text)),
	}}
}

// NewClipboardStringItemWithMetadata creates a string item with the
// given text and metadata (ClipboardItem::new_string_with_metadata).
func NewClipboardStringItemWithMetadata(text, metadata string) ClipboardItem {
	return ClipboardItem{Entries: []ClipboardEntry{
		NewStringClipboardEntry(ClipboardString{Text: text, Metadata: metadata}),
	}}
}

// NewClipboardStringItemWithJSONMetadata creates a string item whose
// metadata is value serialized as JSON
// (ClipboardItem::new_string_with_json_metadata; the marshal error is
// surfaced instead of the reference's unwrap panic).
func NewClipboardStringItemWithJSONMetadata(text string, value any) (ClipboardItem, error) {
	clipboardString, err := NewClipboardString(text).WithJSONMetadata(value)
	if err != nil {
		return ClipboardItem{}, err
	}
	return ClipboardItem{Entries: []ClipboardEntry{
		NewStringClipboardEntry(clipboardString),
	}}, nil
}

// NewClipboardImageItem creates an image item with no metadata
// (ClipboardItem::new_image).
func NewClipboardImageItem(image Image) ClipboardItem {
	return ClipboardItem{Entries: []ClipboardEntry{
		NewImageClipboardEntry(image),
	}}
}

// EntriesOf returns the item's entries (ClipboardItem::entries).
func (item *ClipboardItem) EntriesOf() []ClipboardEntry { return item.Entries }

// Text concatenates all the string entries (ClipboardItem::text:
// push_str per entry — the pinned source concatenates WITHOUT a
// separator even when multiple string entries exist, and the read
// policy produces at most one string entry). When no string entry
// produced text, the external-path entries' displays are concatenated
// instead. The bool reports the reference's Some/None: false when no
// text was collected.
func (item *ClipboardItem) Text() (string, bool) {
	answer := ""
	for i := range item.Entries {
		if item.Entries[i].Kind == ClipboardEntryString {
			answer += item.Entries[i].String.Text
		}
	}
	if answer == "" {
		for i := range item.Entries {
			if item.Entries[i].Kind == ClipboardEntryExternalPaths {
				for _, path := range item.Entries[i].Paths {
					answer += path
				}
			}
		}
	}
	if answer != "" {
		return answer, true
	}
	return "", false
}

// Metadata returns the metadata when this item is exactly one string
// entry (ClipboardItem::metadata: entries.len() == 1 and the first
// entry is a String). The bool reports Some/None.
func (item *ClipboardItem) Metadata() (string, bool) {
	if len(item.Entries) != 1 || item.Entries[0].Kind != ClipboardEntryString {
		return "", false
	}
	metadata := item.Entries[0].String.Metadata
	return metadata, metadata != ""
}

// ---------------------------------------------------------------------------
// ClipboardReadError (crates/gpui/src/platform.rs:3282-3300)
// ---------------------------------------------------------------------------

// ClipboardReadErrorKind selects the read failure variant.
type ClipboardReadErrorKind uint8

// Clipboard read error kinds.
const (
	// ClipboardReadUnavailable is ClipboardReadError::Unavailable: the
	// platform clipboard is not available in this context.
	ClipboardReadUnavailable ClipboardReadErrorKind = iota
	// ClipboardReadDenied is ClipboardReadError::Denied: the platform
	// refused access, carrying a message.
	ClipboardReadDenied
	// ClipboardReadUnsupportedContent is
	// ClipboardReadError::UnsupportedContent: the contents could not be
	// converted into a ClipboardItem.
	ClipboardReadUnsupportedContent
)

// ClipboardReadError is the typed clipboard read failure
// (reference ClipboardReadError). Go adaptation of the Rust enum: a
// kind plus the Denied variant's message.
type ClipboardReadError struct {
	// Kind selects the variant.
	Kind ClipboardReadErrorKind
	// Message is the Denied variant's message.
	Message string
}

// Error renders the reference Display strings exactly.
func (e *ClipboardReadError) Error() string {
	switch e.Kind {
	case ClipboardReadUnavailable:
		return "the clipboard is unavailable"
	case ClipboardReadDenied:
		return "clipboard access was denied: " + e.Message
	case ClipboardReadUnsupportedContent:
		return "the clipboard contents are unsupported"
	}
	return fmt.Sprintf("unknown clipboard read error kind %d", uint8(e.Kind))
}

// ---------------------------------------------------------------------------
// The clipboard text hash (crates/gpui/src/platform.rs:3726-3732)
// ---------------------------------------------------------------------------

// textHash computes the clipboard text-change hash
// (ClipboardString::text_hash: `let mut hasher = SeaHasher::new();
// text.hash(&mut hasher); hasher.finish()`).
//
// The pinned seahash is 4.1.0 (Cargo.lock). The algorithm ported here:
//
//   - input: Rust's Hash for str writes the UTF-8 bytes followed by
//     std's 0xff str terminator (Hash for str -> Hasher::write_str ->
//     write(bytes) + write_u8(0xff)); the streaming hasher buffers
//     write_u8 like any other byte.
//   - state: the four fixed seed lanes
//     0x16f11fe9b0dc5695 / 0xb365a3889309ca41 / 0x9c21ad8d455e0be8 /
//     0x5e4a5fbe8c3e3a75.
//   - each 32-byte block (four little-endian u64 words w0..w3) updates
//     the lanes as diffuse(lane ^ word), where diffuse is the SeaHash
//     round: multiply by 0x6eed0e9da4d94a4f, XOR-fold bits [32..64)
//     shifted by bits [60..64) back in, multiply again.
//   - a sub-32-byte tail is zero-padded into one final block.
//   - the total input length is folded into lane A
//     (a = diffuse(a ^ len)) and the digest is A ^ B ^ C ^ D.
//
// Documented fidelity caveat: the seahash crate source is not vendored
// in this checkout, so the tail padding and the exact length fold are
// the best reading of 4.1.0's documented algorithm, not an
// oracle-verified reproduction. Within this port the hash only gates
// the metadata/text association between its own write and read
// (write_string stores it, read_clipboard_metadata compares it), so
// self-consistency is the load-bearing property; byte parity with a
// Rust CE build writing "GPUI internal text hash" data is a recorded
// limitation until the crate's test vectors are pinned.
func textHash(text string) uint64 {
	// The 0xff terminator rides the same byte stream as the text.
	return seahashSum(len(text)+1, func(i int) byte {
		if i < len(text) {
			return text[i]
		}
		return 0xff
	})
}

// seahashSum is the streaming SeaHasher over size bytes, where byte i
// comes from at(i). A closure avoids materializing the text + 0xff
// copy for the common no-metadata write path's hash computation.
func seahashSum(size int, at func(int) byte) uint64 {
	a := uint64(0x16f11fe9b0dc5695)
	b := uint64(0xb365a3889309ca41)
	c := uint64(0x9c21ad8d455e0be8)
	d := uint64(0x5e4a5fbe8c3e3a75)

	word := func(i int) uint64 {
		var buf [8]byte
		for j := range buf {
			buf[j] = at(i + j)
		}
		return le64(buf[:])
	}

	i := 0
	for ; size-i >= 32; i += 32 {
		a = seahashDiffuse(a ^ word(i))
		b = seahashDiffuse(b ^ word(i+8))
		c = seahashDiffuse(c ^ word(i+16))
		d = seahashDiffuse(d ^ word(i+24))
	}
	if size-i > 0 {
		var block [32]byte
		for j := 0; i+j < size; j++ {
			block[j] = at(i + j)
		}
		a = seahashDiffuse(a ^ le64(block[0:]))
		b = seahashDiffuse(b ^ le64(block[8:]))
		c = seahashDiffuse(c ^ le64(block[16:]))
		d = seahashDiffuse(d ^ le64(block[24:]))
	}
	// Fold the total length in so messages differing only by trailing
	// zero padding stay distinct.
	a = seahashDiffuse(a ^ uint64(size))
	return a ^ b ^ c ^ d
}

// seahashDiffuse is the SeaHash round function.
func seahashDiffuse(x uint64) uint64 {
	const m = 0x6eed0e9da4d94a4f
	x *= m
	fold := x >> 32
	shift := x >> 60
	x ^= fold >> shift
	return x * m
}

// le64 decodes a little-endian uint64.
func le64(b []byte) uint64 {
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

// ---------------------------------------------------------------------------
// The application clipboard surface (crates/gpui/src/platform.rs:339-351)
// ---------------------------------------------------------------------------

// ReadClipboard reads the system clipboard (Platform::read_from_clipboard;
// the Windows platform delegates to the module clipboard.rs ports in
// clipboard_windows.go). It runs on the host (foreground) thread through
// the wake marshal.
//
// Go adaptation of the reference's Option<ClipboardItem>: a nil item
// with a nil error is the None outcome (no readable clipboard
// content); failures are typed errors — ErrNoHost for a test
// application, ErrHostStopped after the host loop exited, and a
// *ClipboardReadError (Denied) when OpenClipboard fails, where the
// pinned reference would log and return None.
func (a *App) ReadClipboard() (*ClipboardItem, error) {
	h, err := a.desktopHost()
	if err != nil {
		return nil, err
	}
	return h.ReadClipboard()
}

// WriteClipboard writes the item to the system clipboard
// (Platform::write_to_clipboard). The whole write — EmptyClipboard,
// the per-entry formats — runs on the host (foreground) thread.
//
// Go adaptation: the reference returns () and logs failures; this port
// surfaces them (the busy clipboard as *ClipboardReadError (Denied),
// Win32 failures wrapped). ExternalPaths entries are skipped on write
// exactly like the reference.
func (a *App) WriteClipboard(item ClipboardItem) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	return h.WriteClipboard(item)
}

// ClipboardReadOutcome is the async read result, the Go shape of the
// reference's Result<Option<ClipboardItem>, ClipboardReadError>.
type ClipboardReadOutcome struct {
	// Item is the read item (nil = Ok(None)).
	Item *ClipboardItem
	// Err is the ClipboardReadError (nil = Ok).
	Err *ClipboardReadError
}

// ReadClipboardAsync reads the clipboard through a task
// (Platform::read_from_clipboard_async). Windows does not override the
// trait default, which is Task::ready(Ok(self.read_from_clipboard())):
// the SYNC read is evaluated eagerly and the returned task is already
// complete. Await it from a foreground task body (AsyncApp.Await); its
// Done() is true immediately.
func (a *App) ReadClipboardAsync() Task[ClipboardReadOutcome] {
	item, err := a.ReadClipboard()
	return a.newReadyClipboardTask(ClipboardReadOutcome{
		Item: item,
		Err:  asClipboardReadError(err),
	})
}

// asClipboardReadError maps a sync-read error into the async error
// vocabulary: *ClipboardReadError passes through, and environment
// failures (no host, stopped host) map to Unavailable, matching the
// reference's "the clipboard is not available in this context" variant.
func asClipboardReadError(err error) *ClipboardReadError {
	if err == nil {
		return nil
	}
	var readErr *ClipboardReadError
	if errors.As(err, &readErr) {
		return readErr
	}
	return &ClipboardReadError{Kind: ClipboardReadUnavailable}
}

// newReadyClipboardTask wraps value in an already-completed foreground
// task (the Go analogue of the reference's Task::ready; the runtime has
// no ready-task constructor, so this builds a task that never starts a
// worker: it is done at construction and its result is readable
// through Task awaits exactly like a completed spawned task).
func (a *App) newReadyClipboardTask(value ClipboardReadOutcome) Task[ClipboardReadOutcome] {
	t := a.sched.newTask(true)
	t.done = true
	t.result = value
	return Task[ClipboardReadOutcome]{t: t}
}

// ---------------------------------------------------------------------------
// Primary selection and find pasteboard (platform.rs:344-351)
// ---------------------------------------------------------------------------

// ErrPrimarySelectionUnavailable is the pinned Windows outcome of the
// primary-selection surface: the reference cfg-gates
// read_from_primary/write_from_primary to Linux and FreeBSD, so the
// methods do not exist on the Windows platform at all. Go compiles one
// cross-platform surface, so the port reports the explicit unavailable
// outcome instead of compiling the call away.
var ErrPrimarySelectionUnavailable = errors.New("gpui: primary selection is unavailable on Windows (read_from_primary/write_from_primary are cfg-gated to Linux/FreeBSD in the pinned platform trait)")

// ErrFindPasteboardUnavailable is the pinned Windows outcome of the
// find-pasteboard surface: read_from_find_pasteboard/
// write_to_find_pasteboard are cfg-gated to macOS in the pinned
// platform trait.
var ErrFindPasteboardUnavailable = errors.New("gpui: the find pasteboard is unavailable on Windows (read_from_find_pasteboard/write_to_find_pasteboard are cfg-gated to macOS in the pinned platform trait)")

// ReadPrimary reads the primary selection (Platform::read_from_primary).
// The pinned Windows behavior is the explicit unavailable outcome.
func (a *App) ReadPrimary() (*ClipboardItem, error) {
	return nil, ErrPrimarySelectionUnavailable
}

// WritePrimary writes the primary selection
// (Platform::write_to_primary). The pinned Windows behavior is the
// explicit unavailable outcome: nothing is written.
func (a *App) WritePrimary(item ClipboardItem) error {
	return ErrPrimarySelectionUnavailable
}

// ReadFindPasteboard reads the find pasteboard
// (Platform::read_from_find_pasteboard). The pinned Windows behavior
// is the explicit unavailable outcome.
func (a *App) ReadFindPasteboard() (*ClipboardItem, error) {
	return nil, ErrFindPasteboardUnavailable
}

// WriteFindPasteboard writes the find pasteboard
// (Platform::write_to_find_pasteboard). The pinned Windows behavior is
// the explicit unavailable outcome: nothing is written.
func (a *App) WriteFindPasteboard(item ClipboardItem) error {
	return ErrFindPasteboardUnavailable
}
