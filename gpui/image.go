package gpui

// This file is ticket17's image decode model: the pinned Image and
// RenderImage (crates/gpui/src/platform.rs `Image` — format, bytes, a
// content hash id; crates/gpui/src/assets.rs `RenderImage` — BGRA
// frames with per-frame rational delays) over the native image codec
// service (internal/native/image.go, the approved one-rebuild artifact
// revision 8): the resource entry (sniffed, the SVG-fallback signal
// preserved) and the clipboard entry (known format), with decoded
// frames uploaded into the polychrome atlas pool through the pinned
// AtlasKey::Image { image_id, frame_index } identity. The image cache
// and asset loading are ticket18; the img element's scene painting
// follows the sprite machinery of ticket10/16.

import (
	"fmt"
	"sync"
	"time"

	"gpui-go/internal/native"
)

// ImageFormat is the image's format (the pinned ImageFormat
// discriminants the codec graph supports).
type ImageFormat = native.ImageFormat

// The supported formats.
const (
	ImageFormatPNG  = native.ImageFormatPNG
	ImageFormatJPEG = native.ImageFormatJPEG
	ImageFormatGIF  = native.ImageFormatGIF
	ImageFormatWebP = native.ImageFormatWebP
	ImageFormatBMP  = native.ImageFormatBMP
	ImageFormatTIFF = native.ImageFormatTIFF
	ImageFormatICO  = native.ImageFormatICO
	ImageFormatPNM  = native.ImageFormatPNM
	// ImageFormatSVG is the probe-reported tag (the SVG renderer is a
	// deferred service); decode rejects it.
	ImageFormatSVG = native.ImageFormatSVG
)

// Image is an image with a format and certain bytes (the pinned
// platform.rs Image): the id is the content hash of the bytes.
type Image struct {
	// Format is the image format the bytes represent.
	Format ImageFormat
	// Bytes are the raw (encoded) image bytes.
	Bytes []byte
	// ID is the content-hash identity of the bytes.
	ID uint64
}

// NewImage creates an image from a format and bytes, hashing the
// content for the id (Image::from_bytes).
func NewImage(format ImageFormat, bytes []byte) Image {
	return Image{Format: format, Bytes: bytes, ID: imageContentHash(bytes)}
}

// ImageFrame is one decoded frame: BGRA pixels, dimensions and the
// rational delay from the previous frame.
type ImageFrame struct {
	// Width and Height are the frame's pixel dimensions.
	Width, Height int
	// Pixels are the BGRA8 bytes (the pinned RGBA→BGRA swap applied).
	Pixels []byte
	// Delay is the frame's delay (the image crate's rational
	// milliseconds; zero for a static decode, the pin's `Frame::new`).
	Delay time.Duration
}

// RenderImage is a decoded, processed image: its BGRA frames (the
// pinned assets.rs RenderImage, bounded to the frame model — the
// scale-factor and atlas-lifetime machinery lands with the element
// rendering).
type RenderImage struct {
	// Frames are the decoded BGRA frames in order.
	Frames []ImageFrame
}

// FrameCount returns the number of frames.
func (r *RenderImage) FrameCount() int { return len(r.Frames) }

// Size returns frame ix's pixel size (the pin's
// RenderImage::size; zero for an out-of-range index).
func (r *RenderImage) Size(ix int) (int, int) {
	if ix < 0 || ix >= len(r.Frames) {
		return 0, 0
	}
	return r.Frames[ix].Width, r.Frames[ix].Height
}

// Delay returns frame ix's delay, the pin's fallback (100ms) for an
// out-of-range index.
func (r *RenderImage) Delay(ix int) time.Duration {
	if ix < 0 || ix >= len(r.Frames) {
		return 100 * time.Millisecond
	}
	return r.Frames[ix].Delay
}

// ImageCodec is the decode seam: the native image codec service plus
// the atlas the decoded frames upload into (the pin pairs the platform
// text/image system with the renderer's atlas).
type ImageCodec struct {
	svc   *native.ImageService
	atlas *Atlas
}

// The process-level image service (the atlas/text services' pattern).
var (
	imageServiceOnce sync.Once
	imageServiceVal  *native.ImageService
	imageServiceErr  error
)

// imageService loads the native library once and fetches the image
// codec service.
func imageService() (*native.ImageService, error) {
	imageServiceOnce.Do(func() {
		lib, err := native.Load(native.Options{})
		if err != nil {
			imageServiceErr = fmt.Errorf("gpui: %w: loading the native artifact: %w", ErrImageService, err)
			return
		}
		svc, err := lib.Image()
		if err != nil {
			imageServiceErr = fmt.Errorf("gpui: %w: %w", ErrImageService, err)
			return
		}
		imageServiceVal = svc
	})
	return imageServiceVal, imageServiceErr
}

// ErrImageService reports a failure to load the native image codec
// service.
var ErrImageService = fmt.Errorf("gpui: native image codec service unavailable")

// NewImageCodec constructs a codec over the process image service with
// its own atlas.
func NewImageCodec() (*ImageCodec, error) {
	svc, err := imageService()
	if err != nil {
		return nil, err
	}
	atlas, err := NewAtlas()
	if err != nil {
		return nil, fmt.Errorf("gpui: NewImageCodec: %w", err)
	}
	return &ImageCodec{svc: svc, atlas: atlas}, nil
}

// Atlas returns the codec's atlas (frame uploads and removals).
func (c *ImageCodec) Atlas() *Atlas { return c.atlas }

// Dispose releases the codec's atlas.
func (c *ImageCodec) Dispose() error { return c.atlas.Dispose() }

// ErrImageFormatUnknown reports bytes the resource-entry sniff could
// not recognize: the caller routes them to the SVG renderer exactly
// like the pin's img.rs (the SVG renderer itself is a deferred service).
var ErrImageFormatUnknown = native.ErrImageFormatUnknown

// DecodeResource decodes through the resource entry (the sniffed
// path): returns the decoded frames or ErrImageFormatUnknown for
// non-codec bytes (the SVG-fallback signal).
func (c *ImageCodec) DecodeResource(bytes []byte) (*RenderImage, error) {
	if c == nil {
		return nil, fmt.Errorf("gpui: no image codec service")
	}
	handle, err := c.svc.DecodeResource(bytes)
	if err != nil {
		return nil, err
	}
	return renderImageOf(c, handle)
}

// DecodeClipboard decodes through the clipboard entry (the known-format
// path; the caller already converted CF_DIB to BMP).
func (c *ImageCodec) DecodeClipboard(bytes []byte, format ImageFormat) (*RenderImage, error) {
	if c == nil {
		return nil, fmt.Errorf("gpui: no image codec service")
	}
	handle, err := c.svc.DecodeClipboard(bytes, format)
	if err != nil {
		return nil, err
	}
	return renderImageOf(c, handle)
}

// renderImageOf drains one native decode handle into the frame model
// and disposes it.
func renderImageOf(c *ImageCodec, handle native.ImageHandle) (*RenderImage, error) {
	svc := c.svc
	count, err := svc.FrameCount(handle)
	if err != nil {
		_ = svc.Dispose(handle)
		return nil, err
	}
	image := &RenderImage{Frames: make([]ImageFrame, count)}
	for i := uint32(0); i < count; i++ {
		info, err := svc.FrameInfo(handle, i)
		if err != nil {
			_ = svc.Dispose(handle)
			return nil, err
		}
		pixels, err := svc.FramePixels(handle, i)
		if err != nil {
			_ = svc.Dispose(handle)
			return nil, err
		}
		image.Frames[i] = ImageFrame{
			Width:  int(info.Width),
			Height: int(info.Height),
			Pixels: pixels,
			Delay:  imageDelayOf(info),
		}
	}
	if err := svc.Dispose(handle); err != nil {
		return nil, err
	}
	return image, nil
}

// imageDelayOf converts the rational millisecond delay (the image
// crate's Delay) to a duration; a zero denominator (the static decode's
// zero delay) stays zero.
func imageDelayOf(info native.ImageFrameInfo) time.Duration {
	if info.DelayDenomMS == 0 {
		return 0
	}
	return time.Duration(info.DelayNumerMS) * time.Millisecond / time.Duration(info.DelayDenomMS)
}

// CodecGraph reports the effective codec graph: the supported formats
// and which decode through the animated frame walk.
func (c *ImageCodec) CodecGraph() ([]native.ImageCodec, error) {
	if c == nil {
		return nil, fmt.Errorf("gpui: no image codec service")
	}
	return c.svc.CodecGraph()
}

// ProbeFormat sniffs the format of bytes without decoding (the pin's
// image::guess_format; false means the SVG-fallback signal).
func (c *ImageCodec) ProbeFormat(bytes []byte) (ImageFormat, bool) {
	if c == nil {
		return 0, false
	}
	return c.svc.ProbeFormat(bytes)
}

// imageContentHash is the FNV-1a content hash of the bytes (the pin's
// `hash` helper identity).
func imageContentHash(bytes []byte) uint64 {
	const offsetBasis uint64 = 0xcbf29ce484222325
	const prime uint64 = 0x00000100000001b3
	h := offsetBasis
	for _, b := range bytes {
		h ^= uint64(b)
		h *= prime
	}
	return h
}

// imageAtlasKey builds the pinned AtlasKey::Image { image_id,
// frame_index } record (the slot reuse the native service documents).
func imageAtlasKey(imageID uint64, frameIndex uint32) native.AtlasKeyRecord {
	key := native.NewAtlasKeyRecord()
	key.Kind = uint32(native.AtlasKeyImage)
	key.FontID = imageID
	key.FontSizeBits = frameIndex
	return key
}

// InsertImageFrame uploads one decoded frame into the polychrome atlas
// pool under the pinned image identity, returning its tile. A cached
// key returns the existing tile with no upload (the pinned
// get_or_insert_with map hit).
func (a *Atlas) InsertImageFrame(imageID uint64, frameIndex int, frame *ImageFrame) (AtlasTile, error) {
	if a == nil {
		return AtlasTile{}, fmt.Errorf("gpui: InsertImageFrame: nil atlas")
	}
	if frame == nil {
		return AtlasTile{}, fmt.Errorf("gpui: InsertImageFrame: nil frame")
	}
	key := imageAtlasKey(imageID, uint32(frameIndex))
	tile, err := a.svc.Insert(a.handle, key, int32(frame.Width), int32(frame.Height), frame.Pixels)
	if err != nil {
		return AtlasTile{}, fmt.Errorf("gpui: InsertImageFrame: %w", err)
	}
	return AtlasTile{
		TextureIndex: tile.TextureIndex,
		TextureKind:  AtlasTextureKind(tile.TextureKind),
		TileID:       tile.TileID,
		Padding:      tile.Padding,
		BoundsX:      tile.BoundsX,
		BoundsY:      tile.BoundsY,
		BoundsW:      tile.BoundsW,
		BoundsH:      tile.BoundsH,
		Generation:   tile.Generation,
	}, nil
}

// RemoveImageFrame removes the image identity's tile, deallocating its
// atlas space (the pin's PlatformAtlas::remove; cache release and GPU
// completion remain separate prerequisites for callers).
func (a *Atlas) RemoveImageFrame(imageID uint64, frameIndex int) error {
	if a == nil {
		return fmt.Errorf("gpui: RemoveImageFrame: nil atlas")
	}
	key := imageAtlasKey(imageID, uint32(frameIndex))
	if err := a.svc.Remove(a.handle, key); err != nil {
		return fmt.Errorf("gpui: RemoveImageFrame: %w", err)
	}
	return nil
}
