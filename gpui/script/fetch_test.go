//go:build windows

package script_test

// Host-provided fetch (ticket 64): a promise of a Response with
// text()/json(), status and headers, against a local httptest server
// (success, 404, network failure -> a rejected promise) and against an
// injected fetcher (deterministic, including the reload-abort: an
// in-flight fetch whose VM reloaded never settles, like a browser
// aborting fetches on navigation).

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/gpui/script"
)

// fetchedView fetches url (with opts) when "go" is clicked and notes
// "status <n> <ok> [text] [ct]" into the log on success, or
// "rejected <reason>" on failure.
func fetchedView(url string, opts string) string {
	return logView + "\nfunction go() {\n" + fmt.Sprintf(`
	fetch(%q, %s).then((r) => {
		return r.text().then((body) => {
			note("status " + r.status + " " + r.ok + " ct=" + r.headers["content-type"] + " body=" + body);
		});
	}).catch((e) => note("rejected " + (e && e.message ? e.message : e)));
}
`, url, opts) + "\n"
}

// wait drives the test app until cond passes: fetches and async
// completions run on goroutines, so their settlement is polled with a
// deadline (localhost round trips settle in the first iterations).
func wait(t testing.TB, ta *gpui.TestApp, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ta.RunUntilParked()
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the condition was not met before the deadline")
}

func TestFetchResolvesAgainstALocalServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Test", "one")
		w.Header().Add("X-Test", "two")
		fmt.Fprint(w, `{"items": [1, 2, 3], "name": "ok"}`)
	}))
	defer srv.Close()

	h, _, ta := schedHost(t, fetchedView(srv.URL+"/data.json", "undefined"))
	h.Click("go")
	wait(t, ta, func() bool { return len(logOf(t, h)) > 0 })
	if got := logOf(t, h)[0]; got != "status 200 true ct=application/json body={\"items\": [1, 2, 3], \"name\": \"ok\"}" {
		t.Fatalf("fetch result = %q", got)
	}
}

func TestFetch404ResolvesWithOkFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	h, _, ta := schedHost(t, fetchedView(srv.URL+"/missing", "undefined"))
	h.Click("go")
	wait(t, ta, func() bool { return len(logOf(t, h)) > 0 })
	if got := logOf(t, h)[0]; !strings.Contains(got, "status 404 false") {
		t.Fatalf("a 404 did not resolve with ok=false: %q", got)
	}
}

func TestFetchNetworkErrorRejectsThePromise(t *testing.T) {
	// A server that is already closed: the connection is refused.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL + "/unreachable"
	srv.Close()

	h, _, ta := schedHost(t, fetchedView(url, "undefined"))
	h.Click("go")
	wait(t, ta, func() bool { return len(logOf(t, h)) > 0 })
	if got := logOf(t, h)[0]; !strings.HasPrefix(got, "rejected fetch "+url) {
		t.Fatalf("a network error did not reject the promise with the URL: %q", got)
	}
}

func TestFetchJsonParsesTheBodyAndRejectsBadJSON(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"n": 7}`)
	}))
	defer ok.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"n": `)
	}))
	defer bad.Close()

	src := logView + "\nfunction go() {\n" + fmt.Sprintf(`
	fetch(%q).then((r) => r.json()).then((v) => note("json " + v.n));
	fetch(%q).then((r) => r.json()).catch((e) => note("bad " + (e && e.name)));
}
`, ok.URL, bad.URL) + "\n"
	h, _, ta := schedHost(t, src)
	h.Click("go")
	wait(t, ta, func() bool { return len(logOf(t, h)) >= 2 })
	got := strings.Join(logOf(t, h), " | ")
	if !strings.Contains(got, "json 7") || !strings.Contains(got, "bad SyntaxError") {
		t.Fatalf("json() results = %q", got)
	}
}

func TestFetchCarriesMethodHeadersAndBody(t *testing.T) {
	var gotReq atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotReq.Store(reqEcho{
			method:  r.Method,
			ct:      r.Header.Get("Content-Type"),
			token:   r.Header.Get("X-Token"),
			body:    string(body),
			hasJSON: strings.Contains(r.Header.Get("Content-Type"), "json"),
		})
		w.WriteHeader(204)
	}))
	defer srv.Close()

	src := logView + "\nfunction go() {\n" + fmt.Sprintf(`
	fetch(%q, { method: "POST", headers: { "X-Token": "secret" }, body: { note: "hello" } })
		.then((r) => note("posted " + r.status));
}
`, srv.URL+"/save") + "\n"
	h, _, ta := schedHost(t, src)
	h.Click("go")
	wait(t, ta, func() bool { return len(logOf(t, h)) > 0 })
	if got := logOf(t, h)[0]; got != "posted 204" {
		t.Fatalf("fetch result = %q", got)
	}
	echo := gotReq.Load().(reqEcho)
	if echo.method != "POST" || echo.token != "secret" || echo.ct != "application/json" || echo.body != `{"note":"hello"}` {
		t.Fatalf("the request the server saw = %+v", echo)
	}
}

type reqEcho struct {
	method, ct, token, body string
	hasJSON                 bool
}

func TestFetchRejectsRelativeURLsWithANamedError(t *testing.T) {
	h, _, _ := schedHost(t, fetchedView("/relative.json", "undefined"))
	err := h.Click("go")
	if err == nil || !strings.Contains(err.Error(), "not absolute") {
		t.Fatalf("a relative URL did not raise a named error: %v", err)
	}
}

func TestTheInjectedFetcherDrivesTheResponse(t *testing.T) {
	// The app owns the transport (zgui's ScriptHost::fetcher): tests
	// and plugins' gated hosts inject their own.
	ta := gpui.NewTestApp()
	t.Cleanup(func() { ta.App().Shutdown() })
	s := &srcView{src: fetchedView("http://injected.test/data", "undefined")}
	h := script.New(script.WithSource(s), script.WithScheduler(script.AppScheduler(ta.App())),
		script.WithFetcher(func(ctx context.Context, req script.FetchRequest) (script.FetchResponse, error) {
			return script.FetchResponse{
				Status:     201,
				StatusText: "Created",
				Headers:    [][2]string{{"Content-Type", "text/plain"}},
				Body:       []byte("injected"),
			}, nil
		}))
	if err := h.Reload(); err != nil {
		t.Fatalf("load: %v", err)
	}
	h.Click("go")
	wait(t, ta, func() bool { return len(logOf(t, h)) > 0 })
	if got := logOf(t, h)[0]; got != "status 201 true ct=text/plain body=injected" {
		t.Fatalf("injected fetch result = %q", got)
	}
}

func TestAFetchInFlightWhenTheVMReloadedNeverSettles(t *testing.T) {
	// The fetcher blocks until its context is cancelled; a reload
	// aborts it (the browser aborts in-flight fetches on navigation),
	// and the old VM's promise never settles — the host stays healthy
	// and the new VM runs.
	ta := gpui.NewTestApp()
	t.Cleanup(func() { ta.App().Shutdown() })
	s := &srcView{src: fetchedView("http://blocked", "undefined")}
	h := script.New(script.WithSource(s), script.WithScheduler(script.AppScheduler(ta.App())),
		script.WithFetcher(func(ctx context.Context, req script.FetchRequest) (script.FetchResponse, error) {
			<-ctx.Done()
			return script.FetchResponse{}, ctx.Err()
		}))
	if err := h.Reload(); err != nil {
		t.Fatalf("load: %v", err)
	}
	h.Click("go")
	// The fetch is in flight (blocked). Reload: the completion must be
	// dropped, not delivered to the dead VM.
	s.Set(withGo(`note("reloaded");`))
	if err := h.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	// Give the blocked goroutine time to observe the cancellation and
	// post its (dropped) completion, then drive the app.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		ta.RunUntilParked()
		time.Sleep(5 * time.Millisecond)
	}
	// The blocked fetch settles on its goroutine with a cancelled
	// context; nothing may land in the log and no error may surface.
	if got := logOf(t, h); len(got) != 0 {
		t.Fatalf("a fetch whose VM reloaded still settled: %v", got)
	}
	if h.Err() != nil {
		t.Fatalf("the dropped completion surfaced as an error: %v", h.Err())
	}
	// The host is healthy: the new VM's handler runs.
	if err := h.Click("go"); err != nil {
		t.Fatalf("click after reload: %v", err)
	}
	if got := logOf(t, h); len(got) != 1 || got[0] != "reloaded" {
		t.Fatalf("the new VM did not run: %v", got)
	}
}

func TestFetchWithoutASchedulerRaisesANamedError(t *testing.T) {
	h, _ := newHost(t, fetchedView("http://localhost/x", "undefined"))
	err := h.Click("go")
	if err == nil || !strings.Contains(err.Error(), "fetch") || !strings.Contains(err.Error(), "scheduler") {
		t.Fatalf("fetch without a scheduler = %v, want a named error", err)
	}
}
