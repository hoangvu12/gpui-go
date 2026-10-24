// The TSX surface fixture (ticket 63): one view that exercises the
// whole typed surface — every element, every style property, both
// events, the state globals and a spread of Tailwind classes — so the
// tests prove the types and the engine agree on all of it (tsc checks
// the file against the generated .d.ts; the script host loads and runs
// it; parseStyle and jsxElement accept each declared key, value and
// prop).
//
// logEvent is the test's bridge global (host.SetGlobal); the declare
// keeps tsc happy and esbuild strips it.

declare function logEvent(kind: string, target: string): void;

function Label(props: { text: string }): any {
	return <text style={{ fontWeight: 700 }}>{props.text}</text>;
}

export function App() {
	const clicks = state("clicks", 0);
	const [row, setRow] = useState("row", -1);
	return (
		<div
			id="root"
			className="flex items-center gap-2 p-[8px] rounded-md border"
			style={{
				width: 320,
				height: "100%",
				minHeight: 24,
				padding: "6px 8px",
				background: "#111827",
				color: "#f9fafb",
				borderColor: "#334155",
				border: 1,
				borderRadius: 4,
				fontSize: 16,
				opacity: 0.9,
				overflowY: "auto",
			}}
			onMouseDown={(e) => logEvent("mousedown", "root:" + e.button)}
			onClick={() => clicks.set(clicks.get() + 1)}
		>
			<Label text="surface" />
			<slot id="demo-slot" name="demo" style={{ display: "block", minHeight: 8 }} />
			<div
				id="counter"
				key="counter"
				className="bg-slate-800 text-white hover:bg-slate-700"
				style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "space-between", flex: 1, gap: 4 }}
				onClick={() => {
					logEvent("click", "counter");
					clicks.set(clicks.get() + 1);
					setRow(0);
				}}
			>
				<text>{bind(clicks, (n: number) => `clicked ${n}`)}</text>
				<Fragment>{row === 0 ? "row selected" : "no row"}</Fragment>
			</div>
		</div>
	);
}
