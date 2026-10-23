package gpui_test

// gpui-perf ticket 03: startup cost of the native artifact loads
// (.scratch/gpui-perf/issues/03-native-load-once.md). Startup is a
// once-per-process cost, so the report runs a fresh child process per
// sample: the child times its first native service (a scene) and then
// its first drawn frame on the real text system (scene, layout, text,
// glyph and atlas services all load by then).
//
//	GPUI_PERF_STARTUP_OUT=evidence/perf/ticket03-x.json \
//	  CGO_ENABLED=0 go test ./gpui -run TestPerfStartupReport -count=1

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/native"
)

const perfStartupChildEnv = "GPUI_PERF_STARTUP_CHILD"

type perfStartupSample struct {
	FirstServiceMs float64 `json:"first_service_ms"`
	FirstFrameMs   float64 `json:"first_frame_ms"`
	NativeCalls    uint64  `json:"native_calls"`
}

// TestPerfStartupChild is the measured child process; it does nothing
// unless the report started it.
func TestPerfStartupChild(t *testing.T) {
	if os.Getenv(perfStartupChildEnv) == "" {
		t.Skip("run by TestPerfStartupReport")
	}
	start := time.Now()
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("first service: %v", err)
	}
	firstService := time.Since(start)
	_ = scene
	p := openPerfDivs(t)
	_ = p
	firstFrame := time.Since(start)
	data, _ := json.Marshal(perfStartupSample{
		FirstServiceMs: float64(firstService.Microseconds()) / 1000,
		FirstFrameMs:   float64(firstFrame.Microseconds()) / 1000,
		NativeCalls:    native.CallCount(),
	})
	fmt.Printf("PERF_STARTUP %s\n", data)
}

// TestPerfStartupReport runs GPUI_PERF_STARTUP_RUNS (default 7) child
// processes and writes the median and min of each measure to
// GPUI_PERF_STARTUP_OUT. It is skipped when that variable is unset.
func TestPerfStartupReport(t *testing.T) {
	out := os.Getenv("GPUI_PERF_STARTUP_OUT")
	if out == "" {
		t.Skip("set GPUI_PERF_STARTUP_OUT to write the startup evidence")
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join("..", out)
	}
	runs := 7
	if v := os.Getenv("GPUI_PERF_STARTUP_RUNS"); v != "" {
		fmt.Sscan(v, &runs)
	}
	var samples []perfStartupSample
	for i := 0; i < runs; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestPerfStartupChild$", "-test.count=1")
		cmd.Env = append(os.Environ(), perfStartupChildEnv+"=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child %d: %v\n%s", i, err, output)
		}
		found := false
		for _, line := range strings.Split(string(output), "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "PERF_STARTUP "); ok {
				var s perfStartupSample
				if err := json.Unmarshal([]byte(rest), &s); err != nil {
					t.Fatal(err)
				}
				samples = append(samples, s)
				found = true
			}
		}
		if !found {
			t.Fatalf("child %d printed no sample:\n%s", i, output)
		}
	}
	stat := func(get func(perfStartupSample) float64) map[string]float64 {
		values := make([]float64, len(samples))
		for i, s := range samples {
			values[i] = get(s)
		}
		sort.Float64s(values)
		return map[string]float64{"min": values[0], "median": values[len(values)/2], "max": values[len(values)-1]}
	}
	firstService := stat(func(s perfStartupSample) float64 { return s.FirstServiceMs })
	firstFrame := stat(func(s perfStartupSample) float64 { return s.FirstFrameMs })
	t.Logf("first service ready: %v ms; first frame: %v ms", firstService, firstFrame)
	doc := map[string]any{
		"label":                  os.Getenv("GPUI_PERF_LABEL"),
		"date":                   time.Now().UTC().Format(time.RFC3339),
		"go":                     runtime.Version(),
		"runs":                   runs,
		"method":                 "one fresh test process per sample (gpui/perf_startup_test.go); timed from the child test's start: first service = gpui.NewScene(), first frame = the 1,000-div benchmark window opened on the real text system and its first frame drawn; the embedded artifact is already materialized in the user cache",
		"first_service_ready_ms": firstService,
		"first_frame_ms":         firstFrame,
		"samples":                samples,
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
