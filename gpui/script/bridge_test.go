//go:build windows

package script_test

// The Go bridge (ticket 64): invoke/call run registered Go actions
// (arguments, return values, errors surfaced as exceptions and
// rejections with useful messages), and shared(key, init) is the
// shared state the app reads and writes through script.NewShared —
// script writes reach Go, Go writes recompute the bindings without a
// re-describe, and the values survive reloads. WithActionResolver and
// a SharedStore view are the seams ticket 67's plugin gates wrap.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/gpui/script"
)

// actionsView is the view the action tests drive: go() calls the
// actions and notes what came back.
const actionsView = `
export function App() {
	return (
		<div id="root" style={{ width: 100, height: 100, background: "#111827" }}>
			<div id="go" style={{ padding: 4 }} onClick={() => go()}>
				<text>go</text>
			</div>
		</div>
	);
}
function note(s: string) {
	const log = state("log", []);
	log.set(log.get().concat([s]));
}
`

func TestInvokeRunsAGoActionWithArgumentsAndReturnValue(t *testing.T) {
	h, _, _ := schedHost(t, actionsView+"\nfunction go() {\n"+
		`const r = invoke("echo", "one", 2, true, { nested: ["x"] });
		note("back " + r.back + " len " + r.len);`+"\n}\n")
	h.Action("echo", func(args []any) (any, error) {
		// Arguments arrive as plain Go values, JSON-shaped.
		if len(args) != 4 {
			return nil, fmt.Errorf("echo got %d arguments, want 4", len(args))
		}
		if args[0] != "one" {
			return nil, fmt.Errorf("arg 0 = %#v", args[0])
		}
		if n, ok := args[1].(int64); !ok || n != 2 {
			return nil, fmt.Errorf("arg 1 = %#v", args[1])
		}
		if args[2] != true {
			return nil, fmt.Errorf("arg 2 = %#v", args[2])
		}
		m, ok := args[3].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("arg 3 = %#v", args[3])
		}
		list, _ := m["nested"].([]any)
		if len(list) != 1 || list[0] != "x" {
			return nil, fmt.Errorf("nested = %#v", m["nested"])
		}
		return map[string]any{"back": "ok", "len": int64(len(args))}, nil
	})
	h.Click("go")
	if got := logOf(t, h); len(got) != 1 || got[0] != "back ok len 4" {
		t.Fatalf("invoke result = %v", got)
	}
}

func TestAnActionErrorSurfacesAsAnExceptionNamingTheAction(t *testing.T) {
	h, _, _ := schedHost(t, actionsView+"\nfunction go() {\n"+
		`invoke("save", { id: 1 });`+"\n}\n")
	h.Action("save", func(args []any) (any, error) {
		return nil, errors.New("the disk is full")
	})
	err := h.Click("go")
	if err == nil {
		t.Fatal("a failing action did not raise")
	}
	for _, want := range []string{"save", "the disk is full"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the exception does not name %q: %v", want, err)
		}
	}
}

func TestInvokingAnUnknownActionIsANamedError(t *testing.T) {
	h, _, _ := schedHost(t, actionsView+"\nfunction go() {\n"+
		`invoke("nope");`+"\n}\n")
	err := h.Click("go")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("an unknown action = %v, want a named error", err)
	}
}

func TestInvokeOnAnAsyncActionSaysWhichToUse(t *testing.T) {
	h, _, _ := schedHost(t, actionsView+"\nfunction go() {\n"+
		`invoke("slow");`+"\n}\n")
	h.AsyncAction("slow", func(ctx context.Context, args []any) (any, error) {
		return nil, nil
	})
	err := h.Click("go")
	if err == nil || !strings.Contains(err.Error(), "call(") {
		t.Fatalf("invoke on an async action = %v, want guidance to call()", err)
	}
}

func TestCallSettlesASyncActionImmediately(t *testing.T) {
	h, _, _ := schedHost(t, actionsView+"\nfunction go() {\n"+
		`call("echo", 21).then((v) => note("then " + v));`+"\n}\n")
	h.Action("echo", func(args []any) (any, error) {
		return args[0].(int64), nil
	})
	h.Click("go")
	// The promise settled during the click's dispatch (its microtask
	// queue drained when the handler returned).
	if got := logOf(t, h); len(got) != 1 || got[0] != "then 21" {
		t.Fatalf("call result = %v", got)
	}
}

func TestCallRejectsWithTheActionError(t *testing.T) {
	h, _, _ := schedHost(t, actionsView+"\nfunction go() {\n"+
		`call("save", 1).then((v) => note("no"), (e) => note("caught " + e.message));`+"\n}\n")
	h.Action("save", func(args []any) (any, error) {
		return nil, errors.New("nope")
	})
	h.Click("go")
	if got := logOf(t, h); len(got) != 1 || !strings.Contains(got[0], "save") || !strings.Contains(got[0], "nope") {
		t.Fatalf("a rejected call = %v", got)
	}
}

func TestCallSettlesAnAsyncActionFromTheHostThread(t *testing.T) {
	h, _, ta := schedHost(t, actionsView+"\nfunction go() {\n"+
		`call("slow", 5).then((v) => note("async " + v));`+"\n}\n")
	h.AsyncAction("slow", func(ctx context.Context, args []any) (any, error) {
		// Blocking work runs off the host thread; the ctx would be
		// cancelled by a reload.
		return args[0].(int64) * 2, nil
	})
	h.Click("go")
	wait(t, ta, func() bool { return len(logOf(t, h)) > 0 })
	if got := logOf(t, h)[0]; got != "async 10" {
		t.Fatalf("async action result = %q", got)
	}
}

func TestAnAsyncActionInFlightWhenTheVMReloadedNeverSettles(t *testing.T) {
	h, s, _ := schedHost(t, actionsView+"\nfunction go() {\n"+
		`call("hang").then((v) => note("settled"), (e) => note("rejected"));`+"\n}\n")
	h.AsyncAction("hang", func(ctx context.Context, args []any) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	h.Click("go")
	s.Set(actionsView + "\nfunction go() {\n" + `note("reloaded");` + "\n}\n")
	if err := h.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := h.Click("go"); err != nil {
		t.Fatalf("click after reload: %v", err)
	}
	if got := logOf(t, h); len(got) != 1 || got[0] != "reloaded" {
		t.Fatalf("the dead VM's action still settled: %v", got)
	}
}

func TestAwaitWorksInsideAsyncHandlers(t *testing.T) {
	// goja runs async functions natively (ES2017); awaiting a call
	// parks the handler and resumes it when the action settles.
	h, _, ta := schedHost(t, actionsView+"\nfunction go() {\n"+
		`(async () => {
	const a = await call("half", 10);
	const b = await call("half", 12);
	note("awaited " + a + " " + b);
})();`+"\n}\n")
	h.Action("half", func(args []any) (any, error) {
		return args[0].(int64) / 2, nil
	})
	h.Click("go")
	wait(t, ta, func() bool { return len(logOf(t, h)) > 0 })
	if got := logOf(t, h)[0]; got != "awaited 5 6" {
		t.Fatalf("awaited result = %q", got)
	}
}

func TestTheActionResolverSeamAnswersNamedGrantErrors(t *testing.T) {
	// Ticket 67's gate shape: a resolver that answers only granted
	// names, with the missing grant named in the error.
	ta := gpui.NewTestApp()
	t.Cleanup(func() { ta.App().Shutdown() })
	granted := map[string]bool{"allowed": true}
	s := &srcView{src: actionsView + "\nfunction go() {\n" +
		`invoke("allowed"); invoke("secret");` + "\n}\n"}
	h := script.New(script.WithSource(s), script.WithScheduler(script.AppScheduler(ta.App())),
		script.WithActionResolver(func(name string) (script.ResolvedAction, error) {
			if !granted[name] {
				return script.ResolvedAction{}, fmt.Errorf("action %q: the script was not granted it (host.grant)", name)
			}
			return script.ResolvedAction{Sync: func(args []any) (any, error) {
				return "granted", nil
			}}, nil
		}))
	if err := h.Reload(); err != nil {
		t.Fatalf("load: %v", err)
	}
	err := h.Click("go")
	if err == nil || !strings.Contains(err.Error(), `"secret"`) || !strings.Contains(err.Error(), "granted") {
		t.Fatalf("a denied action = %v, want a named missing-grant error", err)
	}
}

// sharedView binds shared state: the label shows the shared store's
// "note" value.
const sharedView = `
export function App() {
	const n = shared("note", "initial");
	return (
		<div id="root" style={{ width: 100, height: 100, background: "#111827" }}>
			<div id="go" style={{ padding: 4 }} onClick={() => go()}>
				<text>{bind(n, (v: string) => "shared: " + v)}</text>
			</div>
		</div>
	);
}
function note(s: string) {
	const log = state("log", []);
	log.set(log.get().concat([s]));
}
`

func TestScriptWritesToSharedStateAndGoSeesThem(t *testing.T) {
	ta := gpui.NewTestApp()
	t.Cleanup(func() { ta.App().Shutdown() })
	store := script.NewShared()
	s := &srcView{src: sharedView + "\nfunction go() {\n" +
		`shared("note").set("from-script"); note("wrote");` + "\n}\n"}
	h := script.New(script.WithSource(s), script.WithShared(store))
	if err := h.Reload(); err != nil {
		t.Fatalf("load: %v", err)
	}
	h.Click("go")
	if got, _ := store.Read("note"); got != "from-script" {
		t.Fatalf("the store's value = %v, want what the script wrote", got)
	}
	// The binding recomputed without a re-describe.
	if got, _ := h.Text("go"); got != "shared: from-script" {
		t.Fatalf("the bound label = %q", got)
	}
	if h.DescribeCount() != 1 {
		t.Fatalf("DescribeCount = %d, want 1 (a shared write re-described the tree)", h.DescribeCount())
	}
}

func TestGoWritesToSharedStateAndTheBoundUIUpdates(t *testing.T) {
	ta := gpui.NewTestApp()
	t.Cleanup(func() { ta.App().Shutdown() })
	store := script.NewShared()
	s := &srcView{src: sharedView}
	h := script.New(script.WithSource(s), script.WithShared(store))
	if err := h.Reload(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got, _ := h.Text("go"); got != "shared: initial" {
		t.Fatalf("initial label = %q", got)
	}
	// Go writes the shared store: the binding on that key recomputes
	// (no re-describe), the host asks for a redraw.
	store.Write("note", "from-go")
	if got, _ := h.Text("go"); got != "shared: from-go" {
		t.Fatalf("the bound label after the Go write = %q", got)
	}
	if h.DescribeCount() != 1 {
		t.Fatalf("DescribeCount = %d, want 1 (a Go write re-described the tree)", h.DescribeCount())
	}
}

func TestSharedStateSurvivesReloadsAndOutlivesTheHost(t *testing.T) {
	store := script.NewShared()
	s := &srcView{src: sharedView + "\nfunction go() {\n" +
		`shared("note").set("kept");` + "\n}\n"}
	h := script.New(script.WithSource(s), script.WithShared(store))
	if err := h.Reload(); err != nil {
		t.Fatalf("load: %v", err)
	}
	h.Click("go")
	// Reload (and even a fresh host on the same store): shared values
	// live in the store, not the VM.
	for range 3 {
		if err := h.Reload(); err != nil {
			t.Fatalf("reload: %v", err)
		}
	}
	if got, _ := h.Text("go"); got != "shared: kept" {
		t.Fatalf("the shared value did not survive reloads: %q", got)
	}
	h2 := script.New(script.WithSource(&srcView{src: sharedView}), script.WithShared(store))
	if err := h2.Reload(); err != nil {
		t.Fatalf("load 2: %v", err)
	}
	if got, _ := h2.Text("go"); got != "shared: kept" {
		t.Fatalf("a second host did not see the store's value: %q", got)
	}
}

func TestSharedKeysAndStateKeysAreDistinctValues(t *testing.T) {
	store := script.NewShared()
	src := `
export function App() {
	const a = state("x", "state-x");
	const b = shared("x", "shared-x");
	return (
		<div id="root" style={{ width: 100, height: 100, background: "#111827" }}>
			<div id="go" style={{ padding: 4 }} onClick={() => { shared("x").set("shared-2"); }}>
				<text>{bind(a, (v: string) => v)}|{bind(b, (v: string) => v)}</text>
			</div>
		</div>
	);
}
`
	s := &srcView{src: src}
	h := script.New(script.WithSource(s), script.WithShared(store))
	if err := h.Reload(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got, _ := h.Text("go"); got != "state-x|shared-x" {
		t.Fatalf("initial labels = %q", got)
	}
	h.Click("go")
	if got, _ := h.Text("go"); got != "state-x|shared-2" {
		t.Fatalf("writing the shared key moved the state key too: %q", got)
	}
	if got := h.Get("x"); got != "state-x" {
		t.Fatalf("the private state was touched: %v", got)
	}
}

func TestSharedWithoutAStoreIsANamedError(t *testing.T) {
	// shared(key, init) runs during the describe, so the missing store
	// is a load error that names what to build — the banner shows it.
	h := script.New()
	h.SetSource(&srcView{src: sharedView})
	err := h.Reload()
	if err == nil || !strings.Contains(err.Error(), "shared") || !strings.Contains(err.Error(), "NewShared") {
		t.Fatalf("shared without a store = %v, want a named error", err)
	}
}

func TestSetGlobalStillRefusesTheHostsNames(t *testing.T) {
	h := script.New()
	for _, name := range []string{"setTimeout", "fetch", "console", "call", "invoke", "shared", "URL", "performance"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("SetGlobal(%q) did not collide", name)
				}
			}()
			h.SetGlobal(name, 1)
		}()
	}
}
