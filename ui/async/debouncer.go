package async

import (
	"sync"
	"time"
)

// Debouncer[T] coalesces a stream of rapid Schedule(v) calls into at most one
// write(v) after the caller stops for `delay`. Each Schedule overwrites the
// pending value and resets the timer; only the most recent value gets written.
//
// Intended for UI-driven state persistence where input fires per-frame (arrow
// navigation, drags, etc.) but each write is expensive (whole-file JSON
// rewrite under a mutex). Safe for concurrent Schedule calls. The write runs
// on the time.AfterFunc worker goroutine, so at most one write is in flight.
type Debouncer[T any] struct {
	delay time.Duration
	write func(T)

	mu       sync.Mutex
	timer    *time.Timer
	pending  *T
	inFlight bool
}

// NewDebouncer returns a debouncer that calls write with the most recent
// scheduled value after `delay` of quiet.
func NewDebouncer[T any](delay time.Duration, write func(T)) *Debouncer[T] {
	return &Debouncer[T]{delay: delay, write: write}
}

// Schedule records v as the value to write and (re)arms the debounce timer.
// Repeated calls within `delay` collapse into a single write of the last v.
func (d *Debouncer[T]) Schedule(v T) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.pending = &v

	if d.timer == nil {
		d.timer = time.AfterFunc(d.delay, d.flush)

		return
	}

	d.timer.Reset(d.delay)
}

// flush writes the pending value if any. If another write is already running,
// this flush is dropped — the concurrent write already holds a recent value.
// After the write returns, if new work arrived mid-flight, the timer is
// re-armed so the newer value is persisted after the usual quiet window.
func (d *Debouncer[T]) flush() {
	d.mu.Lock()

	if d.inFlight {
		d.mu.Unlock()

		return
	}

	job := d.pending
	d.pending = nil

	if job == nil {
		d.mu.Unlock()

		return
	}

	d.inFlight = true
	d.mu.Unlock()

	d.write(*job)

	d.mu.Lock()
	d.inFlight = false
	hasMore := d.pending != nil

	if hasMore {
		if d.timer == nil {
			d.timer = time.AfterFunc(d.delay, d.flush)
		} else {
			d.timer.Reset(d.delay)
		}
	}

	d.mu.Unlock()
}

// Flush cancels any armed timer and writes the pending value synchronously on
// the caller's goroutine. Call from a shutdown path so the last scheduled
// value isn't lost when the process exits within the debounce window. If a
// write is already in flight on the timer goroutine, Flush skips it: that
// write is committing a recent value and will observe the cancelled timer.
func (d *Debouncer[T]) Flush() {
	d.mu.Lock()

	if d.timer != nil {
		d.timer.Stop()
	}

	if d.inFlight {
		d.mu.Unlock()

		return
	}

	job := d.pending
	d.pending = nil

	if job == nil {
		d.mu.Unlock()

		return
	}

	d.inFlight = true
	d.mu.Unlock()

	d.write(*job)

	d.mu.Lock()
	d.inFlight = false
	d.mu.Unlock()
}
