package script

// The Go bridge (ticket 64): scripts invoke Go actions (invoke for the
// quick ones, call for a Promise), read and write the app's shared
// state through shared(key, init) — the same handle shape state has,
// so bind recomputes only what changed — and fetch URLs through a
// host-owned transport. Everything async (fetch, async actions) runs
// its blocking work on a goroutine and settles its promise on the host
// thread through the scheduler; a reload drops what belonged to the
// replaced VM (fetches are cancelled, promises never settle: the
// browser aborts in-flight fetches on navigation the same way).
//
// The gateable seams for ticket 67's plugins: ActionResolver answers
// which action a script reaches by name, and SharedStore is where
// shared state lives — a plugin host installs a gated view of the
// app's store and a resolver that checks its grants, and a script that
// reaches past them gets a named missing-grant error.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

// ActionFunc is a quick Go action: it runs inline in the script call
// that invoked it, on the host thread, so it must not block (a slow
// action belongs in AsyncActionFunc, whose promise settles from the
// host thread).
//
// Arguments and return values are plain Go values as goja exported
// them: nil, bool, string, float64/int64, []any and map[string]any
// (JSON-shaped data, like zgui's Value). An error surfaces in the
// script as an exception (invoke) or a rejection (call) with the
// action's name in the message.
type ActionFunc func(args []any) (any, error)

// AsyncActionFunc is a Go action that runs off the host thread. ctx is
// cancelled when the VM that called it is reloaded or shut down; the
// promise call returned resolves (or rejects) with what it returns,
// always from the host thread.
type AsyncActionFunc func(ctx context.Context, args []any) (any, error)

// ResolvedAction is the action a script's invoke/call reached: exactly
// one of Sync and Async is set.
type ResolvedAction struct {
	Sync  ActionFunc
	Async AsyncActionFunc
}

// ActionResolver answers which Go action a script may invoke by name.
// The host's own default answers from its registrations; ticket 67
// installs one that checks a plugin's grants, answering a named
// missing-grant error for what was not granted (the same message shape
// the default uses for unknown names).
type ActionResolver func(name string) (ResolvedAction, error)

// Action registers a quick Go action scripts call as
// invoke("name", ...args) or call("name", ...args). Registering a name
// twice is a programming error (panics, like a SetGlobal collision).
func (h *Host) Action(name string, fn ActionFunc) {
	h.registerAction(name, ResolvedAction{Sync: fn})
}

// AsyncAction registers an off-thread Go action; call("name", ...)
// returns its promise. invoke on an async action is a script error
// that says which to use.
func (h *Host) AsyncAction(name string, fn AsyncActionFunc) {
	h.registerAction(name, ResolvedAction{Async: fn})
}

func (h *Host) registerAction(name string, ra ResolvedAction) {
	if name == "" {
		panic("script: an action needs a name")
	}
	if _, exists := h.actions[name]; exists {
		panic("script: an action named " + name + " is already registered")
	}
	if h.actions == nil {
		h.actions = map[string]ResolvedAction{}
	}
	h.actions[name] = ra
}

// resolveAction answers the action lookup: the installed resolver
// (WithActionResolver, ticket 67's gate) or the host's own table.
func (h *Host) resolveAction(name string) (ResolvedAction, error) {
	if h.actionResolver != nil {
		return h.actionResolver(name)
	}
	if ra, ok := h.actions[name]; ok {
		return ra, nil
	}
	return ResolvedAction{}, fmt.Errorf("no host action named %q", name)
}

// exportArgs exports a FunctionCall's arguments (from index on) as
// plain Go values.
func exportArgs(call goja.FunctionCall, from int) []any {
	if len(call.Arguments) <= from {
		return nil
	}
	args := make([]any, 0, len(call.Arguments)-from)
	for _, a := range call.Arguments[from:] {
		args = append(args, a.Export())
	}
	return args
}

// jsInvoke is the Go half of invoke(name, ...args): the sync action
// bridge. Errors surface as exceptions carrying the action's name.
func (h *Host) jsInvoke(call goja.FunctionCall) goja.Value {
	name := call.Argument(0).String()
	ra, err := h.resolveAction(name)
	if err != nil {
		panic(h.vm.NewTypeError("invoke(%q): %v", name, err))
	}
	if ra.Async != nil {
		panic(h.vm.NewTypeError("invoke(%q): the action is async — use call(%q, ...) and await its promise", name, name))
	}
	v, err := ra.Sync(exportArgs(call, 1))
	if err != nil {
		panic(h.vm.NewGoError(fmt.Errorf("action %q: %w", name, err)))
	}
	return h.vm.ToValue(v)
}

// jsCall is the Go half of call(name, ...args): the promise-returning
// bridge, for both sync (settles immediately, as a microtask) and
// async actions (settles from the host thread when the action
// returns). A missing action is an exception at call time, naming it.
func (h *Host) jsCall(call goja.FunctionCall, gen uint64) goja.Value {
	name := call.Argument(0).String()
	ra, err := h.resolveAction(name)
	if err != nil {
		panic(h.vm.NewTypeError("call(%q): %v", name, err))
	}
	p, resolve, reject := h.newPromise()
	rejectError := func(format string, args ...any) {
		reject(h.jsError("Error", fmt.Sprintf(format, args...)))
	}
	if ra.Sync != nil {
		v, err := ra.Sync(exportArgs(call, 1))
		settle(resolve, rejectError, v, err, name)
		return p
	}
	if h.scheduler == nil {
		panic(h.vm.NewTypeError("call(%q): the async action needs a host scheduler: build the host with script.WithScheduler(script.AppScheduler(app))", name))
	}
	h.runOffThread(gen, func(ctx context.Context) (any, error) {
		// ctx is cancelled when the calling VM reloads (its async work
		// is dropped) or the host goes away.
		return ra.Async(ctx, exportArgs(call, 1))
	}, func(v any, err error) {
		settle(resolve, rejectError, v, err, name)
	})
	return p
}

// settle resolves or rejects one action promise with a useful message.
func settle(resolve func(any), reject func(string, ...any), v any, err error, name string) {
	if err != nil {
		reject("action %q: %v", name, err)
		return
	}
	resolve(v)
}

// ---------------------------------------------------------------------------
// Shared state
// ---------------------------------------------------------------------------

// SharedStore is the Go↔script shared state (zgui's ui.shared): values
// keyed by string, read and written from both sides, outliving every
// reload. NewShared is the app's own store; several hosts may share
// one. Ticket 67 hands a plugin's host a gated view of the app's store
// — the same interface, answering a named missing-grant error for keys
// the plugin was not granted.
type SharedStore interface {
	// Load returns the value under key, storing init when the key is
	// new (the keyed-state contract: init matters only for keys that
	// did not exist).
	Load(key string, init any) (any, error)
	// Read returns the value under key (nil when absent).
	Read(key string) (any, error)
	// Write stores value under key and notifies the hosts bound to the
	// store: the bindings on that key recompute (no re-describe) and
	// the window redraws. Call it from the thread that renders, like
	// every host method.
	Write(key string, value any) error
}

// Shared is the app's shared store (the SharedStore the app builds
// with NewShared and hands to its host with WithShared). Its methods
// are safe from any goroutine, but the host notifications Write
// triggers run on the calling goroutine: keep Write on the render
// thread.
type Shared struct {
	mu     sync.RWMutex
	values map[string]any
	notify []func(key string)
}

// NewShared returns an empty shared store.
func NewShared() *Shared {
	return &Shared{values: map[string]any{}}
}

// Load implements SharedStore.
func (s *Shared) Load(key string, init any) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.values[key]; ok {
		return v, nil
	}
	s.values[key] = init
	return init, nil
}

// Read implements SharedStore.
func (s *Shared) Read(key string) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.values[key], nil
}

// Write implements SharedStore: store the value, then tell every
// attached host (the script's bindings on the key recompute; the
// window redraws).
func (s *Shared) Write(key string, value any) error {
	s.mu.Lock()
	s.values[key] = value
	notify := append([]func(string){}, s.notify...)
	s.mu.Unlock()
	for _, n := range notify {
		n(key)
	}
	return nil
}

// attach subscribes a host: the seam WithShared uses (and ticket 67's
// gated view forwards to the store it wraps). Unexported so only this
// package (and views embedding *Shared) can attach hosts.
func (s *Shared) attach(notify func(key string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notify = append(s.notify, notify)
}

// sharedChanged is what a store write tells the host: mark the key
// dirty, and either flush at the end of the running script turn (a
// script's own set, or a completion) or right now (a Go write with no
// script on the stack).
func (h *Host) sharedChanged(key string) {
	h.dirtyKeys[sharedBucket+key] = true
	if h.inJS > 0 {
		return
	}
	if err := h.flush(); err != nil {
		h.fail(err)
	}
	h.notify()
}

// jsShared is the Go half of shared(key, init): the same handle shape
// state has ({ key, get, set }), so bind works over it. Writes go to
// the shared store; the host's bindings recompute without a
// re-describe.
func (h *Host) jsShared(call goja.FunctionCall) goja.Value {
	if h.shared == nil {
		panic(h.vm.NewTypeError("shared(key, init): the host has no shared store — build it with script.WithShared(script.NewShared())"))
	}
	key := call.Argument(0).String()
	if key == "" {
		panic(h.vm.NewTypeError("shared needs a key, like shared(\"notes\", [])"))
	}
	if _, err := h.shared.Load(key, call.Argument(1).Export()); err != nil {
		panic(h.vm.NewTypeError("shared(%q): %v", key, err))
	}
	o := h.vm.NewObject()
	mustSet(o, "key", key)
	// The marker tells bind which bucket the handle reads (shared keys
	// and state keys are distinct namespaces).
	mustSet(o, "__shared", true)
	mustSet(o, "get", func(call goja.FunctionCall) goja.Value {
		v, err := h.shared.Read(key)
		if err != nil {
			panic(h.vm.NewTypeError("shared(%q).get: %v", key, err))
		}
		return h.vm.ToValue(v)
	})
	mustSet(o, "set", func(call goja.FunctionCall) goja.Value {
		if err := h.shared.Write(key, call.Argument(0).Export()); err != nil {
			panic(h.vm.NewTypeError("shared(%q).set: %v", key, err))
		}
		return goja.Undefined()
	})
	return o
}

// ---------------------------------------------------------------------------
// fetch
// ---------------------------------------------------------------------------

// FetchRequest is one script fetch, as the host's fetcher sees it: the
// URL, an upper-case method, the headers in order, and the body bytes.
type FetchRequest struct {
	URL     string
	Method  string
	Headers [][2]string
	Body    []byte
}

// FetchResponse is what the host's fetcher answers. Headers carry the
// response's names and values in order; the host lower-cases the names
// for the script's headers object.
type FetchResponse struct {
	Status     int
	StatusText string
	Headers    [][2]string
	Body       []byte
}

// Fetcher performs the script's HTTP round trips (zgui's
// ScriptHost::fetcher: the host owns the transport). It runs on a
// goroutine, so it must never touch the VM; ctx is cancelled when the
// fetching VM reloads or the app goes away. An error rejects the
// script's promise with the URL and the reason.
type Fetcher func(ctx context.Context, req FetchRequest) (FetchResponse, error)

// HTTPFetcher is the default transport: a plain net/http client with
// no cookies or credentials, a 30 s timeout, and no redirects
// followed beyond net/http's default.
func HTTPFetcher() Fetcher {
	client := &http.Client{Timeout: 30 * time.Second}
	return func(ctx context.Context, req FetchRequest) (FetchResponse, error) {
		var body io.Reader
		if req.Body != nil {
			body = strings.NewReader(string(req.Body))
		}
		httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, body)
		if err != nil {
			return FetchResponse{}, err
		}
		for _, h := range req.Headers {
			httpReq.Header.Add(h[0], h[1])
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			return FetchResponse{}, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return FetchResponse{}, err
		}
		out := FetchResponse{Status: resp.StatusCode, StatusText: resp.Status, Body: b}
		for name, values := range resp.Header {
			for _, v := range values {
				out.Headers = append(out.Headers, [2]string{name, v})
			}
		}
		return out, nil
	}
}

// jsFetch is the Go half of fetch(url, opts): a promise of a Response
// ({ status, ok, statusText, headers, url, text(), json() }). Network
// failures reject the promise (a TypeError, as in a browser) with the
// URL and the reason; a non-2xx status resolves, like a browser's.
// opts: { method, headers, body } — a string body passes through, an
// object body is sent as JSON with the content-type set.
func (h *Host) jsFetch(call goja.FunctionCall, gen uint64) goja.Value {
	rawURL := call.Argument(0).String()
	if rawURL == "" {
		panic(h.vm.NewTypeError("fetch needs a URL"))
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		panic(h.vm.NewTypeError("fetch(%q): the URL is not absolute (the host has no base URL)", rawURL))
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		panic(h.vm.NewTypeError("fetch(%q): only http and https are supported", rawURL))
	}
	fetcher := h.fetcher
	if h.scheduler == nil {
		panic(h.vm.NewTypeError("fetch needs a host scheduler: build the host with script.WithScheduler(script.AppScheduler(app))"))
	}
	if fetcher == nil {
		panic(h.vm.NewTypeError("fetch: the host has no fetcher — build it with script.WithFetcher(...)"))
	}

	req, err := h.fetchRequest(call.Argument(1), u)
	if err != nil {
		panic(h.vm.NewTypeError("fetch(%q): %v", rawURL, err))
	}
	p, resolve, reject := h.newPromise()
	h.runOffThread(gen, func(ctx context.Context) (any, error) {
		return fetcher(ctx, req)
	}, func(v any, err error) {
		if err != nil {
			reject(h.jsError("TypeError", fmt.Sprintf("fetch %s: %v", rawURL, err)))
			return
		}
		resolve(h.responseObject(v.(FetchResponse), rawURL))
	})
	return p
}

// fetchRequest builds the FetchRequest from the options object:
// method (upper-case, default GET), headers (a string-keyed object)
// and body (a string, or an object sent as JSON).
func (h *Host) fetchRequest(opts goja.Value, u *url.URL) (FetchRequest, error) {
	req := FetchRequest{URL: u.String(), Method: "GET"}
	if goja.IsUndefined(opts) || goja.IsNull(opts) {
		return req, nil
	}
	o, ok := opts.(*goja.Object)
	if !ok {
		return req, fmt.Errorf("the options must be an object")
	}
	for _, k := range o.Keys() {
		switch k {
		case "method":
			req.Method = strings.ToUpper(o.Get(k).String())
		case "headers":
			ho, ok := o.Get(k).(*goja.Object)
			if !ok {
				return req, fmt.Errorf("headers must be an object")
			}
			for _, name := range ho.Keys() {
				req.Headers = append(req.Headers, [2]string{name, ho.Get(name).String()})
			}
		case "body":
			switch b := o.Get(k).Export().(type) {
			case string:
				req.Body = []byte(b)
			case map[string]any, []any:
				encoded, err := json.Marshal(b)
				if err != nil {
					return req, err
				}
				req.Body = encoded
				if !hasHeader(req.Headers, "content-type") {
					req.Headers = append(req.Headers, [2]string{"content-type", "application/json"})
				}
			default:
				return req, fmt.Errorf("body must be a string or a JSON object (a typed array body is not provided yet)")
			}
		default:
			return req, fmt.Errorf("unknown option %q (the host has method, headers and body)", k)
		}
	}
	return req, nil
}

func hasHeader(headers [][2]string, name string) bool {
	for _, h := range headers {
		if strings.EqualFold(h[0], name) {
			return true
		}
	}
	return false
}

// responseObject builds the script's Response: status, ok (2xx),
// statusText, headers (lower-cased names, duplicates joined with ",
// "), url, and text()/json() returning promises (the body is already
// in memory; they settle as microtasks). json() rejects on a body that
// is not JSON.
func (h *Host) responseObject(r FetchResponse, reqURL string) *goja.Object {
	o := h.vm.NewObject()
	mustSet(o, "status", r.Status)
	mustSet(o, "ok", r.Status >= 200 && r.Status < 300)
	mustSet(o, "statusText", r.StatusText)
	mustSet(o, "url", reqURL)
	headers := h.vm.NewObject()
	joined := map[string][]string{}
	for _, kv := range r.Headers {
		name := strings.ToLower(kv[0])
		joined[name] = append(joined[name], kv[1])
	}
	for name, values := range joined {
		mustSet(headers, name, strings.Join(values, ", "))
	}
	mustSet(o, "headers", headers)
	body := string(r.Body)
	mustSet(o, "text", func(call goja.FunctionCall) goja.Value {
		p, resolve, _ := h.newPromise()
		resolve(body)
		return p
	})
	mustSet(o, "json", func(call goja.FunctionCall) goja.Value {
		p, resolve, reject := h.newPromise()
		var v any
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			reject(h.jsError("SyntaxError", fmt.Sprintf("fetch %s: the body is not JSON: %v", reqURL, err)))
		} else {
			resolve(v)
		}
		return p
	})
	return o
}

// newPromise builds a JS Promise whose resolving functions the host
// keeps, to settle from the host thread. The reactions a settle runs
// execute as microtasks, drained when the current Go→JS call returns.
func (h *Host) newPromise() (goja.Value, func(any), func(any)) {
	var resolveFn, rejectFn goja.Callable
	executor := func(call goja.FunctionCall) goja.Value {
		resolveFn, _ = goja.AssertFunction(call.Argument(0))
		rejectFn, _ = goja.AssertFunction(call.Argument(1))
		return goja.Undefined()
	}
	p, err := h.vm.New(h.vm.Get("Promise"), h.vm.ToValue(executor))
	if err != nil || resolveFn == nil || rejectFn == nil {
		panic(h.vm.NewTypeError("the host could not build a Promise: %v", err))
	}
	resolve := func(v any) {
		_, _ = resolveFn(goja.Undefined(), h.vm.ToValue(v))
	}
	reject := func(v any) {
		_, _ = rejectFn(goja.Undefined(), h.vm.ToValue(v))
	}
	return p, resolve, reject
}

// jsError builds a JS error object of the given builtin constructor
// (TypeError, SyntaxError, Error, ...) with a message.
func (h *Host) jsError(ctor, msg string) goja.Value {
	v, err := h.vm.New(h.vm.Get(ctor), h.vm.ToValue(msg))
	if err != nil {
		return h.vm.NewGoError(fmt.Errorf("%s", msg))
	}
	return v
}

// ---------------------------------------------------------------------------
// Off-thread work and its settlement
// ---------------------------------------------------------------------------

// asyncWork is one in-flight off-thread operation (a fetch, an async
// action) belonging to a VM generation.
type asyncWork struct {
	gen    uint64
	cancel func()
}

// runOffThread runs work on a goroutine the scheduler owns and calls
// settle with its result on the host thread. settle runs in the VM's
// world only if that VM is still the live one; otherwise the
// completion is dropped (the browser aborts in-flight fetches on
// navigation; zgui's promise waiters hold a Weak<Vm> for the same
// effect).
func (h *Host) runOffThread(gen uint64, work func(ctx context.Context) (any, error), settle func(v any, err error)) {
	var result any
	var err error
	cancel := h.scheduler.Run(context.Background(), func(ctx context.Context) {
		result, err = work(ctx)
	}, func() {
		h.enterGeneration(gen, func() { settle(result, err) })
	})
	h.trackAsync(gen, cancel)
}

// enterGeneration runs body only when gen is the live VM's, wrapped as
// one script turn: what the settling promise's reactions change is
// flushed and the window is asked to redraw.
func (h *Host) enterGeneration(gen uint64, body func()) {
	if gen != h.vmGen {
		return
	}
	h.runJS(func() error {
		body()
		return nil
	})
}

// trackAsync registers one in-flight operation so a reload can cancel
// it.
func (h *Host) trackAsync(gen uint64, cancel func()) {
	h.async = append(h.async, &asyncWork{gen: gen, cancel: cancel})
}
