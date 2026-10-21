// Package buildcostspec guards the consumer rebuild cost: how much a
// package that uses gpui compiles itself after a one-line edit.
//
// Go compiles every ordinary method of an instantiated generic type in
// each package that references the instantiation. While the fluent
// mixins (Styled, Parent, Interactive) had ordinary methods, a 109-line
// view compiled to a ~30 MB object and rebuilt in ~14 s (a real app's UI
// package: ~37 s), a package defining five Component[Self] components
// compiled to ~171 MB, and a package using them to ~77 MB. The fluent
// methods now carry the lazy marker (gpui/styled.go), so a package
// compiles only the methods it calls. Object size is the gate because,
// unlike wall time, it does not depend on machine load.
package buildcostspec

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// probes are the testdata packages and the cap on each compiled package.
// Measured: probe ~2.7 MB, components ~4.8 MB, compuser ~2.2 MB. Turning
// the mixins' fluent methods back into ordinary methods adds tens of MB
// to each.
var probes = []struct {
	dir, what string
	max       int64
}{
	{"probe", "a view using Div, Img and Canvas", 6 << 20},
	{"components", "a package defining five Component[Self] components", 10 << 20},
	{"compuser", "a package using five Component[Self] components", 6 << 20},
}

func TestConsumerCompileCost(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the probe packages with the go tool")
	}
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, p := range probes {
		t.Run(p.dir, func(t *testing.T) {
			cmd := exec.Command(goTool, "list", "-export", "-f", "{{.Export}}", "./testdata/"+p.dir)
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("go list -export: %v\n%s", err, out)
			}
			path := strings.TrimSpace(string(out))
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat %q: %v", path, err)
			}
			t.Logf("%s: %d bytes (cap %d)", p.what, info.Size(), p.max)
			if info.Size() > p.max {
				t.Fatalf("%s compiles to %d bytes (cap %d): a large method set is being "+
					"instantiated in every consumer package; fluent methods of generic "+
					"mixins need the lazy marker (gpui/styled.go)", p.what, info.Size(), p.max)
			}
		})
	}
}
