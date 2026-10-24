// The tsc error sample (ticket 63): one element per planted error, so
// every one is reported (tsc reports one error per attribute object
// when several land on the same element). tsc must catch a misspelled
// style key, a bad value, an unknown prop — and a bad Class.
//
// This file never runs: the engine rejects each error at load (a
// banner with file:line); the types must reject them first.

const bad: Class = "p-13px";

export function App() {
	return (
		<div>
			<div style={{ colr: "#ffffff" }}>a</div>
			<div style={{ display: "grid" }}>b</div>
			<div onClik={() => {}}>c</div>
			<text>d</text>
		</div>
	);
}
