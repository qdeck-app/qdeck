package async

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"gioui.org/app"

	"github.com/qdeck-app/qdeck/config"
)

// Result wraps the outcome of an async operation.
type Result[T any] struct {
	Value T
	Err   error
	gen   uint64 // set by dispatch, checked by Poll to discard stale results
}

// Runner dispatches work to goroutines and delivers results via channels.
// The UI loop selects on the result channel each frame.
type Runner[T any] struct {
	results chan Result[T]
	window  *app.Window
	cancel  context.CancelFunc
	gen     atomic.Uint64
	mu      sync.Mutex
}

const minBufferSize = 1

func NewRunner[T any](w *app.Window, bufferSize int) *Runner[T] {
	return &Runner[T]{
		results: make(chan Result[T], max(bufferSize, minBufferSize)),
		window:  w,
	}
}

// RunWithTimeout executes fn in a new goroutine with a timeout appropriate for
// the given operation type. When fn completes, the result is sent to the channel
// and the window is invalidated to trigger a frame.
func (r *Runner[T]) RunWithTimeout(opType config.OperationType, fn func(ctx context.Context) (T, error)) {
	timeout := config.TimeoutForOperation(opType)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	r.dispatch(ctx, cancel, func() (T, error) {
		return fn(ctx)
	})
}

// RunBlocking executes fn in a new goroutine without passing context.
// Used for blocking GUI operations (e.g., file pickers) that cannot be meaningfully
// cancelled or timed out. Cancellation is still available via Stop().
func (r *Runner[T]) RunBlocking(fn func() (T, error)) {
	ctx, cancel := context.WithCancel(context.Background())
	r.dispatch(ctx, cancel, fn)
}

// dispatch cancels any prior operation, drains stale results, and launches
// fn in a new goroutine with panic recovery. A generation counter prevents
// cancelled goroutines from delivering stale results.
func (r *Runner[T]) dispatch(ctx context.Context, cancel context.CancelFunc, fn func() (T, error)) {
	r.mu.Lock()

	if r.cancel != nil {
		r.cancel()
	}

	// Drain any stale result from the previous operation.
	select {
	case <-r.results:
	default:
	}

	r.cancel = cancel
	r.gen.Add(1)
	myGen := r.gen.Load()
	r.mu.Unlock()

	go func() {
		defer cancel()

		val, err := r.safeCall(fn)

		if r.gen.Load() != myGen {
			return
		}

		select {
		case r.results <- Result[T]{Value: val, Err: err, gen: myGen}:
		case <-ctx.Done():
			return
		}

		r.window.Invalidate()
	}()
}

// safeCall executes fn and recovers from panics, converting them to errors
// with a full stack trace for debugging.
func (r *Runner[T]) safeCall(fn func() (T, error)) (val T, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("async runner panic: %v\n%s", rec, debug.Stack())
		}
	}()

	return fn()
}

// Poll non-blockingly checks for a result. Call this every frame.
// Stale results from cancelled operations are silently discarded.
func (r *Runner[T]) Poll() (Result[T], bool) {
	select {
	case res := <-r.results:
		if res.gen != r.gen.Load() {
			return Result[T]{}, false
		}

		return res, true
	default:
		return Result[T]{}, false
	}
}

func (r *Runner[T]) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
}
