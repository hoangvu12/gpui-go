package base

// History is a browser-style linear trail with a current entry
// (U:history.rs:3-14). Entries before the current one are revisited with
// Back; entries left behind by going back are restored with Forward.
// Pushing after going back starts a new branch and drops the forward
// entries.
type History[T any] struct {
	entries        []T
	forwardEntries []T
	maxEntries     int
}

// NewHistory returns an empty trail keeping up to 1000 entries
// (U:history.rs:17-23; also Default, 128-132).
func NewHistory[T any]() *History[T] {
	return &History[T]{maxEntries: 1000}
}

// MaxEntries sets the maximum number of root-to-current entries to keep
// (default 1000); lowering it immediately removes the oldest entries
// (U:history.rs:25-32).
func (h *History[T]) MaxEntries(maxEntries int) *History[T] {
	h.maxEntries = maxEntries
	h.enforceMaxEntries()
	return h
}

// Push pushes an entry and drops the forward branch (U:history.rs:34-42).
func (h *History[T]) Push(entry T) {
	h.forwardEntries = h.forwardEntries[:0]
	if h.maxEntries == 0 {
		return
	}
	h.entries = append(h.entries, entry)
	h.enforceMaxEntries()
}

// Current returns the current entry (U:history.rs:44-47).
func (h *History[T]) Current() (T, bool) {
	if len(h.entries) == 0 {
		var zero T
		return zero, false
	}
	return h.entries[len(h.entries)-1], true
}

// ReplaceCurrent replaces the current entry, or pushes when the trail is
// empty (U:history.rs:49-55).
func (h *History[T]) ReplaceCurrent(entry T) {
	if len(h.entries) == 0 {
		h.Push(entry)
		return
	}
	h.entries[len(h.entries)-1] = entry
}

// RemoveCurrent removes and returns the current entry without changing
// the forward branch (U:history.rs:57-60).
func (h *History[T]) RemoveCurrent() (T, bool) {
	if len(h.entries) == 0 {
		var zero T
		return zero, false
	}
	last := h.entries[len(h.entries)-1]
	h.entries = h.entries[:len(h.entries)-1]
	return last, true
}

// CanBack reports whether moving back would keep a root entry current
// (U:history.rs:62-65).
func (h *History[T]) CanBack() bool { return len(h.entries) > 1 }

// CanForward reports whether a forward entry is available
// (U:history.rs:67-70).
func (h *History[T]) CanForward() bool { return len(h.forwardEntries) > 0 }

// Back moves back one entry and returns the new current entry
// (U:history.rs:72-82).
func (h *History[T]) Back() (T, bool) {
	if len(h.entries) <= 1 {
		var zero T
		return zero, false
	}
	last := h.entries[len(h.entries)-1]
	h.entries = h.entries[:len(h.entries)-1]
	h.forwardEntries = append(h.forwardEntries, last)
	return h.Current()
}

// Forward moves forward one entry and returns the restored entry
// (U:history.rs:84-96).
func (h *History[T]) Forward() (T, bool) {
	if h.maxEntries == 0 || len(h.forwardEntries) == 0 {
		var zero T
		return zero, false
	}
	entry := h.forwardEntries[len(h.forwardEntries)-1]
	h.forwardEntries = h.forwardEntries[:len(h.forwardEntries)-1]
	h.entries = append(h.entries, entry)
	h.enforceMaxEntries()
	return h.Current()
}

// Entries returns the entries from the root to the current entry
// (U:history.rs:98-101), as a copy.
func (h *History[T]) Entries() []T { return append([]T(nil), h.entries...) }

// ForwardEntries returns the forward entries from the nearest to the
// furthest (U:history.rs:103-106), as a copy.
func (h *History[T]) ForwardEntries() []T {
	out := make([]T, 0, len(h.forwardEntries))
	for i := len(h.forwardEntries) - 1; i >= 0; i-- {
		out = append(out, h.forwardEntries[i])
	}
	return out
}

// Retain keeps only the entries accepted by keep on both sides of the
// current position (U:history.rs:108-112).
func (h *History[T]) Retain(keep func(T) bool) {
	h.entries = retainSlice(h.entries, keep)
	h.forwardEntries = retainSlice(h.forwardEntries, keep)
}

// Clear clears current, back and forward entries (U:history.rs:114-118).
func (h *History[T]) Clear() {
	h.entries = h.entries[:0]
	h.forwardEntries = h.forwardEntries[:0]
}

// enforceMaxEntries drains the oldest excess entries
// (U:history.rs:120-125).
func (h *History[T]) enforceMaxEntries() {
	if excess := len(h.entries) - h.maxEntries; excess > 0 {
		h.entries = append(h.entries[:0], h.entries[excess:]...)
	}
}

// retainSlice is Vec::retain.
func retainSlice[T any](s []T, keep func(T) bool) []T {
	out := s[:0]
	for _, v := range s {
		if keep(v) {
			out = append(out, v)
		}
	}
	return out
}
