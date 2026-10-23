package script

// The browser-ish globals goja lacks (ticket 64, spec's "host-provided
// globals: timers, fetch and the other browser-ish APIs goja lacks"):
// console, setTimeout/clearTimeout, setInterval/clearInterval,
// queueMicrotask, requestAnimationFrame/cancelAnimationFrame,
// performance.now, atob/btoa, TextEncoder/TextDecoder and
// URL/URLSearchParams. fetch and the Go bridge (call, invoke, shared)
// live in bridge.go. What is deliberately NOT provided (and why) is
// listed in doc.go.
//
// The split mirrors ticket 62's prelude split: JS-shaped logic (console
// formatting, the URLSearchParams semantics) is JS in browserPrelude;
// anything needing a real library (UTF-8, base64, URL parsing) is a Go
// function the prelude calls. queueMicrotask is Promise-based: goja
// drains the microtask queue when the current Go→JS call returns, which
// is exactly the browser's "after the current task".

import (
	"encoding/base64"
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// browserPrelude is the JS half of the globals: console, queueMicrotask,
// TextEncoder/TextDecoder, URL, URLSearchParams and the timer/rAF front
// ends. It runs after the host's h/Fragment prelude, before the bundle.
const browserPrelude = `
globalThis.queueMicrotask = function (fn) {
	if (typeof fn !== "function") throw new TypeError("queueMicrotask needs a function");
	Promise.resolve().then(fn);
};
globalThis.setTimeout = function (fn, delay, ...rest) { return __timer("setTimeout", fn, delay, false, ...rest); };
globalThis.clearTimeout = function (id) { __clearTimer(id); };
globalThis.setInterval = function (fn, delay, ...rest) { return __timer("setInterval", fn, delay, true, ...rest); };
globalThis.clearInterval = function (id) { __clearTimer(id); };
globalThis.requestAnimationFrame = function (fn) { return __raf(fn); };
globalThis.cancelAnimationFrame = function (id) { __cancelRaf(id); };
globalThis.performance = { now: () => __performanceNow() };
globalThis.atob = __atob;
globalThis.btoa = __btoa;

globalThis.TextEncoder = class TextEncoder {
	constructor() { this.encoding = "utf-8"; }
	encode(s) { return __encodeUtf8(s === undefined ? "" : String(s)); }
};
globalThis.TextDecoder = class TextDecoder {
	constructor(label, opts) {
		this.encoding = __decoderLabel(label === undefined ? "utf-8" : label);
		this.fatal = opts !== undefined && opts !== null && opts.fatal === true;
		this.ignoreBOM = false;
	}
	decode(bytes) { return __decodeUtf8(bytes, this.fatal); }
};

globalThis.URLSearchParams = class URLSearchParams {
	constructor(init) {
		this.__pairs = [];
		if (init === undefined || init === null) return;
		if (typeof init === "string") {
			let s = init;
			if (s.charCodeAt(0) === 63) s = s.slice(1); // a leading "?"
			if (s !== "") {
				for (const pair of s.split("&")) {
					if (pair === "") continue;
					const eq = pair.indexOf("=");
					if (eq < 0) this.__pairs.push([__urldecode(pair), ""]);
					else this.__pairs.push([__urldecode(pair.slice(0, eq)), __urldecode(pair.slice(eq + 1))]);
				}
			}
		} else if (init instanceof URLSearchParams) {
			this.__pairs = init.__pairs.map((p) => p.slice());
		} else if (typeof init === "object" && typeof init[Symbol.iterator] === "function") {
			for (const pair of init) {
				if (!Array.isArray(pair) || pair.length < 2) {
					throw new TypeError("URLSearchParams takes [key, value] pairs");
				}
				this.__pairs.push([String(pair[0]), String(pair[1])]);
			}
		} else if (typeof init === "object") {
			for (const k of Object.keys(init)) this.__pairs.push([k, String(init[k])]);
		} else {
			throw new TypeError("URLSearchParams takes a string, pairs or an object");
		}
	}
	append(k, v) { this.__pairs.push([String(k), String(v)]); }
	delete(k) {
		const key = String(k);
		this.__pairs = this.__pairs.filter((p) => p[0] !== key);
	}
	get(k) {
		const key = String(k);
		for (const p of this.__pairs) if (p[0] === key) return p[1];
		return null;
	}
	getAll(k) {
		const key = String(k);
		return this.__pairs.filter((p) => p[0] === key).map((p) => p[1]);
	}
	has(k) {
		const key = String(k);
		for (const p of this.__pairs) if (p[0] === key) return true;
		return false;
	}
	set(k, v) {
		const key = String(k), value = String(v);
		const kept = [];
		let seen = false;
		for (const p of this.__pairs) {
			if (p[0] === key) {
				if (!seen) { kept.push([key, value]); seen = true; }
			} else kept.push(p);
		}
		if (!seen) kept.push([key, value]);
		this.__pairs = kept;
	}
	toString() {
		return this.__pairs.map((p) => __urlencode(p[0]) + "=" + __urlencode(p[1])).join("&");
	}
	forEach(fn, thisArg) {
		for (const p of this.__pairs.slice()) {
			fn.call(thisArg === undefined ? null : thisArg, p[1], p[0], this);
		}
	}
	*entries() { for (const p of this.__pairs) yield p.slice(); }
	*keys() { for (const p of this.__pairs) yield p[0]; }
	*values() { for (const p of this.__pairs) yield p[1]; }
	[Symbol.iterator]() { return this.entries(); }
};
function __urldecode(s) {
	try { return decodeURIComponent(s.replace(/\+/g, " ")); } catch (e) { return s; }
}
function __urlencode(s) { return encodeURIComponent(s).replace(/%20/g, "+"); }

globalThis.URL = class URL {
	constructor(u, base) {
		let b;
		if (base !== undefined) {
			if (base instanceof URL) b = base.href;
			else if (typeof base === "string") b = base;
			else throw new TypeError("URL base must be a string or a URL");
		}
		const p = __urlParse(String(u), b);
		this.__parts = p;
		this.__searchParams = new URLSearchParams(p.search);
	}
	get href() { return this.__parts.href; }
	get protocol() { return this.__parts.protocol; }
	get host() { return this.__parts.host; }
	get hostname() { return this.__parts.hostname; }
	get port() { return this.__parts.port; }
	get pathname() { return this.__parts.pathname; }
	get search() { return this.__parts.search; }
	get searchParams() { return this.__searchParams; }
	get hash() { return this.__parts.hash; }
	get origin() { return this.__parts.origin; }
	toString() { return this.__parts.href; }
};

const console = {
	log: (...a) => __hostLog("log", __fmtArgs(a)),
	info: (...a) => __hostLog("info", __fmtArgs(a)),
	debug: (...a) => __hostLog("debug", __fmtArgs(a)),
	warn: (...a) => __hostLog("warn", __fmtArgs(a)),
	error: (...a) => __hostLog("error", __fmtArgs(a)),
	trace: (...a) => __hostLog("trace", __fmtArgs(a)),
	assert: (cond, ...a) => { if (!cond) __hostLog("error", "Assertion failed" + (a.length ? ": " + __fmtArgs(a) : "")); },
};
globalThis.console = console;

function __stringify(v, depth) {
	if (v === null) return "null";
	if (v === undefined) return "undefined";
	const t = typeof v;
	if (t === "string") return depth === 0 ? v : JSON.stringify(v);
	if (t === "number" || t === "bigint" || t === "boolean" || t === "symbol") return String(v);
	if (t === "function") return v.name ? "[Function: " + v.name + "]" : "[Function (anonymous)]";
	if (v instanceof Error) return String(v);
	if (Array.isArray(v)) {
		if (depth >= 2) return "Array(" + v.length + ")";
		const items = v.slice(0, 100).map((x) => __stringify(x, depth + 1));
		const more = v.length > 100 ? ", … " + (v.length - 100) + " more" : "";
		return "[" + items.join(", ") + more + "]";
	}
	if (t === "object") {
		if (depth >= 2) return "[Object]";
		const keys = Object.keys(v).slice(0, 50);
		const body = keys.map((k) => k + ": " + __stringify(v[k], depth + 1)).join(", ");
		const more = Object.keys(v).length > 50 ? ", …" : "";
		return "{" + body + more + "}";
	}
	return String(v);
}
function __fmtArgs(args) {
	if (args.length === 0) return "";
	if (typeof args[0] === "string" && /%[sdifoOj]/.test(args[0])) {
		const rest = [];
		let out = args[0].replace(/%[sdifoOj%]/g, (m) => {
			if (m === "%%") return "%";
			if (rest.length + 1 >= args.length) return m;
			const v = args[rest.length + 1];
			rest.push(v);
			if (m === "%d" || m === "%i") return String(Math.floor(Number(v)));
			if (m === "%f") return String(Number(v));
			if (m === "%j") { try { return JSON.stringify(v); } catch (e) { return "[Circular]"; } }
			if (m === "%o" || m === "%O") return __stringify(v, 0);
			return __stringify(v, 0);
		});
		const left = args.slice(rest.length + 1).map((a) => __stringify(a, 0));
		return (left.length ? out + " " + left.join(" ") : out);
	}
	return args.map((a) => __stringify(a, 0)).join(" ");
}
`

var browserPreludeProgram = goja.MustCompile("gpui/script/globals.js", browserPrelude, false)

// installGlobals installs the Go half of the browser globals on vm.
// gen is the VM's generation: the timers and frame callbacks a script
// registers belong to it and die with it (see Load).
func (h *Host) installGlobals(vm *goja.Runtime, gen uint64) {
	mustSet(vm, "__hostLog", h.hostLog)
	mustSet(vm, "__timer", h.jsTimerFunc(gen))
	mustSet(vm, "__clearTimer", h.jsClearTimer)
	mustSet(vm, "__raf", h.jsRafFunc(gen))
	mustSet(vm, "__cancelRaf", h.jsCancelRaf)
	mustSet(vm, "__performanceNow", h.nowMs)
	mustSet(vm, "__encodeUtf8", h.encodeUtf8)
	mustSet(vm, "__decoderLabel", h.decoderLabel)
	mustSet(vm, "__decodeUtf8", h.decodeUtf8)
	mustSet(vm, "__atob", h.atob)
	mustSet(vm, "__btoa", h.btoa)
	mustSet(vm, "__urlParse", h.urlParse)
}

// jsClearTimer is clearTimeout/clearInterval from JS: browsers coerce
// the id and ignore anything that is not a number they know.
func (h *Host) jsClearTimer(call goja.FunctionCall) goja.Value {
	if id, ok := jsTimerID(call.Argument(0)); ok {
		h.clearTimer(id)
	}
	return goja.Undefined()
}

// jsCancelRaf is cancelAnimationFrame from JS, with the same id
// tolerance.
func (h *Host) jsCancelRaf(call goja.FunctionCall) goja.Value {
	if id, ok := jsTimerID(call.Argument(0)); ok {
		h.cancelRaf(id)
	}
	return goja.Undefined()
}

// jsTimerID exports a script value as a timer id, reporting false for
// the values a browser would ignore.
func jsTimerID(v goja.Value) (int, bool) {
	switch n := v.Export().(type) {
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// console
// ---------------------------------------------------------------------------

// hostLog is what console.log and friends call: the app's sink
// (WithConsoleSink) or the app's stderr, one line per call.
func (h *Host) hostLog(kind, text string) {
	if h.consoleSink != nil {
		h.consoleSink(kind, text)
		return
	}
	fmt.Fprintf(os.Stderr, "[script] %s: %s\n", kind, text)
}

// ---------------------------------------------------------------------------
// Timers
// ---------------------------------------------------------------------------

// timer is one live setTimeout/setInterval registration. Its gen is
// the VM that scheduled it; a reload cancels every timer of the VM it
// replaced, so no callback ever lands in a dead runtime.
type timer struct {
	id     int
	gen    uint64
	cancel func()
}

// jsTimerFunc builds the Go half of setTimeout/setInterval: __timer(name,
// fn, delay, repeat, arguments). The callback arguments (after fn and
// delay) are passed through, as in a browser. Delays are clamped at
// zero (negative and NaN become 0, as in a browser).
func (h *Host) jsTimerFunc(gen uint64) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0).String()
		fn, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(h.vm.NewTypeError("%s needs a function as its first argument", name))
		}
		delay := call.Argument(2).ToFloat()
		if math.IsNaN(delay) || delay < 0 {
			delay = 0
		}
		repeat := call.Argument(3).ToBoolean()
		if h.scheduler == nil {
			panic(h.vm.NewTypeError("%s needs a host scheduler: build the host with script.WithScheduler(script.AppScheduler(app))", name))
		}
		args := make([]goja.Value, 0, len(call.Arguments)-4)
		args = append(args, call.Arguments[4:]...)

		h.nextTimerID++
		t := &timer{id: h.nextTimerID, gen: gen}
		h.timers[t.id] = t
		t.cancel = h.armTimer(t, fn, args, delay, repeat)
		return h.vm.ToValue(t.id)
	}
}

// armTimer schedules the timer's next firing on the host thread.
func (h *Host) armTimer(t *timer, fn goja.Callable, args []goja.Value, delay float64, repeat bool) func() {
	return h.scheduler.After(time.Duration(delay*float64(time.Millisecond)), func() {
		h.fireTimer(t, fn, args, delay, repeat)
	})
}

// fireTimer runs one firing on the host thread. An erroring callback
// is reported like a handler error (the banner) and the timer keeps
// its schedule, as in a browser; clearInterval/clearTimeout (or a
// reload) is what stops it.
func (h *Host) fireTimer(t *timer, fn goja.Callable, args []goja.Value, delay float64, repeat bool) {
	if h.timers[t.id] != t {
		return // cleared, or replaced by a later registration
	}
	if !repeat {
		delete(h.timers, t.id)
	}
	h.runJS(func() error {
		_, err := fn(goja.Undefined(), args...)
		return err
	})
	if repeat && h.timers[t.id] == t {
		t.cancel = h.armTimer(t, fn, args, delay, true)
	}
}

// clearTimer is clearTimeout and clearInterval: browsers share one
// pool, so either cancels any id. An unknown id is a no-op.
func (h *Host) clearTimer(id int) {
	if t, ok := h.timers[id]; ok {
		delete(h.timers, id)
		if t.cancel != nil {
			t.cancel()
		}
	}
}

// ---------------------------------------------------------------------------
// requestAnimationFrame
// ---------------------------------------------------------------------------

// frameCallback is one pending requestAnimationFrame callback. It runs
// on the host thread, at the window's next frame request, before that
// frame paints (View schedules it through Window.OnNextFrame).
type frameCallback struct {
	id  int
	gen uint64
	fn  goja.Callable
}

// jsRafFunc builds the Go half of requestAnimationFrame: __raf(fn).
// The callback receives the frame's timestamp (performance.now's
// clock). A callback registered from a reload's VM dies with it.
func (h *Host) jsRafFunc(gen uint64) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		fn, ok := goja.AssertFunction(call.Argument(0))
		if !ok {
			panic(h.vm.NewTypeError("requestAnimationFrame needs a function"))
		}
		h.nextFrameID++
		h.frameCallbacks = append(h.frameCallbacks, &frameCallback{id: h.nextFrameID, gen: gen, fn: fn})
		// A redraw is what schedules the frame the callbacks run on.
		h.notify()
		return h.vm.ToValue(h.nextFrameID)
	}
}

// cancelRaf is cancelAnimationFrame. An unknown id is a no-op.
func (h *Host) cancelRaf(id int) {
	keep := h.frameCallbacks[:0]
	for _, c := range h.frameCallbacks {
		if c.id != id {
			keep = append(keep, c)
		}
	}
	h.frameCallbacks = keep
}

// frameCallbacksPending reports whether any requestAnimationFrame
// callback waits for the next frame (View asks each render).
func (h *Host) frameCallbacksPending() bool { return len(h.frameCallbacks) > 0 }

// runFrameCallbacks delivers the pending frame callbacks: each gets
// the frame timestamp (the scheduler's clock — performance.now's), and
// what it changed lands in the frame that is about to paint.
func (h *Host) runFrameCallbacks() {
	callbacks := h.frameCallbacks
	h.frameCallbacks = nil
	now := h.vm.ToValue(float64(h.nowMs()))
	for _, c := range callbacks {
		if c.gen != h.vmGen {
			continue // the VM that asked is gone
		}
		h.runJS(func() error {
			_, err := c.fn(goja.Undefined(), now)
			return err
		})
	}
}

// nowMs is performance.now's clock: the scheduler's (the app's virtual
// clock under the test app, the platform clock in a real app), or wall
// time since the host was built when there is no scheduler.
func (h *Host) nowMs() int64 {
	if h.scheduler != nil {
		return h.scheduler.Now()
	}
	return time.Since(h.epoch).Milliseconds()
}

// ---------------------------------------------------------------------------
// TextEncoder / TextDecoder (UTF-8)
// ---------------------------------------------------------------------------

// encodeUtf8 is TextEncoder.encode: a UTF-8 Uint8Array of the string.
func (h *Host) encodeUtf8(call goja.FunctionCall) goja.Value {
	s := call.Argument(0).String()
	arr, err := h.vm.New(h.vm.Get("Uint8Array"), h.vm.ToValue(h.vm.NewArrayBuffer([]byte(s))))
	if err != nil {
		panic(h.vm.NewTypeError("TextEncoder.encode: %v", err))
	}
	return arr
}

// decoderLabel validates a TextDecoder label. Only the UTF-8 labels
// are supported (a documented divergence: the WHATWG encoding list
// needs CLDR-scale data; see doc.go).
func (h *Host) decoderLabel(label string) string {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "utf-8", "utf8", "unicode-1-1-utf-8":
		return "utf-8"
	}
	panic(h.vm.NewTypeError("TextDecoder: unsupported label %q (the host provides UTF-8 only)", label))
}

// decodeUtf8 is TextDecoder.decode. bytes is a typed array (its
// export is the bytes) or a plain array of numbers. Invalid sequences
// become U+FFFD, or an error with { fatal: true }.
func (h *Host) decodeUtf8(call goja.FunctionCall) goja.Value {
	b := h.bytesOf(call.Argument(0))
	fatal := call.Argument(1).ToBoolean()
	s := string(b)
	if !isUTF8(b) {
		if fatal {
			panic(h.vm.NewTypeError("TextDecoder.decode: the bytes are not valid UTF-8"))
		}
		s = strings.ToValidUTF8(s, "\uFFFD")
	}
	return h.vm.ToValue(s)
}

// bytesOf exports a script value as bytes: a typed array (exported as
// its bytes), a plain array of numbers, or a string.
func (h *Host) bytesOf(v goja.Value) []byte {
	switch e := v.Export().(type) {
	case []byte:
		return e
	case string:
		return []byte(e)
	case []any:
		b := make([]byte, 0, len(e))
		for _, x := range e {
			b = append(b, byte(int(toFloat(x))))
		}
		return b
	}
	panic(h.vm.NewTypeError("expected bytes (a typed array), got %s", jsTypeName(v)))
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}

func isUTF8(b []byte) bool {
	i := 0
	for i < len(b) {
		if b[i] < 0x80 {
			i++
			continue
		}
		n := utf8Len(b[i])
		if n < 2 || i+n > len(b) {
			return false
		}
		for _, c := range b[i+1 : i+n] {
			if c&0xC0 != 0x80 {
				return false
			}
		}
		i += n
	}
	return true
}

// utf8Len returns the length of the UTF-8 sequence a lead byte starts,
// or 0 when the byte cannot start one.
func utf8Len(b byte) int {
	switch {
	case b&0xE0 == 0xC0:
		return 2
	case b&0xF0 == 0xE0:
		return 3
	case b&0xF8 == 0xF0:
		return 4
	}
	return 0
}

// ---------------------------------------------------------------------------
// atob / btoa
// ---------------------------------------------------------------------------

// atob decodes base64 (ASCII whitespace ignored, missing padding
// tolerated — a browser's leniencies; btoa's alphabet otherwise).
func (h *Host) atob(s string) (string, error) {
	clean := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\f', '\r':
			return -1
		}
		return r
	}, s)
	switch len(clean) % 4 {
	case 0:
	case 1:
		return "", fmt.Errorf("atob: invalid base64 length %d", len(clean))
	default:
		clean += strings.Repeat("=", 4-len(clean)%4)
	}
	b, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		return "", fmt.Errorf("atob: %v", err)
	}
	return string(b), nil
}

// btoa encodes a string of Latin-1 characters (browsers' domain).
func (h *Host) btoa(s string) (string, error) {
	for _, r := range s {
		if r > 0xFF {
			return "", fmt.Errorf("btoa: character U+%04X is outside Latin-1", r)
		}
	}
	return base64.StdEncoding.EncodeToString([]byte(s)), nil
}

// ---------------------------------------------------------------------------
// URL
// ---------------------------------------------------------------------------

// urlParse is URL's constructor: net/url parses, ResolveReference
// applies the base. WHATWG's serializer (IDNA, dot-segment
// normalization beyond net/url's) is a documented divergence — see
// doc.go. Setters are not provided either: a URL is parsed, read and
// stringified.
func (h *Host) urlParse(call goja.FunctionCall) goja.Value {
	raw := call.Argument(0).String()
	ref, err := url.Parse(raw)
	if err != nil {
		panic(h.vm.NewTypeError("URL: %v", err))
	}
	if v := call.Argument(1); !goja.IsUndefined(v) && !goja.IsNull(v) {
		base, err := url.Parse(v.String())
		if err != nil {
			panic(h.vm.NewTypeError("URL base: %v", err))
		}
		ref = base.ResolveReference(ref)
	}
	o := h.vm.NewObject()
	mustSet(o, "href", ref.String())
	mustSet(o, "protocol", ref.Scheme+":")
	mustSet(o, "host", ref.Host)
	mustSet(o, "hostname", hostnameOf(ref))
	mustSet(o, "port", portOf(ref))
	mustSet(o, "pathname", pathOf(ref))
	mustSet(o, "search", searchOf(ref))
	mustSet(o, "hash", hashOf(ref))
	mustSet(o, "origin", originOf(ref))
	return o
}

func hostnameOf(u *url.URL) string {
	host := u.Host
	if i := strings.LastIndex(host, ":"); i >= 0 && !strings.Contains(host[i:], "]") {
		host = host[:i]
	}
	return host
}

func portOf(u *url.URL) string {
	host := u.Host
	if i := strings.LastIndex(host, ":"); i >= 0 && !strings.Contains(host[i:], "]") {
		return host[i+1:]
	}
	return ""
}

func pathOf(u *url.URL) string {
	if u.Path == "" && (u.Scheme == "http" || u.Scheme == "https") {
		return "/"
	}
	return u.Path
}

func searchOf(u *url.URL) string {
	if u.RawQuery == "" {
		return ""
	}
	return "?" + u.RawQuery
}

func hashOf(u *url.URL) string {
	if u.Fragment == "" {
		return ""
	}
	return "#" + u.Fragment
}

func originOf(u *url.URL) string {
	if u.Scheme != "http" && u.Scheme != "https" {
		return "null"
	}
	return u.Scheme + "://" + u.Host
}
