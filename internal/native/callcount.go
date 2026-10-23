package native

import "sync/atomic"

// nativeCalls counts the calls made into the native artifact's service
// table (gpui-perf ticket 01). Every service entry's status passes through
// callStatus exactly once, which increments it; the Win32 calls in win.go
// are not artifact calls and are not counted.
var nativeCalls atomic.Uint64

// CallCount returns how many native artifact entries the process has
// called so far. Frame benchmarks read it before and after a frame to
// report native calls per frame.
func CallCount() uint64 { return nativeCalls.Load() }
