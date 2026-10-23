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
	"strings"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// instrumentScript is the page-side event instrumentation: every
// [data-id] element gets one listener per kind of the fixture's
// EventKinds (validated against the harness's vocabulary, which stays
// in step with the EventLog helpers) that records the kind, the
// target id and the event object; the final defaultPrevented state is
// read out after dispatch (matching the replay's post-dispatch
// defaultPrevented semantics).
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
	for (const el of document.querySelectorAll('[data-id]')) {
		for (const kind of %s) {
			el.addEventListener(kind, e => window.__events.push({kind: kind, target: el.dataset.id, event: e}));
		}
	}
	return "instrumented";
})()`, kindList)
}

// eventsScript reads the recorded transcript out of the page, with
// each event's final default-prevented state.
const eventsScript = `JSON.stringify(window.__events.map(r => ({kind: r.kind, target: r.target, defaultPrevented: r.event.defaultPrevented})))`

// RecordFixture records one fixture in headless Chrome and writes its
// files (record.json, chrome.png, page.html) into
// internal/parity/testdata/<name>/. Nothing is shown: Chrome runs
// --headless with a throwaway profile, and the gpui side is not
// involved at all.
func RecordFixture(f Fixture) error {
	if f.HTML == "" || f.View == nil {
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
	// window's (logical size, the harness's fixed scale factor).
	if err := page.setDeviceMetrics(int(f.Window.Width+0.5), int(f.Window.Height+0.5), DeviceScaleFactor); err != nil {
		return err
	}
	if err := page.navigate("file:///" + filepath.ToSlash(pagePath)); err != nil {
		return err
	}
	if err := page.awaitReady(int(f.Window.Width+0.5), int(f.Window.Height+0.5), DeviceScaleFactor); err != nil {
		return err
	}
	if _, err := page.evaluate(instrumentScript(f.EventKinds)); err != nil {
		return fmt.Errorf("instrumenting the page: %w", err)
	}

	// The scripted input sequence, dispatched as trusted input (full
	// browser hit testing, exactly what a user's mouse would do).
	for i, step := range ExpandSteps(f.Steps) {
		if err := page.dispatchStep(step); err != nil {
			return fmt.Errorf("input step %d (%v): %w", i+1, step, err)
		}
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
	png, err := page.screenshot()
	if err != nil {
		return fmt.Errorf("capturing the screenshot: %w", err)
	}

	record := &Record{
		Name:              f.Name,
		Chrome:            browser.version,
		RecordedAt:        time.Now().UTC().Format(time.RFC3339),
		Viewport:          f.Window,
		DeviceScaleFactor: float64(DeviceScaleFactor),
		Boxes:             boxes,
		Events:            events,
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
		return p.dispatchKey(step.Key)
	default:
		return fmt.Errorf("unknown step kind %q", step.Kind)
	}
}

// dispatchKey presses and releases one keystroke, using the same
// keystroke syntax the gpui replay parses.
func (p *cdpPage) dispatchKey(keystroke string) error {
	key, err := gpui.ParseKeystroke(keystroke)
	if err != nil {
		return err
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
	events, err := cdpKeyEvents(key, modifiers)
	if err != nil {
		return err
	}
	for _, event := range events {
		if _, err := p.call("Input.dispatchKeyEvent", event); err != nil {
			return err
		}
	}
	return nil
}

// cdpKeyEvents maps one parsed keystroke onto the DevTools key events
// (keyDown then keyUp, carrying the Windows virtual key code).
func cdpKeyEvents(key gpui.Keystroke, modifiers int) ([]map[string]any, error) {
	mapped, ok := cdpKeys[strings.ToLower(key.Key)]
	if !ok {
		if len(key.Key) == 1 {
			ch := key.Key
			mapped = cdpKey{Key: strings.ToUpper(ch), Code: "Key" + strings.ToUpper(ch), VK: uint16(ch[0])}
			if ch >= "a" && ch <= "z" {
				mapped.VK = uint16(ch[0] - 'a' + 'A')
			}
		} else {
			return nil, fmt.Errorf("the parity recorder cannot type key %q yet (extend cdpKeys in internal/parity/recorder.go)", key.Key)
		}
	}
	base := map[string]any{
		"key":                   mapped.Key,
		"code":                  mapped.Code,
		"windowsVirtualKeyCode": mapped.VK,
		"nativeVirtualKeyCode":  mapped.VK,
		"modifiers":             modifiers,
	}
	down := map[string]any{}
	for k, v := range base {
		down[k] = v
	}
	down["type"] = "keyDown"
	if key.KeyChar != "" && modifiers == 0 {
		down["text"] = key.KeyChar
	}
	up := map[string]any{}
	for k, v := range base {
		up[k] = v
	}
	up["type"] = "keyUp"
	return []map[string]any{down, up}, nil
}

// cdpKey is one key's DevTools identity.
type cdpKey struct {
	Key  string
	Code string
	VK   uint16
}

// cdpKeys maps the pinned keystroke names onto DevTools keys
// (key, code, Windows virtual key code).
var cdpKeys = map[string]cdpKey{
	"enter":      {Key: "Enter", Code: "Enter", VK: 13},
	"escape":     {Key: "Escape", Code: "Escape", VK: 27},
	"tab":        {Key: "Tab", Code: "Tab", VK: 9},
	"backspace":  {Key: "Backspace", Code: "Backspace", VK: 8},
	"delete":     {Key: "Delete", Code: "Delete", VK: 46},
	"space":      {Key: " ", Code: "Space", VK: 32},
	"arrowup":    {Key: "ArrowUp", Code: "ArrowUp", VK: 38},
	"arrowdown":  {Key: "ArrowDown", Code: "ArrowDown", VK: 40},
	"arrowleft":  {Key: "ArrowLeft", Code: "ArrowLeft", VK: 37},
	"arrowright": {Key: "ArrowRight", Code: "ArrowRight", VK: 39},
	"home":       {Key: "Home", Code: "Home", VK: 36},
	"end":        {Key: "End", Code: "End", VK: 35},
	"pageup":     {Key: "PageUp", Code: "PageUp", VK: 33},
	"pagedown":   {Key: "PageDown", Code: "PageDown", VK: 34},
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
