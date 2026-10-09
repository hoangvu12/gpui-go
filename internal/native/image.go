package native

// Image codec service access (ticket17, reserved slot 7).
//
// This file is the Go mirror of the native image service documented in
// reference/native/IMAGE_ABI.md and implemented in
// reference/native/src/image.rs: the pinned `image`-crate decode graph
// (the CE workspace's `image = "0.25.1"`, lock resolution 0.25.10) as
// the pinned checkout uses it — crates/gpui/src/elements/img.rs (the
// resource entry: guess_format sniffing, the GIF and animated-WebP
// frame walks with bad frames skipped, the static WebP branch, the
// SVG-fallback signal), crates/gpui/src/platform.rs
// (decode_static_image: explicit format, EXIF orientation, rgba8, the
// RGBA→BGRA channel swap) and crates/gpui_windows/src/clipboard.rs
// (the clipboard entry: the format is known from the clipboard's
// registered format, never sniffed; CF_DIB becomes BMP before the
// decode).
//
// The service table lives in reserved slot 7 of the bootstrap ABI table
// and is advertised with capability bit 8 ("image-codecs-image-0-25").
// Records are plain fixed-width Go structs whose layout is asserted
// against the native self-check fields at service-fetch time
// (validateImageTable). No Go pointer is retained across calls: the
// encoded bytes are borrowed for the decode, and the decoded frames
// live behind generation-stamped handles until Dispose.

import (
	"errors"
	"fmt"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Status codes and errors
// ---------------------------------------------------------------------------

// Native image service status codes (mirror image_status in
// reference/native/src/image.rs). 0 is success; negative values are
// caller/argument errors; 100+ are internal failures.
const (
	imageStatusOK           int32 = 0
	imageErrStaleHandle     int32 = -1
	imageErrBadHandle       int32 = -2
	imageErrNullArg         int32 = -3
	imageErrBadValue        int32 = -4
	imageErrFormatUnknown   int32 = -5
	imageErrDecode          int32 = -6
	imageErrAllFramesFailed int32 = -7
	imageErrCapacity        int32 = -8
	imageErrImageLimit      int32 = -9
	imageErrPanic           int32 = 101
)

// imageServiceVersion mirrors GPUI_GO_IMAGE_SERVICE_VERSION.
const imageServiceVersion uint32 = 1

func imageStatusName(code int32) string {
	switch code {
	case imageStatusOK:
		return "ok"
	case imageErrStaleHandle:
		return "stale handle (image disposed or slot reused)"
	case imageErrBadHandle:
		return "bad handle (malformed)"
	case imageErrNullArg:
		return "null argument"
	case imageErrBadValue:
		return "bad value (length, capacity, format tag or frame index)"
	case imageErrFormatUnknown:
		return "format unknown (the resource-entry sniff failed; the pin routes these to the SVG renderer)"
	case imageErrDecode:
		return "decode failure (the decoder rejected the bytes)"
	case imageErrAllFramesFailed:
		return "all frames failed (an animated decode produced no usable frame)"
	case imageErrCapacity:
		return "capacity too small (out_needed carries the required bytes)"
	case imageErrImageLimit:
		return "image handle limit exceeded"
	case imageErrPanic:
		return "native panic contained"
	default:
		return "unknown image status"
	}
}

// Image service sentinel errors, distinguishable with errors.Is.
var (
	// ErrImageStaleHandle: the image handle encoded a past generation.
	ErrImageStaleHandle = errors.New("native: image handle is stale")
	// ErrImageBadHandle: the handle never existed or is malformed.
	ErrImageBadHandle = errors.New("native: image handle is invalid")
	// ErrImageNullArg: a required pointer argument was null.
	ErrImageNullArg = errors.New("native: image argument was null")
	// ErrImageBadValue: a request failed validation.
	ErrImageBadValue = errors.New("native: image value rejected")
	// ErrImageFormatUnknown: the sniff could not recognize the bytes
	// (the caller routes to the SVG renderer, exactly like the pin).
	ErrImageFormatUnknown = errors.New("native: image format unknown")
	// ErrImageDecode: the decoder rejected the bytes.
	ErrImageDecode = errors.New("native: image decode failed")
	// ErrImageAllFramesFailed: an animated decode produced no usable
	// frame (the pinned all-frames-failed error).
	ErrImageAllFramesFailed = errors.New("native: image all frames failed")
	// ErrImageCapacity: a pixel dump capacity was too small.
	ErrImageCapacity = errors.New("native: image capacity too small")
	// ErrImageLimit: the live image-handle capacity is exhausted.
	ErrImageLimit = errors.New("native: image handle limit exceeded")
	// ErrImagePanic: a native panic was contained by catch_unwind.
	ErrImagePanic = errors.New("native: image panic contained")
)

func imageError(op string, code int32) error {
	switch code {
	case imageErrStaleHandle:
		return fmt.Errorf("native: %s: %w", op, ErrImageStaleHandle)
	case imageErrBadHandle:
		return fmt.Errorf("native: %s: %w", op, ErrImageBadHandle)
	case imageErrNullArg:
		return fmt.Errorf("native: %s: %w", op, ErrImageNullArg)
	case imageErrBadValue:
		return fmt.Errorf("native: %s: %w", op, ErrImageBadValue)
	case imageErrFormatUnknown:
		return fmt.Errorf("native: %s: %w", op, ErrImageFormatUnknown)
	case imageErrDecode:
		return fmt.Errorf("native: %s: %w", op, ErrImageDecode)
	case imageErrAllFramesFailed:
		return fmt.Errorf("native: %s: %w", op, ErrImageAllFramesFailed)
	case imageErrCapacity:
		return fmt.Errorf("native: %s: %w", op, ErrImageCapacity)
	case imageErrImageLimit:
		return fmt.Errorf("native: %s: %w", op, ErrImageLimit)
	case imageErrPanic:
		return fmt.Errorf("native: %s: %w", op, ErrImagePanic)
	default:
		return fmt.Errorf("native: %s: unknown status %d (%s)", op, code, imageStatusName(code))
	}
}

// ---------------------------------------------------------------------------
// The service table mirror
// ---------------------------------------------------------------------------

// imageNativeTable mirrors GpuiGoImageTable: 8 function pointers then
// 17 self-check/capacity/format-tag scalars; size 136, alignment 8.
type imageNativeTable struct {
	decodeResource  uintptr
	decodeClipboard uintptr
	formatProbe     uintptr
	frameCount      uintptr
	frameInfo       uintptr
	framePixels     uintptr
	dispose         uintptr
	codecGraph      uintptr

	serviceVersion    uint32
	sizeOfTable       uint32
	alignOfTable      uint32
	sizeOfFrameRecord uint32
	sizeOfCodecRecord uint32
	maxImageHandles   uint32
	maxImageBytes     uint32
	maxFrames         uint32
	formatPng         uint32
	formatJpeg        uint32
	formatGif         uint32
	formatWebp        uint32
	formatBmp         uint32
	formatTiff        uint32
	formatIco         uint32
	formatPnm         uint32
	formatSvg         uint32
}

// imageTableFromSlot reads the service table out of DLL memory (the
// bootstrap table's reserved slot 7).
func imageTableFromSlot(slot uintptr) *imageNativeTable {
	return (*imageNativeTable)(*(*unsafe.Pointer)(unsafe.Pointer(&slot)))
}

// validateImageTable asserts the native self-check fields against this
// mirror's compile-time layout (a mismatch fails the fetch instead of
// silently misreading a field).
func validateImageTable(table *imageNativeTable, path string) error {
	if table.serviceVersion != imageServiceVersion {
		return fmt.Errorf("native: image service version = %d, want %d (%s)", table.serviceVersion, imageServiceVersion, path)
	}
	if table.sizeOfTable != uint32(unsafe.Sizeof(imageNativeTable{})) {
		return fmt.Errorf("native: image table size = %d, want %d (%s)", table.sizeOfTable, unsafe.Sizeof(imageNativeTable{}), path)
	}
	if table.alignOfTable != uint32(unsafe.Alignof(imageNativeTable{})) {
		return fmt.Errorf("native: image table alignment = %d, want %d (%s)", table.alignOfTable, unsafe.Alignof(imageNativeTable{}), path)
	}
	if table.sizeOfFrameRecord != uint32(unsafe.Sizeof(imageFrameRecord{})) {
		return fmt.Errorf("native: image frame record size = %d, want %d", table.sizeOfFrameRecord, unsafe.Sizeof(imageFrameRecord{}))
	}
	if table.sizeOfCodecRecord != uint32(unsafe.Sizeof(imageCodecRecord{})) {
		return fmt.Errorf("native: image codec record size = %d, want %d", table.sizeOfCodecRecord, unsafe.Sizeof(imageCodecRecord{}))
	}
	for _, entry := range []struct {
		name string
		ptr  uintptr
	}{
		{"decode_resource", table.decodeResource},
		{"decode_clipboard", table.decodeClipboard},
		{"format_probe", table.formatProbe},
		{"frame_count", table.frameCount},
		{"frame_info", table.frameInfo},
		{"frame_pixels", table.framePixels},
		{"dispose", table.dispose},
		{"codec_graph", table.codecGraph},
	} {
		if entry.ptr == 0 {
			return fmt.Errorf("native: image service entry %s is null (%s)", entry.name, path)
		}
	}
	return nil
}

// imageFrameRecord mirrors GpuiGoImageFrameRecord: width, height, the
// rational millisecond delay, reserved; size 20, alignment 4.
type imageFrameRecord struct {
	width        uint32
	height       uint32
	delayNumerMS uint32
	delayDenomMS uint32
	reserved     uint32
}

// imageCodecRecord mirrors GpuiGoImageCodecRecord: the format tag, the
// animated flag, reserved; size 12, alignment 4.
type imageCodecRecord struct {
	formatTag uint32
	animated  uint32
	reserved  uint32
}

// ---------------------------------------------------------------------------
// The service client
// ---------------------------------------------------------------------------

// ImageService is the image codec service client (reserved slot 7).
type ImageService struct {
	lib   *Library
	table imageNativeTable
}

// Image fetches the image codec service (capability bit 8, reserved
// slot 7).
func (l *Library) Image() (*ImageService, error) {
	if l.released.Load() {
		return nil, ErrClosed
	}
	if l.identity.Capabilities&capImageCodecs == 0 {
		return nil, &CapabilityError{
			Path:     l.identity.Path,
			Required: capImageCodecs,
			Actual:   l.identity.Capabilities,
			Detail:   "image-codecs-image-0-25 capability bit missing",
		}
	}
	slot := l.table.reserved[7]
	if slot == 0 {
		return nil, &CapabilityError{Path: l.identity.Path, Detail: "image service table (reserved slot 7) is null"}
	}
	table := *imageTableFromSlot(slot) // copy out of DLL memory
	if err := validateImageTable(&table, l.identity.Path); err != nil {
		return nil, err
	}
	return &ImageService{lib: l, table: table}, nil
}

// ImageHandle is a validated opaque native decoded-image handle. It
// becomes stale after Dispose (or native slot reuse).
type ImageHandle uint64

// ImageFormat is the ABI format tag space (the pinned ImageFormat
// discriminants the codec graph supports, plus the probe-reported SVG
// tag).
type ImageFormat uint32

// The ABI format tags (mirror image_format in image.rs).
const (
	ImageFormatPNG  ImageFormat = 1
	ImageFormatJPEG ImageFormat = 2
	ImageFormatGIF  ImageFormat = 3
	ImageFormatWebP ImageFormat = 4
	ImageFormatBMP  ImageFormat = 5
	ImageFormatTIFF ImageFormat = 6
	ImageFormatICO  ImageFormat = 7
	ImageFormatPNM  ImageFormat = 8
	// ImageFormatSVG is reported by the probe only; decode rejects it
	// (the SVG renderer is a separate, deferred service).
	ImageFormatSVG ImageFormat = 9
)

// String returns the format's pinned name.
func (f ImageFormat) String() string {
	switch f {
	case ImageFormatPNG:
		return "png"
	case ImageFormatJPEG:
		return "jpeg"
	case ImageFormatGIF:
		return "gif"
	case ImageFormatWebP:
		return "webp"
	case ImageFormatBMP:
		return "bmp"
	case ImageFormatTIFF:
		return "tiff"
	case ImageFormatICO:
		return "ico"
	case ImageFormatPNM:
		return "pnm"
	case ImageFormatSVG:
		return "svg"
	default:
		return fmt.Sprintf("unknown(%d)", uint32(f))
	}
}

// ImageFrameInfo is one decoded frame's geometry and rational delay.
type ImageFrameInfo struct {
	// Width and Height are the frame's pixel dimensions.
	Width, Height uint32
	// DelayNumerMS and DelayDenomMS are the frame delay's rational
	// millisecond parts (the image crate's Delay). Zero for a static
	// decode's single frame.
	DelayNumerMS, DelayDenomMS uint32
}

// ImageCodec is one codec-graph row: the format and whether the pinned
// graph decodes it through the animated frame walk.
type ImageCodec struct {
	// Format is the ABI format tag.
	Format ImageFormat
	// Animated reports the pinned graph's animated formats (GIF, WebP).
	Animated bool
}

// MaxImageHandles reports the native live-handle bound.
func (s *ImageService) MaxImageHandles() uint32 { return s.table.maxImageHandles }

// MaxImageBytes reports the native accepted encoded-byte bound.
func (s *ImageService) MaxImageBytes() uint32 { return s.table.maxImageBytes }

// MaxFrames reports the native per-decode frame bound.
func (s *ImageService) MaxFrames() uint32 { return s.table.maxFrames }

// DecodeResource decodes through the resource entry: the format is
// sniffed from the bytes (image::guess_format, never an extension
// list), then the pinned branch — GIF and animated WebP walk frames,
// static WebP and the other formats decode with EXIF orientation. An
// unrecognized sniff returns ErrImageFormatUnknown so the caller can
// route to the SVG renderer exactly like the pin.
func (s *ImageService) DecodeResource(bytes []byte) (ImageHandle, error) {
	if uint64(len(bytes)) > uint64(s.table.maxImageBytes) {
		return 0, fmt.Errorf("native: image decode: %w: %d bytes over the %d bound", ErrImageBadValue, len(bytes), s.table.maxImageBytes)
	}
	var ptr *byte
	if len(bytes) > 0 {
		ptr = &bytes[0]
	}
	var handle uint64
	code, err := callImageDecodeResource(s.table.decodeResource, ptr, uint32(len(bytes)), &handle)
	if err != nil {
		return 0, err
	}
	if code != imageStatusOK {
		return 0, imageError("image_decode_resource", code)
	}
	return ImageHandle(handle), nil
}

// DecodeClipboard decodes through the clipboard entry: the format is
// KNOWN (the clipboard's registered format; CF_DIB was converted to
// BMP bytes by the caller). GIF and WebP use the animated walks; the
// static formats decode with ImageReader::with_format, matching the
// pin's decode_static_image.
func (s *ImageService) DecodeClipboard(bytes []byte, format ImageFormat) (ImageHandle, error) {
	if uint64(len(bytes)) > uint64(s.table.maxImageBytes) {
		return 0, fmt.Errorf("native: image decode: %w: %d bytes over the %d bound", ErrImageBadValue, len(bytes), s.table.maxImageBytes)
	}
	if format == ImageFormatSVG || format == 0 {
		return 0, fmt.Errorf("native: image decode clipboard: %w: format %v", ErrImageBadValue, format)
	}
	var ptr *byte
	if len(bytes) > 0 {
		ptr = &bytes[0]
	}
	var handle uint64
	code, err := callImageDecodeClipboard(s.table.decodeClipboard, ptr, uint32(len(bytes)), uint32(format), &handle)
	if err != nil {
		return 0, err
	}
	if code != imageStatusOK {
		return 0, imageError("image_decode_clipboard", code)
	}
	return ImageHandle(handle), nil
}

// ProbeFormat sniffs the format without decoding (image::guess_format).
// The second return is false when the bytes are not recognized (the
// pin's SVG-fallback signal).
func (s *ImageService) ProbeFormat(bytes []byte) (ImageFormat, bool) {
	if uint64(len(bytes)) > uint64(s.table.maxImageBytes) {
		return 0, false
	}
	var ptr *byte
	if len(bytes) > 0 {
		ptr = &bytes[0]
	}
	var tag uint32
	code, _ := callImageFormatProbe(s.table.formatProbe, ptr, uint32(len(bytes)), &tag)
	if code != imageStatusOK {
		return 0, false
	}
	return ImageFormat(tag), true
}

// FrameCount returns the decoded image's frame count.
func (s *ImageService) FrameCount(handle ImageHandle) (uint32, error) {
	var count uint32
	code, err := callImageFrameCount(s.table.frameCount, uint64(handle), &count)
	if err != nil {
		return 0, err
	}
	if code != imageStatusOK {
		return 0, imageError("image_frame_count", code)
	}
	return count, nil
}

// FrameInfo returns one frame's geometry and rational delay.
func (s *ImageService) FrameInfo(handle ImageHandle, frameIndex uint32) (ImageFrameInfo, error) {
	var record imageFrameRecord
	code, err := callImageFrameInfo(s.table.frameInfo, uint64(handle), frameIndex, &record)
	if err != nil {
		return ImageFrameInfo{}, err
	}
	if code != imageStatusOK {
		return ImageFrameInfo{}, imageError("image_frame_info", code)
	}
	return ImageFrameInfo{
		Width:        record.width,
		Height:       record.height,
		DelayNumerMS: record.delayNumerMS,
		DelayDenomMS: record.delayDenomMS,
	}, nil
}

// FramePixels copies one frame's BGRA8 bytes (width*height*4, the
// pinned RGBA→BGRA swap already applied).
func (s *ImageService) FramePixels(handle ImageHandle, frameIndex uint32) ([]byte, error) {
	var needed uint32
	code, err := callImageFramePixels(s.table.framePixels, uint64(handle), frameIndex, nil, 0, &needed)
	if err != nil {
		return nil, err
	}
	if code != imageStatusOK && code != imageErrCapacity {
		return nil, imageError("image_frame_pixels", code)
	}
	if needed == 0 {
		info, err := s.FrameInfo(handle, frameIndex)
		if err != nil {
			return nil, err
		}
		if info.Width == 0 || info.Height == 0 {
			return nil, nil
		}
	}
	buf := make([]byte, needed)
	if needed == 0 {
		return buf, nil
	}
	var outNeeded uint32
	code, err = callImageFramePixels(s.table.framePixels, uint64(handle), frameIndex, &buf[0], uint32(cap(buf)), &outNeeded)
	if err != nil {
		return nil, err
	}
	if code != imageStatusOK {
		return nil, imageError("image_frame_pixels", code)
	}
	return buf, nil
}

// Dispose releases the decoded image's native slot.
func (s *ImageService) Dispose(handle ImageHandle) error {
	code, err := callImageDispose(s.table.dispose, uint64(handle))
	if err != nil {
		return err
	}
	if code != imageStatusOK {
		return imageError("image_dispose", code)
	}
	return nil
}

// CodecGraph returns the effective codec graph: every format this
// build supports and which take the animated walk.
func (s *ImageService) CodecGraph() ([]ImageCodec, error) {
	var needed, count uint32
	code, err := callImageCodecGraph(s.table.codecGraph, nil, 0, &count, &needed)
	if err != nil {
		return nil, err
	}
	if code != imageStatusOK && code != imageErrCapacity {
		return nil, imageError("image_codec_graph", code)
	}
	if needed == 0 {
		return nil, nil
	}
	records := make([]imageCodecRecord, needed)
	var outCount, outNeeded uint32
	code, err = callImageCodecGraph(s.table.codecGraph, &records[0], uint32(len(records)), &outCount, &outNeeded)
	if err != nil {
		return nil, err
	}
	if code != imageStatusOK {
		return nil, imageError("image_codec_graph", code)
	}
	out := make([]ImageCodec, outCount)
	for i := uint32(0); i < outCount; i++ {
		out[i] = ImageCodec{
			Format:   ImageFormat(records[i].formatTag),
			Animated: records[i].animated != 0,
		}
	}
	return out, nil
}
