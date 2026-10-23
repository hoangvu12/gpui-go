//go:build windows

// The recorded fixture files (record.json + chrome.png) written by the
// recorder (recorder.go, cmd/parityrecord) and read by the replay
// tests. The files live in internal/parity/testdata/<fixture name>/.
package parity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
)

// Record is the checked-in record.json of one fixture: everything the
// headless-Chrome recorder captured.
type Record struct {
	// Name is the fixture's name (the directory it was recorded into).
	Name string `json:"name"`
	// Chrome is the recording browser's version string, e.g.
	// "Chrome/155.0.8059.85" (the /json/version Browser field).
	Chrome string `json:"chrome"`
	// RecordedAt is the UTC recording time (RFC 3339).
	RecordedAt string `json:"recordedAt"`
	// Viewport is the page's logical size (CSS pixels).
	Viewport Size `json:"viewport"`
	// DeviceScaleFactor is the Chrome device pixel ratio the page was
	// recorded at (2, matching the gpui test window).
	DeviceScaleFactor float64 `json:"deviceScaleFactor"`
	// Boxes are the recorded element boxes, in CSS pixels, keyed by
	// data-id (getBoundingClientRect).
	Boxes map[string]Box `json:"boxes"`
	// Events is the recorded app-visible event transcript.
	Events []EventRecord `json:"events"`
	// Screenshot names the recorded PNG beside this file.
	Screenshot string `json:"screenshot"`

	// image is the decoded screenshot (loaded lazily).
	image *image.RGBA
}

// Image returns the decoded screenshot as opaque RGBA.
func (r *Record) Image() (*image.RGBA, error) {
	if r.image != nil {
		return r.image, nil
	}
	dir, err := RecordDir(r.Name)
	if err != nil {
		return nil, err
	}
	img, err := loadImage(filepath.Join(dir, r.Screenshot))
	if err != nil {
		return nil, fmt.Errorf("decoding %s: %w", r.Screenshot, err)
	}
	r.image = img
	return img, nil
}

// loadImage decodes a PNG file into opaque RGBA.
func loadImage(path string) (*image.RGBA, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return rgbaOf(decoded), nil
}

// rgbaOf converts any decoded image to opaque RGBA (8 bits per
// channel; the screenshots and captures are both opaque).
func rgbaOf(img image.Image) *image.RGBA {
	bounds := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			r, g, b, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			out.Pix[y*out.Stride+x*4+0] = byte(r >> 8)
			out.Pix[y*out.Stride+x*4+1] = byte(g >> 8)
			out.Pix[y*out.Stride+x*4+2] = byte(b >> 8)
			out.Pix[y*out.Stride+x*4+3] = 0xff
		}
	}
	return out
}

// RecordDir resolves the fixture's recorded-file directory,
// internal/parity/testdata/<name>, from any package directory of the
// module (go test) and from the module root (go run
// ./cmd/parityrecord).
func RecordDir(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("parity: no fixture name")
	}
	root, err := moduleRoot()
	if err != nil {
		return "", fmt.Errorf("parity: %w (run from the module root)", err)
	}
	return filepath.Join(root, "internal", "parity", "testdata", name), nil
}

// moduleRoot walks up from the working directory to the module's
// root (the directory holding go.mod).
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

// LoadRecord reads the fixture's record.json (and decodes its
// screenshot lazily on Image()).
func LoadRecord(name string) (*Record, error) {
	dir, err := RecordDir(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "record.json"))
	if err != nil {
		return nil, fmt.Errorf("reading the recorded fixture %s: %w (run the recorder: go run ./cmd/parityrecord -fixture %s)", name, err, name)
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filepath.Join(dir, "record.json"), err)
	}
	if record.Screenshot == "" {
		record.Screenshot = "chrome.png"
	}
	return &record, nil
}

// writeRecord writes record.json and the screenshot into the fixture's
// directory (the recorder's side).
func writeRecord(record *Record, png []byte, html string) error {
	dir, err := RecordDir(record.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	record.Screenshot = "chrome.png"
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "record.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, record.Screenshot), png, 0o644); err != nil {
		return err
	}
	// The page itself, for reading and diffing fixtures by hand.
	return os.WriteFile(filepath.Join(dir, "page.html"), []byte(html), 0o644)
}
