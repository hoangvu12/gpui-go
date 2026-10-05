//go:build windows

package gpui

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"gpui-go/internal/native"
)

// This file owns the renderer slice of the runtime (ticket07): the Go
// integration over the native D3D11 clear-frame present/retire service
// (internal/native, reserved slot 2, capability "renderer-d3d11").
//
// The reference is CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
// crates/gpui_windows/src/directx_devices.rs (device creation),
// directx_renderer.rs (swap chains, clear, present, resize),
// directx_renderer.rs gpu_specs + the nvidia/amd/dxgi modules (device
// identity), platform.rs check_device_lost (removal detection). The
// completion protocol (D3D11_QUERY_EVENT + GetData retirement) is a
// gpui-go service-seam addition per docs/renderer-contract.md — the pinned
// renderer has no per-submission retirement protocol.
//
// Ownership rules enforced here (renderer contract):
//
//   - Acceptance (PresentClear returns a Submission) is separate from
//     completion (a poll returning S_OK). Present or Flush is never
//     completion evidence.
//   - Every accepted submission is tracked in the ledger with
//     accepted-at and terminal-state; retirement happens ONLY after a
//     completed poll (or a native-confirmed loss abort).
//   - A background pacing goroutine (never the foreground thread) polls
//     pending submissions with bounded nonblocking polls, retires
//     completed ones, and dispatches the retirement records to the
//     foreground via App.Update.
//   - In-flight submissions are bounded with typed backpressure before
//     native entry (mirroring the native per-surface cap).
//   - Two-window serialization is native (the service's device mutex) plus
//     the Go-side surface table.

// ---------------------------------------------------------------------------
// Typed errors
// ---------------------------------------------------------------------------

// Renderer sentinels, distinguishable with errors.Is.
var (
	// ErrRendererService reports a failure to load/validate the native
	// renderer service (artifact load or capability/table validation).
	ErrRendererService = errors.New("gpui: native renderer service unavailable")
	// ErrRendererWindowClosed reports a present/surface request through a
	// window whose OS lease is gone.
	ErrRendererWindowClosed = errors.New("gpui: renderer window is closed")
	// ErrRendererInFlight reports the bounded in-flight backpressure: the
	// per-surface pending-submission cap is reached; retire before
	// submitting more.
	ErrRendererInFlight = errors.New("gpui: renderer in-flight submission bound reached")
	// ErrRendererRetireNotCompleted reports a Retire call on a submission
	// whose completion was never observed by a poll (acceptance is not
	// completion).
	ErrRendererRetireNotCompleted = errors.New("gpui: submission is still pending; poll until completion before retiring")
)

// ---------------------------------------------------------------------------
// Colors
// ---------------------------------------------------------------------------

// Color is a clear color: one f32 per channel, 0..1. It crosses the ABI
// as IEEE-754 bits (native.RGBA).
type Color struct {
	R float32
	G float32
	B float32
	A float32
}

// toNative converts the color to the native ABI record.
func (c Color) toNative() native.RGBA {
	return native.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
}

// ---------------------------------------------------------------------------
// The ledger
// ---------------------------------------------------------------------------

// LedgerEventKind identifies one ledger trace event.
type LedgerEventKind uint8

const (
	// LedgerAccepted: a submission token was accepted from the native
	// present (possibly with a present error recorded — still tracked).
	LedgerAccepted LedgerEventKind = iota
	// LedgerPollPending: a poll observed S_FALSE (still pending).
	LedgerPollPending
	// LedgerPollCompleted: a poll observed S_OK — the only completion
	// evidence.
	LedgerPollCompleted
	// LedgerPollFailed: a poll observed a query error or device removal.
	LedgerPollFailed
	// LedgerRetired: the exactly-once terminal record was produced
	// (completed retire or confirmed-loss abort).
	LedgerRetired
	// LedgerQuarantined: retirement was refused (failed without
	// confirmed loss); the record stays tracked.
	LedgerQuarantined
	// LedgerDropped: the submission handle was abandoned without
	// retirement (surface destroyed with pending work, renderer closed).
	LedgerDropped
)

func (k LedgerEventKind) String() string {
	switch k {
	case LedgerAccepted:
		return "accepted"
	case LedgerPollPending:
		return "poll-pending"
	case LedgerPollCompleted:
		return "poll-completed"
	case LedgerPollFailed:
		return "poll-failed"
	case LedgerRetired:
		return "retired"
	case LedgerQuarantined:
		return "quarantined"
	case LedgerDropped:
		return "dropped"
	default:
		return "unknown"
	}
}

// LedgerEvent is one timestamped record of the submission ledger trace.
type LedgerEvent struct {
	// Kind is the event kind.
	Kind LedgerEventKind
	// Submission is the ledger id of the submission (sequential from 1).
	Submission uint64
	// Window is the submitting window's identity.
	Window WindowID
	// At is when the event was recorded.
	At time.Time
	// Terminal is the retire record's terminal value for retire events.
	Terminal uint32
	// DeviceRemovedReason is the confirmed removal reason when present.
	DeviceRemovedReason int32
	// Foreground reports whether the event was recorded on the foreground
	// thread (retirement records are dispatched there via App.Update).
	Foreground bool
}

// LedgerEntry is the traced state of one accepted submission.
type LedgerEntry struct {
	// ID is the sequential ledger identity.
	ID uint64
	// Window is the submitting window's identity.
	Window WindowID
	// AcceptedAt is the acceptance timestamp.
	AcceptedAt time.Time
	// CompletedAt is the first completed-poll timestamp.
	CompletedAt time.Time
	// RetiredAt is the terminal-record timestamp.
	RetiredAt time.Time
	// PendingObserved reports at least one S_FALSE observation.
	PendingObserved bool
	// PollCount is the number of polls recorded.
	PollCount int
	// PresentFailed reports the partial-submit path (the Present call
	// failed while the token stayed tracked).
	PresentFailed bool
	// DeviceRemovedReason is the last confirmed removal reason.
	DeviceRemovedReason int32
	// Terminal is the retire record's terminal value.
	Terminal uint32
	// State is "pending", "completed", "failed", "quarantined",
	// "retired" or "dropped".
	State string
}

// ledger records the submission trace. It is mutex-guarded: the acceptor
// (foreground or test goroutine) writes accepted events, the pacing
// worker writes poll events, and retirement records are written from the
// foreground dispatch (App.Update) when pacing owns the retire, or from
// the retiring goroutine for manual retires.
type ledger struct {
	mu      sync.Mutex
	events  []LedgerEvent
	entries map[uint64]*LedgerEntry
	byHand  map[native.SubmissionHandle]uint64
	seq     uint64
	// retired counts terminal retirements (completed + aborted).
	retired int
	// quarantined counts refused retires that stay tracked.
	quarantined int
}

func newLedger() *ledger {
	return &ledger{
		entries: make(map[uint64]*LedgerEntry),
		byHand:  make(map[native.SubmissionHandle]uint64),
	}
}

// accept records a newly accepted submission and returns its id.
func (l *ledger) accept(handle native.SubmissionHandle, window WindowID, presentFailed, foreground bool) uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	entry := &LedgerEntry{
		ID:            l.seq,
		Window:        window,
		AcceptedAt:    time.Now(),
		PresentFailed: presentFailed,
		State:         "pending",
	}
	l.entries[entry.ID] = entry
	l.byHand[handle] = entry.ID
	l.events = append(l.events, LedgerEvent{
		Kind:       LedgerAccepted,
		Submission: entry.ID,
		Window:     window,
		At:         entry.AcceptedAt,
		Foreground: foreground,
	})
	return entry.ID
}

// record observes a poll outcome.
func (l *ledger) record(handle native.SubmissionHandle, state native.SubmissionStateRecord, foreground bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	id, ok := l.byHand[handle]
	if !ok {
		return
	}
	entry := l.entries[id]
	entry.PollCount++
	entry.DeviceRemovedReason = state.DeviceRemovedReason
	var kind LedgerEventKind
	switch state.State {
	case native.SubmissionPending:
		entry.PendingObserved = true
		kind = LedgerPollPending
	case native.SubmissionCompleted:
		// Never completed before a poll: the state only reaches
		// "completed" through this very record.
		entry.State = "completed"
		entry.CompletedAt = time.Now()
		kind = LedgerPollCompleted
	default:
		entry.State = "failed"
		kind = LedgerPollFailed
	}
	l.events = append(l.events, LedgerEvent{
		Kind:                kind,
		Submission:          entry.ID,
		Window:              entry.Window,
		At:                  time.Now(),
		DeviceRemovedReason: state.DeviceRemovedReason,
		Foreground:          foreground,
	})
}

// retireComplete records the terminal retire of a submission.
func (l *ledger) retireComplete(handle native.SubmissionHandle, record native.RetireRecord, foreground bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	id, ok := l.byHand[handle]
	if !ok {
		return
	}
	entry := l.entries[id]
	if entry.State == "retired" {
		return
	}
	entry.State = "retired"
	entry.RetiredAt = time.Now()
	entry.Terminal = record.Terminal
	entry.DeviceRemovedReason = record.DeviceRemovedReason
	l.retired++
	delete(l.byHand, handle)
	l.events = append(l.events, LedgerEvent{
		Kind:                LedgerRetired,
		Submission:          entry.ID,
		Window:              entry.Window,
		At:                  entry.RetiredAt,
		Terminal:            record.Terminal,
		DeviceRemovedReason: record.DeviceRemovedReason,
		Foreground:          foreground,
	})
}

// retireRefused records a quarantine refusal (retirement not allowed).
func (l *ledger) retireRefused(handle native.SubmissionHandle, foreground bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	id, ok := l.byHand[handle]
	if !ok {
		return
	}
	entry := l.entries[id]
	entry.State = "quarantined"
	l.quarantined++
	l.events = append(l.events, LedgerEvent{
		Kind:       LedgerQuarantined,
		Submission: entry.ID,
		Window:     entry.Window,
		At:         time.Now(),
		Foreground: foreground,
	})
}

// drop records an abandoned submission (no retirement).
func (l *ledger) drop(handle native.SubmissionHandle) {
	l.mu.Lock()
	defer l.mu.Unlock()
	id, ok := l.byHand[handle]
	if !ok {
		return
	}
	entry := l.entries[id]
	entry.State = "dropped"
	delete(l.byHand, handle)
	l.events = append(l.events, LedgerEvent{
		Kind:       LedgerDropped,
		Submission: entry.ID,
		Window:     entry.Window,
		At:         time.Now(),
	})
}

// Snapshot returns a copy of the ledger events plus the invariant
// counters: accepted, retired, quarantined, in-flight (tracked handles
// that are neither retired nor quarantined nor dropped).
func (l *ledger) Snapshot() (events []LedgerEvent, accepted, retired, quarantined, inFlight int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	events = append([]LedgerEvent(nil), l.events...)
	accepted = int(l.seq)
	retired = l.retired
	quarantined = l.quarantined
	for _, entry := range l.entries {
		if entry.State != "dropped" && entry.State != "quarantined" && entry.State != "retired" {
			inFlight++
		}
	}
	return
}

// Entry returns a copy of one submission's ledger entry.
func (l *ledger) Entry(id uint64) (LedgerEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[id]
	if !ok {
		return LedgerEntry{}, false
	}
	return *entry, true
}

// pending returns the tracked submission handles (not yet retired).
func (l *ledger) pending() []native.SubmissionHandle {
	l.mu.Lock()
	defer l.mu.Unlock()
	handles := make([]native.SubmissionHandle, 0, len(l.byHand))
	for handle := range l.byHand {
		handles = append(handles, handle)
	}
	return handles
}

// ---------------------------------------------------------------------------
// The native service (process-global)
// ---------------------------------------------------------------------------

var (
	rendererServiceOnce sync.Once
	rendererServiceVal  *native.RendererService
	rendererServiceErr  error
)

// rendererService loads the native renderer service once per process
// (the same artifact that carries the bootstrap and layout services; the
// renderer capability bit must be present).
func rendererService() (*native.RendererService, error) {
	rendererServiceOnce.Do(func() {
		lib, err := native.Load(native.Options{})
		if err != nil {
			rendererServiceErr = fmt.Errorf("gpui: loading the native artifact: %w", err)
			return
		}
		svc, err := lib.Renderer()
		if err != nil {
			rendererServiceErr = fmt.Errorf("%w: %v", ErrRendererService, err)
			return
		}
		rendererServiceVal = svc
	})
	return rendererServiceVal, rendererServiceErr
}

// ---------------------------------------------------------------------------
// Renderer
// ---------------------------------------------------------------------------

// rendererPollInterval is the pacing worker's poll cadence. Polls are
// bounded and nonblocking (one GetData per pending submission per round);
// the interval keeps the worker off the foreground thread and out of a
// busy spin. The pinned VSyncProvider paces presents with the DWM
// composition interval (vsync.rs); this worker paces *completion polls*,
// a gpui-go addition at the service seam, so the cadence is a short
// bounded interval instead of the vblank interval.
const rendererPollInterval = 2 * time.Millisecond

// Renderer owns the window surfaces of one application and the submission
// ledger proving acceptance-vs-retirement. It is safe for concurrent use;
// the native service serializes immediate-context work behind its device
// mutex, and the Go-side surface table serializes per-window access.
//
// The default Renderer starts a background pacing goroutine when the
// first surface attaches; NewRendererManual creates one without it for
// deterministic test flows (the same real service, only the automatic
// poller is absent).
type Renderer struct {
	svc    *native.RendererService
	ledger *ledger

	// app is set once (first surface attach) and read from the pacing
	// worker, hence the atomic pointer.
	app atomic.Pointer[App]

	mu       sync.Mutex
	surfaces map[WindowID]*Surface
	// inFlightWindow maps a tracked submission handle to its window.
	inFlightWindow map[native.SubmissionHandle]WindowID
	// inFlightCount is the per-window tracked-submission count (the
	// Go-side mirror of the native per-surface bound).
	inFlightCount map[WindowID]int

	pacing   bool
	paceStop chan struct{}
	paceDone chan struct{}
	closed   bool
}

// NewRenderer creates a renderer backed by the embedded native artifact
// (loaded and verified once per process). The pacing worker starts when
// the first surface attaches.
func NewRenderer() (*Renderer, error) {
	svc, err := rendererService()
	if err != nil {
		return nil, err
	}
	return &Renderer{
		svc:            svc,
		ledger:         newLedger(),
		surfaces:       make(map[WindowID]*Surface),
		inFlightWindow: make(map[native.SubmissionHandle]WindowID),
		inFlightCount:  make(map[WindowID]int),
		pacing:         true,
	}, nil
}

// NewRendererManual creates a renderer without the background pacing
// worker: the caller polls and retires manually (deterministic tests).
// The native service and the ledger are the same real ones.
func NewRendererManual() (*Renderer, error) {
	svc, err := rendererService()
	if err != nil {
		return nil, err
	}
	return &Renderer{
		svc:            svc,
		ledger:         newLedger(),
		surfaces:       make(map[WindowID]*Surface),
		inFlightWindow: make(map[native.SubmissionHandle]WindowID),
		inFlightCount:  make(map[WindowID]int),
	}, nil
}

// DeviceInfo returns the environment identity of the shared device:
// adapter description, feature level and driver identity.
func (r *Renderer) DeviceInfo() (*native.DeviceInfo, error) {
	return r.svc.DeviceInfo()
}

// MaxPendingSubmissions is the native per-surface in-flight bound that
// PresentClear enforces with typed backpressure before native entry.
func (r *Renderer) MaxPendingSubmissions() int {
	return r.svc.MaxPendingSubmissions()
}

// Ledger returns a snapshot of the submission ledger trace.
func (r *Renderer) Ledger() (events []LedgerEvent, accepted, retired, quarantined, inFlight int) {
	return r.ledger.Snapshot()
}

// LedgerEntry returns the traced state of one submission.
func (r *Renderer) LedgerEntry(id uint64) (LedgerEntry, bool) {
	return r.ledger.Entry(id)
}

// deviceExtent computes the window's current device-pixel client extent
// (logical bounds times the scale factor, rounded to the device grid).
func deviceExtent(w *Window) (width, height uint32, scale float32, err error) {
	bounds, err := w.Bounds()
	if err != nil {
		return 0, 0, 0, err
	}
	scale, err = w.ScaleFactor()
	if err != nil {
		return 0, 0, 0, err
	}
	return uint32(roundToDevicePixel(bounds.Size.Width, scale)),
		uint32(roundToDevicePixel(bounds.Size.Height, scale)), scale, nil
}

// GetOrCreateSurface attaches a DComp-premultiplied surface to the
// window's HWND, creating it at the window's current device-pixel extent.
func (r *Renderer) GetOrCreateSurface(w *Window) (*Surface, error) {
	return r.GetOrCreateSurfaceMode(w, native.SurfaceModeDCompPremultiplied)
}

// GetOrCreateSurfaceMode attaches a surface in the requested composition
// mode (DComp premultiplied or HWND alpha-ignore) to the window's HWND.
// The window's HWND is leased from the host: the renderer never destroys
// it (Go owns DestroyWindow).
func (r *Renderer) GetOrCreateSurfaceMode(w *Window, mode native.SurfaceMode) (*Surface, error) {
	if w == nil {
		return nil, errors.New("gpui: GetOrCreateSurface requires a live window")
	}
	if !w.Alive() {
		return nil, fmt.Errorf("gpui: GetOrCreateSurface: %w", ErrRendererWindowClosed)
	}
	width, height, scale, err := deviceExtent(w)
	if err != nil {
		return nil, fmt.Errorf("gpui: GetOrCreateSurface: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("gpui: renderer is closed")
	}
	if surface, ok := r.surfaces[w.ID()]; ok {
		// A surface exists; sync its extent with the window's current
		// size (the resize path runs at this safe point).
		if err := r.resizeSurfaceLocked(surface, width, height, scale); err != nil {
			return nil, err
		}
		return surface, nil
	}
	if r.app.Load() == nil {
		r.app.Store(w.app)
	}
	if r.pacing && r.paceStop == nil {
		r.startPacingLocked()
	}
	hwnd := w.Handle().Hwnd()
	handle, err := r.svc.CreateSurface(hwnd, mode, width, height, scale)
	if err != nil {
		return nil, fmt.Errorf("gpui: creating a native surface for window %d: %w", w.ID(), err)
	}
	surface := &Surface{
		renderer: r,
		windowID: w.ID(),
		window:   w,
		handle:   handle,
		mode:     mode,
		hwnd:     hwnd,
		width:    width,
		height:   height,
		scale:    scale,
	}
	r.surfaces[w.ID()] = surface
	return surface, nil
}

// resizeSurfaceLocked resizes a surface when the window extent changed
// (the pinned resize path: unbind, ResizeBuffers, recreate, rebind). It
// is the "resize at a safe point" of the renderer contract — called from
// the surface lookup or the present boundary, with the renderer lock
// held.
func (r *Renderer) resizeSurfaceLocked(surface *Surface, width, height uint32, scale float32) error {
	if surface.width == width && surface.height == height {
		return nil
	}
	if err := r.svc.Resize(surface.handle, width, height); err != nil {
		return fmt.Errorf("gpui: resizing the surface of window %d: %w", surface.windowID, err)
	}
	surface.width = width
	surface.height = height
	surface.scale = scale
	return nil
}

// PresentClear submits one clear+present frame for the window (creating
// the surface on demand in DComp mode). It returns the tracked submission
// token: acceptance is not completion — poll the submission (or let the
// pacing worker) until it completes, then retire.
func (r *Renderer) PresentClear(w *Window, color Color) (*Submission, error) {
	surface, err := r.GetOrCreateSurface(w)
	if err != nil {
		return nil, err
	}
	return surface.PresentClear(color)
}

// Surface is one window's native swap chain. All methods marshal through
// the renderer's native service; the native device mutex serializes
// immediate-context work across surfaces.
type Surface struct {
	renderer *Renderer
	windowID WindowID
	window   *Window
	handle   native.SurfaceHandle
	mode     native.SurfaceMode
	hwnd     uintptr
	width    uint32
	height   uint32
	scale    float32
}

// Info returns the surface's facts from the native service.
func (s *Surface) Info() (native.SurfaceInfo, error) {
	return s.renderer.svc.Info(s.handle)
}

// Mode returns the composition mode the surface was created with.
func (s *Surface) Mode() native.SurfaceMode { return s.mode }

// Size returns the surface's current device-pixel extent.
func (s *Surface) Size() (width, height uint32) { return s.width, s.height }

// Hwnd returns the leased HWND the surface is attached to.
func (s *Surface) Hwnd() uintptr { return s.hwnd }

// NativeHandle returns the surface's native renderer handle (test and
// diagnostics seam for the native draw entries; ticket16).
func (s *Surface) NativeHandle() native.SurfaceHandle { return s.handle }

// WindowID returns the owning window identity.
func (s *Surface) WindowID() WindowID { return s.windowID }

// PresentClear submits one clear+present frame. Bounded in-flight
// backpressure applies (the native per-surface cap, mirrored Go-side
// before native entry). A present failure still returns the tracked
// submission token together with the error (the partial-submit rule).
func (s *Surface) PresentClear(color Color) (*Submission, error) {
	r := s.renderer
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("gpui: renderer is closed")
	}
	// Typed backpressure before native entry: mirror the native
	// per-surface bound with the Go-side in-flight table.
	if bound := r.svc.MaxPendingSubmissions(); r.inFlightCount[s.windowID] >= bound {
		return nil, fmt.Errorf("gpui: PresentClear window %d: %w", s.windowID, ErrRendererInFlight)
	}
	// Sync the surface extent with the window at this safe point (the
	// stale-extent rule: submit only at the current extent).
	if s.window != nil && s.window.Alive() {
		if width, height, scale, err := deviceExtent(s.window); err == nil {
			if err := r.resizeSurfaceLocked(s, width, height, scale); err != nil {
				return nil, err
			}
		}
	}
	handle, err := r.svc.PresentClear(s.handle, color.toNative())
	if err != nil && !errors.Is(err, native.ErrRendererPresent) {
		// Rejected before acceptance (or a hard failure): no token.
		return nil, fmt.Errorf("gpui: PresentClear window %d: %w", s.windowID, err)
	}
	presentFailed := err != nil
	id := r.ledger.accept(handle, s.windowID, presentFailed, r.isForeground())
	r.inFlightWindow[handle] = s.windowID
	r.inFlightCount[s.windowID]++
	sub := &Submission{renderer: r, id: id, handle: handle, partialErr: err}
	return sub, err
}

// Destroy frees the surface. It is refused while un-retired submissions
// remain (drain them first) and makes the surface handle stale.
func (s *Surface) Destroy() error {
	r := s.renderer
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("gpui: renderer is closed")
	}
	if err := r.svc.DestroySurface(s.handle); err != nil {
		return fmt.Errorf("gpui: destroying the surface of window %d: %w", s.windowID, err)
	}
	delete(r.surfaces, s.windowID)
	return nil
}

// isForeground reports whether the current goroutine is the attached
// host's foreground thread (ledger trace evidence).
func (r *Renderer) isForeground() bool {
	app := r.app.Load()
	if app == nil {
		return false
	}
	host := app.Host()
	if host == nil {
		return false
	}
	return host.IsForegroundThread()
}

// ---------------------------------------------------------------------------
// Submission
// ---------------------------------------------------------------------------

// Submission is one accepted clear+present frame: the native token plus
// its ledger identity. Acceptance is not completion.
type Submission struct {
	renderer   *Renderer
	id         uint64
	handle     native.SubmissionHandle
	partialErr error
}

// ID returns the ledger id of the submission (sequential from 1).
func (sub *Submission) ID() uint64 { return sub.id }

// Handle returns the native submission handle.
func (sub *Submission) Handle() native.SubmissionHandle { return sub.handle }

// PartialErr returns the present error of a partial submit (a tracked
// submission whose Present call failed), or nil.
func (sub *Submission) PartialErr() error { return sub.partialErr }

// Poll performs one bounded, nonblocking poll and records it in the
// ledger. Pending (S_FALSE) observations are recorded as such; a
// completed poll is the only completion evidence.
func (sub *Submission) Poll() (native.SubmissionStateRecord, error) {
	r := sub.renderer
	state, err := r.svc.Poll(sub.handle)
	if err != nil {
		return state, err
	}
	r.ledger.record(sub.handle, state, r.isForeground())
	return state, nil
}

// Retire produces the exactly-once terminal record. The Go-side guard
// rejects retirement while the submission is still pending (no completed
// poll); retirement of a failed submission is left to the native
// confirmed-loss check; a second retire surfaces the native
// stale-handle error.
func (sub *Submission) Retire() (native.RetireRecord, error) {
	r := sub.renderer
	// Go-side guard: acceptance is not completion.
	if entry, ok := r.ledger.Entry(sub.id); ok && entry.State == "pending" {
		return native.RetireRecord{}, fmt.Errorf("gpui: Retire submission %d: %w", sub.id, ErrRendererRetireNotCompleted)
	}
	record, err := r.svc.Retire(sub.handle)
	if err != nil {
		if errors.Is(err, native.ErrRendererQuarantine) {
			r.ledger.retireRefused(sub.handle, r.isForeground())
		}
		return record, err
	}
	sub.recordRetire(record)
	return record, nil
}

// recordRetire records the terminal state: through App.Update when this
// is a pacing renderer retiring off the foreground thread (the pacing
// worker and background callers dispatch the record to the foreground),
// or directly otherwise.
func (sub *Submission) recordRetire(record native.RetireRecord) {
	r := sub.renderer
	if r.pacing && !r.isForeground() {
		app := r.app.Load()
		if app != nil {
			handle := sub.handle
			terminal := record.Terminal
			reason := record.DeviceRemovedReason
			app.Spawn(func(cx *AsyncApp) struct{} {
				cx.Update(func(*App) {
					r.ledger.retireComplete(handle, native.RetireRecord{
						Terminal:            terminal,
						DeviceRemovedReason: reason,
					}, true)
				})
				return struct{}{}
			})
			r.finishInFlight(sub.handle)
			return
		}
	}
	r.ledger.retireComplete(sub.handle, record, r.isForeground())
	r.finishInFlight(sub.handle)
}

// finishInFlight removes a retired handle from the in-flight tables.
func (r *Renderer) finishInFlight(handle native.SubmissionHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if window, ok := r.inFlightWindow[handle]; ok {
		delete(r.inFlightWindow, handle)
		if r.inFlightCount[window] > 0 {
			r.inFlightCount[window]--
		}
	}
}

// WaitForCompletion polls the submission until it completes, fails or the
// timeout expires, sleeping between polls (bounded, nonblocking polls).
// It must not run on the foreground thread — it is a test/background
// convenience, not a foreground busy wait.
func (sub *Submission) WaitForCompletion(timeout time.Duration) (native.SubmissionStateRecord, error) {
	deadline := time.Now().Add(timeout)
	var last native.SubmissionStateRecord
	for {
		state, err := sub.Poll()
		if err != nil {
			return state, err
		}
		last = state
		if state.State != native.SubmissionPending {
			return state, nil
		}
		if time.Now().After(deadline) {
			return last, fmt.Errorf("gpui: submission %d did not complete within %v", sub.id, timeout)
		}
		time.Sleep(rendererPollInterval)
	}
}

// WaitForRetirement waits until the submission's ledger entry reaches a
// terminal state (retired, quarantined or dropped).
func (sub *Submission) WaitForRetirement(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if entry, ok := sub.renderer.ledger.Entry(sub.id); ok {
			if entry.State == "retired" || entry.State == "quarantined" || entry.State == "dropped" {
				return nil
			}
		}
		time.Sleep(time.Millisecond)
	}
	return fmt.Errorf("gpui: submission %d was not retired within %v", sub.id, timeout)
}

// ---------------------------------------------------------------------------
// The pacing worker
// ---------------------------------------------------------------------------

// startPacingLocked launches the background pacing goroutine (the
// renderer mutex must be held). It never touches app state directly;
// retirement records are dispatched to the foreground via App.Update
// (App.Spawn + cx.Update).
func (r *Renderer) startPacingLocked() {
	r.paceStop = make(chan struct{})
	r.paceDone = make(chan struct{})
	stop := r.paceStop
	go r.paceLoop(stop)
}

// paceLoop is the single pacing worker: round-robin over the pending
// submissions with one bounded, nonblocking poll each, retiring
// completed ones and dispatching the terminal records to the foreground.
// It stops on Close. The stop channel is captured once by the caller:
// Close may nil the r.paceStop field for restart bookkeeping while this
// loop is running, and a nil-channel select here would lose the stop
// signal and hang Close on <-r.paceDone.
func (r *Renderer) paceLoop(stop chan struct{}) {
	defer close(r.paceDone)
	ticker := time.NewTicker(rendererPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		r.paceOnce(stop)
	}
}

// paceOnce performs one polling round.
func (r *Renderer) paceOnce(stop chan struct{}) {
	for _, handle := range r.ledger.pending() {
		select {
		case <-stop:
			return
		default:
		}
		state, err := r.svc.Poll(handle)
		if err != nil {
			// A stale handle was retired natively: either a manual retire
			// already recorded it synchronously, or this worker's own
			// foreground dispatch is still in flight and will record it.
			// Neither case may drop the entry — dropping is only for
			// abandonment at Close. Leave the entry alone and move on.
			continue
		}
		r.ledger.record(handle, state, false)
		switch state.State {
		case native.SubmissionCompleted:
			// The only completion evidence: retire now.
			record, err := r.svc.Retire(handle)
			if err != nil {
				if errors.Is(err, native.ErrRendererQuarantine) {
					r.ledger.retireRefused(handle, false)
				}
				continue
			}
			// Dispatch the terminal record to the foreground via
			// App.Update: the pacing worker never runs app code inline.
			r.dispatchRetire(handle, record)
		case native.SubmissionFailed:
			// Failed (device removal or query error): retirement is only
			// allowed on confirmed loss; let the native side decide
			// (quarantine keeps the record tracked).
			record, err := r.svc.Retire(handle)
			if err == nil {
				r.dispatchRetire(handle, record)
			} else if errors.Is(err, native.ErrRendererQuarantine) {
				r.ledger.retireRefused(handle, false)
			}
		}
	}
}

// dispatchRetire hands the terminal record to the foreground (App.Spawn +
// cx.Update) and removes the handle from the in-flight table.
func (r *Renderer) dispatchRetire(handle native.SubmissionHandle, record native.RetireRecord) {
	app := r.app.Load()
	if app == nil {
		r.ledger.retireComplete(handle, record, false)
	} else {
		terminal := record.Terminal
		reason := record.DeviceRemovedReason
		app.Spawn(func(cx *AsyncApp) struct{} {
			cx.Update(func(*App) {
				r.ledger.retireComplete(handle, native.RetireRecord{
					Terminal:            terminal,
					DeviceRemovedReason: reason,
				}, true)
			})
			return struct{}{}
		})
	}
	r.finishInFlight(handle)
}

// Close stops the pacing worker and destroys every surface. Surfaces
// with un-retired submissions refuse to be destroyed (drain them first);
// Close reports that typed error. The native library itself stays loaded
// for the process lifetime (distribution contract: no hot unload).
func (r *Renderer) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return errors.New("gpui: renderer already closed")
	}
	r.closed = true
	stop := r.paceStop
	r.paceStop = nil
	r.mu.Unlock()
	if stop != nil {
		close(stop)
		<-r.paceDone
	}
	var firstErr error
	r.mu.Lock()
	surfaces := make([]*Surface, 0, len(r.surfaces))
	for _, surface := range r.surfaces {
		surfaces = append(surfaces, surface)
	}
	r.mu.Unlock()
	for _, surface := range surfaces {
		if err := r.svc.DestroySurface(surface.handle); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("gpui: destroying the surface of window %d: %w", surface.windowID, err)
		}
	}
	// Any still-tracked submissions are dropped trace events (the ledger
	// must stay honest about abandonment).
	for _, handle := range r.ledger.pending() {
		r.ledger.drop(handle)
	}
	return firstErr
}

// InFlight returns the Go-side in-flight submission count (accepted,
// not yet retired).
func (r *Renderer) InFlight() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.inFlightWindow)
}

// BackgroundAppearance selects the draw path's clear color (the pin's
// WindowBackgroundAppearance reduced to the opaque/transparent
// distinction; the native renderer service's tags).
type BackgroundAppearance = native.BackgroundAppearance

// Background appearance constants (native.BackgroundOpaque /
// native.BackgroundTransparent).
const (
	// BackgroundOpaque clears with [1,1,1,1] (the pin's opaque
	// appearance).
	BackgroundOpaque = native.BackgroundOpaque
	// BackgroundTransparent clears with [0,0,0,0] (the pin's
	// alpha-blended appearance).
	BackgroundTransparent = native.BackgroundTransparent
)

// DrawScene renders the finished scene into the window's back buffer,
// presents it and returns the tracked submission (ticket16: the pin's
// DirectXRenderer::draw = render + present, with the ledger's
// acceptance-vs-completion protocol). The scene handle must reference
// a finished scene (kernel-assigned draw orders, compiled plan); the
// atlas handle must own the scene's sprite textures (nil when the
// scene has no sprite batches).
func (s *Surface) DrawScene(scene *Scene, atlas *Atlas, appearance BackgroundAppearance) (*Submission, error) {
	if scene == nil {
		return nil, fmt.Errorf("gpui: DrawScene: nil scene")
	}
	var atlasHandle native.AtlasHandle
	if atlas != nil {
		atlasHandle = atlas.handle
	}
	r := s.renderer
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("gpui: renderer is closed")
	}
	// Sync the surface extent with the window at this safe point (the
	// stale-extent rule).
	if s.window != nil && s.window.Alive() {
		if width, height, scale, err := deviceExtent(s.window); err == nil {
			if err := r.resizeSurfaceLocked(s, width, height, scale); err != nil {
				return nil, err
			}
		}
	}
	if err := scene.FinishIfNeeded(); err != nil {
		return nil, fmt.Errorf("gpui: DrawScene: %w", err)
	}
	handle, err := r.svc.DrawScene(s.handle, native.SceneHandle(scene.Handle()), native.BackgroundAppearance(appearance), atlasHandle)
	if err != nil && !errors.Is(err, native.ErrRendererPresent) {
		return nil, fmt.Errorf("gpui: DrawScene window %d: %w", s.windowID, err)
	}
	presentFailed := err != nil
	id := r.ledger.accept(handle, s.windowID, presentFailed, r.isForeground())
	r.inFlightWindow[handle] = s.windowID
	r.inFlightCount[s.windowID]++
	sub := &Submission{renderer: r, id: id, handle: handle, partialErr: err}
	return sub, err
}

// RenderScene renders the finished scene without presenting (the
// pin's render body; pair with ReadPixels for verification, then
// DrawScene to present).
func (s *Surface) RenderScene(scene *Scene, atlas *Atlas, appearance BackgroundAppearance) error {
	if scene == nil {
		return fmt.Errorf("gpui: RenderScene: nil scene")
	}
	var atlasHandle native.AtlasHandle
	if atlas != nil {
		atlasHandle = atlas.handle
	}
	r := s.renderer
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("gpui: renderer is closed")
	}
	if s.window != nil && s.window.Alive() {
		if width, height, scale, err := deviceExtent(s.window); err == nil {
			if err := r.resizeSurfaceLocked(s, width, height, scale); err != nil {
				return err
			}
		}
	}
	if err := scene.FinishIfNeeded(); err != nil {
		return fmt.Errorf("gpui: RenderScene: %w", err)
	}
	if err := r.svc.RenderScene(s.handle, native.SceneHandle(scene.Handle()), native.BackgroundAppearance(appearance), atlasHandle); err != nil {
		return fmt.Errorf("gpui: RenderScene window %d: %w", s.windowID, err)
	}
	return nil
}

// ReadPixels reads the surface's current back buffer through a staging
// texture (the pin's render_to_image tail): BGRA->RGBA-swapped RGBA
// bytes. Call it after RenderScene (before presenting) for a
// deterministic read of the rendered frame.
func (s *Surface) ReadPixels() ([]byte, error) {
	r := s.renderer
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("gpui: renderer is closed")
	}
	pixels, err := r.svc.ReadPixels(s.handle, nil)
	if err != nil {
		return nil, fmt.Errorf("gpui: ReadPixels window %d: %w", s.windowID, err)
	}
	return pixels, nil
}
