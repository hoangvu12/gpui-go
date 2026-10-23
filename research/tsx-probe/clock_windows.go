package tsxprobe

import (
	"syscall"
	"time"
	"unsafe"
)

// time.Now is quantized to about 1 ms on this PC; the probe times with
// QueryPerformanceCounter instead (no cgo needed).
var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	qpcProc  = kernel32.NewProc("QueryPerformanceCounter")
	qpfProc  = kernel32.NewProc("QueryPerformanceFrequency")
	qpcFreq  int64
)

func init() { qpfProc.Call(uintptr(unsafe.Pointer(&qpcFreq))) }

// Clock is a monotonic instant from QueryPerformanceCounter.
type Clock int64

// Now reads the performance counter.
func Now() Clock {
	var c int64
	qpcProc.Call(uintptr(unsafe.Pointer(&c)))
	return Clock(c)
}

// Since is the time elapsed from c.
func Since(c Clock) time.Duration {
	d := int64(Now() - c)
	return time.Duration(d/qpcFreq*1e9 + d%qpcFreq*1e9/qpcFreq)
}
