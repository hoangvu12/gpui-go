//go:build windows

package clipspec

// Ticket 26's clipboard gates against the REAL system clipboard: the
// CF_HTML format's round trip (the "HTML Format" registered format,
// Chromium's HtmlToCFHtml/CFHtmlToHTML, written and read through the
// host), the round trip rerun under contention (another window holding
// the clipboard — the port's bounded OpenClipboard retry with the
// clear error), the paste edit action's real end-to-end flow (the
// parity fixture pins the event order; this gate pins the clipboard
// half), and the clipboard-change observer (AddClipboardFormatListener
// / WM_CLIPBOARDUPDATE).
//
// WARNING: like the ticket23 corpus, these tests OPEN AND OVERWRITE
// the machine's real system clipboard; each test saves the previous
// contents and restores them at cleanup (the machine's formats are
// best-effort: the text, metadata and HTML entries round-trip; images
// and file lists do not — an empty previous board is restored as an
// empty clipboard).

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/hoangvu12/gpui-go/gpui"
)

// ---------------------------------------------------------------------------
// The save/restore helper
// ---------------------------------------------------------------------------

// savedClipboard is the previous board's restore closure.
type savedClipboard struct {
	item   *gpui.ClipboardItem
	exists bool
}

// saveClipboard reads the current board (tolerating transient viewer
// contention) for a later restore.
func saveClipboard(t *testing.T, h *gpui.Host) savedClipboard {
	t.Helper()
	item := hostRead(t, h)
	return savedClipboard{item: item, exists: item != nil}
}

// restore writes the saved board back (EmptyClipboard first, so a
// None-previous board does not keep the test's formats).
func (s savedClipboard) restore(t *testing.T, h *gpui.Host) {
	t.Helper()
	foreignOpenWithRetry(t)
	if r, _, _ := foreignEmptyClipboard.Call(); r == 0 {
		t.Fatalf("foreign EmptyClipboard during restore failed")
	}
	foreignCloseClipboard.Call()
	if s.exists {
		hostWrite(t, h, *s.item)
	}
}

// withSavedClipboard runs f with the previous board saved and restored
// around it.
func withSavedClipboard(t *testing.T, h *gpui.Host, f func()) {
	t.Helper()
	saved := saveClipboard(t, h)
	f()
	saved.restore(t, h)
}

// ---------------------------------------------------------------------------
// The CF_HTML round trip
// ---------------------------------------------------------------------------

// requireHTMLItem asserts the item is one string entry with the given
// text and HTML ("" = none).
func requireHTMLItem(t *testing.T, item *gpui.ClipboardItem, text, html string) {
	t.Helper()
	if item == nil {
		t.Fatalf("read clipboard item = nil, want one string entry %q/%q", text, html)
	}
	if len(item.Entries) != 1 || item.Entries[0].Kind != gpui.ClipboardEntryString {
		t.Fatalf("read item entries = %+v, want exactly one string entry", item.Entries)
	}
	got := item.Entries[0].String
	if got.Text != text || got.HTML != html {
		t.Fatalf("string entry = text %q html %q, want text %q html %q", got.Text, got.HTML, text, html)
	}
}

// TestClipboardHTMLFormatRoundTrip writes and reads the HTML format
// through the host: the entry's HTML companion publishes as the
// "HTML Format" registered format (Chromium's WriteHTML → HtmlToCFHtml
// with the header offsets), the read parses it back (CFHtmlToHTML),
// and the raw published bytes carry the CF_HTML header shape the
// offsets describe. The plain-text write is unchanged beside it.
func TestClipboardHTMLFormatRoundTrip(t *testing.T) {
	h := newHost(t)
	withSavedClipboard(t, h, func() {
		const text = "plain 你好 text"
		const html = "<b>plain</b> 你好 <i>text</i>"
		hostWrite(t, h, gpui.ClipboardItem{Entries: []gpui.ClipboardEntry{
			gpui.NewStringClipboardEntry(gpui.NewClipboardString(text).WithHTML(html)),
		}})
		item := hostRead(t, h)
		requireHTMLItem(t, item, text, html)

		// The raw published CF_HTML bytes: the "HTML Format" data
		// parses back to the fragment and carries the header shape.
		htmlFormat := foreignRegisterFormat(t, "HTML Format")
		raw, ok := foreignFormatData(t, htmlFormat)
		if !ok {
			t.Fatal("the HTML Format data is not on the clipboard after the HTML write")
		}
		payload := strings.TrimRight(string(raw), "\x00")
		if !strings.HasPrefix(payload, "Version:0.9\r\n") {
			t.Fatalf("CF_HTML payload does not start with the header: %q", payload[:20])
		}
		// The header's four offsets land where the markup says (the
		// port's own reader checks them by extracting the fragment).
		meta := cfHTMLMetadataOf(t, payload)
		if meta.fragmentStart < 105 || meta.fragmentEnd > len(payload) {
			t.Fatalf("CF_HTML fragment offsets = %d..%d (payload %d bytes)", meta.fragmentStart, meta.fragmentEnd, len(payload))
		}
		if got := payload[meta.fragmentStart:meta.fragmentEnd]; got != html {
			t.Fatalf("CF_HTML fragment = %q, want %q", got, html)
		}
		// The text write is unchanged beside the HTML.
		plain, ok := foreignFormatData(t, cfUnicodeText)
		if !ok {
			t.Fatal("CF_UNICODETEXT is not on the clipboard after the HTML write")
		}
		if text := utf16BytesToString(t, plain); text != "plain 你好 text" {
			t.Fatalf("CF_UNICODETEXT = %q, want %q", text, "plain 你好 text")
		}

		// A foreign CF_HTML (Chrome's own shape, written by another
		// process's copy) is read back as the HTML companion of the
		// text entry.
		foreignSetBytes(t, cfUnicodeText, utf16BytesOf(t, "foreign text"), true)
		foreignSetBytes(t, htmlFormat, []byte(chromeShapeCFHTML(t, "<em>foreign</em> html")+"\x00"), false)
		item = hostRead(t, h)
		requireHTMLItem(t, item, "foreign text", "<em>foreign</em> html")

		// A text-only write clears the HTML companion (an EmptyClipboard
		// write republishes only the formats the item carries).
		hostWrite(t, h, gpui.NewClipboardStringItem("text only"))
		item = hostRead(t, h)
		requireHTMLItem(t, item, "text only", "")
		clipboardStillOpenable(t)
	})
}

// TestClipboardHTMLFormatRoundTripUnderContention reruns the round
// trip while another window repeatedly holds the clipboard open (the
// machine's clipboard-viewer contention — the shape that flaked the
// ticket23 corpus): the port's bounded OpenClipboard retry (Chromium's
// 5 attempts with a 5 ms backoff, clipboard_win.cc:102-130) absorbs
// the short holds, so the write and the read still round-trip.
func TestClipboardHTMLFormatRoundTripUnderContention(t *testing.T) {
	h := newHost(t)
	withSavedClipboard(t, h, func() {
		const text = "contended round trip"
		const html = "<u>contended</u>"
		item := gpui.ClipboardItem{Entries: []gpui.ClipboardEntry{
			gpui.NewStringClipboardEntry(gpui.NewClipboardString(text).WithHTML(html)),
		}}

		// The churn: a real owner window holds the clipboard for 12 ms
		// every 30 ms, from a locked OS thread (holdClipboard's
		// discipline). The port's retry budget (5 attempts spanning
		// ~25 ms) rides through each hold.
		_, hwnd := holdWindow(t, h)
		stop := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if r, _, _ := foreignOpenClipboard.Call(hwnd); r != 0 {
					time.Sleep(12 * time.Millisecond)
					foreignCloseClipboard.Call()
				}
				time.Sleep(18 * time.Millisecond)
			}
		}()
		defer func() {
			close(stop)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("the contention goroutine did not stop")
			}
		}()

		// The round trip under churn: several write/read cycles must
		// all succeed through the retry.
		for i := 0; i < 3; i++ {
			hostWrite(t, h, item)
			got := hostRead(t, h)
			requireHTMLItem(t, got, text, html)
		}
		clipboardStillOpenable(t)
	})
}

// TestClipboardBusyRetryClearError pins the retry's failure side: a
// clipboard held open (the deterministic owner-window hold) exhausts
// the port's 5 OpenClipboard attempts and reports the typed Denied
// error with a message that names the attempts (the clear error of
// the ticket's acceptance) — fast, with no hang — and normal
// operation resumes after the holder releases.
func TestClipboardBusyRetryClearError(t *testing.T) {
	h := newHost(t)
	_, hwnd := holdWindow(t, h)
	saved := saveClipboard(t, h)
	t.Cleanup(func() { saved.restore(t, h) })

	hostWrite(t, h, gpui.NewClipboardStringItem("before-busy"))

	hold := holdClipboard(t, hwnd)
	start := time.Now()
	_, err := h.ReadClipboard()
	if err == nil {
		t.Fatal("read during a held clipboard succeeded; want the typed Denied error")
	}
	requireDenied(t, "read", err)
	if msg := err.Error(); !strings.Contains(msg, "5 attempts") {
		t.Fatalf("busy read error = %q, want the retry attempt count in the message", msg)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("busy read took %v; the retry budget is ~25 ms", elapsed)
	}

	start = time.Now()
	err = h.WriteClipboard(gpui.NewClipboardStringItem("during-busy"))
	if err == nil {
		t.Fatal("write during a held clipboard succeeded; want the typed Denied error")
	}
	requireDenied(t, "write", err)
	if msg := err.Error(); !strings.Contains(msg, "5 attempts") {
		t.Fatalf("busy write error = %q, want the retry attempt count in the message", msg)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("busy write took %v", elapsed)
	}
	hold.release()

	// The pre-busy write survived; normal operation resumes (the
	// save/restore around the whole test returns the user's board at
	// the end).
	item := hostRead(t, h)
	requireStringItem(t, item, "before-busy", "")
	hostWrite(t, h, gpui.NewClipboardStringItem("after-busy"))
	item = hostRead(t, h)
	requireStringItem(t, item, "after-busy", "")
	clipboardStillOpenable(t)
}

// ---------------------------------------------------------------------------
// The paste edit action (the real clipboard half)
// ---------------------------------------------------------------------------

// pasteHandler is the paste test's input handler: a text model with a
// selection and the edit-action surface, recording the events the core
// fires.
type pasteHandler struct {
	text   string
	sel    gpui.UTF16Range
	events []string
}

func (h *pasteHandler) AcceptsTextInput(*gpui.Window, *gpui.App) bool { return true }

func (h *pasteHandler) DispatchInput(input string, w *gpui.Window, app *gpui.App) {
	h.ReplaceTextInRange(nil, input, w, app)
}

func (h *pasteHandler) ReplaceTextInRange(rng *gpui.UTF16Range, text string, w *gpui.Window, app *gpui.App) {
	// A nil range replaces the selection (the caret for a collapsed
	// selection), exactly like the reference's replace_text_in_range.
	start, end := h.sel.Start, h.sel.End
	if rng != nil {
		start, end = rng.Start, rng.End
	}
	h.text = h.text[:start] + text + h.text[end:]
	h.sel = gpui.UTF16Range{Start: start + len(text), End: start + len(text)}
}

func (h *pasteHandler) SelectedTextRange(bool, *gpui.Window, *gpui.App) (gpui.UTF16Selection, bool) {
	return gpui.UTF16Selection{Range: h.sel}, true
}

func (h *pasteHandler) MarkedTextRange(*gpui.Window, *gpui.App) (gpui.UTF16Range, bool) {
	return gpui.UTF16Range{}, false
}

func (h *pasteHandler) TextForRange(rng gpui.UTF16Range, w *gpui.Window, app *gpui.App) (string, *gpui.UTF16Range, bool) {
	start, end := min(rng.Start, rng.End), max(rng.Start, rng.End)
	if start > len(h.text) {
		start = len(h.text)
	}
	if end > len(h.text) {
		end = len(h.text)
	}
	adjusted := gpui.UTF16Range{Start: start, End: end}
	return h.text[start:end], &adjusted, true
}

func (h *pasteHandler) ReplaceAndMarkTextInRange(rng *gpui.UTF16Range, text string, newSelectedRange *gpui.UTF16Range, w *gpui.Window, app *gpui.App) {
	h.ReplaceTextInRange(rng, text, w, app)
}

func (h *pasteHandler) UnmarkText(*gpui.Window, *gpui.App) {}

func (h *pasteHandler) BoundsForRange(gpui.UTF16Range, *gpui.Window, *gpui.App) (gpui.Bounds, bool) {
	return gpui.Bounds{}, false
}

func (h *pasteHandler) CharacterIndexForPoint(gpui.Point, *gpui.Window, *gpui.App) (int, bool) {
	return 0, false
}

func (h *pasteHandler) SetSelectedTextRange(gpui.UTF16Range, *gpui.Window, *gpui.App) {}

func (h *pasteHandler) TextLengthUTF16(*gpui.Window, *gpui.App) (int, bool) {
	return len(h.text), true
}

func (h *pasteHandler) Copy(w *gpui.Window, app *gpui.App) bool {
	h.events = append(h.events, "copy")
	return false
}

func (h *pasteHandler) Cut(w *gpui.Window, app *gpui.App) bool {
	h.events = append(h.events, "cut")
	return false
}

func (h *pasteHandler) Paste(item *gpui.ClipboardItem, w *gpui.Window, app *gpui.App) bool {
	text := ""
	if item != nil {
		if t, ok := item.Text(); ok {
			text = t
		}
	}
	h.events = append(h.events, "paste:"+text)
	return false
}

func (h *pasteHandler) BeforeInput(inputType, data string, w *gpui.Window, app *gpui.App) bool {
	h.events = append(h.events, "beforeinput:"+inputType+":"+data)
	return false
}

func (h *pasteHandler) Select(w *gpui.Window, app *gpui.App) {
	h.events = append(h.events, "select")
}

func (h *pasteHandler) SelectAll(w *gpui.Window, app *gpui.App) {
	h.sel = gpui.UTF16Range{Start: 0, End: len(h.text)}
}

// TestClipboardPasteEditActionRoundTrip pins the paste edit action's
// real end-to-end flow: a real application window with a focused input
// handler, a real clipboard item (text plus the HTML companion), and
// the Ctrl+V key dispatch — the paste event carries the read item, the
// beforeinput carries the text, and the insertion lands in the
// handler. The parity fixture pins the event order (its hostless
// replay reads nothing); this gate pins the clipboard half. The cut
// command runs the same real write: its clipboard text is what the
// next paste inserts.
func TestClipboardPasteEditActionRoundTrip(t *testing.T) {
	before := runtime.NumGoroutine()
	app := gpui.NewApp()
	h := gpui.NewHost()
	if err := app.Attach(h); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := h.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}
	t.Cleanup(func() {
		if err := h.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
		expectNoLeakedGoroutines(t, before)
	})
	saved := saveClipboard(t, h)
	t.Cleanup(func() { saved.restore(t, h) })

	window, err := app.OpenWindow(gpui.WindowOptions{Title: "clipspec paste action", Show: false})
	if err != nil {
		t.Fatalf("OpenWindow: %v", err)
	}
	t.Cleanup(func() { window.Close() })

	handler := &pasteHandler{text: "start:", sel: gpui.UTF16Range{Start: 6, End: 6}}
	key := gpui.MustParseKeystroke("ctrl-v")

	// Paste the real item: the events and the insertion.
	appWrite(t, app, gpui.ClipboardItem{Entries: []gpui.ClipboardEntry{
		gpui.NewStringClipboardEntry(gpui.NewClipboardString("pasted 你好").WithHTML("<b>pasted</b> 你好")),
	}})
	handler.events = nil
	app.Update(func(a *gpui.App) {
		window.SetInputHandler(handler)
		window.DispatchKeyEvent(&gpui.KeyDownEvent{Keystroke: key}, a)
	})
	if got := strings.Join(handler.events, "|"); got != "paste:pasted 你好|beforeinput:insertFromPaste:pasted 你好" {
		t.Fatalf("paste events = %q", got)
	}
	if handler.text != "start:pasted 你好" {
		t.Fatalf("text after paste = %q", handler.text)
	}

	// The cut command's real write: the selection's text lands on the
	// clipboard, the selection is deleted, and a following paste
	// inserts what the cut wrote (the clipboard item's plain text —
	// the input's shape, EnclosingTextControl's plain-only write).
	handler.sel = gpui.UTF16Range{Start: 0, End: 6} // "start:"
	cutKey := gpui.MustParseKeystroke("ctrl-x")
	handler.events = nil
	app.Update(func(a *gpui.App) {
		window.SetInputHandler(handler)
		window.DispatchKeyEvent(&gpui.KeyDownEvent{Keystroke: cutKey}, a)
	})
	if got := strings.Join(handler.events, "|"); got != "cut|beforeinput:deleteByCut:" {
		t.Fatalf("cut events = %q", got)
	}
	if handler.text != "pasted 你好" {
		t.Fatalf("text after cut = %q, want the cut to delete the selection", handler.text)
	}
	item := hostRead(t, h)
	requireStringItem(t, item, "start:", "")

	// Paste what the cut wrote: the plain text comes back (the HTML
	// companion does not enter a plain input's insertion).
	handler.events = nil
	app.Update(func(a *gpui.App) {
		window.SetInputHandler(handler)
		window.DispatchKeyEvent(&gpui.KeyDownEvent{Keystroke: key}, a)
	})
	if got := strings.Join(handler.events, "|"); got != "paste:start:|beforeinput:insertFromPaste:start:" {
		t.Fatalf("second paste events = %q", got)
	}
	// The cut left the caret at 0, so the paste inserts there.
	if handler.text != "start:pasted 你好" {
		t.Fatalf("text after the second paste = %q", handler.text)
	}
	clipboardStillOpenable(t)
}

// ---------------------------------------------------------------------------
// The clipboard-change observer
// ---------------------------------------------------------------------------

// TestClipboardChangeObserver pins the observer end to end: registering
// through the application installs the format listener on the host's
// platform window; a foreign write (another application's clipboard
// change) posts WM_CLIPBOARDUPDATE, and the callback runs inside an
// application update. The observer fires once per change.
func TestClipboardChangeObserver(t *testing.T) {
	before := runtime.NumGoroutine()
	app := gpui.NewApp()
	h := gpui.NewHost()
	if err := app.Attach(h); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := h.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}
	t.Cleanup(func() {
		if err := h.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
		expectNoLeakedGoroutines(t, before)
	})
	saved := saveClipboard(t, h)
	t.Cleanup(func() { saved.restore(t, h) })

	changes := make(chan struct{}, 8)
	if err := app.OnClipboardChange(func() { changes <- struct{}{} }); err != nil {
		t.Fatalf("OnClipboardChange: %v", err)
	}
	// A nil callback is a programmer error.
	if err := app.OnClipboardChange(nil); err == nil {
		t.Fatal("OnClipboardChange(nil) succeeded, want an error")
	}

	// A foreign write: the observer fires.
	foreignSetUTF16(t, "changed by a foreign app", true)
	select {
	case <-changes:
	case <-time.After(10 * time.Second):
		t.Fatal("the clipboard-change observer did not fire on a foreign write")
	}

	// Our own write fires it too (the listener is system-wide, exactly
	// like Chromium's ClipboardMonitor — our process's writes notify).
	appWrite(t, app, gpui.NewClipboardStringItem("our own write"))
	select {
	case <-changes:
	case <-time.After(10 * time.Second):
		t.Fatal("the clipboard-change observer did not fire on our own write")
	}

	// A second foreign write: one more notification (the observer is
	// re-armed, not one-shot).
	foreignSetUTF16(t, "second change", true)
	select {
	case <-changes:
	case <-time.After(10 * time.Second):
		t.Fatal("the clipboard-change observer did not fire on the second change")
	}

	// No spurious notifications without a change: nothing further
	// arrives before a short deadline.
	select {
	case <-changes:
		t.Fatal("a clipboard-change notification arrived without a clipboard change")
	case <-time.After(300 * time.Millisecond):
	}
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

// cfHTMLShape is the fragment metadata the spec's own check reads
// (the offset fields, for the raw-bytes assertions).
type cfHTMLShape struct {
	fragmentStart int
	fragmentEnd   int
}

// cfHTMLMetadataOf parses the raw CF_HTML payload's header offsets —
// a tiny local copy of the port's reader for independence (the spec
// must not assert the implementation against itself).
func cfHTMLMetadataOf(t *testing.T, payload string) cfHTMLShape {
	t.Helper()
	number := func(name string) int {
		at := strings.Index(payload, name)
		if at < 0 {
			t.Fatalf("CF_HTML payload lacks %q", name)
		}
		rest := payload[at+len(name):]
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		var n int
		if _, err := fmt.Sscanf(rest[:end], "%d", &n); err != nil {
			t.Fatalf("CF_HTML %q number %q: %v", name, rest[:end], err)
		}
		return n
	}
	return cfHTMLShape{fragmentStart: number("StartFragment:"), fragmentEnd: number("EndFragment:")}
}

// chromeShapeCFHTML builds a foreign CF_HTML payload the way another
// Chromium-shaped application would (the writer's own shape, with a
// source URL).
func chromeShapeCFHTML(t *testing.T, html string) string {
	t.Helper()
	const header = "Version:0.9\r\nStartHTML:%010d\r\nEndHTML:%010d\r\nStartFragment:%010d\r\nEndFragment:%010d\r\n"
	const sourceURL = "SourceURL:https://foreign.test/copy\r\n"
	const startMarkup = "<html>\r\n<body>\r\n"
	const startFragment = "<!--StartFragment-->"
	const endFragment = "<!--EndFragment-->"
	const endMarkup = "\r\n</body>\r\n</html>"
	headersOffset := 105 + len(sourceURL)
	startHTML := headersOffset
	fragmentStart := headersOffset + len(startMarkup) + len(startFragment)
	fragmentEnd := fragmentStart + len(html)
	endHTML := fragmentEnd + len(endFragment) + len(endMarkup)
	return fmt.Sprintf(header, startHTML, endHTML, fragmentStart, fragmentEnd) +
		sourceURL + startMarkup + startFragment + html + endFragment + endMarkup
}

// utf16BytesOf renders text as NUL-terminated UTF-16 bytes.
func utf16BytesOf(t *testing.T, text string) []byte {
	t.Helper()
	units, err := utf16FromString(text)
	if err != nil {
		t.Fatalf("UTF16FromString: %v", err)
	}
	return utf16UnitsToBytes(units)
}

// utf16BytesToString decodes NUL-terminated UTF-16 bytes.
func utf16BytesToString(t *testing.T, data []byte) string {
	t.Helper()
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = uint16(data[2*i]) | uint16(data[2*i+1])<<8
	}
	for i, unit := range units {
		if unit == 0 {
			return string(utf16DecodeOf(units[:i]))
		}
	}
	return string(utf16DecodeOf(units))
}

// utf16UnitsToBytes renders UTF-16 code units as bytes.
func utf16UnitsToBytes(units []uint16) []byte {
	out := make([]byte, len(units)*2)
	for i, unit := range units {
		out[2*i] = byte(unit)
		out[2*i+1] = byte(unit >> 8)
	}
	return out
}

// utf16DecodeOf decodes UTF-16 units (lossy for unpaired surrogates).
func utf16DecodeOf(units []uint16) []rune {
	return utf16.Decode(units)
}

// TestMain guards the whole corpus's clipboard courtesy: the ticket23
// tests overwrite the machine's real board without restoring (their
// original design), so the package saves the board before the corpus
// and restores it after — the per-test save/restores of ticket 26's
// tests compose underneath (they run inside m.Run and each returns the
// board it found).
func TestMain(m *testing.M) {
	// The pre-corpus board: a short-lived host reads it (the same
	// bounded retry the test helpers use).
	h := gpui.NewHost()
	if err := h.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "clipspec: host start for the clipboard guard failed: %v\n", err)
		os.Exit(1)
	}
	saved := readBoardWithRetry(h)
	if err := h.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "clipspec: host stop for the clipboard guard failed: %v\n", err)
	}

	code := m.Run()

	// The post-corpus restore: the same board back (an empty one stays
	// empty — EmptyClipboard).
	rh := gpui.NewHost()
	if err := rh.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "clipspec: host start for the clipboard restore failed: %v\n", err)
		os.Exit(1)
	}
	openForeignWithRetry()
	if r, _, _ := foreignEmptyClipboard.Call(); r == 0 {
		fmt.Fprintln(os.Stderr, "clipspec: the clipboard guard's EmptyClipboard failed")
	}
	foreignCloseClipboard.Call()
	if saved != nil {
		writeBoardWithRetry(rh, *saved)
	}
	if err := rh.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "clipspec: host stop for the clipboard restore failed: %v\n", err)
	}
	os.Exit(code)
}

// openForeignWithRetry is foreignOpenWithRetry without the testing.T
// (TestMain has none): a bounded retry over the machine's transient
// clipboard-viewer contention.
func openForeignWithRetry() {
	deadline := time.Now().Add(2 * time.Second)
	for {
		if r, _, _ := foreignOpenClipboard.Call(0); r != 0 {
			return
		}
		if !time.Now().Before(deadline) {
			fmt.Fprintln(os.Stderr, "clipspec: the clipboard guard's foreign open stayed busy for 2s")
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// readBoardWithRetry is saveClipboard without the testing.T (TestMain
// has none).
func readBoardWithRetry(h *gpui.Host) *gpui.ClipboardItem {
	deadline := time.Now().Add(2 * time.Second)
	for {
		item, err := h.ReadClipboard()
		if err == nil {
			return item
		}
		var readErr *gpui.ClipboardReadError
		if !errors.As(err, &readErr) || readErr.Kind != gpui.ClipboardReadDenied {
			return nil
		}
		if !time.Now().Before(deadline) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// writeBoardWithRetry is hostWrite without the testing.T.
func writeBoardWithRetry(h *gpui.Host, item gpui.ClipboardItem) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := h.WriteClipboard(item); err == nil {
			return
		} else {
			var readErr *gpui.ClipboardReadError
			if !errors.As(err, &readErr) || readErr.Kind != gpui.ClipboardReadDenied {
				return
			}
			if !time.Now().Before(deadline) {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
}
