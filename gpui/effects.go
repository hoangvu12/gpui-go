package gpui

import "reflect"

// effectKind enumerates the queued effect types. They mirror the
// reference's Effect enum: Notify, Emit, Defer, RefreshWindows,
// NotifyGlobalObservers and EntityCreated.
type effectKind uint8

const (
	// effNotify delivers observer notifications for one entity,
	// coalesced while pending and re-notifiable from callbacks.
	effNotify effectKind = iota + 1
	// effEmit delivers one event occurrence to the matching listeners of
	// its source entity.
	effEmit
	// effDefer runs a callback at the end of the current effect cycle.
	effDefer
	// effRefresh marks windows for redraw.
	effRefresh
	// effNotifyGlobal delivers global observer notifications for one
	// declared type, coalesced while pending.
	effNotifyGlobal
	// effEntityCreated delivers new-entity observers for one entity
	// type.
	effEntityCreated
)

// effect is one queued effect. The queue is FIFO: an already queued
// effect precedes a newly deferred callback; each Emit occurrence is
// queued separately.
type effect struct {
	kind effectKind
	// emitter is the source entity identity (Notify, Emit, EntityCreated).
	emitter EntityID
	// payloadType is the declared payload Go type (Emit), used with the
	// emitter identity for event routing; it also carries the global type
	// for NotifyGlobal and the entity type for EntityCreated via the
	// dedicated fields below.
	payloadType reflect.Type
	// payload is the payloadBox[P] storage for Emit.
	payload any
	// callback is the deferred callback for Defer.
	callback func(*App)
	// globalType is the declared global type for NotifyGlobal.
	globalType reflect.Type
	// entityType is the entity state type for EntityCreated.
	entityType reflect.Type
}
