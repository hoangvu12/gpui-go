//go:build windows

package gpui

// The CF_HTML codec, ported from Chromium 0d4dcd4f
// %TEMP%\research-src\chromium, ui/base/clipboard/clipboard_util_win.cc:
//
//   - HtmlToCFHtml (:1258-1375): wrap the HTML fragment in the
//     "<html>\r\n<body>\r\n<!--StartFragment-->FRAGMENT<!--EndFragment-->\r\n</body>\r\n</html>"
//     markup and prefix the "Version:0.9" header whose four offsets
//     (StartHTML/EndHTML/StartFragment/EndFragment) are 10-digit
//     zero-padded byte counts into the whole CF_HTML payload; an
//     optional "SourceURL:<url>\r\n" line sits between the header and
//     the markup and counts into the offsets.
//   - CFHtmlExtractMetadata (:1393-1435) and CFHtmlToHTML (:1377-1391):
//     read the fragment back — prefer the StartFragment/EndFragment
//     comments (find the first "<html" case-insensitively, then the
//     fragment start is the byte after the StartFragment comment's
//     closing '>' and the fragment end is the '<' that starts the LAST
//     EndFragment comment), falling back to the header's byte-count
//     offsets when the comments are absent (the OpenOffice Writer
//     shape); the extracted fragment is trimmed of ASCII whitespace.
//   - The registered format name is "HTML Format"
//     (ui/base/clipboard/clipboard_format_type_win.cc:132-137,
//     RegisterClipboardFormatChecked(L"HTML Format")), and the payload
//     is written with one trailing NUL (clipboard_win.cc:183-196
//     CreateGlobalData's size+1).
//
// The codec is pure string work; the Win32 exchange (the registered
// format, the write and read sides) lives in clipboard_windows.go, and
// the round trip is pinned by internal/clipspec.

import (
	"fmt"
	"strconv"
	"strings"
)

// The CF_HTML header and markup constants (clipboard_util_win.cc's
// kHeader, kSourceUrlPrefix, kStartMarkup, kEndMarkup, kStartFragment,
// kEndFragment). The header's four numbers are %010d — Chromium's
// MAX_DIGITS 10 with NUMBER_FORMAT "%010zu".
const (
	cfHTMLHeader           = "Version:0.9\r\nStartHTML:%010d\r\nEndHTML:%010d\r\nStartFragment:%010d\r\nEndFragment:%010d\r\n"
	cfHTMLSourceURLPrefix  = "SourceURL:"
	cfHTMLStartMarkup      = "<html>\r\n<body>\r\n"
	cfHTMLEndMarkup        = "\r\n</body>\r\n</html>"
	cfHTMLStartFragment    = "<!--StartFragment-->"
	cfHTMLEndFragment      = "<!--EndFragment-->"
	cfHTMLStartFragmentTag = "<!--StartFragment"
	cfHTMLEndFragmentTag   = "<!--EndFragment"
)

// htmlToCFHTML converts text/html markup into the MS CF_HTML payload
// (clipboard_util_win.cc HtmlToCFHtml, :1258-1375). An empty html
// yields the empty payload (the writer skips the format). baseURL, when
// non-empty, publishes the SourceURL line.
func htmlToCFHTML(html, baseURL string) string {
	if html == "" {
		return ""
	}
	markup := cfHTMLStartMarkup + cfHTMLStartFragment + html + cfHTMLEndFragment + cfHTMLEndMarkup

	// The offsets: the header's own length with all four numbers
	// rendered at their fixed 10 digits (C++'s
	// strlen(kHeader) - strlen(NUMBER_FORMAT)*4 + MAX_DIGITS*4), plus
	// the SourceURL line's when present (+2 for its \r\n).
	headersOffset := len(fmt.Sprintf(cfHTMLHeader, 0, 0, 0, 0))
	if baseURL != "" {
		headersOffset += len(cfHTMLSourceURLPrefix) + len(baseURL) + 2
	}
	startHTML := headersOffset
	startFragment := headersOffset + len(cfHTMLStartMarkup) + len(cfHTMLStartFragment)
	endFragment := startFragment + len(html)
	endHTML := endFragment + len(cfHTMLEndFragment) + len(cfHTMLEndMarkup)

	result := fmt.Sprintf(cfHTMLHeader, startHTML, endHTML, startFragment, endFragment)
	if baseURL != "" {
		result += cfHTMLSourceURLPrefix + baseURL + "\r\n"
	}
	return result + markup
}

// cfHTMLMetadata is the parsed CF_HTML header set
// (CFHtmlExtractMetadata's out-parameters): the source URL and the
// fragment's byte range. -1 is std::string::npos (absent).
type cfHTMLMetadata struct {
	baseURL       string
	htmlStart     int
	fragmentStart int
	fragmentEnd   int
}

// cfHTMLExtractMetadata parses the CF_HTML header
// (clipboard_util_win.cc CFHtmlExtractMetadata, :1393-1435): the
// SourceURL line, and the fragment range — from the
// StartFragment/EndFragment comments when they exist (the Chromium
// shape), else the header's StartFragment:/EndFragment: byte counts
// (the OpenOffice shape). htmlStart is the first "<html"
// (case-insensitive) or -1.
func cfHTMLExtractMetadata(cfHTML string) cfHTMLMetadata {
	meta := cfHTMLMetadata{htmlStart: -1, fragmentStart: -1, fragmentEnd: -1}

	// The source URL: "SourceURL:" through the next newline, trimmed.
	if lineStart := strings.Index(cfHTML, cfHTMLSourceURLPrefix); lineStart >= 0 {
		srcEnd := strings.IndexByte(cfHTML[lineStart:], '\n')
		if srcEnd >= 0 {
			meta.baseURL = trimASCIIWhitespace(cfHTML[lineStart+len(cfHTMLSourceURLPrefix) : lineStart+srcEnd])
		}
	}

	// The markup start: the first "<html", case-insensitive.
	markupStart := strings.Index(strings.ToLower(cfHTML), "<html")
	if markupStart < 0 {
		markupStart = 0
	}
	meta.htmlStart = markupStart

	// The fragment: the StartFragment comment's closing '>' to the '<'
	// starting the LAST EndFragment comment; when the comment is
	// absent, the header's byte counts (atoi of the digits after
	// "StartFragment:"/"EndFragment:").
	tagStart := strings.Index(cfHTML[markupStart:], cfHTMLStartFragmentTag)
	if tagStart >= 0 {
		tagStart += markupStart
		if gt := strings.IndexByte(cfHTML[tagStart:], '>'); gt >= 0 {
			meta.fragmentStart = tagStart + gt + 1
		}
		if tagEnd := strings.LastIndex(cfHTML, cfHTMLEndFragmentTag); tagEnd >= 0 {
			// rfind('<', tag_end): the '<' at or before the comment's
			// own start — the comment's first byte.
			meta.fragmentEnd = tagEnd
		}
	} else {
		if n, ok := cfHTMLHeaderNumber(cfHTML, "StartFragment:"); ok {
			meta.fragmentStart = n
		}
		if n, ok := cfHTMLHeaderNumber(cfHTML, "EndFragment:"); ok {
			meta.fragmentEnd = n
		}
	}
	return meta
}

// cfHTMLHeaderNumber reads one "Name:<digits>" header number
// (C++ atoi: the leading digits after the name, 0 when absent).
func cfHTMLHeaderNumber(cfHTML, name string) (int, bool) {
	at := strings.Index(cfHTML, name)
	if at < 0 {
		return 0, false
	}
	rest := cfHTML[at+len(name):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0, false
	}
	return n, true
}

// cfHTMLToHTML extracts the HTML fragment of a CF_HTML payload
// (clipboard_util_win.cc CFHtmlToHtml, :1377-1391): the byte range the
// metadata parsed, trimmed of ASCII whitespace (base::TRIM_ALL). ok is
// false when the fragment's range is absent.
func cfHTMLToHTML(cfHTML string) (string, bool) {
	meta := cfHTMLExtractMetadata(cfHTML)
	if meta.fragmentStart < 0 || meta.fragmentEnd < 0 ||
		meta.fragmentStart > meta.fragmentEnd ||
		meta.fragmentEnd > len(cfHTML) {
		return "", false
	}
	return trimASCIIWhitespace(cfHTML[meta.fragmentStart:meta.fragmentEnd]), true
}

// trimASCIIWhitespace is base::TrimWhitespaceASCII(TRIM_ALL).
func trimASCIIWhitespace(s string) string {
	return strings.Trim(s, " \t\n\r\v\f")
}
