package native

import (
	"errors"
	"testing"
)

// gpui-perf ticket 03: a source is verified and loaded once while any
// handle to it is open; each handle keeps its own release state and
// generation; the last Close releases the shared verified handle, and the
// next Load verifies from scratch.
func TestLoadSharesVerifiedModuleWhileOpen(t *testing.T) {
	opt := Options{CacheRoot: sharedCacheRoot(t)}
	first := mustLoad(t, opt)
	second := mustLoad(t, opt)
	if first.shared != second.shared {
		t.Fatal("second Load of the same source did not reuse the verified load")
	}
	if first.module != second.module {
		t.Error("handles of one source load different modules")
	}
	if a, b := first.Identity(), second.Identity(); a.Generation == b.Generation || a.SHA256 != b.SHA256 || a.Path != b.Path {
		t.Errorf("identities: %+v vs %+v (want one artifact, distinct generations)", a, b)
	}

	// Closing one handle leaves the other usable and the load shared.
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); !errors.Is(err, ErrDoubleRelease) {
		t.Errorf("second Close = %v, want ErrDoubleRelease", err)
	}
	if _, _, err := first.RoundTrip([]byte("x")); !errors.Is(err, ErrClosed) {
		t.Errorf("RoundTrip after Close = %v, want ErrClosed", err)
	}
	if _, _, err := second.RoundTrip([]byte("still open")); err != nil {
		t.Errorf("the other handle stopped working: %v", err)
	}
	third := mustLoad(t, opt)
	if third.shared != second.shared {
		t.Error("a Load while a handle is open did not reuse the verified load")
	}
	shared := second.shared
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := third.Close(); err != nil {
		t.Fatal(err)
	}
	if sharedLoads[loadKey{cacheRoot: opt.CacheRoot}] == shared {
		t.Error("the shared load outlived its last handle")
	}

	// After the last Close the next Load verifies again.
	fresh := mustLoad(t, opt)
	defer fresh.Close()
	if fresh.shared == shared {
		t.Error("Load after the last Close reused a released verified handle")
	}
	if _, _, err := fresh.RoundTrip([]byte("fresh")); err != nil {
		t.Errorf("fresh load: %v", err)
	}
}

// A failing load is never shared: every call runs the load and reports the
// same typed error.
func TestLoadFailureIsReportedEveryTime(t *testing.T) {
	auth := testAuthority(t)
	m := *auth
	m.SHA256 = "00" + auth.SHA256[2:]
	bundle := writeBundle(t, embeddedDLL, &m)
	for i := 0; i < 2; i++ {
		if _, err := Load(Options{BundleDir: bundle}); !errors.Is(err, ErrWrongHash) {
			t.Errorf("load %d: got %v, want ErrWrongHash", i, err)
		}
	}
	if _, ok := sharedLoads[loadKey{bundleDir: bundle}]; ok {
		t.Error("a failed load was registered for sharing")
	}
}
