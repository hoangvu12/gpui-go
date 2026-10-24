//go:build windows

package main

import "syscall"

// newProcAttr isolates the child from the console: a new process group
// means Ctrl+C reaches the supervisor alone, which owns the graceful
// shutdown (it asks the app to save its state and exit, then kills only
// that process if it lingered).
func newProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
