package portfixture

// Compile-level pins of the verified public API shape
// (.scratch/signature-validation/api.go): each fixture signature is
// expressed as a function type and assigned the real runtime function.
// If any signature drifts from the compile-verified shape, this file
// stops compiling.

import (
	"gpui-go/conformance"
	"gpui-go/gpui"
)

var (
	_ func(gpui.AppContext, *gpui.Scope, func(*int, *gpui.Context[int])) gpui.Entity[int]                                        = gpui.NewEntity[int]
	_ func(gpui.Entity[int], *gpui.Scope) gpui.Entity[int]                                                                       = gpui.Entity[int].RetainInto
	_ func(gpui.Entity[int], *gpui.Scope)                                                                                        = gpui.Entity[int].TransferInto
	_ func(gpui.Entity[int])                                                                                                     = gpui.Entity[int].Release
	_ func(gpui.Entity[int]) gpui.WeakEntity[int]                                                                                = gpui.Entity[int].Downgrade
	_ func(gpui.WeakEntity[int], *gpui.Scope) (gpui.Entity[int], bool)                                                           = gpui.WeakEntity[int].UpgradeInto
	_ func(gpui.Entity[int], gpui.AppContext, func(*int, *gpui.App) bool) bool                                                   = gpui.Entity[int].Read[bool]
	_ func(gpui.Entity[int], gpui.AppContext, func(*int, *gpui.Context[int]))                                                    = gpui.Entity[int].Update
	_ func(gpui.Entity[int], gpui.AppContext, func(*int, *gpui.Context[int]) bool) bool                                          = gpui.Entity[int].UpdateWith[bool]
	_ func(gpui.Entity[int], gpui.AppContext, *gpui.Window, func(*int, *gpui.Window, *gpui.Context[int])) error                  = gpui.Entity[int].UpdateIn
	_ func(gpui.Entity[int], gpui.AppContext, *gpui.Window, func(*int, *gpui.Window, *gpui.Context[int]) bool) (bool, error)     = gpui.Entity[int].UpdateInWith[bool]
	_ func(gpui.WeakEntity[int], gpui.AppContext, func(*int, *gpui.App) bool) (bool, error)                                      = gpui.WeakEntity[int].TryRead[bool]
	_ func(gpui.WeakEntity[int], gpui.AppContext, func(*int, *gpui.Context[int])) error                                          = gpui.WeakEntity[int].TryUpdate
	_ func(gpui.WeakEntity[int], gpui.AppContext, func(*int, *gpui.Context[int]) bool) (bool, error)                             = gpui.WeakEntity[int].TryUpdateWith[bool]
	_ func(gpui.WeakEntity[int], gpui.AppContext, *gpui.Window, func(*int, *gpui.Window, *gpui.Context[int])) error              = gpui.WeakEntity[int].TryUpdateIn
	_ func(gpui.WeakEntity[int], gpui.AppContext, *gpui.Window, func(*int, *gpui.Window, *gpui.Context[int]) bool) (bool, error) = gpui.WeakEntity[int].TryUpdateInWith[bool]

	_ func(gpui.Event[int, uint32], *gpui.Context[int], uint32)                                                                                           = gpui.Event[int, uint32].Emit
	_ func(gpui.Event[int, uint32], gpui.AppContext, gpui.Entity[int], uint32)                                                                            = gpui.Event[int, uint32].EmitOn
	_ func(gpui.Event[int, uint32], *gpui.Context[int], func(*gpui.Scope) uint32)                                                                         = gpui.Event[int, uint32].EmitOwned
	_ func(gpui.Event[int, uint32], *gpui.Context[bool], gpui.Entity[int], func(*bool, gpui.Entity[int], *uint32, *gpui.Context[bool])) gpui.Subscription = gpui.Event[int, uint32].Subscribe[bool]
	_ func(gpui.Event[int, uint32], *gpui.Context[int], func(*int, *uint32, *gpui.Context[int])) gpui.Subscription                                        = gpui.Event[int, uint32].SubscribeSelf
	_ func() gpui.Event[int, uint32]                                                                                                                      = gpui.DefineEvent[int, uint32]

	_ func(gpui.Subscription)              = gpui.Subscription.Cancel
	_ func(gpui.Subscription, *gpui.Scope) = gpui.Subscription.TransferInto
	_ func(gpui.Subscription)              = gpui.Subscription.Detach

	_ func(*gpui.Context[int]) gpui.Entity[int]                                                                              = (*gpui.Context[int]).Entity
	_ func(*gpui.Context[int]) *gpui.Scope                                                                                   = (*gpui.Context[int]).Scope
	_ func(*gpui.Context[int])                                                                                               = (*gpui.Context[int]).Notify
	_ func(*gpui.Context[int], gpui.Entity[int], func(*int, gpui.Entity[int], *gpui.Context[int])) gpui.Subscription         = (*gpui.Context[int]).Observe[int]
	_ func(*gpui.Context[int], func(*int, *uint32, *gpui.Window, *gpui.Context[int])) func(*uint32, *gpui.Window, *gpui.App) = (*gpui.Context[int]).Listener[uint32]

	_ func(*gpui.Scope) *gpui.Scope = (*gpui.Scope).Child
	_ func(*gpui.Scope)             = (*gpui.Scope).Close

	// The sealed AppContext is satisfied by *App and *Context[S] and by
	// nothing outside the gpui package.
	_ gpui.AppContext = (*gpui.App)(nil)
	_ gpui.AppContext = (*gpui.Context[int])(nil)

	// The port fixture entry point keeps its specified shape.
	_ func(*conformance.Envelope) (*conformance.Trace, []string, error) = Run
)
