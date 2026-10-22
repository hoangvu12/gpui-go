//go:build !windows

package base

import "github.com/hoangvu12/gpui-go/gpui"

// platformReadReduceMotion reports the unknown preference where the port
// reads no platform setting (U:reduce_motion.rs:174-183; the macOS and
// Linux readers are out of scope for the Windows port).
func platformReadReduceMotion() (reduce bool, ok bool) { return false, false }

// platformFollowReduceMotion follows nothing (U:reduce_motion.rs:182).
func platformFollowReduceMotion(*gpui.App) {}
