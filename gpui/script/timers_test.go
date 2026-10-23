//go:build windows

package script_test

// Host-provided timers (ticket 64, spec's "host-provided globals"):
// setTimeout/clearTimeout, setInterval/clearInterval, queueMicrotask
// and performance.now, driven through the app scheduler's virtual
// clock (TestApp.AdvanceClock), so ordering and cancellation are
// deterministic. Reload cancels the dead VM's timers; a failed reload
// keeps them, because the old VM still renders.

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/gpui/script"
)

// schedHost is a host on its own test app, whose virtual clock drives
// the timers. mustLoadShared loads src through the dev source.
func schedHost(t testing.TB, src string) (*script.Host, *srcView, *gpui.TestApp) {
	t.Helper()
	ta := gpui.NewTestApp()
	t.Cleanup(func() { ta.App().Shutdown() })
	s := &srcView{src: src}
	h := script.New(script.WithSource(s), script.WithScheduler(script.AppScheduler(ta.App())))
	if err := h.Reload(); err != nil {
		t.Fatalf("load view.tsx: %v", err)
	}
	return h, s, ta
}

// logView is a view whose handlers append to the keyed "log" state,
// which the tests read through Host.Get — the script side of the
// timer assertions. note appends through set (a state value is a
// snapshot: get returns a copy, so a change is a new value, like
// zgui's immutable state).
const logView = `
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

// withGo appends the given function body to logView's go handler.
func withGo(body string) string {
	return logView + "\nfunction go() { " + body + " }\n"
}

// logOf returns the host's "log" state as a string slice.
func logOf(t testing.TB, h *script.Host) []string {
	t.Helper()
	v, _ := h.Get("log").([]any)
	var out []string
	for _, e := range v {
		out = append(out, e.(string))
	}
	return out
}

func TestSetTimeoutFiresOnTheVirtualClock(t *testing.T) {
	h, _, ta := schedHost(t, withGo(`setTimeout(() => note("fired"), 50);`))
	h.Click("go")
	if got := logOf(t, h); len(got) != 0 {
		t.Fatalf("the timer fired before its deadline: %v", got)
	}
	ta.AdvanceClock(49)
	if got := logOf(t, h); len(got) != 0 {
		t.Fatalf("the timer fired at 49 of 50 ms: %v", got)
	}
	ta.AdvanceClock(1)
	if got := logOf(t, h); len(got) != 1 || got[0] != "fired" {
		t.Fatalf("the timer did not fire at its deadline: %v", got)
	}
}

func TestClearTimeoutCancelsTheCallback(t *testing.T) {
	h, _, ta := schedHost(t, withGo(`
		const id = setTimeout(() => note("fired"), 50);
		clearTimeout(id);
	`))
	h.Click("go")
	ta.AdvanceClock(100)
	if got := logOf(t, h); len(got) != 0 {
		t.Fatalf("a cleared timeout fired: %v", got)
	}
}

func TestTimersFireInRegistrationOrderForEqualDeadlines(t *testing.T) {
	// Browsers fire timers with equal deadlines in registration order;
	// the scheduler's FIFO task queue preserves it.
	h, _, ta := schedHost(t, withGo(`
		setTimeout(() => note("a"));
		setTimeout(() => note("b"));
		Promise.resolve().then(() => note("micro"));
	`))
	h.Click("go")
	// The microtask ran at the end of the click's dispatch, before any
	// timer (they are macrotasks).
	if got := logOf(t, h); len(got) != 1 || got[0] != "micro" {
		t.Fatalf("the microtask did not run before the timers: %v", got)
	}
	ta.RunUntilParked()
	if got := strings.Join(logOf(t, h), ","); got != "micro,a,b" {
		t.Fatalf("timer order = %q, want %q", got, "micro,a,b")
	}
}

func TestTimersFireInDeadlineOrder(t *testing.T) {
	h, _, ta := schedHost(t, withGo(`
		setTimeout(() => note("late"), 50);
		setTimeout(() => note("early"), 10);
	`))
	h.Click("go")
	ta.AdvanceClock(100)
	if got := strings.Join(logOf(t, h), ","); got != "early,late" {
		t.Fatalf("timer order = %q, want %q", got, "early,late")
	}
}

func TestSetTimeoutPassesItsArgumentsToTheCallback(t *testing.T) {
	h, _, ta := schedHost(t, withGo(`
		setTimeout((a, b) => note(a + "-" + b), 10, "x", 7);
	`))
	h.Click("go")
	ta.AdvanceClock(10)
	if got := strings.Join(logOf(t, h), ","); got != "x-7" {
		t.Fatalf("callback arguments = %q, want %q", got, "x-7")
	}
}

func TestSetIntervalFiresRepeatedlyUntilCleared(t *testing.T) {
	h, _, ta := schedHost(t, withGo(`
		const id = setInterval(() => note("tick"), 10);
		setTimeout(() => clearInterval(id), 35);
	`))
	h.Click("go")
	ta.AdvanceClock(34)
	if got := len(logOf(t, h)); got != 3 {
		t.Fatalf("interval fired %d times at 34 ms, want 3", got)
	}
	ta.AdvanceClock(100)
	if got := len(logOf(t, h)); got != 3 {
		t.Fatalf("a cleared interval kept firing: %d times", got)
	}
}

func TestAThrowingTimeoutReportsTheErrorAndKeepsTheHostAlive(t *testing.T) {
	h, _, ta := schedHost(t, withGo(`
		setTimeout(() => { throw new TypeError("boom"); }, 10);
		setTimeout(() => note("after"), 20);
	`))
	h.Click("go")
	ta.AdvanceClock(30)
	if h.Err() == nil || !strings.Contains(h.Err().Error(), "boom") {
		t.Fatalf("the timer's error was not reported: %v", h.Err())
	}
	// The host keeps running: the later timer still fires.
	if got := strings.Join(logOf(t, h), ","); got != "after" {
		t.Fatalf("the host stopped after a throwing timer: %v", got)
	}
}

func TestATimerThatChangesStateUpdatesBoundValuesWithoutReDescribing(t *testing.T) {
	src := `
export function App() {
	const n = state("n", 0);
	return (
		<div id="root" style={{ width: 100, height: 100, background: "#111827" }}>
			<div id="go" style={{ padding: 4 }} onClick={() => setTimeout(() => n.set(n.get() + 1), 10)}>
				<text>{bind(n, (v: number) => "n " + v)}</text>
			</div>
		</div>
	);
}
`
	h, _, ta := schedHost(t, src)
	h.Click("go")
	ta.AdvanceClock(10)
	if got, _ := h.Text("go"); got != "n 1" {
		t.Fatalf("bound text = %q, want %q", got, "n 1")
	}
	if h.DescribeCount() != 1 {
		t.Fatalf("DescribeCount = %d, want 1 (a timer-driven update re-described the tree)", h.DescribeCount())
	}
}

func TestTimersAreCancelledWhenAReloadLands(t *testing.T) {
	// A reload replaces the VM: the timers it scheduled must never
	// fire into it. A failed reload keeps the old VM rendering, so its
	// timers keep running.
	h, s, ta := schedHost(t, withGo(`
		setTimeout(() => note("old-vm"), 50);
	`))
	h.Click("go")

	// A failed reload: the old VM survives, its timer still fires.
	s.Set(withGo("<div"))
	if err := h.Reload(); err == nil {
		t.Fatal("the broken source reloaded")
	}
	ta.AdvanceClock(60)
	if got := strings.Join(logOf(t, h), ","); got != "old-vm" {
		t.Fatalf("a failed reload killed the old VM's timers: %v", got)
	}

	// A good reload: the new VM's timers live, the old one's die.
	s.Set(withGo(`
		setTimeout(() => note("new-vm"), 50);
	`))
	if err := h.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	h.Click("go")
	ta.AdvanceClock(60)
	if got := strings.Join(logOf(t, h), ","); got != "old-vm,new-vm" {
		t.Fatalf("timers across reload = %v", got)
	}
}

func TestSetIntervalIsCancelledByReload(t *testing.T) {
	h, s, ta := schedHost(t, withGo(`
		setInterval(() => note("tick"), 10);
	`))
	h.Click("go")
	ta.AdvanceClock(20)
	if got := len(logOf(t, h)); got != 2 {
		t.Fatalf("the interval fired %d times, want 2", got)
	}
	// Reload the same source: the old interval dies with its VM. The
	// new run does not schedule another (nothing calls go again).
	s.Set(withGo(`state("log", []);`))
	if err := h.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	ta.AdvanceClock(100)
	if got := len(logOf(t, h)); got != 2 {
		t.Fatalf("an interval survived its VM's reload: %d fires", got)
	}
}

func TestTimersWithoutASchedulerRaiseANamedError(t *testing.T) {
	h, _ := newHost(t, withGo(`setTimeout(() => note("x"), 10);`))
	err := h.Click("go")
	if err == nil || !strings.Contains(err.Error(), "setTimeout") {
		t.Fatalf("setTimeout without a scheduler = %v, want a named error", err)
	}
}

func TestQueueMicrotaskRunsAtTheEndOfTheDispatch(t *testing.T) {
	h, _, _ := schedHost(t, withGo(`
		queueMicrotask(() => note("micro"));
		note("sync");
	`))
	h.Click("go")
	// The click's dispatch drained the microtask queue when it
	// returned: micro ran after the synchronous part, without any
	// scheduler step.
	if got := strings.Join(logOf(t, h), ","); got != "sync,micro" {
		t.Fatalf("microtask ordering = %q, want %q", got, "sync,micro")
	}
}

func TestQueueMicrotaskErrorsAreReported(t *testing.T) {
	h, _, _ := schedHost(t, withGo(`queueMicrotask(() => { throw new TypeError("micro boom"); });`))
	h.Click("go")
	if h.Err() == nil || !strings.Contains(h.Err().Error(), "micro boom") {
		t.Fatalf("a microtask's error was not reported: %v", h.Err())
	}
}

func TestPerformanceNowFollowsTheVirtualClock(t *testing.T) {
	h, _, ta := schedHost(t, withGo(`
		note("t" + Math.floor(performance.now()));
	`))
	h.Click("go")
	first := strings.Join(logOf(t, h), ",")
	ta.AdvanceClock(25)
	h.Click("go")
	second := strings.Join(logOf(t, h), ",")
	if first != "t0" {
		t.Fatalf("first performance.now() = %q, want t0", first)
	}
	if second != "t0,t25" {
		t.Fatalf("performance.now() after 25 ms = %q, want t25", second)
	}
}

func TestRequestAnimationFrameDrivesTheNextFrame(t *testing.T) {
	src := `
export function App() {
	const n = state("n", 0);
	return (
		<div id="root" style={{ width: 100, height: 100, background: "#111827" }}>
			<div id="go" style={{ padding: 4 }} onClick={() => requestAnimationFrame((t) => n.set(t))}>
				<text>{bind(n, (v: number) => "frame " + v)}</text>
			</div>
		</div>
	);
}
`
	ta := gpui.NewTestApp()
	s := &srcView{src: src}
	h := script.New(script.WithSource(s), script.WithScheduler(script.AppScheduler(ta.App())))
	if err := h.Reload(); err != nil {
		t.Fatalf("load: %v", err)
	}
	w := openScriptWindowWithApp(t, h, ta)
	if got, _ := h.Text("go"); got != "frame 0" {
		t.Fatalf("initial text = %q", got)
	}
	w.clickBox(t, "go")
	// The click's redraw schedules the frame callback; delivering the
	// frame request runs it before the frame paints, so the frame that
	// follows shows the value it set. The timestamp is the scheduler
	// clock the callback ran on.
	w.cx.RunUntilParked()
	ta.AdvanceClock(16)
	w.window.SimulateFrameRequest()
	if got, _ := h.Text("go"); got != "frame 16" {
		t.Fatalf("text after the frame = %q, want %q", got, "frame 16")
	}
	// The bound value updated without a re-describe.
	if h.DescribeCount() != 1 {
		t.Fatalf("DescribeCount = %d, want 1", h.DescribeCount())
	}
}

func TestCancelAnimationFrameStopsTheCallback(t *testing.T) {
	src := `
export function App() {
	const n = state("n", 0);
	return (
		<div id="root" style={{ width: 100, height: 100, background: "#111827" }}>
			<div id="go" style={{ padding: 4 }} onClick={() => {
				const id = requestAnimationFrame(() => n.set(99));
				cancelAnimationFrame(id);
			}}>
				<text>{bind(n, (v: number) => "frame " + v)}</text>
			</div>
		</div>
	);
}
`
	ta := gpui.NewTestApp()
	s := &srcView{src: src}
	h := script.New(script.WithSource(s), script.WithScheduler(script.AppScheduler(ta.App())))
	if err := h.Reload(); err != nil {
		t.Fatalf("load: %v", err)
	}
	w := openScriptWindowWithApp(t, h, ta)
	w.clickBox(t, "go")
	w.cx.RunUntilParked()
	ta.AdvanceClock(16)
	w.window.SimulateFrameRequest()
	if got, _ := h.Text("go"); got != "frame 0" {
		t.Fatalf("a cancelled frame callback ran: %q", got)
	}
}

func TestRequestAnimationFrameCallbacksDieWithTheReload(t *testing.T) {
	src := `
export function App() {
	const n = state("n", 0);
	return (
		<div id="root" style={{ width: 100, height: 100, background: "#111827" }}>
			<div id="go" style={{ padding: 4 }} onClick={() => requestAnimationFrame(() => n.set(99))}>
				<text>{bind(n, (v: number) => "frame " + v)}</text>
			</div>
		</div>
	);
}
`
	ta := gpui.NewTestApp()
	s := &srcView{src: src}
	h := script.New(script.WithSource(s), script.WithScheduler(script.AppScheduler(ta.App())))
	if err := h.Reload(); err != nil {
		t.Fatalf("load: %v", err)
	}
	w := openScriptWindowWithApp(t, h, ta)
	w.clickBox(t, "go")
	w.cx.RunUntilParked()
	if err := h.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	ta.AdvanceClock(16)
	w.window.SimulateFrameRequest()
	if got, _ := h.Text("go"); got != "frame 0" {
		t.Fatalf("a frame callback survived its VM's reload: %q", got)
	}
}

func TestATimerUpdateRedrawsTheWindow(t *testing.T) {
	// The full path a timer drives: the callback runs JS on the host
	// thread (the test app's clock fires it inline), the binding it
	// changes recomputes, and the window redraws — no re-describe.
	src := `
export function App() {
	const n = state("n", 0);
	return (
		<div id="root" style={{ width: 100, height: 100, background: "#111827" }}>
			<div id="go" style={{ padding: 4 }} onClick={() => setTimeout(() => n.set(n.get() + 1), 10)}>
				<text>{bind(n, (v: number) => "n " + v)}</text>
			</div>
		</div>
	);
}
`
	ta := gpui.NewTestApp()
	s := &srcView{src: src}
	h := script.New(script.WithSource(s), script.WithScheduler(script.AppScheduler(ta.App())))
	if err := h.Reload(); err != nil {
		t.Fatalf("load: %v", err)
	}
	w := openScriptWindowWithApp(t, h, ta)
	if got, _ := h.Text("go"); got != "n 0" {
		t.Fatalf("initial text = %q", got)
	}
	before := gpui.WindowDrawCount(w.window)
	w.clickBox(t, "go")
	ta.AdvanceClock(10)
	if got, _ := h.Text("go"); got != "n 1" {
		t.Fatalf("text after the timer = %q, want %q", got, "n 1")
	}
	if gpui.WindowDrawCount(w.window) <= before {
		t.Fatal("the timer's update did not redraw the window")
	}
	if h.DescribeCount() != 1 {
		t.Fatalf("DescribeCount = %d, want 1 (a timer update re-described the tree)", h.DescribeCount())
	}
}

func TestGoWritesToSharedStateRedrawTheBoundUI(t *testing.T) {
	// Go writes the shared store; the binding recomputes and the
	// window redraws it, without re-describing (no script ran).
	src := `
export function App() {
	const note = shared("note", "initial");
	return (
		<div id="root" style={{ width: 100, height: 100, background: "#111827" }}>
			<div id="go" style={{ padding: 4 }}>
				<text>{bind(note, (v: string) => "shared: " + v)}</text>
			</div>
		</div>
	);
}
`
	ta := gpui.NewTestApp()
	store := script.NewShared()
	s := &srcView{src: src}
	h := script.New(script.WithSource(s), script.WithShared(store))
	if err := h.Reload(); err != nil {
		t.Fatalf("load: %v", err)
	}
	w := openScriptWindowWithApp(t, h, ta)
	if got, _ := h.Text("go"); got != "shared: initial" {
		t.Fatalf("initial text = %q", got)
	}
	before := gpui.WindowDrawCount(w.window)
	if err := store.Write("note", "from-go"); err != nil {
		t.Fatalf("write: %v", err)
	}
	// The write marked the window dirty; a parked run draws it.
	w.cx.RunUntilParked()
	if got, _ := h.Text("go"); got != "shared: from-go" {
		t.Fatalf("text after the Go write = %q", got)
	}
	if gpui.WindowDrawCount(w.window) <= before {
		t.Fatal("the shared-state write did not redraw the window")
	}
	if h.DescribeCount() != 1 {
		t.Fatalf("DescribeCount = %d, want 1 (a shared write re-described the tree)", h.DescribeCount())
	}
}
