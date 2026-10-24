// The app's view (the tsxtracer page with a slot): a 640x400 white
// page centering a slate slot region. The slot's children are not this
// view's — the registry owns them: the "greeting" plugin's
// contribution (plugin.tsx), or the fallback children while no plugin
// fills it.
//
// The slot's own handlers log like the root's (the app logs its own
// elements; the plugin logs its box through the granted "log" action).

export function App() {
	return (
		<div
			id="root"
			className="flex items-center justify-center"
			style={{ width: 640, height: 400, background: "#ffffff" }}
			onMouseDown={() => logEvent("mousedown", "root")}
			onClick={() => logEvent("click", "root")}
		>
			<slot
				id="slot"
				name="main"
				style={{ display: "block", padding: 16, background: "#f1f5f9" }}
				onMouseDown={() => logEvent("mousedown", "slot")}
				onClick={() => logEvent("click", "slot")}
			/>
		</div>
	);
}
