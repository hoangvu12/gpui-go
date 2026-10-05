package gpui

// ScopeState is the close state of a scope.
type ScopeState uint8

const (
	// ScopeOpen accepts new ownership and result delivery.
	ScopeOpen ScopeState = iota + 1
	// ScopeClosing is marked unavailable to new ownership and result
	// delivery; work is being cancelled.
	ScopeClosing
	// ScopeClosed has released all of its resources.
	ScopeClosed
)

func (s ScopeState) String() string {
	switch s {
	case ScopeOpen:
		return "open"
	case ScopeClosing:
		return "closing"
	case ScopeClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Scope is one node of the acyclic ownership tree. Each scope owns
// entity leases, subscription registrations and tasks; Child creates
// shorter-lived sub-scopes; Close cancels and releases deterministically.
//
// The documented close order (contract: "Logical ownership" and "Scopes
// and cancellation") is:
//
//  1. mark the whole closing subtree unavailable to new ownership and
//     result delivery (top-down, children in creation order);
//  2. cancel registrations and tasks of the whole subtree: each scope's
//     own registrations and tasks in reverse registration order, the
//     scope itself first, then children in reverse creation order;
//  3. release resources: children in reverse creation order, then the
//     scope's own leases in reverse acquisition order.
//
// Entity release hooks triggered by lease releases run at the next
// effect-flush boundary, never mid-close. A close initiated outside an
// update enters the app's foreground update/flush machinery. Cleanup uses
// ordered records, not Go map iteration.
type Scope struct {
	app      *App
	parent   *Scope
	state    ScopeState
	children []*Scope       // creation order
	leases   []*entityLease // acquisition order
	subs     []*subReg      // registration order
	tasks    []*taskRun     // registration order
	seq      int            // lease acquisition counter
	// closeGen is the scope's close generation: bumped once when the
	// scope starts closing. Result-delivery tokens record it at
	// registration and discard late results after any bump. A scope
	// never reopens in this slice, so one bump is terminal; the
	// generation is the forward-compatible gate for scope reuse.
	closeGen uint64
}

// newScope creates a scope. A nil parent makes a root-owned scope (the
// app's root scope and payload arena scopes use this); otherwise the
// scope becomes a child of parent.
func newScope(app *App, parent *Scope) *Scope {
	s := &Scope{app: app, parent: parent, state: ScopeOpen}
	if parent != nil {
		parent.children = append(parent.children, s)
	}
	return s
}

// App returns the application this scope belongs to.
func (s *Scope) App() *App { return s.app }

// Parent returns the parent scope, or nil for app-level scopes.
func (s *Scope) Parent() *Scope { return s.parent }

// State reports the close state of the scope.
func (s *Scope) State() ScopeState { return s.state }

// Child creates a child scope for a shorter lifetime without creating
// another entity. It panics when the scope is closing or closed.
func (s *Scope) Child() *Scope {
	s.requireOpen()
	return newScope(s.app, s)
}

// requireOpen panics with the misuse diagnostic when the scope is not
// open for new ownership.
func (s *Scope) requireOpen() {
	if s.state != ScopeOpen {
		panic("gpui: scope is closing or closed and cannot accept new ownership")
	}
}

// Close closes the scope and its subtree, idempotently, in the documented
// order. When initiated outside an update it enters the app's foreground
// update/flush machinery so that release hooks triggered by released
// leases run at the flush boundary.
func (s *Scope) Close() {
	if s.state != ScopeOpen {
		return
	}
	if s.app.updateDepth == 0 && !s.app.flushing {
		s.app.Update(func(*App) { closeScopeTree(s, nil) })
		return
	}
	closeScopeTree(s, nil)
}

// closeScopeTree closes the subtree rooted at s in the documented phases.
// between, when non-nil, runs between the cancellation phase and the
// resource-release phase (the entity release callbacks use this slot).
func closeScopeTree(s *Scope, between func()) {
	if s.state != ScopeOpen {
		return
	}
	markClosingTree(s)
	cancelTree(s)
	if between != nil {
		between()
	}
	releaseTree(s)
}

// markClosingTree marks the whole closing subtree unavailable (top-down,
// children in creation order).
func markClosingTree(s *Scope) {
	if s.state != ScopeOpen {
		return
	}
	s.closeGen++
	s.state = ScopeClosing
	for _, child := range s.children {
		markClosingTree(child)
	}
}

// cancelTree cancels the registrations and tasks of the whole subtree:
// each scope's own work in reverse registration order, the scope itself
// first, then children in reverse creation order.
func cancelTree(s *Scope) {
	if s.state == ScopeClosed {
		return
	}
	cancelScopeWork(s)
	for i := len(s.children) - 1; i >= 0; i-- {
		cancelTree(s.children[i])
	}
}

// cancelScopeWork cancels one scope's own registrations and tasks in
// reverse registration order.
func cancelScopeWork(s *Scope) {
	for i := len(s.tasks) - 1; i >= 0; i-- {
		s.tasks[i].cancel()
	}
	for i := len(s.subs) - 1; i >= 0; i-- {
		s.subs[i].cancel()
	}
	s.tasks = nil
	s.subs = nil
}

// releaseTree releases the subtree's resources: children in reverse
// creation order, then the scope's own leases in reverse acquisition
// order. Lease releases queue entity drops; their hooks run at the next
// effect-flush boundary.
func releaseTree(s *Scope) {
	if s.state == ScopeClosed {
		return
	}
	for i := len(s.children) - 1; i >= 0; i-- {
		releaseTree(s.children[i])
	}
	for i := len(s.leases) - 1; i >= 0; i-- {
		leaseRelease(s.leases[i])
	}
	s.leases = nil
	s.state = ScopeClosed
}

// addLease registers an owned lease with this scope in acquisition order.
func (s *Scope) addLease(lease *entityLease) {
	s.requireOpen()
	s.seq++
	lease.seq = s.seq
	s.leases = append(s.leases, lease)
}

// removeLease removes one lease registration.
func (s *Scope) removeLease(lease *entityLease) {
	for i, candidate := range s.leases {
		if candidate == lease {
			s.leases = append(s.leases[:i], s.leases[i+1:]...)
			return
		}
	}
}

// addSub registers a subscription cancellation with this scope.
func (s *Scope) addSub(reg *subReg) {
	s.requireOpen()
	s.subs = append(s.subs, reg)
}

// removeSub removes one subscription registration.
func (s *Scope) removeSub(reg *subReg) {
	for i, candidate := range s.subs {
		if candidate == reg {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			return
		}
	}
}

// addTask registers a task cancellation with this scope.
func (s *Scope) addTask(t *taskRun) {
	s.requireOpen()
	s.tasks = append(s.tasks, t)
}

// removeTask removes one task registration.
func (s *Scope) removeTask(t *taskRun) {
	for i, candidate := range s.tasks {
		if candidate == t {
			s.tasks = append(s.tasks[:i], s.tasks[i+1:]...)
			return
		}
	}
}
