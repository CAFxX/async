package async

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// Future is a future for the specified type T.
//
// It supports both lazy and eager evaluation, both synchronous and asynchronous.
// It is especially useful when the results of one or more asynchronous operations
// may need to be consumed by multiple dependant asynchronous operations that
// themselves may or may not be executed.
type Future[T any] struct {
	once sync.Once
	fn   func() (T, error)
	res  T
	err  error

	nonBlocking atomic.Bool

	onceDone sync.Once
	done     chan struct{}

	onceEager sync.Once
}

var panicHook func(*panicError) // for testing

// NewFuture wraps the provided function into a Future handle that can
// be used to asynchronously execute the function and obtain its results.
//
// The wrapped function is not invoked immediately by NewFuture. It is
// invoked at most once when Result, Eager or Done are invoked
// (regardless of the number of invocations to these functions).
//
// If the provided function panics, the panic is caught and forwarded to
// all callers of Result.
func NewFuture[T any](fn func() (T, error)) *Future[T] {
	if fn == nil {
		panic("nil function")
	}
	w := &Future[T]{fn: fn}
	runtime.SetFinalizer(w, func(w *Future[T]) {
		if perr, ok := w.err.(*panicError); ok {
			defer func() {
				if panicHook != nil && recover() != nil {
					panicHook(perr)
				}
			}()
			panic("ignored: " + perr.Error())
		}
	})
	return w
}

// NonBlocking can be used to signal that the function wrapped by the
// Future is expected to execute quickly (no more than a few µs) and
// to not block (e.g. waiting for I/O). This allows the Future runtime
// to avoid executing the function in a separate goroutine, and
// reduces the amount of synchorinzation needed - potentially yielding
// higher performance.
//
// NonBlocking, if used, should be called before any call to Eager,
// Done, Result, or Resolve.
//
// After a call to NonBlocking, calls to Eager and Done will also block
// until the wrapped function has completed execution.
//
// If used inappropriately (e.g. for wrapped functions that block, or
// take longer than a few µs) this will slow down your code by
// inhibiting concurrent execution: in case of doubt avoid using it.
func (w *Future[T]) NonBlocking() *Future[T] {
	w.nonBlocking.Store(true)
	return w
}

// Eager signals to the Future runtime that execution of the wrapped
// function should be started now (if it has not been started yet).
//
// If your code calls Eager, it MUST eventually call Result as well:
// failure to do so will cause any panic deriving from the execution
// of the wrapped function to be delivered to the Go runtime,
// terminating the process.
func (w *Future[T]) Eager() {
	w.onceEager.Do(func() {
		if w.nonBlocking.Load() {
			w.resolve()
		} else {
			go w.resolve()
		}
	})
}

func (w *Future[T]) resolve() {
	w.once.Do(func() {
		defer func() {
			w.fn = nil
			if r := recover(); r != nil {
				w.err = &panicError{recovered: r, stackTrace: debug.Stack()}
			}
			w.onceDone.Do(func() {
				if w.done == nil {
					w.done = closedChan
				}
			})
			if w.done != closedChan {
				close(w.done)
			}
		}()
		w.res, w.err = w.fn()
	})
}

func (w *Future[T]) result(doPanic bool) (T, error) {
	if perr, ok := w.err.(*panicError); ok && doPanic {
		runtime.SetFinalizer(w, nil)
		panic(perr.Error())
	}
	return w.res, w.err
}

// Result returns the results returned by the wrapped function, once
// execution of the function has completed.
//
// Multiple Result calls will always return the same result, and the
// wrapped function will be invoked at most once.
//
// If the context is cancelled before the results become available,
// Result returns immediately (without waiting for the function to
// complete) with the error from the context.
//
// If the wrapped function panicked, Result will propagate that panic
// to each function that calls Result.
func (w *Future[T]) Result(ctx context.Context) (T, error) {
	ctxDone := ctx.Done()

	if ctxDone == nil {
		// If the context can't be cancelled, let's avoid spawning
		// a goroutine and allocating a channel. Just call the
		// function synchronously.
		w.resolve()
		return w.result(true)
	}

	select {
	case <-ctxDone:
		// If the context is cancelled, avoid starting evaluation.
		// We can't return now, because if the Future is already done
		// we want to return the result that is already available.
	default:
		w.Eager()
	}

	done := w._done()

	// If the result is available, we prioritize it over returning
	// the error from the cancelled context.
	select {
	case <-done:
		return w.result(true)
	default:
	}

	select {
	case <-done:
		return w.result(true)
	case <-ctxDone:
		var zero T
		return zero, ctx.Err()
	}
}

// Done returns a channel that is closed once the wrapped
// function has completed execution. Once this happens, calls
// to Result are guaranteed to not block. If the wrapped
// function has not been invoked yet by a previous call to
// Eager or Result, it is started.
//
// If your code calls Done, it MUST eventually call Result as well:
// failure to do so will cause any panic deriving from the execution
// of the wrapped function to be delivered to the Go runtime,
// terminating the process.
func (w *Future[T]) Done() <-chan struct{} {
	w.Eager()
	return w._done()
}

func (w *Future[T]) _done() <-chan struct{} {
	w.onceDone.Do(func() {
		if w.done == nil {
			w.done = make(chan struct{})
		}
	})
	return w.done
}

// Resolve synchronously invokes the wrapped function if it has not
// been invoked yet. It returns the error returned by the invocation.
//
// This method is mostly useful as an argument to (*errgroup.Group).Go().
func (w *Future[T]) Resolve() error {
	w.resolve()
	_, err := w.result(true)
	return err
}

type panicError struct {
	recovered  any
	stackTrace []byte
}

func (p *panicError) Error() string {
	return fmt.Sprintf("panic: %v\n%s", p.recovered, p.stackTrace)
}

var closedChan chan struct{}

func init() {
	closedChan = make(chan struct{})
	close(closedChan)
}
