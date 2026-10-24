//go:build !windows

package main

import "syscall"

// newProcAttr is the non-Windows no-op (the module's apps are
// Windows-only, but the tool stays portable; signal handling covers
// the graceful stop here).
func newProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}
