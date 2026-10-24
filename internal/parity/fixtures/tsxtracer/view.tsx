// The TSX twin of the tracer fixture's view. The numbers are the
// tracer's shared constants (fixtures/tracer/tracer.go); the page they
// must match is internal/parity/testdata/tracer-box-click/page.html:
// a 200x80 box, 8px padding, a 1px #334155 border, #e2e8f0 background,
// #0f172a 16px text, centered in a 640x400 white page, with mousedown
// and click logged on both the box and the root.
//
// JSX elements, a CSS-named style object and a Tailwind className
// together build the same box the Go twin builds with the fluent API:
// the className sizes and pads it (arbitrary px values, so the
// comparison does not depend on the rem scale), the style object
// colors it and sets its font size.

export function App() {
	return (
		<div
			id="root"
			className="flex items-center justify-center"
			style={{ width: 640, height: 400, background: "#ffffff" }}
			onMouseDown={() => logEvent("mousedown", "root")}
			onClick={() => logEvent("click", "root")}
		>
			<div
				id="box"
				// leading-[1.618]: the page pins the reference's 1.618 line
				// height (body font 16px/1.618); the port's default is CSS
				// normal since browser-parity D1, so the class opts into the
				// golden ratio to draw the page's text placement (the Go twin
				// uses LineHeight(PhiLineHeight())).
				className="w-[200px] h-[80px] p-[8px] border leading-[1.618]"
				style={{ borderColor: "#334155", background: "#e2e8f0", color: "#0f172a", fontSize: 16 }}
				onMouseDown={() => logEvent("mousedown", "box")}
				onClick={() => logEvent("click", "box")}
			>
				Hello!
			</div>
		</div>
	);
}
