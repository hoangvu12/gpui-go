package script

// The plugin time budget (ticket 67, ADR 0003's containment decision):
// every turn that runs a plugin's script — its load, a handler, a
// timer, a frame callback, a promise settlement — runs under a
// watchdog that interrupts the goja runtime once the budget is spent.
// goja's Runtime.Interrupt sets an atomic flag the interpreter checks
// on its loop back-edges; the running call then returns an
// InterruptedError (an uncatchable exception: a plugin cannot swallow
// its budget with try/catch), which the host reports as the plugin's
// failure.
//
// Threading: Interrupt is the one goja call made from another
// goroutine, and it is documented safe for exactly that use — it only
// stores the flag under the runtime's interrupt lock. Everything else
// about the VM stays on the host thread, including ClearInterrupt,
// which the disarm path below runs on the host thread under the same
// mutex the watchdog's timer callback takes, so a late timer cannot
// leave a stray flag armed on the next turn.

import (
	"errors"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// DefaultPluginBudget is the per-call time budget a plugin runs under
// when the registration does not say otherwise (zgui's 50 ms). A
// budget of zero — an explicit WithPluginBudget(0) — disables the
// watchdog: a trust decision the app makes for a plugin it owns.
const DefaultPluginBudget = 50 * time.Millisecond

// budgetExceeded is the interrupt reason the watchdog passes to
// goja: the InterruptedError carries it (Unwrap) and its message, so
// the failure that reaches the placeholder names the plugin, the
// budget and the interrupted .tsx position.
type budgetExceeded struct {
	plugin string
	budget time.Duration
}

func (e *budgetExceeded) Error() string {
	return "plugin " + quotePlugin(e.plugin) + ": a script call exceeded its " +
		e.budget.String() + " budget and was interrupted"
}

func quotePlugin(name string) string { return `"` + name + `"` }

// watchdog arms goja's interrupt for one budgeted turn. All state is
// mutex-guarded so the timer's goroutine and the host thread agree on
// whether a call is still in flight: only an in-flight call is
// interrupted, and a fired interrupt is cleared (from the host thread)
// before the runtime is used again.
type watchdog struct {
	mu     sync.Mutex
	active bool
	fired  bool
	vm     *goja.Runtime
	timer  *time.Timer
}

// arm starts the countdown for one turn on vm.
func (w *watchdog) arm(vm *goja.Runtime, budget time.Duration, reason *budgetExceeded) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.active, w.fired, w.vm = true, false, vm
	w.timer = time.AfterFunc(budget, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if !w.active {
			return
		}
		w.fired = true
		vm.Interrupt(reason)
	})
}

// disarm stops the countdown and clears an interrupt that fired after
// the script had already returned (goja keeps the flag set until
// cleared, and a stray one would abort the next turn at its first
// instruction). Runs on the host thread, serialized with the timer
// callback by mu, so the runtime is not in use when it is cleared.
func (w *watchdog) disarm() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	w.active = false
	if w.fired && w.vm != nil {
		w.fired = false
		w.vm.ClearInterrupt()
	}
}

// runBudgeted runs one script turn under the host's budget (budget 0
// runs the body directly, so app hosts pay nothing). An interrupt that
// fired returns as the InterruptedError, whose message is the budget
// reason plus the interrupted call's position (mapped through the
// inline source map).
func (h *Host) runBudgeted(vm *goja.Runtime, body func() error) error {
	if h.budget <= 0 {
		return body()
	}
	h.watchdog.arm(vm, h.budget, &budgetExceeded{plugin: h.pluginName, budget: h.budget})
	defer h.watchdog.disarm()
	err := body()
	if err == nil {
		return nil
	}
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		var exceeded *budgetExceeded
		if errors.As(interrupted, &exceeded) {
			return interrupted
		}
	}
	return err
}
