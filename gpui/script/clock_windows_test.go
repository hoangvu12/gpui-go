//go:build windows

package script_test

// time.Now is quantized to about 1 ms on this PC, which cannot see a
// 0.001 ms bound update; the timing tests read QueryPerformanceCounter
// instead (no cgo), as the probe did.

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	qpcDLL  = syscall.NewLazyDLL("kernel32.dll")
	qpcProc = qpcDLL.NewProc("QueryPerformanceCounter")
	qpfProc = qpcDLL.NewProc("QueryPerformanceFrequency")
	qpcFreq int64
)

func init() {
	qpfProc.Call(uintptr(unsafe.Pointer(&qpcFreq)))
}

// qpcNow reads the performance counter.
func qpcNow() int64 {
	var c int64
	qpcProc.Call(uintptr(unsafe.Pointer(&c)))
	return c
}

// qpcSince returns the time elapsed since start.
func qpcSince(start int64) time.Duration {
	d := qpcNow() - start
	return time.Duration(d/qpcFreq*int64(time.Second) + d%qpcFreq*int64(time.Second)/qpcFreq)
}

// msOf renders a duration in milliseconds.
func msOf(d time.Duration) float64 {
	return float64(d.Nanoseconds()) / 1e6
}
