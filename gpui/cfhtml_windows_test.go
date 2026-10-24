//go:build windows

package gpui

import (
	"strings"
	"testing"
)

// The CF_HTML codec gates (browser-core ticket 26): the writer's
// header shape and offsets (Chromium's HtmlToCFHtml,
// clipboard_util_win.cc:1258-1375), the reader's fragment extraction
// (:1377-1435) over both the Chromium and the OpenOffice payload
// shapes, and the round trip. The real Win32 exchange round trip is
// pinned by internal/clipspec.

// TestCFHTMLWriterShape pins HtmlToCFHtml's output: the header's four
// 10-digit offsets pointing at the markup's own bytes, the optional
// SourceURL line, and the wrapped markup.
func TestCFHTMLWriterShape(t *testing.T) {
	const html = "<b>hello</b> 你好"
	payload := htmlToCFHTML(html, "")

	header := "Version:0.9\r\n"
	if !strings.HasPrefix(payload, header) {
		t.Fatalf("payload does not start with the CF_HTML header: %q", payload[:30])
	}
	// The four offsets, parsed back out of the header.
	startHTML, _ := cfHTMLHeaderNumber(payload, "StartHTML:")
	endHTML, _ := cfHTMLHeaderNumber(payload, "EndHTML:")
	startFragment, _ := cfHTMLHeaderNumber(payload, "StartFragment:")
	endFragment, _ := cfHTMLHeaderNumber(payload, "EndFragment:")
	if startHTML != 105 {
		t.Errorf("StartHTML = %d, want 105 (the header's fixed length)", startHTML)
	}
	// The offsets point where the markup says they do.
	if got := payload[startHTML:]; !strings.HasPrefix(got, cfHTMLStartMarkup) {
		t.Errorf("StartHTML(%d) does not point at the markup: %q", startHTML, payload[startHTML:startHTML+12])
	}
	if got := payload[startFragment:]; !strings.HasPrefix(got, html) {
		t.Errorf("StartFragment(%d) does not point at the fragment: %q", startFragment, payload[startFragment:startFragment+16])
	}
	if got := payload[endFragment:]; !strings.HasPrefix(got, cfHTMLEndFragment) {
		t.Errorf("EndFragment(%d) does not point at the EndFragment comment: %q", endFragment, payload[endFragment:endFragment+20])
	}
	if endHTML != len(payload) {
		t.Errorf("EndHTML = %d, want %d (the payload's whole length)", endHTML, len(payload))
	}
	if want := cfHTMLStartMarkup + cfHTMLStartFragment + html + cfHTMLEndFragment + cfHTMLEndMarkup; payload[startHTML:] != want {
		t.Errorf("markup = %q, want %q", payload[startHTML:], want)
	}

	// The SourceURL line sits between the header and the markup and
	// shifts every offset by its own length.
	const url = "https://example.test/page"
	withURL := htmlToCFHTML(html, url)
	shift := len("SourceURL:") + len(url) + 2
	if got, _ := cfHTMLHeaderNumber(withURL, "StartHTML:"); got != 105+shift {
		t.Errorf("StartHTML with a source URL = %d, want %d", got, 105+shift)
	}
	if !strings.Contains(withURL, "SourceURL:"+url+"\r\n") {
		t.Errorf("payload with a source URL = %q, want the SourceURL line", withURL[:130])
	}

	// An empty html yields the empty payload (the write skips the
	// format).
	if got := htmlToCFHTML("", ""); got != "" {
		t.Errorf("htmlToCFHTML(\"\") = %q, want empty", got)
	}
}

// TestCFHTMLReaderRoundTrip pins the reader over the writer's payload:
// the extracted fragment is the original html, on both the
// comment-marked and the header-offset paths, and the extracted base
// URL survives.
func TestCFHTMLReaderRoundTrip(t *testing.T) {
	const html = "<b>hello</b> 你好"
	if got, ok := cfHTMLToHTML(htmlToCFHTML(html, "")); !ok || got != html {
		t.Fatalf("round trip = %q,%v want %q,true", got, ok, html)
	}
	if got, ok := cfHTMLToHTML(htmlToCFHTML(html, "https://example.test/")); !ok || got != html {
		t.Fatalf("round trip with a source URL = %q,%v want %q,true", got, ok, html)
	}
	meta := cfHTMLExtractMetadata(htmlToCFHTML(html, "https://example.test/page"))
	if meta.baseURL != "https://example.test/page" {
		t.Errorf("extracted base URL = %q", meta.baseURL)
	}
	// The SourceURL line shifts the markup start by its own length.
	withURLShift := 105 + len("SourceURL:") + len("https://example.test/page") + 2
	if meta.htmlStart != withURLShift {
		t.Errorf("extracted htmlStart = %d, want %d", meta.htmlStart, withURLShift)
	}

	// The reader trims ASCII whitespace off the fragment edges
	// (base::TrimWhitespaceASCII TRIM_ALL).
	if got, ok := cfHTMLToHTML(htmlToCFHTML("\r\n"+html+"  ", "")); !ok || got != html {
		t.Fatalf("trimmed round trip = %q,%v want %q,true", got, ok, html)
	}
}

// TestCFHTMLReaderOpenOfficeFallback pins the header-offset path
// (CFHtmlExtractMetadata's fallback when the fragment comments are
// absent — the OpenOffice Writer payload shape): the StartFragment and
// EndFragment byte counts from the header select the fragment.
func TestCFHTMLReaderOpenOfficeFallback(t *testing.T) {
	const fragment = "a fragment without comments"
	// The fixed header is 105 bytes; the markup "<html><body>" +
	// fragment + "</body></html>" starts at 105, so the fragment spans
	// 117..144 and the whole payload is 158 bytes.
	payload := "Version:0.9\r\nStartHTML:0000000105\r\nEndHTML:0000000158\r\n" +
		"StartFragment:0000000117\r\nEndFragment:0000000144\r\n" +
		"<html><body>" + fragment + "</body></html>"
	if got, ok := cfHTMLToHTML(payload); !ok || got != fragment {
		t.Fatalf("fallback read = %q,%v want %q,true", got, ok, fragment)
	}
}

// TestCFHTMLReaderForeignPayloads pins the reader against foreign
// shapes: the trailing NUL of CreateGlobalData (clipboard_win.cc:
// 183-196) rides after </html> harmlessly, the fragment range stays
// clamped to the payload, and a payload without a fragment reports
// ok=false instead of panicking.
func TestCFHTMLReaderForeignPayloads(t *testing.T) {
	const html = "<i>x</i>"
	if got, ok := cfHTMLToHTML(htmlToCFHTML(html, "") + "\x00"); !ok || got != html {
		t.Fatalf("NUL-terminated read = %q,%v want %q,true", got, ok, html)
	}

	// Header numbers past the payload's end: no fragment (clamped, not
	// panicking).
	payload := "Version:0.9\r\nStartFragment:0000009999\r\nEndFragment:0000009999\r\n"
	if _, ok := cfHTMLToHTML(payload); ok {
		t.Fatal("out-of-range fragment offsets parsed, want ok=false")
	}

	// No fragment markers and no header numbers: ok=false.
	if _, ok := cfHTMLToHTML("Version:0.9\r\nnot html at all"); ok {
		t.Fatal("marker-less payload parsed, want ok=false")
	}
}
