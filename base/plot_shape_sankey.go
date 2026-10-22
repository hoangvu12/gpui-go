package base

// The Sankey layout (U:plot/shape/sankey.rs, after d3-sankey).
//
// Go adaptations (recorded deviations):
//
//   - SankeyError is an enum with a payload upstream
//     (U:plot/shape/sankey.rs:121-128); the Go type is a comparable struct
//     implementing error, built by SankeyMissingNode and
//     SankeyCircularLink, so `err == SankeyCircularLink()` holds.
//   - Topology/Layout return (SankeyGraph, error) for Result.
//   - f32 arithmetic is kept in float32 with the upstream operation order;
//     `0.99_f32.powi(i)` is computed by the same square-and-multiply loop
//     as LLVM's powi (compiler-rt __powisf2).
//   - sankey_link_path returns (*gpui.Path, bool) for Option<Path>; the
//     path is built through the native tessellator (PathBuilder.Build).

import (
	"fmt"
	"math"
	"sort"

	"github.com/hoangvu12/gpui-go/gpui"
)

// sankeyStaggerRatio is the vertical offset, as a fraction of node
// height, applied to stagger runs of equal-height single-node columns so
// their otherwise-flat ribbons curve (U:plot/shape/sankey.rs:7-9).
const sankeyStaggerRatio float32 = 0.15

// SankeyAlign is the horizontal alignment of nodes across layers, like
// d3-sankey's sankeyLeft / sankeyRight / sankeyCenter / sankeyJustify
// (U:plot/shape/sankey.rs:11-22). Justify is the default.
type SankeyAlign uint8

const (
	// SankeyAlignJustify is the default alignment.
	SankeyAlignJustify SankeyAlign = iota
	// SankeyAlignLeft aligns nodes by depth.
	SankeyAlignLeft
	// SankeyAlignRight aligns nodes by height.
	SankeyAlignRight
	// SankeyAlignCenter centers nodes between their flows.
	SankeyAlignCenter
)

// String renders the variant name like the derived Debug.
func (a SankeyAlign) String() string {
	switch a {
	case SankeyAlignLeft:
		return "Left"
	case SankeyAlignRight:
		return "Right"
	case SankeyAlignCenter:
		return "Center"
	}
	return "Justify"
}

// SankeyLink is an input link: Source and Target are indices into the
// node list (d3-sankey's default nodeId), Value is the flow amount
// (U:plot/shape/sankey.rs:24-46).
type SankeyLink struct {
	Source int
	Target int
	Value  float64
}

// NewSankeyLink returns a link from the source node index to the target
// node index carrying value (U:plot/shape/sankey.rs:37-45).
func NewSankeyLink(source, target int, value float64) SankeyLink {
	return SankeyLink{Source: source, Target: target, Value: value}
}

// SankeyNodeLayout is a node with computed layout
// (U:plot/shape/sankey.rs:48-71).
type SankeyNodeLayout struct {
	Index int
	// Value is the node's throughput in the layout's value space:
	// max(sum of incoming, sum of outgoing). With a non-linear
	// SankeyValueScale this is in scaled units.
	Value float64
	// Depth is the topological distance from any source node (longest
	// path).
	Depth int
	// Height is the topological distance to any sink node (longest path).
	Height int
	// Layer is the horizontal column index after alignment.
	Layer          int
	X0, X1, Y0, Y1 float32
	// SourceLinks are indices into SankeyGraph.Links of the outgoing
	// links.
	SourceLinks []int
	// TargetLinks are indices into SankeyGraph.Links of the incoming
	// links.
	TargetLinks []int
}

// SankeyLinkLayout is a link with computed layout
// (U:plot/shape/sankey.rs:73-100). Like d3-sankey, Y0 and Y1 are the
// vertical centers of the ribbon at the source and target end; each end
// has its own width.
type SankeyLinkLayout struct {
	Index  int
	Source int
	Target int
	// Value is the flow value in the layout's value space.
	Value  float64
	Y0, Y1 float32
	// Width is the nominal width from the global value scale, used by the
	// layout relaxation; equals both end widths on a balanced graph.
	Width float32
	// SourceWidth is the ribbon width at the source end.
	SourceWidth float32
	// TargetWidth is the ribbon width at the target end.
	TargetWidth float32
}

// SankeyGraph is the computed Sankey layout
// (U:plot/shape/sankey.rs:102-108).
type SankeyGraph struct {
	Nodes []SankeyNodeLayout
	Links []SankeyLinkLayout
}

// LayerCount returns the number of layers (max layer + 1), 0 for an empty
// graph (U:plot/shape/sankey.rs:111-118).
func (g *SankeyGraph) LayerCount() int {
	n := 0
	for i := range g.Nodes {
		n = max(n, g.Nodes[i].Layer+1)
	}
	return n
}

// SankeyErrorKind tells the SankeyError variants apart.
type SankeyErrorKind uint8

const (
	// SankeyErrorMissingNode: a link references a node index out of range.
	SankeyErrorMissingNode SankeyErrorKind = iota
	// SankeyErrorCircularLink: the graph contains a circular link.
	SankeyErrorCircularLink
)

// SankeyError is a reason a Sankey layout could not be computed
// (U:plot/shape/sankey.rs:121-139).
type SankeyError struct {
	Kind SankeyErrorKind
	// Node is the missing node index (MissingNode only).
	Node int
}

// SankeyMissingNode is SankeyError::MissingNode(index).
func SankeyMissingNode(index int) SankeyError {
	return SankeyError{Kind: SankeyErrorMissingNode, Node: index}
}

// SankeyCircularLink is SankeyError::CircularLink.
func SankeyCircularLink() SankeyError { return SankeyError{Kind: SankeyErrorCircularLink} }

// Error implements error like the upstream Display
// (U:plot/shape/sankey.rs:130-137).
func (e SankeyError) Error() string {
	if e.Kind == SankeyErrorMissingNode {
		return fmt.Sprintf("missing node: %d", e.Node)
	}
	return "circular link"
}

// SankeyValueScale is how flow values map to node heights and ribbon
// widths (U:plot/shape/sankey.rs:141-151). Linear is the default.
type SankeyValueScale uint8

const (
	// SankeyValueScaleLinear: height is proportional to the value.
	SankeyValueScaleLinear SankeyValueScale = iota
	// SankeyValueScaleSqrt: height is proportional to the square root of
	// the value, compressing a wide value range.
	SankeyValueScaleSqrt
)

// apply maps a raw value into the layout's value space
// (U:plot/shape/sankey.rs:154-160).
func (s SankeyValueScale) apply(value float64) float64 {
	if s == SankeyValueScaleSqrt {
		// Guard against tiny negatives from bad data.
		return math.Sqrt(math.Max(value, 0))
	}
	return value
}

// Sankey is the Sankey layout generator (U:plot/shape/sankey.rs:163-174).
type Sankey struct {
	nodeWidth   float32
	nodePadding float32
	align       SankeyAlign
	iterations  int
	valueScale  SankeyValueScale
	x0, y0      float32
	x1, y1      float32
}

// NewSankey returns a generator with the d3-sankey defaults: node width
// 24, padding 8, Justify, 6 iterations, Linear, extent [[0,0],[1,1]]
// (U:plot/shape/sankey.rs:176-196).
func NewSankey() Sankey {
	return Sankey{nodeWidth: 24, nodePadding: 8, iterations: 6, x1: 1, y1: 1}
}

// NodeWidth sets the node rectangle width (U:plot/shape/sankey.rs:198-202).
func (s Sankey) NodeWidth(nodeWidth float32) Sankey { s.nodeWidth = nodeWidth; return s }

// NodePadding sets the vertical gap between nodes in a column
// (U:plot/shape/sankey.rs:204-208).
func (s Sankey) NodePadding(nodePadding float32) Sankey { s.nodePadding = nodePadding; return s }

// NodeAlign sets the node alignment (U:plot/shape/sankey.rs:210-214).
func (s Sankey) NodeAlign(align SankeyAlign) Sankey { s.align = align; return s }

// Iterations sets the number of relaxation passes
// (U:plot/shape/sankey.rs:216-220).
func (s Sankey) Iterations(iterations int) Sankey { s.iterations = iterations; return s }

// ValueScale sets how values map to heights
// (U:plot/shape/sankey.rs:222-226).
func (s Sankey) ValueScale(valueScale SankeyValueScale) Sankey { s.valueScale = valueScale; return s }

// Extent sets the layout bounds as [[x0, y0], [x1, y1]]
// (U:plot/shape/sankey.rs:228-235).
func (s Sankey) Extent(x0, y0, x1, y1 float32) Sankey {
	s.x0, s.y0, s.x1, s.y1 = x0, y0, x1, y1
	return s
}

// Size is Extent(0, 0, width, height) (U:plot/shape/sankey.rs:237-240).
func (s Sankey) Size(width, height float32) Sankey { return s.Extent(0, 0, width, height) }

// Topology computes the topology only: node value, depth, height, layer
// and horizontal positions, without the vertical placement
// (U:plot/shape/sankey.rs:242-301). nodeCount is the number of nodes;
// links reference nodes by index. It fails when a link references a node
// out of range or the graph contains a cycle.
func (s Sankey) Topology(nodeCount int, links []SankeyLink) (SankeyGraph, error) {
	for _, link := range links {
		if link.Source < 0 || link.Source >= nodeCount {
			return SankeyGraph{}, SankeyMissingNode(link.Source)
		}
		if link.Target < 0 || link.Target >= nodeCount {
			return SankeyGraph{}, SankeyMissingNode(link.Target)
		}
	}

	graph := SankeyGraph{
		Nodes: make([]SankeyNodeLayout, nodeCount),
		Links: make([]SankeyLinkLayout, len(links)),
	}
	for i := range graph.Nodes {
		graph.Nodes[i].Index = i
	}
	for i, link := range links {
		graph.Links[i] = SankeyLinkLayout{
			Index:  i,
			Source: link.Source,
			Target: link.Target,
			// Layout works in scaled value space.
			Value: s.valueScale.apply(link.Value),
		}
	}
	if nodeCount == 0 {
		return graph, nil
	}

	sankeyComputeNodeLinks(&graph)
	sankeyComputeNodeValues(&graph)
	if err := sankeyComputeNodeRanks(&graph); err != nil {
		return SankeyGraph{}, err
	}
	s.computeNodeLayers(&graph)
	return graph, nil
}

// Layout computes the full layout: Topology plus the vertical node
// placement and link breadths (U:plot/shape/sankey.rs:303-311).
func (s Sankey) Layout(nodeCount int, links []SankeyLink) (SankeyGraph, error) {
	graph, err := s.Topology(nodeCount, links)
	if err != nil {
		return SankeyGraph{}, err
	}
	return s.LayoutFrom(graph), nil
}

// LayoutFrom completes the layout for a graph produced by Topology on this
// generator's extent (U:plot/shape/sankey.rs:313-337).
func (s Sankey) LayoutFrom(graph SankeyGraph) SankeyGraph {
	if len(graph.Nodes) == 0 {
		return graph
	}
	s.computeNodeLayers(&graph)

	columns := make([][]int, graph.LayerCount())
	for _, node := range graph.Nodes {
		columns[node.Layer] = append(columns[node.Layer], node.Index)
	}

	s.computeNodeBreadths(&graph, columns)
	sankeyComputeLinkBreadths(&graph)
	s.centerColumns(&graph)
	s.staggerFlatColumns(&graph)
	return graph
}

// centerColumns vertically centers each column's stack of nodes within
// the extent (U:plot/shape/sankey.rs:339-371).
func (s Sankey) centerColumns(graph *SankeyGraph) {
	layers := graph.LayerCount()
	if layers == 0 {
		return
	}
	lo := make([]float32, layers)
	hi := make([]float32, layers)
	for l := range lo {
		lo[l] = float32(math.Inf(1))
		hi[l] = float32(math.Inf(-1))
	}
	for _, node := range graph.Nodes {
		lo[node.Layer] = rustMinF32(lo[node.Layer], node.Y0)
		hi[node.Layer] = rustMaxF32(hi[node.Layer], node.Y1)
	}
	offsets := make([]float32, layers)
	for l := range offsets {
		if plotIsFinite(lo[l]) && hi[l] > lo[l] {
			offsets[l] = (s.y0 + s.y1 - lo[l] - hi[l]) / 2
		}
	}
	sankeyApplyLayerOffsets(graph, offsets)
}

// staggerFlatColumns nudges runs of adjacent single-node columns of
// (near-)equal height off the shared center line
// (U:plot/shape/sankey.rs:373-423).
func (s Sankey) staggerFlatColumns(graph *SankeyGraph) {
	layers := graph.LayerCount()
	if layers < 2 {
		return
	}
	count := make([]int, layers)
	single := make([]int, layers)
	for _, node := range graph.Nodes {
		count[node.Layer]++
		single[node.Layer] = node.Index
	}
	heights := make([]float32, layers)
	for l := range heights {
		if count[l] == 1 {
			n := &graph.Nodes[single[l]]
			heights[l] = n.Y1 - n.Y0
		}
	}

	// Offset the odd columns of each flat run, leaving the even ones on
	// the center line, so consecutive ribbons bend down then back up.
	offsets := make([]float32, layers)
	run := 0
	for l := 1; l < layers; l++ {
		flat := count[l] == 1 && count[l-1] == 1 && abs32(heights[l]-heights[l-1]) < 1e-3
		if flat {
			run++
			if run%2 == 1 {
				// Bound the nudge by the slack so the node stays inside
				// the extent.
				slack := rustMaxF32(s.y1-s.y0-heights[l], 0)
				offsets[l] = rustMinF32(heights[l]*sankeyStaggerRatio, slack/2)
			}
		} else {
			run = 0
		}
	}
	sankeyApplyLayerOffsets(graph, offsets)
}

// alignLayer is the node's layer under the alignment
// (U:plot/shape/sankey.rs:425-452).
func (s Sankey) alignLayer(graph *SankeyGraph, index, n int) int {
	node := &graph.Nodes[index]
	switch s.align {
	case SankeyAlignLeft:
		return node.Depth
	case SankeyAlignRight:
		return n - 1 - node.Height
	case SankeyAlignCenter:
		if len(node.TargetLinks) > 0 {
			return node.Depth
		}
		if len(node.SourceLinks) > 0 {
			m := -1
			for _, link := range node.SourceLinks {
				d := graph.Nodes[graph.Links[link].Target].Depth
				if m < 0 || d < m {
					m = d
				}
			}
			return max(m-1, 0)
		}
		return 0
	}
	if len(node.SourceLinks) == 0 {
		return n - 1
	}
	return node.Depth
}

// computeNodeLayers assigns each node's layer and horizontal position
// (U:plot/shape/sankey.rs:454-477).
func (s Sankey) computeNodeLayers(graph *SankeyGraph) {
	n := 0
	for i := range graph.Nodes {
		n = max(n, graph.Nodes[i].Depth+1)
	}
	var kx float32
	if n > 1 {
		kx = (s.x1 - s.x0 - s.nodeWidth) / float32(n-1)
	}
	layers := make([]int, len(graph.Nodes))
	for index := range layers {
		layers[index] = min(s.alignLayer(graph, index, n), n-1)
	}
	for index, layer := range layers {
		node := &graph.Nodes[index]
		node.Layer = layer
		node.X0 = s.x0 + float32(layer)*kx
		node.X1 = node.X0 + s.nodeWidth
	}
}

// computeNodeBreadths places the nodes vertically
// (U:plot/shape/sankey.rs:479-496).
func (s Sankey) computeNodeBreadths(graph *SankeyGraph, columns [][]int) {
	maxColumnLen := 0
	for _, column := range columns {
		maxColumnLen = max(maxColumnLen, len(column))
	}
	py := s.nodePadding
	if maxColumnLen > 1 {
		py = rustMinF32(s.nodePadding, (s.y1-s.y0)/float32(maxColumnLen-1))
	}

	s.initializeNodeBreadths(graph, columns, py)

	for i := 0; i < s.iterations; i++ {
		alpha := powiF32(0.99, i)
		beta := rustMaxF32(1-alpha, float32(i+1)/float32(s.iterations))
		s.relaxRightToLeft(graph, columns, alpha, beta, py)
		s.relaxLeftToRight(graph, columns, alpha, beta, py)
	}
}

// initializeNodeBreadths is the initial vertical placement
// (U:plot/shape/sankey.rs:498-545).
func (s Sankey) initializeNodeBreadths(graph *SankeyGraph, columns [][]int, py float32) {
	// Scale factor between flow value and pixels, clamped to zero so
	// heights never invert.
	ky := float32(math.Inf(1))
	for _, column := range columns {
		var valueSum float64
		for _, index := range column {
			valueSum += graph.Nodes[index].Value
		}
		if valueSum > 0 {
			k := (s.y1 - s.y0 - float32(len(column)-1)*py) / float32(valueSum)
			ky = rustMinF32(ky, k)
		}
	}
	if !plotIsFinite(ky) {
		ky = 0
	}
	ky = rustMaxF32(ky, 0)

	for _, column := range columns {
		y := s.y0
		for _, index := range column {
			nodeHeight := float32(graph.Nodes[index].Value) * ky
			node := &graph.Nodes[index]
			node.Y0 = y
			node.Y1 = y + nodeHeight
			y = node.Y1 + py
		}

		// Distribute the leftover vertical space evenly (signed).
		leftover := (s.y1 - y + py) / float32(len(column)+1)
		for i, index := range column {
			node := &graph.Nodes[index]
			dy := leftover * float32(i+1)
			node.Y0 += dy
			node.Y1 += dy
		}
	}

	for i := range graph.Links {
		graph.Links[i].Width = float32(graph.Links[i].Value) * ky
	}

	for _, column := range columns {
		for _, index := range column {
			sankeySortSourceLinks(graph, index)
			sankeySortTargetLinks(graph, index)
		}
	}
}

// relaxLeftToRight repositions each node based on its incoming links
// (U:plot/shape/sankey.rs:547-579).
func (s Sankey) relaxLeftToRight(graph *SankeyGraph, columns [][]int, alpha, beta, py float32) {
	for i := 1; i < len(columns); i++ {
		for _, target := range columns[i] {
			var y, w float32
			for _, linkIndex := range graph.Nodes[target].TargetLinks {
				link := &graph.Links[linkIndex]
				v := float32(link.Value) * (float32(graph.Nodes[target].Layer) - float32(graph.Nodes[link.Source].Layer))
				y += sankeyTargetTop(graph, link.Source, target, py) * v
				w += v
			}
			if w <= 0 {
				continue
			}
			dy := (y/w - graph.Nodes[target].Y0) * alpha
			graph.Nodes[target].Y0 += dy
			graph.Nodes[target].Y1 += dy
			sankeyReorderNodeLinks(graph, target)
		}
		sankeySortColumn(graph, columns[i])
		s.resolveCollisions(graph, columns[i], beta, py)
	}
}

// relaxRightToLeft repositions each node based on its outgoing links
// (U:plot/shape/sankey.rs:581-613).
func (s Sankey) relaxRightToLeft(graph *SankeyGraph, columns [][]int, alpha, beta, py float32) {
	for i := len(columns) - 2; i >= 0; i-- {
		for _, source := range columns[i] {
			var y, w float32
			for _, linkIndex := range graph.Nodes[source].SourceLinks {
				link := &graph.Links[linkIndex]
				v := float32(link.Value) * (float32(graph.Nodes[link.Target].Layer) - float32(graph.Nodes[source].Layer))
				y += sankeySourceTop(graph, source, link.Target, py) * v
				w += v
			}
			if w <= 0 {
				continue
			}
			dy := (y/w - graph.Nodes[source].Y0) * alpha
			graph.Nodes[source].Y0 += dy
			graph.Nodes[source].Y1 += dy
			sankeyReorderNodeLinks(graph, source)
		}
		sankeySortColumn(graph, columns[i])
		s.resolveCollisions(graph, columns[i], beta, py)
	}
}

// resolveCollisions is d3's middle-out collision resolution
// (U:plot/shape/sankey.rs:615-629).
func (s Sankey) resolveCollisions(graph *SankeyGraph, column []int, beta, py float32) {
	if len(column) == 0 {
		return
	}
	i := len(column) >> 1
	subjectY0 := graph.Nodes[column[i]].Y0
	subjectY1 := graph.Nodes[column[i]].Y1
	sankeyPushUp(graph, column[:i], subjectY0-py, beta, py)
	sankeyPushDown(graph, column[i+1:], subjectY1+py, beta, py)
	sankeyPushUp(graph, column, s.y1, beta, py)
	sankeyPushDown(graph, column, s.y0, beta, py)
}

// sankeyApplyLayerOffsets shifts every node (and its attached link ends)
// by its layer's offset (U:plot/shape/sankey.rs:632-647).
func sankeyApplyLayerOffsets(graph *SankeyGraph, offsets []float32) {
	nodeOffset := make([]float32, len(graph.Nodes))
	for i := range graph.Nodes {
		nodeOffset[i] = offsets[graph.Nodes[i].Layer]
	}
	for i := range graph.Nodes {
		node := &graph.Nodes[i]
		dy := nodeOffset[node.Index]
		node.Y0 += dy
		node.Y1 += dy
	}
	for i := range graph.Links {
		link := &graph.Links[i]
		link.Y0 += nodeOffset[link.Source]
		link.Y1 += nodeOffset[link.Target]
	}
}

// sankeyComputeNodeLinks is U:plot/shape/sankey.rs:649-655.
func sankeyComputeNodeLinks(graph *SankeyGraph) {
	for index := range graph.Links {
		source, target := graph.Links[index].Source, graph.Links[index].Target
		graph.Nodes[source].SourceLinks = append(graph.Nodes[source].SourceLinks, index)
		graph.Nodes[target].TargetLinks = append(graph.Nodes[target].TargetLinks, index)
	}
}

// sankeyComputeNodeValues is U:plot/shape/sankey.rs:657-671.
func sankeyComputeNodeValues(graph *SankeyGraph) {
	for index := range graph.Nodes {
		var outgoing, incoming float64
		for _, link := range graph.Nodes[index].SourceLinks {
			outgoing += graph.Links[link].Value
		}
		for _, link := range graph.Nodes[index].TargetLinks {
			incoming += graph.Links[link].Value
		}
		graph.Nodes[index].Value = math.Max(outgoing, incoming)
	}
}

// sankeyComputeNodeRanks assigns the longest-path depth and height from a
// single topological ordering; a node left unordered means a cycle
// (U:plot/shape/sankey.rs:673-722).
func sankeyComputeNodeRanks(graph *SankeyGraph) error {
	n := len(graph.Nodes)
	incoming := make([]int, n)
	var order []int
	for i := range graph.Nodes {
		incoming[i] = len(graph.Nodes[i].TargetLinks)
		if incoming[i] == 0 {
			order = append(order, i)
		}
	}

	depths := make([]int, n)
	for visited := 0; visited < len(order); visited++ {
		index := order[visited]
		depth := depths[index] + 1
		for _, link := range graph.Nodes[index].SourceLinks {
			target := graph.Links[link].Target
			depths[target] = max(depths[target], depth)
			incoming[target]--
			if incoming[target] == 0 {
				order = append(order, target)
			}
		}
	}
	if len(order) != n {
		return SankeyCircularLink()
	}

	heights := make([]int, n)
	for i := len(order) - 1; i >= 0; i-- {
		index := order[i]
		for _, link := range graph.Nodes[index].SourceLinks {
			heights[index] = max(heights[index], heights[graph.Links[link].Target]+1)
		}
	}
	for i := range graph.Nodes {
		graph.Nodes[i].Depth = depths[i]
		graph.Nodes[i].Height = heights[i]
	}
	return nil
}

// sankeyLess orders by y (incomparable counts as equal) then index,
// a total order (U:plot/shape/sankey.rs:728-733).
func sankeyLess(ya, yb float32, a, b int) bool {
	if ya < yb {
		return true
	}
	if ya > yb {
		return false
	}
	return a < b
}

// sankeySortSourceLinks sorts a node's outgoing links by the target node's
// y0, then link index (U:plot/shape/sankey.rs:724-737).
func sankeySortSourceLinks(graph *SankeyGraph, index int) {
	links := graph.Nodes[index].SourceLinks
	sort.Slice(links, func(i, j int) bool {
		a, b := links[i], links[j]
		return sankeyLess(graph.Nodes[graph.Links[a].Target].Y0, graph.Nodes[graph.Links[b].Target].Y0, a, b)
	})
}

// sankeySortTargetLinks sorts a node's incoming links by the source node's
// y0, then link index (U:plot/shape/sankey.rs:739-750).
func sankeySortTargetLinks(graph *SankeyGraph, index int) {
	links := graph.Nodes[index].TargetLinks
	sort.Slice(links, func(i, j int) bool {
		a, b := links[i], links[j]
		return sankeyLess(graph.Nodes[graph.Links[a].Source].Y0, graph.Nodes[graph.Links[b].Source].Y0, a, b)
	})
}

// sankeyReorderNodeLinks re-sorts the link lists of a moved node's
// neighbors on the opposite ends (d3's reorderNodeLinks,
// U:plot/shape/sankey.rs:752-768).
func sankeyReorderNodeLinks(graph *SankeyGraph, index int) {
	for i := 0; i < len(graph.Nodes[index].TargetLinks); i++ {
		link := graph.Nodes[index].TargetLinks[i]
		sankeySortSourceLinks(graph, graph.Links[link].Source)
	}
	for i := 0; i < len(graph.Nodes[index].SourceLinks); i++ {
		link := graph.Nodes[index].SourceLinks[i]
		sankeySortTargetLinks(graph, graph.Links[link].Target)
	}
}

// sankeySortColumn stably sorts a column by node y0
// (U:plot/shape/sankey.rs:770-777).
func sankeySortColumn(graph *SankeyGraph, column []int) {
	sort.SliceStable(column, func(i, j int) bool {
		return graph.Nodes[column[i]].Y0 < graph.Nodes[column[j]].Y0
	})
}

// sankeyPushDown pushes overlapping nodes down (d3's
// resolveCollisionsTopToBottom, U:plot/shape/sankey.rs:779-790).
func sankeyPushDown(graph *SankeyGraph, column []int, y, alpha, py float32) {
	for _, index := range column {
		node := &graph.Nodes[index]
		dy := (y - node.Y0) * alpha
		if dy > 1e-6 {
			node.Y0 += dy
			node.Y1 += dy
		}
		y = node.Y1 + py
	}
}

// sankeyPushUp pushes overlapping nodes up (d3's
// resolveCollisionsBottomToTop, U:plot/shape/sankey.rs:792-803).
func sankeyPushUp(graph *SankeyGraph, column []int, y, alpha, py float32) {
	for i := len(column) - 1; i >= 0; i-- {
		node := &graph.Nodes[column[i]]
		dy := (node.Y1 - y) * alpha
		if dy > 1e-6 {
			node.Y0 -= dy
			node.Y1 -= dy
		}
		y = node.Y0 - py
	}
}

// sankeyTargetTop is d3's targetTop (U:plot/shape/sankey.rs:805-826).
func sankeyTargetTop(graph *SankeyGraph, source, target int, py float32) float32 {
	sourceNode := &graph.Nodes[source]
	y := sourceNode.Y0 - float32(max(len(sourceNode.SourceLinks)-1, 0))*py/2
	for _, linkIndex := range sourceNode.SourceLinks {
		link := &graph.Links[linkIndex]
		if link.Target == target {
			break
		}
		y += link.Width + py
	}
	for _, linkIndex := range graph.Nodes[target].TargetLinks {
		link := &graph.Links[linkIndex]
		if link.Source == source {
			break
		}
		y -= link.Width
	}
	return y
}

// sankeySourceTop is d3's sourceTop (U:plot/shape/sankey.rs:828-849).
func sankeySourceTop(graph *SankeyGraph, source, target int, py float32) float32 {
	targetNode := &graph.Nodes[target]
	y := targetNode.Y0 - float32(max(len(targetNode.TargetLinks)-1, 0))*py/2
	for _, linkIndex := range targetNode.TargetLinks {
		link := &graph.Links[linkIndex]
		if link.Source == source {
			break
		}
		y += link.Width + py
	}
	for _, linkIndex := range graph.Nodes[source].SourceLinks {
		link := &graph.Links[linkIndex]
		if link.Target == target {
			break
		}
		y -= link.Width
	}
	return y
}

// sankeyComputeLinkBreadths assigns each link's y0/y1 (ribbon centers) and
// per-end widths by stacking the sorted link lists within each node
// (U:plot/shape/sankey.rs:851-899).
func sankeyComputeLinkBreadths(graph *SankeyGraph) {
	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		nodeY0 := node.Y0
		nodeHeight := node.Y1 - node.Y0

		var outgoing float64
		for _, link := range node.SourceLinks {
			outgoing += graph.Links[link].Value
		}
		y0 := nodeY0
		for _, li := range node.SourceLinks {
			link := &graph.Links[li]
			var width float32
			if outgoing > 0 {
				width = float32(link.Value/outgoing) * nodeHeight
			}
			link.SourceWidth = width
			link.Y0 = y0 + width/2
			y0 += width
		}

		var incoming float64
		for _, link := range node.TargetLinks {
			incoming += graph.Links[link].Value
		}
		y1 := nodeY0
		for _, li := range node.TargetLinks {
			link := &graph.Links[li]
			var width float32
			if incoming > 0 {
				width = float32(link.Value/incoming) * nodeHeight
			}
			link.TargetWidth = width
			link.Y1 = y1 + width/2
			y1 += width
		}
	}
}

// SankeyLinkPath builds the filled ribbon path for a link, the equivalent
// of d3-sankey's sankeyLinkHorizontal(): a horizontal cubic bezier with
// control points at the horizontal midpoint, thickened to the per-end
// link widths (clamped to minWidth so tiny flows stay visible)
// (U:plot/shape/sankey.rs:901-933). It returns false when the path does
// not build.
func SankeyLinkPath(source, target *SankeyNodeLayout, link *SankeyLinkLayout, minWidth float32, origin gpui.Point) (*gpui.Path, bool) {
	sourceHalf := rustMaxF32(link.SourceWidth, minWidth) / 2
	targetHalf := rustMaxF32(link.TargetWidth, minWidth) / 2
	sx := source.X1
	tx := target.X0
	mx := (sx + tx) / 2
	ox, oy := origin.X, origin.Y

	b := gpui.FillPathBuilder()
	b.MoveTo(sx+ox, link.Y0-sourceHalf+oy)
	b.CubicBezierTo(
		tx+ox, link.Y1-targetHalf+oy,
		mx+ox, link.Y0-sourceHalf+oy,
		mx+ox, link.Y1-targetHalf+oy,
	)
	b.LineTo(tx+ox, link.Y1+targetHalf+oy)
	b.CubicBezierTo(
		sx+ox, link.Y0+sourceHalf+oy,
		mx+ox, link.Y1+targetHalf+oy,
		mx+ox, link.Y0+sourceHalf+oy,
	)
	b.Close()
	path, err := b.Build()
	if err != nil {
		return nil, false
	}
	return path, true
}

// powiF32 is LLVM's powi for f32 (compiler-rt __powisf2): square and
// multiply in float32, the lowering of `f32::powi`.
func powiF32(a float32, b int) float32 {
	recip := b < 0
	if recip {
		b = -b
	}
	r := float32(1)
	for {
		if b&1 != 0 {
			r *= a
		}
		b /= 2
		if b == 0 {
			break
		}
		a *= a
	}
	if recip {
		return 1 / r
	}
	return r
}

// rustMinF32 is f32::min: when one operand is NaN the other is returned.
func rustMinF32(a, b float32) float32 {
	if a != a {
		return b
	}
	if b != b {
		return a
	}
	if b < a {
		return b
	}
	return a
}

// rustMaxF32 is f32::max: when one operand is NaN the other is returned.
func rustMaxF32(a, b float32) float32 {
	if a != a {
		return b
	}
	if b != b {
		return a
	}
	if b > a {
		return b
	}
	return a
}

func plotIsFinite(v float32) bool {
	return !math.IsInf(float64(v), 0) && !math.IsNaN(float64(v))
}
