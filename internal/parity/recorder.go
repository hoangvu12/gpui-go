//go:build windows

// The recorder half of the harness: drives the locally installed
// headless Chrome (never a visible window, a throwaway profile) over
// the DevTools protocol and writes the checked-in fixture files —
// element boxes, the event log of the scripted input sequence, a
// screenshot and the Chromium version — for the replay tests to
// compare against (cmd/parityrecord is the command).
package parity

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// instrumentScript is the page-side event instrumentation: every
// [data-id] element gets one listener per kind of the fixture's
// EventKinds (validated against the harness's vocabulary, which stays
// in step with the EventLog helpers) that records the kind, the
// target id and the event object; the final defaultPrevented state is
// read out after dispatch (matching the replay's post-dispatch
// defaultPrevented semantics). An element's data-events attribute
// narrows the kinds it binds to (a comma-separated subset of the same
// vocabulary) - the key fixtures use it to bind key events without
// click on an ancestor whose click the port does not yet bubble for
// keyboard activations (docs/browser-parity-harness.md, known
// divergences). keydown and keyup entries additionally carry the DOM
// key name (e.key), the physical code (e.code) and location
// (e.location, KeyboardEvent's DOM_KEY_LOCATION_* value) and the
// focused element's data-id at listener time
// (document.activeElement, with body normalized to "" - the replay's
// KeyDown/KeyUp helpers record the same set).
func instrumentScript(kinds []string) string {
	// A fixture with no instrumented events (a painting fixture) has a
	// nil kinds slice; normalize it to an empty list so the page script
	// iterates zero kinds instead of JSON's null.
	if kinds == nil {
		kinds = []string{}
	}
	kindList, _ := json.Marshal(kinds)
	return fmt.Sprintf(`(() => {
	window.__events = [];
	const focusOf = () => {
		const el = document.activeElement;
		if (el && el !== document.body && el.dataset && el.dataset.id) return el.dataset.id;
		return "";
	};
	for (const el of document.querySelectorAll('[data-id]')) {
		let bound = %s;
		if (el.dataset.events) {
			bound = el.dataset.events.split(',').map(k => k.trim()).filter(k => k.length > 0);
		}
		for (const kind of bound) {
			el.addEventListener(kind, e => {
				const entry = {kind: kind, target: el.dataset.id, event: e};
				if (kind === "keydown" || kind === "keyup") {
					entry.key = e.key;
					entry.code = e.code;
					entry.location = e.location;
					entry.focus = focusOf();
				}
				window.__events.push(entry);
			});
		}
	}
	return "instrumented";
})()`, kindList)
}

// eventsScript reads the recorded transcript out of the page: each
// event's final default-prevented state (read live from the event
// object, after the whole dispatch, matching the replay's post-dispatch
// markDefaultPrevented) and the key, code, location and focus fields
// of key entries (listener-time snapshots; the location is a number
// only key entries carry, 0 otherwise).
const eventsScript = `JSON.stringify(window.__events.map(r => ({kind: r.kind, target: r.target, defaultPrevented: r.event.defaultPrevented, key: r.key || "", code: r.code || "", location: (r.kind === "keydown" || r.kind === "keyup") ? r.location : 0, focus: r.focus || ""})))`

// RecordFixture records one fixture in headless Chrome and writes its
// files (record.json, chrome.png, page.html) into
// internal/parity/testdata/<name>/. Nothing is shown: Chrome runs
// --headless with a throwaway profile, and the gpui side is not
// involved at all.
func RecordFixture(f Fixture) error {
	if f.HTML == "" || (f.View == nil && f.ViewWithWindow == nil) {
		return fmt.Errorf("parity: fixture %q is incomplete (name, HTML and view are required)", f.Name)
	}
	if err := validate(f); err != nil {
		return err
	}
	chrome, err := FindChrome()
	if err != nil {
		return err
	}

	dir, err := os.MkdirTemp("", "parity-record-"+f.Name)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	pagePath := filepath.Join(dir, "page.html")
	if err := os.WriteFile(pagePath, []byte(f.HTML), 0o644); err != nil {
		return err
	}

	browser, err := launchChrome(chrome, dir, f.ChromeFlags)
	if err != nil {
		return err
	}
	defer browser.stop()

	page, err := browser.openPage()
	if err != nil {
		return err
	}
	defer func() { _ = page.close() }()
	// The viewport and device pixel ratio must match the gpui test
	// window's (logical size, the fixture's scale factor — the harness
	// default 2.0 unless the fixture overrides it).
	scale := FixtureScale(f)
	if err := page.setDeviceMetrics(int(f.Window.Width+0.5), int(f.Window.Height+0.5), scale); err != nil {
		return err
	}
	// The fixture's emulated media features apply before the page loads,
	// so the first style resolution already sees them (forced-colors
	// active restyles the page from scratch).
	if len(f.EmulatedMedia) > 0 {
		if err := page.setEmulatedMedia(f.EmulatedMedia); err != nil {
			return err
		}
	}
	if err := page.navigate("file:///" + filepath.ToSlash(pagePath)); err != nil {
		return err
	}
	if err := page.awaitReady(int(f.Window.Width+0.5), int(f.Window.Height+0.5), scale); err != nil {
		return err
	}
	if _, err := page.evaluate(instrumentScript(f.EventKinds)); err != nil {
		return fmt.Errorf("instrumenting the page: %w", err)
	}

	// The scripted input sequence, dispatched as trusted input (full
	// browser hit testing, exactly what a user's mouse would do). A wait
	// step advances the page's current animations deterministically by
	// its milliseconds (see dispatchStep).
	for i, step := range ExpandSteps(f.Steps) {
		if err := page.dispatchStep(step); err != nil {
			return fmt.Errorf("input step %d (%v): %w", i+1, step, err)
		}
	}

	// Settle: wait for two animation frames before reading anything, so
	// the state the record captures is the settled post-input state —
	// the last input's rendering work (a scroll's compositor update, the
	// anchored elements' recalculation, the intersection observers of
	// position-visibility: anchors-visible) has landed, exactly like the
	// replay, whose boxes come from a fully parked, drawn final frame
	// (ticket 25 found the un-settled read capturing a scroll one key
	// behind). The wait steps leave the page's animations paused, which
	// the settle's frames do not move; they only let the renderer commit
	// the paused state.
	if _, err := page.call("Runtime.evaluate", map[string]any{
		"expression":    "new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve(1))))",
		"awaitPromise":  true,
		"returnByValue": true,
	}); err != nil {
		return fmt.Errorf("settling the page after the input steps: %w", err)
	}

	// The event transcript, the boxes and the screenshot of the final
	// state.
	eventsJSON, err := page.evaluate(eventsScript)
	if err != nil {
		return fmt.Errorf("reading the event log: %w", err)
	}
	var events []EventRecord
	if err := json.Unmarshal([]byte(eventsJSON), &events); err != nil {
		return fmt.Errorf("parsing the event log: %w", err)
	}
	boxesJSON, err := page.evaluate(boxesScript(f.BoxIDs))
	if err != nil {
		return fmt.Errorf("reading the element boxes: %w", err)
	}
	var boxes map[string]Box
	if err := json.Unmarshal([]byte(boxesJSON), &boxes); err != nil {
		return fmt.Errorf("parsing the element boxes: %w", err)
	}
	var computed map[string]string
	if f.ComputedScript != "" {
		computedJSON, err := page.evaluate(f.ComputedScript)
		if err != nil {
			return fmt.Errorf("evaluating the fixture's computed script: %w", err)
		}
		if computedJSON != "" {
			if err := json.Unmarshal([]byte(computedJSON), &computed); err != nil {
				return fmt.Errorf("parsing the computed script's result (it must be a JSON object of strings): %w", err)
			}
		}
	}
	png, err := page.screenshot()
	if err != nil {
		return fmt.Errorf("capturing the screenshot: %w", err)
	}

	record := &Record{
		Name:              f.Name,
		Chrome:            browser.version,
		RecordedAt:        time.Now().UTC().Format(time.RFC3339),
		Viewport:          f.Window,
		DeviceScaleFactor: float64(scale),
		Boxes:             boxes,
		Events:            events,
		Computed:          computed,
	}
	if err := writeRecord(record, png, f.HTML); err != nil {
		return err
	}
	dir2, _ := RecordDir(f.Name)
	fmt.Printf("recorded %s (Chrome %s): %d boxes, %d events, %d-byte screenshot -> %s\n",
		f.Name, browser.version, len(boxes), len(events), len(png), dir2)
	return nil
}

// boxesScript builds the getBoundingClientRect script for the
// fixture's compared ids.
func boxesScript(ids []string) string {
	idList, _ := json.Marshal(ids)
	return fmt.Sprintf(`JSON.stringify(Object.fromEntries(%s.map(id => {
	const r = document.querySelector('[data-id="' + id + '"]').getBoundingClientRect();
	return [id, {x: r.x, y: r.y, width: r.width, height: r.height}];
})))`, idList)
}

// ---------------------------------------------------------------------------
// The headless browser process
// ---------------------------------------------------------------------------

// headlessChrome is one launched headless Chrome with its DevTools
// endpoint up.
type headlessChrome struct {
	cmd     *exec.Cmd
	port    int
	version string
}

// launchChrome starts headless Chrome with a throwaway profile and a
// picked-free DevTools port, and waits for the endpoint to answer.
// extraFlags are the fixture's own ChromeFlags, appended after the
// harness's flags (a duplicate of a harness flag is Chrome's to
// resolve; keep the sets disjoint).
// The port is picked by briefly listening and closing (a tiny race
// another process could win), so a launch whose endpoint never comes
// up is retried on a fresh port.
//
// Chrome's smooth-scrolling setting is disabled: with it on, keyboard
// scrolls arrive as animated (compositor-driven) offsets that land
// nondeterministically (PaintLayerScrollableArea::ScrollAnimatorEnabled
// reads the setting, paint_layer_scrollable_area.cc:771-777), which a
// recorded fixture cannot compare against the port's instant scrolls.
// The smooth-scrolling ticket (browser-core 45, spec D13) re-records
// its fixtures when the port scrolls smoothly.
func launchChrome(path, dir string, extraFlags []string) (*headlessChrome, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		// Pick a free port by briefly listening, then hand it to Chrome.
		probe, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		port := probe.Addr().(*net.TCPAddr).Port
		_ = probe.Close()

		args := []string{
			"--headless=new",
			fmt.Sprintf("--remote-debugging-port=%d", port),
			"--user-data-dir=" + filepath.Join(dir, "profile"),
			"--no-first-run",
			"--no-default-browser-check",
			"--disable-background-timer-throttling",
			"--disable-smooth-scrolling",
			"about:blank",
		}
		// The fixture's flags go before the URL argument.
		args = append(args[:len(args)-1], append(append([]string{}, extraFlags...), "about:blank")...)
		cmd := exec.Command(path, args...)
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("starting %s: %w", path, err)
		}
		h := &headlessChrome{cmd: cmd, port: port}

		// Wait for the DevTools endpoint; the version comes with it.
		deadline := time.Now().Add(20 * time.Second)
		for {
			resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
			if err == nil {
				var version struct {
					Browser string `json:"Browser"`
				}
				_ = json.NewDecoder(resp.Body).Decode(&version)
				resp.Body.Close()
				h.version = version.Browser
				if h.version != "" {
					return h, nil
				}
			}
			if !time.Now().Before(deadline) {
				h.stop()
				lastErr = fmt.Errorf("headless Chrome's DevTools endpoint never came up on port %d", port)
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	return nil, lastErr
}

// stop kills the whole browser process tree (Chrome's launcher exits
// early; the browser itself must go too).
func (h *headlessChrome) stop() {
	// /T takes the tree, /F force: the recorder owns this Chrome.
	_ = exec.Command("taskkill", "/PID", fmt.Sprint(h.cmd.Process.Pid), "/T", "/F").Run()
	_, _ = h.cmd.Process.Wait()
}

// openPage connects to the browser's page target.
func (h *headlessChrome) openPage() (*cdpPage, error) {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", h.port))
	if err != nil {
		return nil, err
	}
	var targets []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
		WS   string `json:"webSocketDebuggerUrl"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&targets)
	resp.Body.Close()
	var pageURL string
	for _, target := range targets {
		if target.Type == "page" {
			pageURL = target.WS
		}
	}
	if pageURL == "" {
		return nil, fmt.Errorf("headless Chrome exposes no page target (%d targets)", len(targets))
	}
	ws, err := dialWS(pageURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to the page target: %w", err)
	}
	return &cdpPage{ws: ws, nextID: 1}, nil
}

// ---------------------------------------------------------------------------
// The DevTools page session
// ---------------------------------------------------------------------------

// cdpPage is one page's DevTools session: commands are
// request/response over the websocket, skipping the protocol's event
// notifications (nothing here needs them; readiness is polled).
type cdpPage struct {
	ws     *wsConn
	nextID int
}

// call sends one CDP command and returns its reply object.
func (p *cdpPage) call(method string, params map[string]any) (map[string]any, error) {
	p.nextID++
	id := p.nextID
	message, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if err := p.ws.send(message); err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	for {
		raw, err := p.ws.receive()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", method, err)
		}
		var reply struct {
			ID     *int           `json:"id"`
			Error  *cdpError      `json:"error"`
			Result map[string]any `json:"result"`
		}
		if err := json.Unmarshal(raw, &reply); err != nil {
			return nil, fmt.Errorf("%s: parsing the reply: %w", method, err)
		}
		if reply.ID == nil {
			continue // an event notification
		}
		if *reply.ID != id {
			continue // a stale reply (cannot happen: single-threaded)
		}
		if reply.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, reply.Error)
		}
		return reply.Result, nil
	}
}

// cdpError is a protocol-level command error.
type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *cdpError) Error() string { return fmt.Sprintf("%d %s", e.Code, e.Message) }

// evaluate runs one JavaScript expression and returns its string
// value.
func (p *cdpPage) evaluate(expression string) (string, error) {
	result, err := p.call("Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
	})
	if err != nil {
		return "", err
	}
	if details, _ := result["exceptionDetails"].(map[string]any); details != nil {
		exception, _ := details["exception"].(map[string]any)
		description, _ := exception["description"].(string)
		if description == "" {
			text, _ := details["text"].(string)
			description = text
		}
		return "", fmt.Errorf("%s", description)
	}
	inner, _ := result["result"].(map[string]any)
	if inner == nil {
		return "", fmt.Errorf("no result object (%v)", result)
	}
	value, _ := inner["value"].(string)
	return value, nil
}

// setDeviceMetrics pins the viewport to width x height CSS pixels at
// the harness's device pixel ratio (the gpui test window's fixed scale
// factor).
func (p *cdpPage) setDeviceMetrics(width, height int, scale float32) error {
	_, err := p.call("Emulation.setDeviceMetricsOverride", map[string]any{
		"width": width, "height": height, "deviceScaleFactor": scale, "mobile": false,
	})
	return err
}

// setEmulatedMedia applies the fixture's emulated media features
// (Emulation.setEmulatedMedia), before the page loads: forced-colors
// active restyles the page (the browser's forced-colours mode, whose
// emulated palette the fixture reads back through its ComputedScript).
func (p *cdpPage) setEmulatedMedia(features []EmulatedMediaFeature) error {
	list := make([]map[string]string, len(features))
	for i, feature := range features {
		list[i] = map[string]string{"name": feature.Name, "value": feature.Value}
	}
	_, err := p.call("Emulation.setEmulatedMedia", map[string]any{"features": list})
	return err
}

// navigate loads the page.
func (p *cdpPage) navigate(rawURL string) error {
	_, err := p.call("Page.navigate", map[string]any{"url": rawURL})
	return err
}

// awaitReady waits for the page's load event and its fonts (system
// fonts resolve immediately, but the wait is cheap and correct), then
// verifies the viewport took the requested logical size.
func (p *cdpPage) awaitReady(width, height int, scale float32) error {
	deadline := time.Now().Add(20 * time.Second)
	for {
		state, err := p.evaluate("document.readyState")
		if err == nil && state == "complete" {
			break
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("the page never finished loading (readyState %q: %v)", state, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := p.call("Runtime.evaluate", map[string]any{
		"expression":    "document.fonts.ready.then(() => true)",
		"awaitPromise":  true,
		"returnByValue": true,
	}); err != nil {
		return err
	}
	size, err := p.evaluate(`[window.innerWidth, window.innerHeight, devicePixelRatio].join(",")`)
	if err != nil {
		return err
	}
	want := fmt.Sprintf("%d,%d,%g", width, height, scale)
	if size != want {
		return fmt.Errorf("the page's viewport is %s, want %s (setDeviceMetrics did not take)", size, want)
	}
	return nil
}

// dispatchStep dispatches one scripted input step as trusted input.
func (p *cdpPage) dispatchStep(step Step) error {
	switch step.Kind {
	case StepMove, StepDown, StepUp:
		eventType := map[StepKind]string{StepMove: "mouseMoved", StepDown: "mousePressed", StepUp: "mouseReleased"}[step.Kind]
		button := step.Button
		if button == "" {
			button = ButtonLeft
		}
		_, err := p.call("Input.dispatchMouseEvent", map[string]any{
			"type": eventType, "x": step.X, "y": step.Y, "button": button, "clickCount": 1,
		})
		return err
	case StepKey:
		return p.dispatchKey(step.Key, step.Code, keyPressFull)
	case StepKeyDown:
		return p.dispatchKey(step.Key, step.Code, keyPressDown)
	case StepKeyUp:
		return p.dispatchKey(step.Key, step.Code, keyPressUp)
	case StepWait:
		if step.Ms <= 0 {
			return fmt.Errorf("a wait step must advance a positive number of milliseconds")
		}
		// Commit the pending style change first: a synchronous style read
		// forces the style recalc (and so the transition creation) the
		// input step's listener triggered, so the transitions exist
		// before time advances under them.
		if _, err := p.evaluate("document.body.offsetWidth; getComputedStyle(document.body).color; 'committed'"); err != nil {
			return fmt.Errorf("committing the pending style change: %w", err)
		}
		// Advance and freeze: every animation the page now runs pauses
		// and its currentTime advances by exactly the step's
		// milliseconds (the Web Animations seek — the deterministic clock
		// this harness uses, since a virtual-time budget does not drive
		// the CSS transition timeline in --headless=new: the transitions
		// created by a style change sit unstarted at currentTime 0 until
		// a frame runs, and no frames are produced during a budget, so
		// the seek is the only reliable sample). A later wait step seeks
		// the then-current animations, so a transition a later input step
		// started begins at 0 again, and a mid-flight interruption reads
		// the paused progress, which the browser's own reversing rule
		// then consumes. The boxes, computed styles and the screenshot
		// below all sample the paused state.
		if _, err := p.evaluate(fmt.Sprintf(`(() => {
			for (const a of document.getAnimations({subtree: true})) {
				a.pause();
				a.currentTime = (a.currentTime == null ? 0 : a.currentTime) + %d;
			}
			return "advanced";
		})()`, step.Ms)); err != nil {
			return fmt.Errorf("advancing the page's animations by %dms: %w", step.Ms, err)
		}
		return nil
	case StepScript:
		// The page half of a scripted state change (browser-core 46):
		// evaluate the expression synchronously on the settled page —
		// the Go half (Step.Run) makes the same change on the replay side
		// (replay.go's applyStep). The record's two-frame settle below
		// then captures whatever the change did (Chrome's scroll
		// anchoring runs at the next layout, which the settle's frames
		// deliver).
		if _, err := p.evaluate(step.Script); err != nil {
			return fmt.Errorf("running the page script: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown step kind %q", step.Kind)
	}
}

// keyPressMode selects which half of a keystroke the recorder sends
// (browser-core ticket 12): a full press (the CDP keyDown+keyUp pair
// of a StepKey step), or one half alone (a StepKeyDown/StepKeyUp
// step's single event, so a key can be held at the record's settle).
type keyPressMode uint8

const (
	keyPressFull keyPressMode = iota
	keyPressDown
	keyPressUp
)

// dispatchKey presses and/or releases one keystroke, using the same
// keystroke syntax the gpui replay parses. An explicit physical code
// (Step.Code, the numpad spelling) overrides the key-derived one, and
// the keystroke takes the platform's spelling first
// (gpui.Keystroke.WithSimulatedCode), so the DOM key, code and
// location the page observes are the ones a real key press of the
// same physical key reports (browser-core 13).
func (p *cdpPage) dispatchKey(keystroke, code string, mode keyPressMode) error {
	key, err := gpui.ParseKeystroke(keystroke)
	if err != nil {
		return err
	}
	if code != "" {
		key.Code = code
	}
	key = key.WithSimulatedCode()
	events, err := cdpKeyEvents(key)
	if err != nil {
		return err
	}
	if mode == keyPressDown {
		events = events[:1]
	} else if mode == keyPressUp {
		events = events[1:]
	}
	for _, event := range events {
		if _, err := p.call("Input.dispatchKeyEvent", event); err != nil {
			return err
		}
	}
	return nil
}

// cdpKeyEvents maps one platform-spelled keystroke onto the DevTools
// key events (keyDown then keyUp). Input.dispatchKeyEvent builds the
// DOM event from what it is given: the key param becomes e.key
// verbatim, the code param becomes e.code (input_handler.cc's
// KeyStringToDomKey / CodeStringToDomCode), and the modifiers,
// location and isKeypad params carry the modifier state and
// KeyboardEvent.location (input_handler.cc's GetEventModifiers maps
// location 1/2 to kIsLeft/kIsRight and isKeypad to kIsKeyPad). So the
// recorder must send the whole set: the DOM key, the physical code,
// the unshifted Windows virtual key code (nativeVirtualKeyCode keeps
// the event from being marked skip-if-unhandled) and the location the
// code implies.
func cdpKeyEvents(key gpui.Keystroke) ([]map[string]any, error) {
	vk, ok := cdpVKOfCode[key.Code]
	if !ok {
		return nil, fmt.Errorf("the parity recorder cannot type key %q (code %q) yet (extend cdpVKOfCode in internal/parity/recorder.go)", key.Key, key.Code)
	}
	modifiers := 0
	if key.Modifiers.Alt {
		modifiers |= 1
	}
	if key.Modifiers.Control {
		modifiers |= 2
	}
	if key.Modifiers.Platform {
		modifiers |= 4
	}
	if key.Modifiers.Shift {
		modifiers |= 8
	}
	base := map[string]any{
		"key":                   domKeyOf(key.Key),
		"code":                  key.Code,
		"windowsVirtualKeyCode": vk,
		"nativeVirtualKeyCode":  vk,
		"modifiers":             modifiers,
	}
	switch key.Location {
	case gpui.KeyLocationLeft:
		base["location"] = 1
	case gpui.KeyLocationRight:
		base["location"] = 2
	case gpui.KeyLocationNumpad:
		base["isKeypad"] = true
	}
	down := map[string]any{}
	for k, v := range base {
		down[k] = v
	}
	down["type"] = "keyDown"
	if key.KeyChar != "" && modifiers == 0 {
		down["text"] = key.KeyChar
	}
	// Enter needs its keypress (browser-core ticket 12): the DOM
	// dispatches the Enter activation click from the '\r' keypress
	// (html_element.cc:4318-4324 — between the keydown and the keyup),
	// and Input.dispatchKeyEvent only generates that keypress when the
	// keyDown carries text (keyboard_event_manager.cc:384-386: empty
	// text ends the keypress fallthrough). A real Enter press types
	// '\r', so the recorder spells it the same way — without this, a
	// recorded Enter produces no keypress and no activation at all.
	if key.Key == "enter" && modifiers == 0 {
		down["text"] = "\r"
	}
	up := map[string]any{}
	for k, v := range base {
		up[k] = v
	}
	up["type"] = "keyUp"
	return []map[string]any{down, up}, nil
}

// cdpVKOfCode maps a physical DOM code onto the Windows virtual key
// code Input.dispatchKeyEvent reports for it (the unshifted US virtual
// key: BracketRight is VK_OEM_6, Numpad1 is VK_NUMPAD1). The recorder
// derives everything else from the keystroke; this table is the one
// piece the keystroke spelling cannot carry.
var cdpVKOfCode = map[string]uint16{}

func init() {
	// The letter, digit and numpad-digit runs (KeyA..KeyZ, Digit0..9,
	// Numpad0..9).
	for i := 0; i < 26; i++ {
		cdpVKOfCode["Key"+string(rune('A'+i))] = uint16('A' + i)
	}
	for i := 0; i < 10; i++ {
		cdpVKOfCode["Digit"+string(rune('0'+i))] = uint16('0' + i)
		cdpVKOfCode["Numpad"+string(rune('0'+i))] = uint16(0x60 + i)
	}
	// The US punctuation keys (VK_OEM_*).
	for _, pair := range [][2]any{
		{"Backquote", uint16(0xC0)}, {"Minus", uint16(0xBD)}, {"Equal", uint16(0xBB)},
		{"BracketLeft", uint16(0xDB)}, {"BracketRight", uint16(0xDD)}, {"Backslash", uint16(0xDC)},
		{"Semicolon", uint16(0xBA)}, {"Quote", uint16(0xDE)}, {"Comma", uint16(0xBC)},
		{"Period", uint16(0xBE)}, {"Slash", uint16(0xBF)},
	} {
		cdpVKOfCode[pair[0].(string)] = pair[1].(uint16)
	}
	// The remaining numpad keys.
	for _, pair := range [][2]any{
		{"NumpadDivide", uint16(0x6F)}, {"NumpadMultiply", uint16(0x6A)},
		{"NumpadSubtract", uint16(0x6D)}, {"NumpadAdd", uint16(0x6B)},
		{"NumpadDecimal", uint16(0x6E)}, {"NumpadEnter", uint16(0x0D)},
	} {
		cdpVKOfCode[pair[0].(string)] = pair[1].(uint16)
	}
	// The immutable names the keystroke grammar spells.
	for _, pair := range [][2]any{
		{"Enter", uint16(0x0D)}, {"Escape", uint16(0x1B)}, {"Tab", uint16(0x09)},
		{"Backspace", uint16(0x08)}, {"Delete", uint16(0x2E)}, {"Space", uint16(0x20)},
		{"ArrowUp", uint16(0x26)}, {"ArrowDown", uint16(0x28)},
		{"ArrowLeft", uint16(0x25)}, {"ArrowRight", uint16(0x27)},
		{"Home", uint16(0x24)}, {"End", uint16(0x23)},
		{"PageUp", uint16(0x21)}, {"PageDown", uint16(0x22)},
		{"Insert", uint16(0x2D)},
	} {
		cdpVKOfCode[pair[0].(string)] = pair[1].(uint16)
	}
	for i := 0; i < 24; i++ {
		cdpVKOfCode[fmt.Sprintf("F%d", i+1)] = uint16(0x70 + i)
	}
}

// screenshot captures the viewport at device resolution as PNG bytes.
func (p *cdpPage) screenshot() ([]byte, error) {
	result, err := p.call("Page.captureScreenshot", map[string]any{"format": "png"})
	if err != nil {
		return nil, err
	}
	data64, _ := result["data"].(string)
	if data64 == "" {
		return nil, fmt.Errorf("empty screenshot data")
	}
	return base64.StdEncoding.DecodeString(data64)
}

// close closes the page session.
func (p *cdpPage) close() error { return p.ws.close() }
