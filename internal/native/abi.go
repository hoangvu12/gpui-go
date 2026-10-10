package native

import (
	"strconv"
	"unsafe"
)

// Private ABI constants, mirroring `reference/native/src/lib.rs` (ticket02).
// The Rust side is the source of truth; these values and the struct mirrors
// below are validated against the DLL's own self-check fields at load time.
const (
	// abiMagic reads "GPGO" big-endian (0x47 0x50 0x47 0x4F).
	abiMagic uint32 = 0x4750474F
	// abiVersion is the private ABI schema major version of this slice.
	abiVersion uint32 = 1
	// nativeRevision mirrors GPUI_GO_NATIVE_REVISION. Ticket02 shipped
	// revision 1; ticket06 (layout service) bumped it to 2; ticket07
	// (renderer service) bumped it to 3; ticket08 (scene kernel service)
	// bumped it to 4; ticket09 (text geometry service) bumped it to 5;
	// ticket10 (glyph raster + atlas services, scene sprite primitives)
	// bumped it to 6; ticket16 (path primitives + the pinned PathBuilder
	// tessellation in the scene service, the scene drawing pipeline in
	// the renderer service) bumped it to 7; ticket17 (the image codec
	// service in reserved slot 7) bumped it to 8; ticket19 (the SVG
	// service in reserved slot 8, extending the reserved slot array from
	// 8 to 16 entries — an additive ABI record growth, 152→216 bytes,
	// mirrored here in lockstep) bumped it to 9.
	nativeRevision uint32 = 9
	// maxBufferLen mirrors GPUI_GO_MAX_BUFFER_LEN.
	maxBufferLen = 4096
	// maxTextLen mirrors the text service's MAX_TEXT_BYTES (1 MiB text
	// length bound of the shape request).
	maxTextLen = 1 << 20
	// ceCommitHex is the gpui-CE source pin of the artifact family.
	ceCommitHex = "254b5dbd47cbb5acbcc5bbdcbb322a339276c88a"
)

// Capability bits. Bit 0 (bootstrap buffer round trip) is required of every
// artifact; bit 1 (layout service) is required only by the layout service
// fetch (Library.Layout), not by Load. Unknown extra bits are
// forward-compatible, never rejected.
const (
	capBootstrapBufferRoundTrip uint64 = 1 << 0
	capLayoutTaffy              uint64 = 1 << 1 // "layout-taffy-0-13-0"
	capRendererD3D11            uint64 = 1 << 2 // "renderer-d3d11"
	capSceneKernel              uint64 = 1 << 3 // "scene-kernel-v1"
	capTextParley               uint64 = 1 << 4 // "text-parley-0-11-1"
	capGlyphRaster              uint64 = 1 << 5 // "glyph-raster-dwrite"
	capAtlasD3D11               uint64 = 1 << 6 // "glyph-atlas-d3d11"
	capSceneDrawPaths           uint64 = 1 << 7 // "scene-draw-paths" (ticket16)
	capImageCodecs              uint64 = 1 << 8 // "image-codecs-image-0-25" (ticket17)
	capSvgResvg                 uint64 = 1 << 9 // "svg-resvg-0-48" (ticket19)

	// requiredCapabilities is the mask the loader demands of every artifact
	// it accepts. Unknown extra bits are forward-compatible, never rejected.
	requiredCapabilities = capBootstrapBufferRoundTrip
)

// Native status codes (GpuiGoBufferResponse.status and the export return).
const (
	statusOK            int32 = 0
	statusBadMagic      int32 = 1
	statusBadABIVersion int32 = 2
	statusNullRequest   int32 = 3
	statusLengthOverMax int32 = 4
	statusNullData      int32 = 5
	statusNullResponse  int32 = 6
	statusPanic         int32 = 7
)

func statusName(code int32) string {
	switch code {
	case statusOK:
		return "ok"
	case statusBadMagic:
		return "bad magic"
	case statusBadABIVersion:
		return "bad abi version"
	case statusNullRequest:
		return "null request"
	case statusLengthOverMax:
		return "length over max (4096)"
	case statusNullData:
		return "null data with nonzero length"
	case statusNullResponse:
		return "null response"
	case statusPanic:
		return "panic contained"
	default:
		return "unknown"
	}
}

// abiTable mirrors the Rust `GpuiGoAbiTable` ([repr(C)]).
//
// Field order and widths must match the Rust definition exactly. The table
// itself carries size/alignment self-check fields that are compared against
// this mirror before any other field is interpreted. Layout (windows/amd64):
// bufferRoundTrip @0, reserved @8..136 (16 service-table slots), magic @136,
// abiVersion @140, nativeRevision @144, ceCommit @148..188 (4 bytes padding),
// capabilities @192, sizeOfTable @200, alignOfTable @204,
// sizeOfBufferRequest @208, sizeOfBufferResponse @212; size 216, alignment 8.
// Ticket19 grew the reserved slot array from 8 to 16 entries (the additive
// ABI record growth mirrored with the Rust-side self-checks enforcing the
// match).
type abiTable struct {
	bufferRoundTrip      uintptr // Option<unsafe extern "system" fn(..) -> i32>
	reserved             [16]uintptr
	magic                uint32
	abiVersion           uint32
	nativeRevision       uint32
	ceCommit             [40]byte
	capabilities         uint64
	sizeOfTable          uint32
	alignOfTable         uint32
	sizeOfBufferRequest  uint32
	sizeOfBufferResponse uint32
}

// bufferRequest mirrors the Rust `GpuiGoBufferRequest` ([repr(C)]).
// magic @0, abiVersion @4, length @8, data @16; size 24, alignment 8.
type bufferRequest struct {
	magic      uint32
	abiVersion uint32
	length     uint32
	data       uintptr
}

// bufferResponse mirrors the Rust `GpuiGoBufferResponse` ([repr(C)]).
// status @0, echoedLen @4 (4 bytes padding), checksum @8, data @16;
// size 24, alignment 8.
type bufferResponse struct {
	status    int32
	echoedLen uint32
	checksum  uint64
	data      uintptr
}

// layoutSelfChecks asserts that the Go mirrors agree with the sizes the
// ticket02 artifact reports for itself. It runs once per library load, before
// any other table field is trusted.
func (t *abiTable) layoutSelfChecks() error {
	if t.sizeOfTable != uint32(unsafe.Sizeof(abiTable{})) {
		return &ABIError{Field: "size_of_table", Expected: strconv.Itoa(int(unsafe.Sizeof(abiTable{}))), Actual: strconv.Itoa(int(t.sizeOfTable))}
	}
	if t.alignOfTable != uint32(unsafe.Alignof(abiTable{})) {
		return &ABIError{Field: "align_of_table", Expected: strconv.Itoa(int(unsafe.Alignof(abiTable{}))), Actual: strconv.Itoa(int(t.alignOfTable))}
	}
	if t.sizeOfBufferRequest != uint32(unsafe.Sizeof(bufferRequest{})) {
		return &ABIError{Field: "size_of_buffer_request", Expected: strconv.Itoa(int(unsafe.Sizeof(bufferRequest{}))), Actual: strconv.Itoa(int(t.sizeOfBufferRequest))}
	}
	if t.sizeOfBufferResponse != uint32(unsafe.Sizeof(bufferResponse{})) {
		return &ABIError{Field: "size_of_buffer_response", Expected: strconv.Itoa(int(unsafe.Sizeof(bufferResponse{}))), Actual: strconv.Itoa(int(t.sizeOfBufferResponse))}
	}
	return nil
}
