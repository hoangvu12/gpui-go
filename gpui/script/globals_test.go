//go:build windows

package script_test

// The browser globals (ticket 64): console (formatting and the sink),
// atob/btoa, TextEncoder/TextDecoder, URL/URLSearchParams and the
// unhandled-rejection reporting. Each drives a script through the
// host's public surface and observes the behaviour a browser would
// show.

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui/script"
)

// runGo loads a view whose click handler runs body (a JS statement)
// and returns the host. The handler's error, if any, is the test's to
// check.
func runGo(t testing.TB, body string) (*script.Host, error) {
	t.Helper()
	h, _ := newHost(t, withGo(body))
	return h, h.Click("go")
}

// newHostWithConsole loads the withGo view on a host whose console
// output goes to sink instead of stderr.
func newHostWithConsole(t testing.TB, body string, sink func(kind, text string)) (*script.Host, error) {
	t.Helper()
	s := &srcView{src: withGo(body)}
	h := script.New(script.WithSource(s), script.WithConsoleSink(sink))
	if err := h.Reload(); err != nil {
		t.Fatalf("load view.tsx: %v", err)
	}
	return h, nil
}

func TestConsoleFormatsValuesLikeADevtoolsConsole(t *testing.T) {
	var got []string
	h, _ := newHostWithConsole(t, `
	console.log("plain");
	console.log("n =", 42, true, null, undefined);
	console.log({ name: "x", nested: { k: [1, "two"] } });
	console.log([1, 2, "three"]);
	console.warn("careful %d", 7);
	console.error(new TypeError("boom"));
	`, func(kind, text string) { got = append(got, kind+"|"+text) })
	if err := h.Click("go"); err != nil {
		t.Fatalf("click: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("console called %d times: %v", len(got), got)
	}
	want := []string{
		"log|plain",
		"log|n = 42 true null undefined",
		"log|{name: \"x\", nested: {k: Array(2)}}",
		"log|[1, 2, \"three\"]",
		"warn|careful 7",
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("console output %d = %q, want %q", i, got[i], w)
		}
	}
	if !strings.Contains(got[5], "error|") || !strings.Contains(got[5], "TypeError: boom") {
		t.Fatalf("console.error = %q", got[5])
	}
}

func TestConsoleWithASink(t *testing.T) {
	var got []string
	h, _ := newHostWithConsole(t, `
	console.log("a", 1);
	console.error("bad");
	`, func(kind, text string) { got = append(got, kind+"|"+text) })
	if err := h.Click("go"); err != nil {
		t.Fatalf("click: %v", err)
	}
	if len(got) != 2 || got[0] != "log|a 1" || got[1] != "error|bad" {
		t.Fatalf("console sink = %v", got)
	}
}

func TestAtobAndBtoaRoundTrip(t *testing.T) {
	h, err := runGo(t, `
	const b = btoa("hello, world");
	const back = atob(b);
	if (back !== "hello, world") throw new Error("round trip broken: " + back);
	if (b !== "aGVsbG8sIHdvcmxk") throw new Error("encoding wrong: " + b);
	note("b64 ok");
	`)
	if err != nil {
		t.Fatalf("btoa/atob: %v", err)
	}
	if got := logOf(t, h); len(got) != 1 || got[0] != "b64 ok" {
		t.Fatalf("b64 = %v", got)
	}
}

func TestAtobRejectsInvalidInput(t *testing.T) {
	_, err := runGo(t, `atob("!!!!");`)
	if err == nil || !strings.Contains(err.Error(), "atob") {
		t.Fatalf("bad base64 = %v, want a named error", err)
	}
}

func TestTextEncoderAndDecoderRoundTrip(t *testing.T) {
	h, err := runGo(t, `
	const bytes = new TextEncoder().encode("héllo →");
	if (bytes.length !== 10) throw new Error("utf8 length " + bytes.length);
	if (bytes[0] !== 0x68) throw new Error("first byte " + bytes[0]);
	const text = new TextDecoder().decode(bytes);
	if (text !== "héllo →") throw new Error("round trip " + text);
	note("utf8 ok");
	`)
	if err != nil {
		t.Fatalf("TextEncoder/TextDecoder: %v", err)
	}
	if got := logOf(t, h); len(got) != 1 || got[0] != "utf8 ok" {
		t.Fatalf("utf8 = %v", got)
	}
}

func TestTextDecoderReplacesInvalidBytesByDefault(t *testing.T) {
	h, err := runGo(t, `
	const bytes = new Uint8Array([0x61, 0xff, 0x62]);
	const text = new TextDecoder().decode(bytes);
	if (text !== "a\uFFFDb") throw new Error("replacement " + JSON.stringify(text));
	note("replaced");
	`)
	if err != nil {
		t.Fatalf("TextDecoder: %v", err)
	}
	if got := logOf(t, h); len(got) != 1 || got[0] != "replaced" {
		t.Fatalf("replacement = %v", got)
	}
}

func TestTextDecoderFatalThrowsOnInvalidBytes(t *testing.T) {
	_, err := runGo(t, `
	const bytes = new Uint8Array([0x61, 0xff]);
	new TextDecoder("utf-8", { fatal: true }).decode(bytes);
	`)
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("fatal decode = %v, want a named error", err)
	}
}

func TestTextDecoderRejectsUnsupportedLabels(t *testing.T) {
	_, err := runGo(t, `new TextDecoder("shift-jis");`)
	if err == nil || !strings.Contains(err.Error(), "shift-jis") {
		t.Fatalf("an unsupported label = %v, want a named error", err)
	}
}

func TestURLParsesAndResolves(t *testing.T) {
	h, err := runGo(t, `
	const u = new URL("/docs/guide?q=go#frag", "https://example.com:8443");
	if (u.protocol !== "https:") throw new Error("protocol " + u.protocol);
	if (u.host !== "example.com:8443") throw new Error("host " + u.host);
	if (u.hostname !== "example.com") throw new Error("hostname " + u.hostname);
	if (u.port !== "8443") throw new Error("port " + u.port);
	if (u.pathname !== "/docs/guide") throw new Error("pathname " + u.pathname);
	if (u.search !== "?q=go") throw new Error("search " + u.search);
	if (u.hash !== "#frag") throw new Error("hash " + u.hash);
	if (u.origin !== "https://example.com:8443") throw new Error("origin " + u.origin);
	if (u.toString() !== "https://example.com:8443/docs/guide?q=go#frag") throw new Error("href " + u.toString());
	if (u.searchParams.get("q") !== "go") throw new Error("searchParams " + u.searchParams);
	note("url ok");
	`)
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	if got := logOf(t, h); len(got) != 1 || got[0] != "url ok" {
		t.Fatalf("url = %v", got)
	}
}

func TestURLParsesFullURLsAndEmptyPaths(t *testing.T) {
	h, err := runGo(t, `
	const u = new URL("http://example.com");
	if (u.pathname !== "/") throw new Error("pathname " + u.pathname);
	if (u.search !== "") throw new Error("search " + u.search);
	if (u.hash !== "") throw new Error("hash " + u.hash);
	const withQuery = new URL("https://a.test/x?b=2&b=3&a=1");
	if (withQuery.searchParams.getAll("b").join(",") !== "2,3") throw new Error("getAll");
	note("parsed");
	`)
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	if got := logOf(t, h); len(got) != 1 || got[0] != "parsed" {
		t.Fatalf("url = %v", got)
	}
}

func TestURLSearchParamsBehavesLikeTheBrowser(t *testing.T) {
	h, err := runGo(t, `
	const p = new URLSearchParams("a=1&b=two+a&c");
	if (p.get("a") !== "1") throw new Error("get a " + p.get("a"));
	if (p.get("b") !== "two a") throw new Error("plus is not a space: " + p.get("b"));
	if (p.get("c") !== "") throw new Error("bare key " + JSON.stringify(p.get("c")));
	if (p.get("missing") !== null) throw new Error("missing key");
	if (!p.has("a")) throw new Error("has");
	p.append("a", "2");
	if (p.getAll("a").join(",") !== "1,2") throw new Error("append " + p.getAll("a"));
	p.set("a", "9");
	if (p.getAll("a").join(",") !== "9") throw new Error("set " + p.getAll("a"));
	p.delete("b");
	if (p.has("b")) throw new Error("delete");
	if (p.toString() !== "a=9&c=") throw new Error("toString " + p.toString());
	const round = new URLSearchParams({ x: "1", y: "2" });
	if (round.get("y") !== "2") throw new Error("object init");
	const pairs = [];
	new URLSearchParams("k=v&k=w").forEach((v, k) => pairs.push(k + "=" + v));
	if (pairs.join(";") !== "k=v;k=w") throw new Error("forEach " + pairs.join(";"));
	note("usp ok");
	`)
	if err != nil {
		t.Fatalf("URLSearchParams: %v", err)
	}
	if got := logOf(t, h); len(got) != 1 || got[0] != "usp ok" {
		t.Fatalf("usp = %v", got)
	}
}

func TestURLSearchParamsKeepsInsertionOrder(t *testing.T) {
	// WHATWG's serializer keeps entry order (net/url's Encode sorts;
	// the prelude implements the browser's order instead).
	h, err := runGo(t, `
	const p = new URLSearchParams("b=1&a=2");
	if (p.toString() !== "b=1&a=2") throw new Error("order " + p.toString());
	note("order ok");
	`)
	if err != nil {
		t.Fatalf("URLSearchParams order: %v", err)
	}
	if got := logOf(t, h); len(got) != 1 || got[0] != "order ok" {
		t.Fatalf("order = %v", got)
	}
}

func TestAnUnhandledPromiseRejectionIsReported(t *testing.T) {
	// The rejection is not handled by the end of the script turn: it
	// is reported like a handler error (the banner), as a browser
	// reports unhandled rejections to the console.
	h, err := runGo(t, `Promise.reject(new TypeError("nobody hears"));`)
	if err != nil {
		t.Fatalf("the handler ran: %v", err)
	}
	if h.Err() == nil || !strings.Contains(h.Err().Error(), "nobody hears") {
		t.Fatalf("the unhandled rejection was not reported: %v", h.Err())
	}
}

func TestAHandledRejectionIsNotReported(t *testing.T) {
	h, err := runGo(t, `
	Promise.reject(new TypeError("heard")).catch((e) => note("caught " + e.message));
	`)
	if err != nil {
		t.Fatalf("the handler ran: %v", err)
	}
	if h.Err() != nil {
		t.Fatalf("a handled rejection was reported: %v", h.Err())
	}
	if got := logOf(t, h); len(got) != 1 || got[0] != "caught heard" {
		t.Fatalf("the catch ran: %v", got)
	}
}
