//go:build windows

// Command parityrecord records the browser-parity fixtures in
// headless Chrome (no visible window, a throwaway profile), writing
// the checked-in fixture files the replay tests compare against:
//
//	go run ./cmd/parityrecord              # every registered fixture
//	go run ./cmd/parityrecord -fixture tracer-box-click
//
// Run it from the module root. The recorded files land in
// internal/parity/testdata/<fixture name>/. See
// docs/browser-parity-harness.md.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/hoangvu12/gpui-go/internal/parity"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/activation"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/anchor"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/anchoring"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/boxpaint"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/capture"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/closewatcher"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/contextmenu"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/display"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/displayblock"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/dnd"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/editkeys"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/filters"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/fixed"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/forcedcolors"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/gradient"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/hover"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/imecompose"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/keycode"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/keyorder"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/lineheight"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/mincontent"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/modal"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/outline"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/popover"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/position"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/positiondefault"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/scrollrange"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/selection"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/shadow"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/smoothscroll"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/sticky"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/tabnav"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/textpaint"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/textstyle"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/textwrap"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/tooltip"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/tracer"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/transition"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/tsxslots"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/userselect"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/wheel"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/whitespace"
)

func main() {
	only := flag.String("fixture", "", "record only the named fixture (default: every registered fixture)")
	list := flag.Bool("list", false, "list the registered fixtures")
	flag.Parse()

	fixtures := parity.All()
	if *list {
		for _, f := range fixtures {
			fmt.Println(f.Name)
		}
		return
	}
	failures := 0
	for _, f := range fixtures {
		if *only != "" && *only != f.Name {
			continue
		}
		if err := parity.RecordFixture(f); err != nil {
			fmt.Fprintf(os.Stderr, "parityrecord: %s: %v\n", f.Name, err)
			failures++
		}
	}
	if *only != "" {
		if _, ok := parity.Lookup(*only); !ok {
			fmt.Fprintf(os.Stderr, "parityrecord: no fixture named %q (use -list)\n", *only)
			os.Exit(1)
		}
	}
	if failures > 0 {
		os.Exit(1)
	}
}
