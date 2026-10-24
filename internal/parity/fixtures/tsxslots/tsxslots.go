//go:build windows

// Package tsxslots is the plugin fixture (ticket 67, modelled on the
// tsxtracer fixture of ticket 62): a slot in the app's view, filled by
// a plugin through the script host's plugin registry, replayed against
// the same kind of Chrome record as every other fixture. The page's
// "plugin contribution" is static HTML — the record proves the plugin
// path (a separate goja runtime contributing through a granted action)
// lays out, paints and dispatches events exactly like the browser
// renders the equivalent markup.
//
// The plugin's box logs its events through a granted action (the grant
// gate in the event path); the app's root and slot log through the
// app's own global, exactly as tsxtracer's view does.
package tsxslots

import (
	_ "embed"
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/gpui/script"
	"github.com/hoangvu12/gpui-go/gpui/script/compile"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// appSrc is the app's view (view.tsx): the tracer's page shape with a
// slot where the plugin contributes.
//
//go:embed view.tsx
var appSrc string

// pluginSrc is the plugin (plugin.tsx): the manifest naming the slot
// it fills, and the tracer's box as its contribution.
//
//go:embed plugin.tsx
var pluginSrc string

// The page's numbers (the tracer's shape, one slot layer added): a
// 640x400 white page centering a slate slot region (16px padding),
// which the plugin fills with the tracer's 200x80 box (8px padding,
// 1px #334155 border, #e2e8f0 background, #0f172a 16px text).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	slotPadding = 16.0

	boxWidth   = 200.0
	boxHeight  = 80.0
	boxPadding = 8.0
	boxBorder  = 1.0

	pageBackground = 0xffffff
	slotBackground = 0xf1f5f9
	boxBackground  = 0xe2e8f0
	borderColor    = 0x334155
	textColor      = 0x0f172a

	textSize = 16.0

	// clickX/clickY lands inside the plugin's box, clear of its text
	// run (the tracer's own point: the box sits where the tracer's box
	// did — the slot centers its content, so the padding cancels out).
	clickX = 400.0
	clickY = 230.0
)

// Fixture is the plugin-slot fixture.
var Fixture = parity.Fixture{
	Name:   "plugin-slot",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	Steps: []parity.Step{
		parity.Click(clickX, clickY),
	},
	BoxIDs:     []string{"slot", "greeting", "root"},
	EventKinds: []string{"mousedown", "click"},
	View:       View(),
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), copied
		// from the tracer's proven numbers — this fixture is the
		// tracer's page with one uniform slab (the slot) added, so the
		// correct-vs-correct statistics land in the same band; the
		// discrimination test proves a real defect still fails Tier A.
		MaxMeanChannelDiff:    0.1,
		MaxROIMeanChannelDiff: 0.5,
		MaxHotPixelFraction:   0.006,
	},
}

func init() { parity.Register(Fixture) }

// AppSource returns the app's view source (view.tsx, the embedded
// copy) — the fixture's own test and any discrimination twin build
// hosts from it.
func AppSource() string { return appSrc }

// PluginSource returns the plugin's source (plugin.tsx, the embedded
// copy).
func PluginSource() string { return pluginSrc }

// page is the HTML/CSS twin: the slot region with the plugin's box as
// its content. The gpui twin builds the slot with the slot element and
// the box from the plugin's manifest — the shape this fixture exists
// to compare.
const page = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>
	html, body {
		margin: 0;
		padding: 0;
		background: #ffffff;
	}
	body {
		width: 640px;
		height: 400px;
		display: flex;
		align-items: center;
		justify-content: center;
		font: 16px/1.618 "Segoe UI", system-ui, sans-serif;
	}
	.slot {
		box-sizing: border-box;
		display: block;
		padding: 16px;
		background: #f1f5f9;
	}
	.box {
		box-sizing: border-box;
		width: 200px;
		height: 80px;
		padding: 8px;
		border: 1px solid #334155;
		background: #e2e8f0;
		color: #0f172a;
	}
</style>
</head>
<body data-id="root">
	<div class="slot" data-id="slot">
		<div class="box" data-id="greeting">Hello!</div>
	</div>
</body>
</html>
`

// View returns the fixture's view function: one app host and its
// plugin per event log (the harness calls the view every frame; the
// host, its VM and the plugin's live in the closure, as the harness
// guide allows), lowered per call.
func View() func(log *parity.EventLog) gpui.AnyElement {
	var host *script.Host
	var bound *parity.EventLog
	return func(log *parity.EventLog) gpui.AnyElement {
		if host == nil || bound != log {
			bound = log
			host = newHost(log)
		}
		return host.Element()
	}
}

// newHost builds the app host with its plugin registered: the app's
// view declares the slot; the plugin's manifest fills it; the plugin
// logs through the granted "log" action while the app logs through its
// own global (the tsxtracer pattern).
func newHost(log *parity.EventLog) *script.Host {
	host := script.New()
	host.SetGlobal("logEvent", func(kind, target string) { log.Append(kind, target) })
	host.Action("log", func(args []any) (any, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("the log action takes (kind, target)")
		}
		log.Append(args[0].(string), args[1].(string))
		return nil, nil
	})
	slots := host.Slots()
	plugin, err := compile.String("plugin.tsx", pluginSrc)
	if err != nil {
		panic("tsxslots: compiling plugin.tsx: " + err.Error())
	}
	if err := slots.Add("greeting", plugin,
		script.WithPluginGrants(script.Grants{Actions: []string{"log"}})); err != nil {
		panic("tsxslots: adding the plugin: " + err.Error())
	}
	app, err := compile.String("view.tsx", appSrc)
	if err != nil {
		panic("tsxslots: compiling view.tsx: " + err.Error())
	}
	if err := host.Load(app); err != nil {
		panic("tsxslots: loading view.tsx: " + err.Error())
	}
	return host
}
