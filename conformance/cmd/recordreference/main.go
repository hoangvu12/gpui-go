// Command recordreference runs the pinned reference harness for one fixture
// envelope and records the raw trace, run metadata and environment identity
// under conformance/recorded/<fixture-id>/.
//
// Usage:
//
//	go run ./conformance/cmd/recordreference -harness <path-to-gpui-reference-harness.exe> -envelope <path-to-envelope.json> -out conformance/recorded
//
// The reference harness binary is maintainer-built from the pinned CE source
// (see reference/README.md). This command is the only supported way to refresh
// a recorded reference trace.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gpui-go/conformance"
)

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "recordreference:", err)
	os.Exit(1)
}

func main() {
	harness := flag.String("harness", "", "path to the gpui-reference-harness binary")
	envelope := flag.String("envelope", "", "path to the fixture envelope JSON")
	out := flag.String("out", "conformance/recorded", "directory for recorded traces")
	timeout := flag.Duration("timeout", 2*time.Minute, "timeout for the harness run")
	flag.Parse()

	if *harness == "" || *envelope == "" {
		fmt.Fprintln(os.Stderr, "usage: recordreference -harness <exe> -envelope <json> [-out dir]")
		os.Exit(2)
	}

	harnessAbs, err := filepath.Abs(*harness)
	if err != nil {
		fatal(err)
	}
	envelopeAbs, err := filepath.Abs(*envelope)
	if err != nil {
		fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	result, err := conformance.RunReferenceFixture(ctx, harnessAbs, envelopeAbs, *out)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("recorded %s: %d events, meta at %s\n",
		result.Trace.FixtureID, len(result.Trace.Events), result.MetaPath)
}
