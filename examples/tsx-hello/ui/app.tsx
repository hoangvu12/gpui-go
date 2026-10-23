// The tsx-hello view (ADR 0003's authoring surface): JSX elements, a
// Tailwind className, a CSS-named style object and keyed state with a
// bound label — the same shape a browser component would have.
//
// The page centers the card; clicking the card counts. The label is a
// bound value on the keyed state, so a click recomputes only that
// label (the host's fast path), never the whole tree.
export function App() {
	const clicks = state("clicks", 0);
	return (
		<div
			id="page"
			className="flex items-center justify-center"
			style={{ width: 480, height: 320, background: "#1e293b" }}
		>
			<div
				id="card"
				className="flex items-center justify-center w-[160px] h-[96px] rounded-lg"
				style={{ background: "#2563eb", color: "#ffffff", fontSize: 20 }}
				onClick={() => clicks.set(clicks.get() + 1)}
			>
				<text id="label">{bind(clicks, (n: number) => `Hello TSX! ${n}`)}</text>
			</div>
		</div>
	);
}
