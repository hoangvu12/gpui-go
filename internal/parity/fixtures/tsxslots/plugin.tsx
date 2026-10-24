// The plugin: its manifest names the slot it fills ("main", the slot
// view.tsx declares), and its component describes that slot's content
// — the tracer's box (200x80, 8px padding, a 1px #334155 border,
// #e2e8f0 background, #0f172a 16px text), so the slot region holds
// exactly what the fixture's page holds.
//
// The plugin runs in its own goja runtime (its own keyed state,
// bindings and budget) and reaches the app only through its grants:
// the "log" action, granted at registration. Its box logs mousedown
// and click through invoke("log", ...) — the grant-gated event path
// this fixture exercises.

export function Main() {
	return (
		<div
			id="greeting"
			className="w-[200px] h-[80px] p-[8px] border"
			style={{ borderColor: "#334155", background: "#e2e8f0", color: "#0f172a", fontSize: 16 }}
			onMouseDown={() => invoke("log", "mousedown", "greeting")}
			onClick={() => invoke("log", "click", "greeting")}
		>
			Hello!
		</div>
	);
}

export const plugin: PluginManifest = { slots: { main: Main } };
