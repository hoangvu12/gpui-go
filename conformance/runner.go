package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// RunResult is the outcome of one reference harness run.
type RunResult struct {
	// Trace is the parsed trace the harness wrote.
	Trace *Trace
	// MetaPath is the path of the run-meta.json sidecar recording the run
	// identity and environment.
	MetaPath string
	// RawStdout and RawStderr are the harness's captured output streams,
	// also retained on disk as raw-stdout.txt and raw-stderr.txt.
	RawStdout []byte
	RawStderr []byte
}

// runMeta is the sidecar record of one reference harness invocation. It
// deliberately lives outside the trace: traces must stay deterministic,
// while run metadata carries timestamps, toolchain and environment
// identity.
type runMeta struct {
	RunStartedAt   string   `json:"run_started_at"`
	HarnessExe     string   `json:"harness_exe"`
	HarnessSHA256  string   `json:"harness_sha256"`
	EnvelopePath   string   `json:"envelope_path"`
	EnvelopeSHA256 string   `json:"envelope_sha256"`
	Argv           []string `json:"argv"`
	GoVersion      string   `json:"go_version"`
	OS             string   `json:"os"`
	Arch           string   `json:"arch"`
	ExitError      string   `json:"exit_error,omitempty"`
}

// maxStderrTail bounds how much harness stderr is echoed into the error
// returned for a failed run; the full output is retained on disk.
const maxStderrTail = 2048

// RunReferenceFixture executes the pinned reference harness binary
// (harnessExe) against the fixture envelope at envelopePath and retains the
// raw run evidence under outDir.
//
// The harness is invoked as
//
//	harnessExe <envelopePath> <traceOutPath>
//
// with traceOutPath = outDir/<fixture-id>/trace.json, matching the Rust
// harness CLI contract (gpui-reference-harness <envelope.json>
// <trace-out.json>). The run directory additionally receives
// raw-stdout.txt, raw-stderr.txt and run-meta.json. The runner captures the
// harness's stdout and stderr separately, parses the written trace, and
// verifies that the trace's envelope_sha256 matches the actual bytes of the
// envelope file. On harness failure the evidence files are still written
// and the returned error carries the exit status and a stderr tail.
func RunReferenceFixture(ctx context.Context, harnessExe string, envelopePath string, outDir string) (*RunResult, error) {
	env, err := LoadEnvelope(envelopePath)
	if err != nil {
		return nil, err
	}
	if err := safeFixtureID(env.FixtureID); err != nil {
		return nil, fmt.Errorf("envelope %s: %w", envelopePath, err)
	}
	envelopeSHA, err := SHA256Envelope(envelopePath)
	if err != nil {
		return nil, err
	}

	runDir := filepath.Join(outDir, env.FixtureID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating run dir %s: %w", runDir, err)
	}
	tracePath := filepath.Join(runDir, "trace.json")

	// Resolve the harness identity before running it so a missing or
	// unreadable binary fails fast, before any output is produced.
	exePath := harnessExe
	if resolved, lookErr := exec.LookPath(harnessExe); lookErr == nil {
		exePath = resolved
	}
	absExe, err := filepath.Abs(exePath)
	if err != nil {
		return nil, fmt.Errorf("resolving harness path %s: %w", harnessExe, err)
	}
	harnessSHA, err := sha256File(absExe)
	if err != nil {
		return nil, err
	}

	argv := []string{absExe, envelopePath, tracePath}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, harnessExe, envelopePath, tracePath)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	// Retain raw evidence before judging success, so failed runs keep
	// their output for diagnosis.
	stdoutPath := filepath.Join(runDir, "raw-stdout.txt")
	stderrPath := filepath.Join(runDir, "raw-stderr.txt")
	if err := os.WriteFile(stdoutPath, stdout.Bytes(), 0o644); err != nil {
		return nil, fmt.Errorf("writing %s: %w", stdoutPath, err)
	}
	if err := os.WriteFile(stderrPath, stderr.Bytes(), 0o644); err != nil {
		return nil, fmt.Errorf("writing %s: %w", stderrPath, err)
	}

	meta := runMeta{
		RunStartedAt:   time.Now().Format(time.RFC3339),
		HarnessExe:     absExe,
		HarnessSHA256:  harnessSHA,
		EnvelopePath:   envelopePath,
		EnvelopeSHA256: envelopeSHA,
		Argv:           argv,
		GoVersion:      runtime.Version(),
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
	}
	if runErr != nil {
		meta.ExitError = runErr.Error()
	}
	metaPath := filepath.Join(runDir, "run-meta.json")
	if err := writeJSONFile(metaPath, meta); err != nil {
		return nil, err
	}

	if runErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("reference harness %s aborted: %w (raw evidence retained in %s)", harnessExe, ctxErr, runDir)
		}
		tail := stderr.Bytes()
		if len(tail) > maxStderrTail {
			tail = tail[len(tail)-maxStderrTail:]
		}
		return nil, fmt.Errorf("reference harness %s failed: %w (raw evidence retained in %s; stderr tail: %s)", harnessExe, runErr, runDir, string(tail))
	}

	trace, err := LoadTrace(tracePath)
	if err != nil {
		return nil, err
	}
	if err := VerifyEnvelopeSHA256(trace, envelopePath); err != nil {
		return nil, fmt.Errorf("run %s produced a trace that does not match its envelope: %w", tracePath, err)
	}
	return &RunResult{
		Trace:     trace,
		MetaPath:  metaPath,
		RawStdout: stdout.Bytes(),
		RawStderr: stderr.Bytes(),
	}, nil
}

// safeFixtureID rejects fixture ids that would escape the run directory
// when joined into a path.
func safeFixtureID(id string) error {
	if id == "" {
		return fmt.Errorf("empty fixture_id")
	}
	if id == "." || id == ".." || strings.ContainsAny(id, `/\:`) {
		return fmt.Errorf("unsafe fixture id %q", id)
	}
	return nil
}

// writeJSONFile writes v as indented JSON with a trailing newline.
func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
