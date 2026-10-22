package base

import (
	"math"
	"testing"
)

// The Sankey tests of U:plot/shape/sankey.rs (12/12).

const sankeyEpsilon float32 = 1e-3

func sankeyLinks(links ...[3]float64) []SankeyLink {
	out := make([]SankeyLink, len(links))
	for i, l := range links {
		out[i] = NewSankeyLink(int(l[0]), int(l[1]), l[2])
	}
	return out
}

// mustSankey adapts a (graph, err) pair: mustSankey(t)(s.Layout(..)).
func mustSankey(t *testing.T) func(SankeyGraph, error) SankeyGraph {
	t.Helper()
	return func(g SankeyGraph, err error) SankeyGraph {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
}

// U:plot/shape/sankey.rs:948-980
func TestSankeyBuilder(t *testing.T) {
	s := NewSankey()
	if s.nodeWidth != 24 || s.nodePadding != 8 || s.align != SankeyAlignJustify || s.iterations != 6 ||
		s.x0 != 0 || s.y0 != 0 || s.x1 != 1 || s.y1 != 1 {
		t.Fatalf("defaults = %+v", s)
	}
	s = NewSankey().NodeWidth(12).NodePadding(10).NodeAlign(SankeyAlignLeft).Iterations(10).Size(400, 300)
	if s.nodeWidth != 12 || s.nodePadding != 10 || s.align != SankeyAlignLeft || s.iterations != 10 ||
		s.x0 != 0 || s.y0 != 0 || s.x1 != 400 || s.y1 != 300 {
		t.Fatalf("built = %+v", s)
	}
	s = NewSankey().Extent(10, 20, 30, 40)
	if s.x0 != 10 || s.y0 != 20 || s.x1 != 30 || s.y1 != 40 {
		t.Fatalf("extent = %+v", s)
	}
}

// U:plot/shape/sankey.rs:982-1046
func TestSankeyLayoutChain(t *testing.T) {
	links := sankeyLinks([3]float64{0, 1, 5}, [3]float64{1, 2, 5})
	graph := mustSankey(t)(NewSankey().NodeWidth(10).Size(100, 100).Layout(3, links))

	for i, n := range graph.Nodes {
		if n.Depth != i || n.Height != 2-i || n.Layer != i {
			t.Fatalf("node %d depth/height/layer = %d/%d/%d", i, n.Depth, n.Height, n.Layer)
		}
	}
	if graph.LayerCount() != 3 {
		t.Fatalf("layer count = %d", graph.LayerCount())
	}
	if graph.Nodes[0].X0 != 0 || graph.Nodes[1].X0 != 45 || graph.Nodes[2].X0 != 90 {
		t.Fatalf("x0s = %v %v %v", graph.Nodes[0].X0, graph.Nodes[1].X0, graph.Nodes[2].X0)
	}
	for _, n := range graph.Nodes {
		if n.X1-n.X0 != 10 || n.Value != 5 {
			t.Fatalf("node = %+v", n)
		}
		// Every node carries the full flow, so all span the full height.
		if abs32(n.Y1-n.Y0-100) >= sankeyEpsilon {
			t.Fatalf("node height = %v", n.Y1-n.Y0)
		}
	}
	for _, l := range graph.Links {
		// The chain is balanced, so both ribbon ends span the nodes.
		if abs32(l.Width-100) >= sankeyEpsilon || abs32(l.SourceWidth-100) >= sankeyEpsilon || abs32(l.TargetWidth-100) >= sankeyEpsilon {
			t.Fatalf("link = %+v", l)
		}
	}

	// Topology agrees with Layout on the topological fields.
	topology := mustSankey(t)(NewSankey().NodeWidth(10).Size(100, 100).Topology(3, links))
	if topology.LayerCount() != 3 {
		t.Fatalf("topology layer count = %d", topology.LayerCount())
	}
	for i, a := range topology.Nodes {
		b := graph.Nodes[i]
		if a.Depth != b.Depth || a.Height != b.Height || a.Layer != b.Layer || a.Value != b.Value || a.X0 != b.X0 {
			t.Fatalf("topology node %d = %+v, layout %+v", i, a, b)
		}
	}

	// Completing a unit-extent topology on the final extent matches a
	// direct layout on that extent.
	topology = mustSankey(t)(NewSankey().NodeWidth(10).Topology(3, links))
	completed := NewSankey().NodeWidth(10).Size(100, 100).LayoutFrom(topology)
	for i, a := range completed.Nodes {
		b := graph.Nodes[i]
		if a.X0 != b.X0 || a.Y0 != b.Y0 || a.X1 != b.X1 || a.Y1 != b.Y1 {
			t.Fatalf("completed node %d = %+v, layout %+v", i, a, b)
		}
	}
	for i, a := range completed.Links {
		b := graph.Links[i]
		if a.Y0 != b.Y0 || a.Y1 != b.Y1 || a.Width != b.Width {
			t.Fatalf("completed link %d = %+v, layout %+v", i, a, b)
		}
	}
}

// U:plot/shape/sankey.rs:1048-1058
func TestSankeyTopologyLargeChain(t *testing.T) {
	const nodeCount = 50_000
	links := make([]SankeyLink, nodeCount-1)
	for s := range links {
		links[s] = NewSankeyLink(s, s+1, 1)
	}
	graph := mustSankey(t)(NewSankey().Topology(nodeCount, links))
	if graph.Nodes[0].Height != nodeCount-1 || graph.Nodes[nodeCount-1].Depth != nodeCount-1 {
		t.Fatalf("height %d depth %d", graph.Nodes[0].Height, graph.Nodes[nodeCount-1].Depth)
	}
}

// U:plot/shape/sankey.rs:1060-1080
func TestSankeyAlignment(t *testing.T) {
	// A -> B -> C, plus a short branch A -> D.
	links := sankeyLinks([3]float64{0, 1, 1}, [3]float64{1, 2, 1}, [3]float64{0, 3, 1})
	layers := func(align SankeyAlign) [4]int {
		g := mustSankey(t)(NewSankey().NodeAlign(align).Size(100, 100).Layout(4, links))
		var out [4]int
		for i, n := range g.Nodes {
			out[i] = n.Layer
		}
		return out
	}
	for align, want := range map[SankeyAlign][4]int{
		SankeyAlignLeft:    {0, 1, 2, 1},
		SankeyAlignRight:   {0, 1, 2, 2},
		SankeyAlignJustify: {0, 1, 2, 2},
		SankeyAlignCenter:  {0, 1, 2, 1},
	} {
		if got := layers(align); got != want {
			t.Fatalf("%v layers = %v, want %v", align, got, want)
		}
	}
}

// U:plot/shape/sankey.rs:1082-1117
func TestSankeyLinkOffsets(t *testing.T) {
	// One source fanning out into two targets.
	graph := mustSankey(t)(NewSankey().NodeWidth(10).Size(100, 100).Layout(3, sankeyLinks([3]float64{0, 1, 30}, [3]float64{0, 2, 10})))
	source := graph.Nodes[0]
	sourceHeight := source.Y1 - source.Y0
	var totalWidth float32
	for _, l := range graph.Links {
		totalWidth += l.Width
	}
	if abs32(totalWidth-sourceHeight) >= sankeyEpsilon {
		t.Fatalf("total width %v, source height %v", totalWidth, sourceHeight)
	}
	// Widths are proportional to values.
	if abs32(graph.Links[0].Width/graph.Links[1].Width-3) >= sankeyEpsilon {
		t.Fatalf("width ratio = %v", graph.Links[0].Width/graph.Links[1].Width)
	}
	// Outgoing ribbons stack contiguously within the source node.
	first, second := graph.Links[0], graph.Links[1]
	if !(graph.Links[0].Y0 < graph.Links[1].Y0) {
		first, second = graph.Links[1], graph.Links[0]
	}
	if abs32(first.Y0-first.SourceWidth/2-source.Y0) >= sankeyEpsilon {
		t.Fatal("first ribbon not at the source top")
	}
	if abs32(first.Y0+first.SourceWidth/2-(second.Y0-second.SourceWidth/2)) >= sankeyEpsilon {
		t.Fatal("ribbons not contiguous")
	}
	// Each target has a single incoming ribbon filling its full height.
	for _, l := range graph.Links {
		target := graph.Nodes[l.Target]
		if abs32(l.Y1-l.TargetWidth/2-target.Y0) >= sankeyEpsilon || abs32(l.Y1+l.TargetWidth/2-target.Y1) >= sankeyEpsilon {
			t.Fatalf("link %d does not fill its target", l.Index)
		}
	}
}

// U:plot/shape/sankey.rs:1119-1140
func TestSankeyImbalancedLinkWidths(t *testing.T) {
	graph := mustSankey(t)(NewSankey().Size(100, 100).Layout(3, sankeyLinks([3]float64{0, 1, 10}, [3]float64{1, 2, 7})))
	b, c, out := graph.Nodes[1], graph.Nodes[2], graph.Links[1]
	if abs32(out.SourceWidth-(b.Y1-b.Y0)) >= sankeyEpsilon || abs32(out.TargetWidth-(c.Y1-c.Y0)) >= sankeyEpsilon {
		t.Fatalf("end widths = %v / %v", out.SourceWidth, out.TargetWidth)
	}
	// The two ends differ: B is taller (value 10) than C (value 7).
	if !(out.SourceWidth > out.TargetWidth) {
		t.Fatal("source end not wider")
	}
	// Both ends stay centered on their nodes' filled ranges.
	if abs32(out.Y0-(b.Y0+b.Y1)/2) >= sankeyEpsilon || abs32(out.Y1-(c.Y0+c.Y1)/2) >= sankeyEpsilon {
		t.Fatal("ends not centered")
	}
}

// U:plot/shape/sankey.rs:1142-1178
func TestSankeySqrtScaleFillsNodes(t *testing.T) {
	links := sankeyLinks([3]float64{0, 1, 90}, [3]float64{1, 2, 40}, [3]float64{1, 3, 50})
	sqrt := mustSankey(t)(NewSankey().ValueScale(SankeyValueScaleSqrt).Size(100, 100).Layout(4, links))
	for _, n := range sqrt.Nodes {
		h := n.Y1 - n.Y0
		var incoming, outgoing float32
		for _, l := range n.TargetLinks {
			incoming += sqrt.Links[l].TargetWidth
		}
		for _, l := range n.SourceLinks {
			outgoing += sqrt.Links[l].SourceWidth
		}
		if len(n.TargetLinks) > 0 && abs32(incoming-h) >= sankeyEpsilon {
			t.Fatalf("node %d incoming %v != height %v", n.Index, incoming, h)
		}
		if len(n.SourceLinks) > 0 && abs32(outgoing-h) >= sankeyEpsilon {
			t.Fatalf("node %d outgoing %v != height %v", n.Index, outgoing, h)
		}
	}
	// Two leaf nodes (50 and 40) show the sqrt compression directly.
	ratio := (sqrt.Nodes[3].Y1 - sqrt.Nodes[3].Y0) / (sqrt.Nodes[2].Y1 - sqrt.Nodes[2].Y0)
	if abs32(ratio-float32(math.Sqrt(float64(float32(50)/40)))) >= 0.02 {
		t.Fatalf("ratio = %v", ratio)
	}
}

// U:plot/shape/sankey.rs:1180-1195
func TestSankeyValueConservation(t *testing.T) {
	graph := mustSankey(t)(NewSankey().Size(100, 100).Layout(3, sankeyLinks([3]float64{0, 1, 10}, [3]float64{1, 2, 7})))
	if graph.Nodes[1].Value != 10 {
		t.Fatalf("value = %v", graph.Nodes[1].Value)
	}
	for _, n := range graph.Nodes {
		if !(n.Y0 <= n.Y1) || !(n.Y0 >= -sankeyEpsilon) || !(n.Y1 <= 100+sankeyEpsilon) {
			t.Fatalf("node = %+v", n)
		}
	}
}

// U:plot/shape/sankey.rs:1197-1227
func TestSankeyVerticalCentering(t *testing.T) {
	graph := mustSankey(t)(NewSankey().NodePadding(20).Extent(0, 10, 100, 90).Layout(5,
		sankeyLinks([3]float64{0, 2, 40}, [3]float64{1, 2, 10}, [3]float64{2, 3, 25}, [3]float64{2, 4, 25})))
	layers := graph.LayerCount()
	lo := make([]float32, layers)
	hi := make([]float32, layers)
	for l := range lo {
		lo[l], hi[l] = float32(math.Inf(1)), float32(math.Inf(-1))
	}
	for _, n := range graph.Nodes {
		lo[n.Layer] = rustMinF32(lo[n.Layer], n.Y0)
		hi[n.Layer] = rustMaxF32(hi[n.Layer], n.Y1)
	}
	// Extent is [10, 90], midpoint 50; every column's midpoint matches.
	for l := 0; l < layers; l++ {
		if abs32((lo[l]+hi[l])/2-50) >= sankeyEpsilon {
			t.Fatalf("layer %d midpoint = %v", l, (lo[l]+hi[l])/2)
		}
	}
	for _, n := range graph.Nodes {
		if !(n.Y0 >= 10-sankeyEpsilon) || !(n.Y1 <= 90+sankeyEpsilon) {
			t.Fatalf("node outside the extent: %+v", n)
		}
	}
}

// U:plot/shape/sankey.rs:1229-1278
func TestSankeyStaggerFlatColumns(t *testing.T) {
	graph := mustSankey(t)(NewSankey().NodePadding(20).Size(100, 100).Layout(6, sankeyLinks(
		[3]float64{0, 1, 100}, [3]float64{1, 2, 40}, [3]float64{1, 3, 30}, [3]float64{1, 4, 20}, [3]float64{1, 5, 10})))
	c0 := (graph.Nodes[0].Y0 + graph.Nodes[0].Y1) / 2
	c1 := (graph.Nodes[1].Y0 + graph.Nodes[1].Y1) / 2
	if !(abs32(c0-c1) > sankeyEpsilon) {
		t.Fatalf("centers %v and %v not staggered", c0, c1)
	}
	for _, n := range graph.Nodes {
		if !(n.Y0 >= -sankeyEpsilon) || !(n.Y1 <= 100+sankeyEpsilon) {
			t.Fatalf("node outside the extent: %+v", n)
		}
	}
	// Each ribbon end stays attached to its node after the shift.
	for _, n := range graph.Nodes {
		y := n.Y0
		for _, li := range n.SourceLinks {
			l := graph.Links[li]
			if abs32(l.Y0-(y+l.SourceWidth/2)) >= sankeyEpsilon {
				t.Fatalf("link %d source end detached", li)
			}
			y += l.SourceWidth
		}
		y = n.Y0
		for _, li := range n.TargetLinks {
			l := graph.Links[li]
			if abs32(l.Y1-(y+l.TargetWidth/2)) >= sankeyEpsilon {
				t.Fatalf("link %d target end detached", li)
			}
			y += l.TargetWidth
		}
	}
}

// U:plot/shape/sankey.rs:1280-1298
func TestSankeyCircularLink(t *testing.T) {
	s := NewSankey().Size(100, 100)
	if _, err := s.Layout(2, sankeyLinks([3]float64{0, 1, 1}, [3]float64{1, 0, 1})); err != SankeyCircularLink() {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.Layout(1, sankeyLinks([3]float64{0, 0, 1})); err != SankeyCircularLink() {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.Layout(2, sankeyLinks([3]float64{0, 5, 1})); err != SankeyMissingNode(5) {
		t.Fatalf("err = %v", err)
	}
}

// U:plot/shape/sankey.rs:1300-1326
func TestSankeyDegenerate(t *testing.T) {
	graph := mustSankey(t)(NewSankey().Size(100, 100).Layout(0, nil))
	if len(graph.Nodes) != 0 || len(graph.Links) != 0 || graph.LayerCount() != 0 {
		t.Fatalf("empty graph = %+v", graph)
	}

	// All-zero link values must not produce NaN coordinates.
	graph = mustSankey(t)(NewSankey().Size(100, 100).Layout(2, sankeyLinks([3]float64{0, 1, 0})))
	for _, n := range graph.Nodes {
		if abs32(n.Y1-n.Y0) >= sankeyEpsilon || !plotIsFinite(n.X0) || !plotIsFinite(n.X1) || !plotIsFinite(n.Y0) || !plotIsFinite(n.Y1) {
			t.Fatalf("node = %+v", n)
		}
	}

	// Isolated nodes collapse into a single column without dividing by
	// zero.
	graph = mustSankey(t)(NewSankey().Size(100, 100).Layout(2, nil))
	if graph.LayerCount() != 1 {
		t.Fatalf("layer count = %d", graph.LayerCount())
	}
	for _, n := range graph.Nodes {
		if n.X0 != 0 || !plotIsFinite(n.Y0) || !plotIsFinite(n.Y1) {
			t.Fatalf("node = %+v", n)
		}
	}
}
