// Command visualcalib runs ticket 33's bounded visual-calibration slice
// on the final native artifact: the repeat-run noise measurement over
// deterministic scenes through the render-to-image path (the exact-gate
// calibration basis) and the material-injected-defect detection check
// (the gates' sensitivity validation). The output is the audit's
// executed measurement record.
//
//	go run ./conformance/cmd/visualcalib
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"gpui-go/gpui"
)

// nativeManifestIdentity reads the embedded manifest's recorded
// artifact identity (the provenance pair the calibration ran against).
type manifestIdentity struct {
	NativeRevision uint32 `json:"native_revision"`
	Capabilities   uint64 `json:"capabilities_mask"`
	Bytes          int64  `json:"bytes"`
	SHA256         string `json:"sha256"`
}

func nativeManifestIdentity() (manifestIdentity, error) {
	var m manifestIdentity
	raw, err := os.ReadFile("../../../internal/native/artifacts/manifest.json")
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, err
	}
	return m, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "visualcalib:", err)
	os.Exit(1)
}

func main() {
	app := gpui.NewApp()
	host := gpui.NewHost()
	if err := app.Attach(host); err != nil {
		fatal(err)
	}
	if err := host.Start(); err != nil {
		fatal(err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(host) }()
	defer func() {
		_ = host.Stop()
		select {
		case <-runDone:
		case <-time.After(5 * time.Second):
		}
	}()

	window, err := app.OpenWindow(gpui.WindowOptions{Title: "visualcalib", Show: true, Bounds: gpui.Bounds{Size: gpui.Size{Width: 260, Height: 180}}})
	if err != nil {
		fatal(fmt.Errorf("OpenWindow (real window expected): %w", err))
	}
	renderer, err := gpui.NewRendererManual()
	if err != nil {
		fatal(err)
	}
	defer func() { _ = renderer.Close() }()

	surface, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		fatal(err)
	}
	info, err := surface.Info()
	if err != nil {
		fatal(err)
	}
	width, height := info.Width, info.Height
	mask := gpui.Bounds{Size: gpui.Size{Width: float32(width), Height: float32(height)}}

	quad := func(x, y, w, h float32, hex uint32) gpui.Quad {
		return gpui.Quad{
			Bounds:      gpui.Bounds{Origin: gpui.Point{X: x, Y: y}, Size: gpui.Size{Width: w, Height: h}},
			ContentMask: mask,
			Background:  gpui.SolidBackground(gpui.RgbaToHsla(hex)),
		}
	}

	buildScene := func(fills ...gpui.Quad) *gpui.Scene {
		scene, err := gpui.NewScene()
		if err != nil {
			fatal(err)
		}
		for i, q := range fills {
			if err := scene.InsertQuad(gpui.Quad{Bounds: q.Bounds, ContentMask: q.ContentMask, Background: q.Background}); err != nil {
				fatal(err)
			}
			_ = i
		}
		if err := scene.Finish(); err != nil {
			fatal(err)
		}
		return scene
	}

	scenes := map[string]func() *gpui.Scene{
		"solid-red": func() *gpui.Scene {
			return buildScene(quad(0, 0, float32(width), float32(height), 0xd91f1fff))
		},
		"two-tone": func() *gpui.Scene {
			return buildScene(
				quad(0, 0, float32(width)/2, float32(height), 0x2563abff),
				quad(float32(width)/2, 0, float32(width)/2, float32(height), 0xf59e0bff),
			)
		},
	}

	// The noise measurement: N repeat captures per scene; the run-set
	// hashes must all be identical (the deterministic pipeline's
	// measured repeat-run noise is zero at the byte level).
	const runs = 10
	for _, name := range []string{"solid-red", "two-tone"} {
		build := scenes[name]
		hashes := map[string]int{}
		for i := 0; i < runs; i++ {
			scene := build()
			image, err := surface.RenderToImage(scene, nil, gpui.WindowBackgroundOpaque)
			if err != nil {
				fatal(fmt.Errorf("scene %q run %d: %w", name, i, err))
			}
			digest := sha256.Sum256(image.Pixels())
			hashes[hex.EncodeToString(digest[:])]++
			if err := scene.Dispose(); err != nil {
				fatal(err)
			}
		}
		if len(hashes) != 1 {
			fmt.Printf("scene %-10s NOISE: %d distinct hashes over %d runs (FAIL — the exact gate basis is violated)\n", name, len(hashes), runs)
			for h, n := range hashes {
				fmt.Printf("  %s ×%d\n", h, n)
			}
			os.Exit(1)
		}
		for h := range hashes {
			fmt.Printf("scene %-10s noise=0: %d/%d runs byte-identical (sha256 %s…)\n", name, hashes[h], runs, h[:16])
		}
	}

	// The injected-defect detection: material mutations of a captured
	// image must be detected by the byte-level exact gate (the
	// calibration's sensitivity validation — a gate that cannot see an
	// injected defect cannot certify closeness).
	base := scenes["two-tone"]()
	baseline, err := surface.RenderToImage(base, nil, gpui.WindowBackgroundOpaque)
	if err != nil {
		fatal(err)
	}
	if err := base.Dispose(); err != nil {
		fatal(err)
	}
	baselineHash := sha256.Sum256(baseline.Pixels())
	baselineHex := hex.EncodeToString(baselineHash[:])

	defects := []struct {
		name   string
		inject func(pixels []byte) []byte
	}{
		{"one-pixel-color", func(pixels []byte) []byte {
			out := append([]byte(nil), pixels...)
			// A single pixel's blue channel +1 (the mildest material
			// defect).
			out[2] ^= 1
			return out
		}},
		{"pixel-displacement", func(pixels []byte) []byte {
			out := append([]byte(nil), pixels...)
			// A 1-pixel horizontal displacement of every row (the
			// classic sub-pixel rendering defect): each row shifts by
			// one pixel. (The two-tone scene's rows are constant, so a
			// row-replacement mutation would be invisible by scene
			// construction, not gate insensitivity — the displacement
			// is the honest material defect.)
			row := baseline.BytesPerRow()
			for i := 0; i < len(out); i += row {
				copy(out[i:i+row-4], out[i+4:i+row])
			}
			return out
		}},
		{"extent-error", func(pixels []byte) []byte {
			// A truncation defect (the size observable differs).
			return append([]byte(nil), pixels[:len(pixels)/2]...)
		}},
	}
	for _, defect := range defects {
		mutated := defect.inject(baseline.Pixels())
		mutHash := sha256.Sum256(mutated)
		mutHex := hex.EncodeToString(mutHash[:])
		if mutHex == baselineHex {
			fmt.Printf("defect %-18s NOT DETECTED (FAIL — the exact gate is insensitive to this material defect)\n", defect.name)
			os.Exit(1)
		}
		diffBytes := 0
		for i := 0; i < len(mutated) && i < len(baseline.Pixels()); i++ {
			if mutated[i] != baseline.Pixels()[i] {
				diffBytes++
			}
		}
		fmt.Printf("defect %-18s detected: hash differs (%d differing bytes; %d vs %d total)\n", defect.name, diffBytes, len(mutated), len(baseline.Pixels()))
	}

	// The identity record: the exact artifact this calibration ran on.
	identity, idErr := nativeManifestIdentity()
	if idErr != nil {
		fatal(idErr)
	}
	fmt.Printf("artifact: rev %d, mask %#x, %d bytes, sha256 %s…\n", identity.NativeRevision, identity.Capabilities, identity.Bytes, identity.SHA256[:16])
	fmt.Println("calibration conclusion: the exact gates run at threshold 0 (bit-exact) — justified by the measured zero repeat-run noise; the ~98% visual closeness intent stays intent (not a frozen SSIM/pixel threshold)")
}
