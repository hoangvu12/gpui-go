package base

// Port of U:dock/state.rs: the persisted dock schema.
//
// serde maps to encoding/json with custom marshalers that keep the frozen
// wire shape: the externally tagged PanelInfo ({"stack":{...}},
// {"tabs":{...}}, {"panel":<value>}), the renamed DockPlacement strings,
// `version` written as null when absent, and the optional docks omitted
// rather than nulled. Like serde, a missing required field and an unknown
// enum tag are errors and unknown object fields are ignored. Pixels are
// float32 numbers; persisted values compare by value (PanelInfo.Equal,
// PanelState.Equal), not by bytes.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"github.com/hoangvu12/gpui-go/gpui"
)

// DockAreaState serializes a whole DockArea (U:dock/state.rs:4-23). It
// mirrors a persisted, on-disk schema shipped to end users.
type DockAreaState struct {
	// Version marks the persisted state's compatibility; nil is absent.
	Version    *uint64
	Center     PanelState
	LeftDock   *DockState
	RightDock  *DockState
	BottomDock *DockState
}

// MarshalJSON writes the derived Serialize shape: version (null when
// absent), center, then each present dock.
func (s DockAreaState) MarshalJSON() ([]byte, error) {
	type wire struct {
		Version    *uint64    `json:"version"`
		Center     PanelState `json:"center"`
		LeftDock   *DockState `json:"left_dock,omitempty"`
		RightDock  *DockState `json:"right_dock,omitempty"`
		BottomDock *DockState `json:"bottom_dock,omitempty"`
	}
	return json.Marshal(wire(s))
}

// UnmarshalJSON reads the derived Deserialize shape: version defaults to
// absent, center is required.
func (s *DockAreaState) UnmarshalJSON(data []byte) error {
	fields, err := jsonObjectFields(data, "DockAreaState", "center")
	if err != nil {
		return err
	}
	var out DockAreaState
	if raw, ok := fields["version"]; ok && !isJSONNull(raw) {
		var v uint64
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		out.Version = &v
	}
	if err := json.Unmarshal(fields["center"], &out.Center); err != nil {
		return err
	}
	for key, dst := range map[string]**DockState{"left_dock": &out.LeftDock, "right_dock": &out.RightDock, "bottom_dock": &out.BottomDock} {
		if raw, ok := fields[key]; ok && !isJSONNull(raw) {
			d := new(DockState)
			if err := json.Unmarshal(raw, d); err != nil {
				return err
			}
			*dst = d
		}
	}
	*s = out
	return nil
}

// Equal is the derived PartialEq.
func (s *DockAreaState) Equal(other *DockAreaState) bool {
	if (s.Version == nil) != (other.Version == nil) || (s.Version != nil && *s.Version != *other.Version) {
		return false
	}
	return s.Center.Equal(&other.Center) && dockStateEqual(s.LeftDock, other.LeftDock) &&
		dockStateEqual(s.RightDock, other.RightDock) && dockStateEqual(s.BottomDock, other.BottomDock)
}

func dockStateEqual(a, b *DockState) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(b)
}

// DockState serializes one dock (U:dock/state.rs:25-59).
type DockState struct {
	panel     PanelState
	placement DockPlacement
	size      float32
	open      bool
}

// NewDockState is DockState::new (U:dock/state.rs:35-42).
func NewDockState(panel PanelState, placement DockPlacement, size float32, open bool) DockState {
	return DockState{panel: panel, placement: placement, size: size, open: open}
}

// Panel returns the dock's layout (U:dock/state.rs:44-46).
func (d *DockState) Panel() *PanelState { return &d.panel }

// Placement returns the dock's placement (U:dock/state.rs:48-50).
func (d *DockState) Placement() DockPlacement { return d.placement }

// Size returns the dock's size (U:dock/state.rs:52-54).
func (d *DockState) Size() float32 { return d.size }

// Open reports whether the dock is open (U:dock/state.rs:56-58).
func (d *DockState) Open() bool { return d.open }

// Equal is the derived PartialEq.
func (d *DockState) Equal(other *DockState) bool {
	return d.panel.Equal(&other.panel) && d.placement == other.placement && d.size == other.size && d.open == other.open
}

type dockStateWire struct {
	Panel     PanelState    `json:"panel"`
	Placement DockPlacement `json:"placement"`
	Size      float32       `json:"size"`
	Open      bool          `json:"open"`
}

// MarshalJSON writes {panel, placement, size, open}.
func (d DockState) MarshalJSON() ([]byte, error) {
	return json.Marshal(dockStateWire{d.panel, d.placement, d.size, d.open})
}

// UnmarshalJSON requires every field, like the derived Deserialize.
func (d *DockState) UnmarshalJSON(data []byte) error {
	if _, err := jsonObjectFields(data, "DockState", "panel", "placement", "size", "open"); err != nil {
		return err
	}
	var w dockStateWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*d = DockState{w.Panel, w.Placement, w.Size, w.Open}
	return nil
}

// PanelState serializes one container or leaf (U:dock/state.rs:61-71).
// The zero value is PanelState::default: an empty name, no children and
// PanelInfo::Panel(Null) (U:dock/state.rs:128-136).
type PanelState struct {
	PanelName string
	Children  []PanelState
	Info      PanelInfo
}

// NewPanelState is PanelState::new (U:dock/state.rs:138-149): a leaf for a
// panel with the given name.
func NewPanelState(panelName string) PanelState { return PanelState{PanelName: panelName} }

// AddChild appends a child (U:dock/state.rs:151-153).
func (s *PanelState) AddChild(panel PanelState) { s.Children = append(s.Children, panel) }

// Clone deep-copies the state.
func (s *PanelState) Clone() PanelState {
	out := PanelState{PanelName: s.PanelName, Info: s.Info.clone()}
	if s.Children != nil {
		out.Children = make([]PanelState, len(s.Children))
		for i := range s.Children {
			out.Children[i] = s.Children[i].Clone()
		}
	}
	return out
}

// Equal is the derived PartialEq.
func (s *PanelState) Equal(other *PanelState) bool {
	if s.PanelName != other.PanelName || len(s.Children) != len(other.Children) || !s.Info.Equal(other.Info) {
		return false
	}
	for i := range s.Children {
		if !s.Children[i].Equal(&other.Children[i]) {
			return false
		}
	}
	return true
}

type panelStateWire struct {
	PanelName string       `json:"panel_name"`
	Children  []PanelState `json:"children"`
	Info      PanelInfo    `json:"info"`
}

// MarshalJSON writes {panel_name, children, info}; no children is [].
func (s PanelState) MarshalJSON() ([]byte, error) {
	children := s.Children
	if children == nil {
		children = []PanelState{}
	}
	return json.Marshal(panelStateWire{s.PanelName, children, s.Info})
}

// UnmarshalJSON requires every field, like the derived Deserialize.
func (s *PanelState) UnmarshalJSON(data []byte) error {
	if _, err := jsonObjectFields(data, "PanelState", "panel_name", "children", "info"); err != nil {
		return err
	}
	var w panelStateWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*s = PanelState{w.PanelName, w.Children, w.Info}
	return nil
}

// panelInfoKind is the PanelInfo variant. The zero value is Panel, so the
// zero PanelInfo is PanelInfo::Panel(Value::Null).
type panelInfoKind uint8

const (
	panelInfoPanel panelInfoKind = iota
	panelInfoStack
	panelInfoTabs
)

// PanelInfo is the per-node payload (U:dock/state.rs:73-84): a stack
// (split) with its slot sizes and axis index (0 horizontal, 1 vertical),
// a tab group with its active index, or a leaf panel's own opaque JSON.
type PanelInfo struct {
	kind        panelInfoKind
	sizes       []float32
	axis        uint64
	activeIndex uint64
	value       json.RawMessage
}

// PanelInfoStack is PanelInfo::stack (U:dock/state.rs:87-92).
func PanelInfoStack(sizes []float32, axis gpui.Axis) PanelInfo {
	ix := uint64(1)
	if axis == gpui.AxisHorizontal {
		ix = 0
	}
	return PanelInfo{kind: panelInfoStack, sizes: sizes, axis: ix}
}

// PanelInfoTabs is PanelInfo::tabs (U:dock/state.rs:94-96).
func PanelInfoTabs(activeIndex int) PanelInfo {
	return PanelInfo{kind: panelInfoTabs, activeIndex: uint64(activeIndex)}
}

// PanelInfoPanel is PanelInfo::panel (U:dock/state.rs:98-100): a leaf's
// opaque JSON value. A nil value is JSON null.
func PanelInfoPanel(info json.RawMessage) PanelInfo {
	return PanelInfo{kind: panelInfoPanel, value: info}
}

// IsStack reports the Stack variant.
func (i PanelInfo) IsStack() bool { return i.kind == panelInfoStack }

// IsTabs reports the Tabs variant.
func (i PanelInfo) IsTabs() bool { return i.kind == panelInfoTabs }

// IsPanel reports the Panel variant.
func (i PanelInfo) IsPanel() bool { return i.kind == panelInfoPanel }

// Axis returns a stack's axis (U:dock/state.rs:102-111): 0 is
// horizontal, anything else vertical.
func (i PanelInfo) Axis() (gpui.Axis, bool) {
	if i.kind != panelInfoStack {
		return 0, false
	}
	if i.axis == 0 {
		return gpui.AxisHorizontal, true
	}
	return gpui.AxisVertical, true
}

// Sizes returns a stack's slot sizes (U:dock/state.rs:113-118).
func (i PanelInfo) Sizes() ([]float32, bool) {
	if i.kind != panelInfoStack {
		return nil, false
	}
	return i.sizes, true
}

// ActiveIndex returns a tab group's active index (U:dock/state.rs:120-125).
func (i PanelInfo) ActiveIndex() (int, bool) {
	if i.kind != panelInfoTabs {
		return 0, false
	}
	return int(i.activeIndex), true
}

// Value returns a leaf's opaque JSON value; JSON null when unset.
func (i PanelInfo) Value() (json.RawMessage, bool) {
	if i.kind != panelInfoPanel {
		return nil, false
	}
	if len(i.value) == 0 {
		return json.RawMessage("null"), true
	}
	return i.value, true
}

func (i PanelInfo) clone() PanelInfo {
	i.sizes = slices.Clone(i.sizes)
	i.value = slices.Clone(i.value)
	return i
}

// Equal is the derived PartialEq; leaf values compare as JSON values.
func (i PanelInfo) Equal(other PanelInfo) bool {
	if i.kind != other.kind {
		return false
	}
	switch i.kind {
	case panelInfoStack:
		return i.axis == other.axis && slices.Equal(i.sizes, other.sizes)
	case panelInfoTabs:
		return i.activeIndex == other.activeIndex
	}
	a, _ := i.Value()
	b, _ := other.Value()
	return jsonValuesEqual(a, b)
}

// String renders the variant for diagnostics.
func (i PanelInfo) String() string {
	data, err := json.Marshal(i)
	if err != nil {
		return fmt.Sprintf("PanelInfo(%v)", err)
	}
	return string(data)
}

type panelInfoStackWire struct {
	Sizes []float32 `json:"sizes"`
	Axis  uint64    `json:"axis"`
}

type panelInfoTabsWire struct {
	ActiveIndex uint64 `json:"active_index"`
}

// MarshalJSON writes the externally tagged shape with the frozen tags.
func (i PanelInfo) MarshalJSON() ([]byte, error) {
	switch i.kind {
	case panelInfoStack:
		sizes := i.sizes
		if sizes == nil {
			sizes = []float32{}
		}
		return json.Marshal(map[string]panelInfoStackWire{"stack": {sizes, i.axis}})
	case panelInfoTabs:
		return json.Marshal(map[string]panelInfoTabsWire{"tabs": {i.activeIndex}})
	}
	value, _ := i.Value()
	return json.Marshal(map[string]json.RawMessage{"panel": value})
}

// UnmarshalJSON reads the externally tagged shape: exactly one known tag.
func (i *PanelInfo) UnmarshalJSON(data []byte) error {
	var tagged map[string]json.RawMessage
	if err := json.Unmarshal(data, &tagged); err != nil {
		return err
	}
	if tagged == nil || len(tagged) != 1 {
		return fmt.Errorf("PanelInfo: expected an object with exactly one variant tag")
	}
	for tag, body := range tagged {
		switch tag {
		case "stack":
			if _, err := jsonObjectFields(body, "PanelInfo::Stack", "sizes", "axis"); err != nil {
				return err
			}
			var w panelInfoStackWire
			if err := json.Unmarshal(body, &w); err != nil {
				return err
			}
			*i = PanelInfo{kind: panelInfoStack, sizes: w.Sizes, axis: w.Axis}
		case "tabs":
			if _, err := jsonObjectFields(body, "PanelInfo::Tabs", "active_index"); err != nil {
				return err
			}
			var w panelInfoTabsWire
			if err := json.Unmarshal(body, &w); err != nil {
				return err
			}
			*i = PanelInfo{kind: panelInfoTabs, activeIndex: w.ActiveIndex}
		case "panel":
			*i = PanelInfo{kind: panelInfoPanel, value: slices.Clone(body)}
		default:
			return fmt.Errorf("PanelInfo: unknown variant %q, expected one of stack, tabs, panel", tag)
		}
	}
	return nil
}

// DockPlacement is the placement of a dock relative to the center area
// (U:dock/state.rs:156-170). Its serialized names are frozen.
type DockPlacement uint8

const (
	// DockPlacementCenter is "center".
	DockPlacementCenter DockPlacement = iota
	// DockPlacementLeft is "left".
	DockPlacementLeft
	// DockPlacementBottom is "bottom".
	DockPlacementBottom
	// DockPlacementRight is "right".
	DockPlacementRight
)

// Axis is DockPlacement::axis (U:dock/state.rs:173-178).
func (p DockPlacement) Axis() gpui.Axis {
	if p == DockPlacementBottom {
		return gpui.AxisVertical
	}
	return gpui.AxisHorizontal
}

// IsLeft is U:dock/state.rs:180-182.
func (p DockPlacement) IsLeft() bool { return p == DockPlacementLeft }

// IsBottom is U:dock/state.rs:184-186.
func (p DockPlacement) IsBottom() bool { return p == DockPlacementBottom }

// IsRight is U:dock/state.rs:188-190.
func (p DockPlacement) IsRight() bool { return p == DockPlacementRight }

// String renders the variant name (the derived Debug).
func (p DockPlacement) String() string {
	switch p {
	case DockPlacementCenter:
		return "Center"
	case DockPlacementLeft:
		return "Left"
	case DockPlacementBottom:
		return "Bottom"
	case DockPlacementRight:
		return "Right"
	}
	return fmt.Sprintf("DockPlacement(%d)", uint8(p))
}

// MarshalText writes the frozen serde rename.
func (p DockPlacement) MarshalText() ([]byte, error) {
	switch p {
	case DockPlacementCenter:
		return []byte("center"), nil
	case DockPlacementLeft:
		return []byte("left"), nil
	case DockPlacementBottom:
		return []byte("bottom"), nil
	case DockPlacementRight:
		return []byte("right"), nil
	}
	return nil, fmt.Errorf("base: invalid DockPlacement %d", uint8(p))
}

// UnmarshalText reads the frozen serde rename.
func (p *DockPlacement) UnmarshalText(text []byte) error {
	switch string(text) {
	case "center":
		*p = DockPlacementCenter
	case "left":
		*p = DockPlacementLeft
	case "bottom":
		*p = DockPlacementBottom
	case "right":
		*p = DockPlacementRight
	default:
		return fmt.Errorf("DockPlacement: unknown variant %q, expected one of center, left, bottom, right", text)
	}
	return nil
}

// jsonObjectFields decodes a JSON object's fields and requires the named
// ones (serde's "missing field" error).
func jsonObjectFields(data []byte, typeName string, required ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, fmt.Errorf("%s: expected an object", typeName)
	}
	for _, name := range required {
		if _, ok := fields[name]; !ok {
			return nil, fmt.Errorf("%s: missing field `%s`", typeName, name)
		}
	}
	return fields, nil
}

func isJSONNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

// jsonValuesEqual compares two JSON documents as values (serde_json::Value
// equality: numbers compare by their textual form).
func jsonValuesEqual(a, b json.RawMessage) bool {
	decode := func(raw json.RawMessage) (any, bool) {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, false
		}
		return v, true
	}
	va, okA := decode(a)
	vb, okB := decode(b)
	return okA && okB && reflect.DeepEqual(va, vb)
}
