// The probe's sample view: a zterm-like sidebar (examples/zterm/sidebar.go)
// with six agent rows, a counter and a 200-row list. It is described once
// per (re)load; state lives in the Go host, keyed, so a reload keeps it.
//
// Two update styles are used on purpose:
//   - useState (React-like): setting it re-runs App, the whole tree is
//     described again (the "re-describe" path);
//   - state + bind (zgui-like): a click sets the value and only the bound
//     text is recomputed; App does not run again.

type Status = "working" | "blocked" | "error" | "done" | "idle";

interface Agent {
  pane: string;
  name: string;
  where: string;
  status: Status;
}

const agents: Agent[] = [
  { pane: "p1", name: "Refactor the layout pass", where: "gpui-go", status: "working" },
  { pane: "p2", name: "Port the IME tests", where: "zterm", status: "blocked" },
  { pane: "p3", name: "Fix shadow blur sigma", where: "gpui", status: "error" },
  { pane: "p4", name: "Write the release notes", where: "docs", status: "done" },
  { pane: "p5", name: "Probe goja reload", where: "research", status: "idle" },
  { pane: "p6", name: "Tidy the class mapper", where: "gpui", status: "working" },
];

const markColor: Record<Status, Color> = {
  working: "#60a5fa",
  blocked: "#f59e0b",
  error: "#ef4444",
  done: "#22c55e",
  idle: "#6b7280",
};

function AgentRow({ agent, current, onSelect }: { agent: Agent; current: boolean; onSelect: () => void }) {
  return (
    <div
      key={"agent-" + agent.pane}
      className={current ? "rounded-md bg-white/10" : "rounded-md"}
      style={{ display: "flex", alignItems: "center", gap: 8, padding: "6px 8px" }}
      onClick={onSelect}
    >
      <div style={{ width: 8, height: 8, borderRadius: 4, background: markColor[agent.status] }} />
      <div style={{ display: "flex", flexDirection: "column", flex: 1 }}>
        <text style={{ color: "#e5e7eb", fontSize: 13 }}>{agent.name}</text>
        <text style={{ color: "#9ca3af", fontSize: 11 }}>{`${agent.status} · ${agent.where}`}</text>
      </div>
    </div>
  );
}

function SessionRow({ i, selected, onSelect }: { i: number; selected: boolean; onSelect: (i: number) => void }) {
  return (
    <div
      key={"row-" + i}
      className={selected ? "px-2 py-1 rounded bg-blue-500/30" : "px-2 py-1 rounded"}
      style={{ display: "flex", justifyContent: "space-between", color: "#d1d5db", fontSize: 12 }}
      onClick={() => onSelect(i)}
    >
      <text>{`session ${i}`}</text>
      <text style={{ color: "#6b7280" }}>{`${(i * 7) % 60}m ago`}</text>
    </div>
  );
}

export function App() {
  const [current, setCurrent] = useState("current", "p1");
  const [selected, setSelected] = useState("selected", -1);
  const clicks = state("clicks", 0);

  return (
    <div style={{ display: "flex", flexDirection: "column", width: 240, height: "100%", background: "#111827" }}>
      <div className="flex items-center gap-1.5 px-4 pt-1 pb-1.5" style={{ fontSize: 14 }}>
        <text style={{ fontWeight: 600, color: "#d1d5db" }}>Agents</text>
        <text style={{ color: "#6b7280" }}>{String(agents.length)}</text>
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 1, padding: "2px 8px" }}>
        {agents.map((a) => (
          <AgentRow agent={a} current={a.pane === current} onSelect={() => setCurrent(a.pane)} />
        ))}
      </div>
      <div
        key="counter"
        className="mx-2 my-2 px-3 py-1.5 rounded-md bg-gray-700"
        style={{ color: "#f9fafb", fontSize: 13 }}
        onClick={() => clicks.set(clicks.get() + 1)}
      >
        <text>{bind(clicks, (n: number) => `Clicked ${n} times`)}</text>
      </div>
      <div key="sessions" style={{ display: "flex", flexDirection: "column", flex: 1, minHeight: 0, overflowY: "scroll", padding: "0px 8px" }}>
        {Array.from({ length: 200 }, (_, i) => (
          <SessionRow i={i} selected={i === selected} onSelect={setSelected} />
        ))}
      </div>
    </div>
  );
}
