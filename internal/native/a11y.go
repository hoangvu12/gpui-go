// A11y service access (ticket22, reserved slot 9).
//
// The pinned AccessKit adapter stack (accesskit 0.24.1 +
// accesskit_windows 0.33.1, the CE lock resolution) behind the pin's
// WindowsWindow a11y surface: Adapter::new at window setup (the
// initial minimal tree = a Role::Window root with the window title),
// tree publication with update-if-active semantics (the serialized
// update is decoded only when an assistive technology has connected —
// the pin's factory-not-called behavior), WM_GETOBJECT handling with
// the nested-message lock discipline (activation returns the stored
// initial tree; the LRESULT conversion happens outside the service
// lock), and the queued action handoff (the pin's async_channel
// dispatch, drained by polling — never a synchronous callback into Go
// state). The table mirror carries self-check fields validated at
// fetch (validateA11yTable). No Go pointer is retained across calls:
// the node/children/string buffers are borrowed for the publish.

package native

import (
	"errors"
	"fmt"
	"unsafe"
)

// a11yServiceVersion mirrors GPUI_GO_A11Y_SERVICE_VERSION.
const a11yServiceVersion uint32 = 1

// A11y role tags (mirror a11y_role in a11y.rs — the port's published
// role slice mapped onto the pinned accesskit::Role variants).
const (
	// A11yRoleWindow is the window root role.
	A11yRoleWindow uint32 = 1
	// A11yRoleGenericContainer is a plain grouping node.
	A11yRoleGenericContainer uint32 = 2
	// A11yRoleButton is an interactive button.
	A11yRoleButton uint32 = 3
	// A11yRoleTextInput is an editable text field.
	A11yRoleTextInput uint32 = 4
	// A11yRoleTextRun is one run of text inside a text input.
	A11yRoleTextRun uint32 = 5
	// A11yRoleStaticText is static text (the pin's Role::Label).
	A11yRoleStaticText uint32 = 6
	// A11yRoleCheckBox is a toggleable check box.
	A11yRoleCheckBox uint32 = 7
	// A11yRoleSlider is an adjustable slider.
	A11yRoleSlider uint32 = 8
)

// A11y action tags (mirror a11y_action in a11y.rs).
const (
	// A11yActionClick activates the node.
	A11yActionClick uint32 = 1
	// A11yActionFocus moves keyboard focus.
	A11yActionFocus uint32 = 2
	// A11yActionBlur clears window focus.
	A11yActionBlur uint32 = 3
	// A11yActionIncrement increases the value.
	A11yActionIncrement uint32 = 4
	// A11yActionDecrement decreases the value.
	A11yActionDecrement uint32 = 5
	// A11yActionSetValue assigns the value.
	A11yActionSetValue uint32 = 6
	// A11yActionReplaceSelectedText replaces the selection.
	A11yActionReplaceSelectedText uint32 = 7
	// A11yActionSetTextSelection changes the text selection.
	A11yActionSetTextSelection uint32 = 8
	// A11yActionShowContextMenu opens the context menu.
	A11yActionShowContextMenu uint32 = 9
)

// A11y action payload tags (mirror a11y_action_data in a11y.rs).
const (
	// A11yActionDataNone carries no payload.
	A11yActionDataNone uint32 = 0
	// A11yActionDataValue carries a string value.
	A11yActionDataValue uint32 = 1
	// A11yActionDataTextSelection carries a text selection.
	A11yActionDataTextSelection uint32 = 2
)

// A11y node record flags.
const (
	// A11yNodeFlagHidden hides the node and its subtree.
	A11yNodeFlagHidden uint32 = 0x1
	// A11yNodeFlagHasBounds carries physical-pixel bounds.
	A11yNodeFlagHasBounds uint32 = 0x2
)

// A11y node flags (mirror a11y_status in a11y.rs).
const (
	a11yStatusOK          int32 = 0
	a11yStatusStaleHandle int32 = -1
	a11yStatusBadHandle   int32 = -2
	a11yStatusNullArg     int32 = -3
	a11yStatusBadValue    int32 = -4
	a11yStatusBadString   int32 = -5
	a11yStatusRecordSize  int32 = -6
	a11yStatusCapacity    int32 = -7
	a11yStatusPanic       int32 = 100
)

// A11yNodeRecord mirrors GpuiGoA11yNodeRecord (104 bytes, alignment 8):
// one serialized node of a published tree. Offsets are byte indices
// into the publish's children (u64) and string (u8) buffers.
type A11yNodeRecord struct {
	// ID is the node id (0 is every tree's root).
	ID             uint64
	Role           uint32
	Flags          uint32
	ActionsMask    uint32
	ChildrenCount  uint32
	ChildrenOffset uint32
	LabelOffset    uint32
	LabelLen       uint32
	DescOffset     uint32
	DescLen        uint32
	ValueOffset    uint32
	ValueLen       uint32
	AuthorOff      uint32
	AuthorLen      uint32
	BoundsX0Bits   uint64
	BoundsY0Bits   uint64
	BoundsX1Bits   uint64
	BoundsY1Bits   uint64
	RecordSize     uint32
}

// A11yPublishRecord mirrors GpuiGoA11yPublishRecord (20 bytes,
// alignment 4).
type A11yPublishRecord struct {
	// Status of the call.
	Status int32
	// Active reports whether an assistive technology has connected
	// (when false the serialized update was not decoded — the pin's
	// factory-not-called semantics).
	Active uint32
	// Applied reports whether the update reached the live tree.
	Applied uint32
	// EventsRaised reports whether change events went to the client.
	EventsRaised uint32
	// RecordSize self-check.
	RecordSize uint32
}

// A11yActionRecord mirrors GpuiGoA11yActionRecord (56 bytes, alignment
// 8): one drained action request.
type A11yActionRecord struct {
	// TargetNode is the node the action targets.
	TargetNode  uint64
	Action      uint32
	DataTag     uint32
	ValueOffset uint32
	ValueLen    uint32
	AnchorNode  uint64
	AnchorChar  uint32
	FocusNode   uint64
	FocusChar   uint32
	// RecordSize self-check.
	RecordSize uint32
}

// a11yNativeTable mirrors GpuiGoA11yTable: 9 function pointers then 9
// self-check/capacity scalars; size 112, alignment 8.
type a11yNativeTable struct {
	adapterCreate     uintptr
	adapterDestroy    uintptr
	publish           uintptr
	handleWmGetObject uintptr
	pollActions       uintptr
	isActive          uintptr
	pendingActions    uintptr
	panicProbe        uintptr

	serviceVersion      uint32
	sizeOfTable         uint32
	alignOfTable        uint32
	sizeOfNodeRecord    uint32
	sizeOfPublishRecord uint32
	sizeOfActionRecord  uint32
	maxAdapters         uint32
	maxNodes            uint32
	maxStrings          uint32
}

// a11yTableFromSlot reads the service table out of DLL memory (the
// bootstrap table's reserved slot 9).
func a11yTableFromSlot(slot uintptr) *a11yNativeTable {
	return (*a11yNativeTable)(*(*unsafe.Pointer)(unsafe.Pointer(&slot)))
}

// validateA11yTable asserts the native self-check fields against this
// mirror's compile-time layout.
func validateA11yTable(table *a11yNativeTable, path string) error {
	if table.serviceVersion != a11yServiceVersion {
		return fmt.Errorf("native: a11y service version = %d, want %d (%s)", table.serviceVersion, a11yServiceVersion, path)
	}
	if table.sizeOfTable != uint32(unsafe.Sizeof(a11yNativeTable{})) {
		return fmt.Errorf("native: a11y table size = %d, want %d (%s)", table.sizeOfTable, unsafe.Sizeof(a11yNativeTable{}), path)
	}
	if table.alignOfTable != uint32(unsafe.Alignof(a11yNativeTable{})) {
		return fmt.Errorf("native: a11y table alignment = %d, want %d (%s)", table.alignOfTable, unsafe.Alignof(a11yNativeTable{}), path)
	}
	if table.sizeOfNodeRecord != uint32(unsafe.Sizeof(A11yNodeRecord{})) {
		return fmt.Errorf("native: a11y node record size = %d, want %d", table.sizeOfNodeRecord, unsafe.Sizeof(A11yNodeRecord{}))
	}
	if table.sizeOfPublishRecord != uint32(unsafe.Sizeof(A11yPublishRecord{})) {
		return fmt.Errorf("native: a11y publish record size = %d, want %d", table.sizeOfPublishRecord, unsafe.Sizeof(A11yPublishRecord{}))
	}
	if table.sizeOfActionRecord != uint32(unsafe.Sizeof(A11yActionRecord{})) {
		return fmt.Errorf("native: a11y action record size = %d, want %d", table.sizeOfActionRecord, unsafe.Sizeof(A11yActionRecord{}))
	}
	return nil
}

// A11yService is the accessibility provider service client (reserved
// slot 9). The adapter create/publish/getobject entries must be called
// on the window's message thread (the pin's a11y_init discipline — the
// foreground OLE STA host).
type A11yService struct {
	lib   *Library
	table a11yNativeTable
}

// A11y fetches the a11y provider service (capability bit 10, reserved
// slot 9).
func (l *Library) A11y() (*A11yService, error) {
	if l.released.Load() {
		return nil, ErrClosed
	}
	if l.identity.Capabilities&capA11yAccesskit == 0 {
		return nil, &CapabilityError{
			Path:     l.identity.Path,
			Required: capA11yAccesskit,
			Actual:   l.identity.Capabilities,
			Detail:   "a11y-accesskit capability bit missing",
		}
	}
	slot := l.table.reserved[9]
	if slot == 0 {
		return nil, &CapabilityError{Path: l.identity.Path, Detail: "a11y service table (reserved slot 9) is null"}
	}
	table := *a11yTableFromSlot(slot) // copy out of DLL memory
	if err := validateA11yTable(&table, l.identity.Path); err != nil {
		return nil, err
	}
	return &A11yService{lib: l, table: table}, nil
}

// A11yAdapterHandle is a validated opaque native adapter handle. It
// becomes stale after Destroy (or native slot reuse).
type A11yAdapterHandle uint64

// The a11y service errors.
var (
	// ErrA11yStaleHandle reports a handle that encoded a past
	// generation (destroyed or slot reused since).
	ErrA11yStaleHandle = errors.New("native: a11y adapter handle is stale (destroyed or slot reused)")
	// ErrA11yBadHandle reports a malformed or never-issued handle.
	ErrA11yBadHandle = errors.New("native: a11y handle is malformed")
	// ErrA11yBadValue reports an invalid count, offset, tag or
	// record-size value.
	ErrA11yBadValue = errors.New("native: a11y argument rejected")
	// ErrA11yBadString reports non-UTF-8 serialized string bytes.
	ErrA11yBadString = errors.New("native: a11y string pool is not valid UTF-8")
	// ErrA11yCapacity reports a too-small poll buffer (retry with the
	// reported need; the actions were not drained).
	ErrA11yCapacity = errors.New("native: a11y poll buffer too small")
	// ErrA11yPanic reports a contained native panic.
	ErrA11yPanic = errors.New("native: a11y service panicked")
)

// a11yError maps a native status code to an error.
func a11yError(code int32) error {
	switch code {
	case a11yStatusStaleHandle:
		return ErrA11yStaleHandle
	case a11yStatusBadHandle:
		return ErrA11yBadHandle
	case a11yStatusBadValue:
		return ErrA11yBadValue
	case a11yStatusBadString:
		return ErrA11yBadString
	case a11yStatusCapacity:
		return ErrA11yCapacity
	case a11yStatusPanic:
		return ErrA11yPanic
	default:
		return fmt.Errorf("native: a11y service status %d", code)
	}
}

// A11yTreeUpdate is one serialized tree publication: the node records
// plus the two side buffers (child ids and the UTF-8 string pool).
type A11yTreeUpdate struct {
	// Nodes are the serialized nodes.
	Nodes []A11yNodeRecord
	// Children is the concatenated child-id array (the records index
	// into it).
	Children []uint64
	// Strings is the UTF-8 string pool (the records slice into it).
	Strings []byte
	// RootID is the tree root's node id (the port's window root).
	RootID uint64
	// FocusID is the focused node id.
	FocusID uint64
}

// A11yWmGetObjectResult carries the WM_GETOBJECT outcome.
type A11yWmGetObjectResult struct {
	// Handled is 1 when the adapter answered (the caller must return
	// LResult; 0 means DefWindowProc).
	Handled uint32
	// LResult is the answer when Handled is 1.
	LResult int64
}

// A11yActionRequest is one drained action with its decoded payload.
type A11yActionRequest struct {
	// TargetNode is the action's target.
	TargetNode uint64
	// Action is the action tag.
	Action uint32
	// Value is the string payload (SetValue/ReplaceSelectedText).
	Value string
	// SelectionAnchorNode/Char and SelectionFocusNode/Char carry the
	// SetTextSelection payload.
	SelectionAnchorNode uint64
	SelectionAnchorChar uint32
	SelectionFocusNode  uint64
	SelectionFocusChar  uint32
}

// CreateAdapter creates the UIA provider adapter for the window (the
// pin's a11y_init: Adapter::new + the pending initial tree). Must run
// on the window's message thread, never inside a WM_GETOBJECT handler.
func (s *A11yService) CreateAdapter(hwnd uintptr, isFocused bool, initialLabel string) (A11yAdapterHandle, error) {
	if s == nil {
		return 0, fmt.Errorf("native: no a11y service")
	}
	handle, code, err := callA11yAdapterCreate(s.table.adapterCreate, hwnd, isFocused, initialLabel)
	if err != nil {
		return 0, err
	}
	if code != a11yStatusOK {
		return 0, a11yError(code)
	}
	return handle, nil
}

// DestroyAdapter drops the adapter (the pin's window closure dropping
// A11yState; the UIA provider references release apartment-affine).
func (s *A11yService) DestroyAdapter(handle A11yAdapterHandle) error {
	if s == nil {
		return fmt.Errorf("native: no a11y service")
	}
	code, err := callA11yAdapterDestroy(s.table.adapterDestroy, handle)
	if err != nil {
		return err
	}
	if code != a11yStatusOK {
		return a11yError(code)
	}
	return nil
}

// Publish applies one serialized tree update with the pin's
// update_if_active semantics: when no assistive technology has
// connected the update is NOT decoded (Active=false, nothing applied);
// when active the update reaches the live tree and the change events
// are raised to the client (outside the service lock, exactly like the
// pin's post-borrow events.raise()).
func (s *A11yService) Publish(handle A11yAdapterHandle, update *A11yTreeUpdate) (A11yPublishRecord, error) {
	if s == nil {
		return A11yPublishRecord{}, fmt.Errorf("native: no a11y service")
	}
	if update == nil {
		return A11yPublishRecord{}, fmt.Errorf("native: nil a11y tree update")
	}
	out, code, err := callA11yPublish(
		s.table.publish,
		handle,
		update.Nodes,
		update.Children,
		update.Strings,
		update.RootID,
		update.FocusID,
	)
	if err != nil {
		return A11yPublishRecord{}, err
	}
	if code != a11yStatusOK {
		return out, a11yError(code)
	}
	if out.RecordSize != uint32(unsafe.Sizeof(A11yPublishRecord{})) {
		return out, fmt.Errorf("native: a11y publish record size %d does not match the mirror", out.RecordSize)
	}
	return out, nil
}

// HandleWmGetObject answers a WM_GETOBJECT for the adapter's window
// (the pin's handle_wm_getobject: the LRESULT conversion runs outside
// the service lock because UiaReturnRawElementProvider may deliver a
// nested WM_GETOBJECT back into the window procedure).
func (s *A11yService) HandleWmGetObject(handle A11yAdapterHandle, wparam uintptr, lparam uintptr) (A11yWmGetObjectResult, error) {
	if s == nil {
		return A11yWmGetObjectResult{}, fmt.Errorf("native: no a11y service")
	}
	result, code, err := callA11yHandleWmGetObject(s.table.handleWmGetObject, handle, wparam, lparam)
	if err != nil {
		return A11yWmGetObjectResult{}, err
	}
	if code != a11yStatusOK {
		return result, a11yError(code)
	}
	return result, nil
}

// PollActions drains the queued action requests (the pin's
// async_channel handoff — do_action only queues; the foreground side
// dispatches). The capacity protocol: a too-small record or string
// buffer returns ErrA11yCapacity with the needs (retry; nothing was
// drained).
func (s *A11yService) PollActions(handle A11yAdapterHandle, records []A11yActionRecord, stringsBuf []byte) ([]A11yActionRequest, uint32, uint32, error) {
	if s == nil {
		return nil, 0, 0, fmt.Errorf("native: no a11y service")
	}
	count, stringsNeeded, code, err := callA11yPollActions(
		s.table.pollActions,
		handle,
		records,
		stringsBuf,
	)
	if err != nil {
		return nil, 0, 0, err
	}
	if code != a11yStatusOK {
		return nil, count, stringsNeeded, a11yError(code)
	}
	requests := make([]A11yActionRequest, 0, count)
	for i := uint32(0); i < count && int(i) < len(records); i++ {
		record := records[i]
		if record.RecordSize != uint32(unsafe.Sizeof(A11yActionRecord{})) {
			return nil, count, stringsNeeded, fmt.Errorf("native: a11y action record size %d does not match the mirror", record.RecordSize)
		}
		request := A11yActionRequest{
			TargetNode: record.TargetNode,
			Action:     record.Action,
		}
		if record.ValueLen > 0 {
			start := int(record.ValueOffset)
			end := start + int(record.ValueLen)
			if end > len(stringsBuf) {
				return nil, count, stringsNeeded, fmt.Errorf("native: a11y action value offset out of bounds")
			}
			request.Value = string(stringsBuf[start:end])
		}
		if record.DataTag == A11yActionDataTextSelection {
			request.SelectionAnchorNode = record.AnchorNode
			request.SelectionAnchorChar = record.AnchorChar
			request.SelectionFocusNode = record.FocusNode
			request.SelectionFocusChar = record.FocusChar
		}
		requests = append(requests, request)
	}
	return requests, count, stringsNeeded, nil
}

// IsActive reports whether an assistive technology has activated the
// adapter (the pin's active flag: the Go side forces a frame on the
// transition so the complete tree reaches the client — the
// activation-before-complete-tree behavior).
func (s *A11yService) IsActive(handle A11yAdapterHandle) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("native: no a11y service")
	}
	active, code, err := callA11yIsActive(s.table.isActive, handle)
	if err != nil {
		return false, err
	}
	if code != a11yStatusOK {
		return false, a11yError(code)
	}
	return active != 0, nil
}

// PendingActions reports the undrained action queue depth.
func (s *A11yService) PendingActions(handle A11yAdapterHandle) (uint32, error) {
	if s == nil {
		return 0, fmt.Errorf("native: no a11y service")
	}
	count, code, err := callA11yPendingActions(s.table.pendingActions, handle)
	if err != nil {
		return 0, err
	}
	if code != a11yStatusOK {
		return 0, a11yError(code)
	}
	return count, nil
}
