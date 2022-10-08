package async

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFutureNoContextCancel(t *testing.T) {
	f := NewFuture(func() (int, error) {
		return 42, nil
	})
	if f.done != nil {
		t.Fatalf("done: %v", f.done)
	}
	res, err := f.Result(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res != 42 {
		t.Fatalf("res: %v", res)
	}
}

func TestFutureContext(t *testing.T) {
	f := NewFuture(func() (int, error) {
		return 42, nil
	})
	if f.done != nil {
		t.Fatalf("done: %v", f.done)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	res, err := f.Result(ctx)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res != 42 {
		t.Fatalf("res: %v", res)
	}
}

func TestFuturePanic(t *testing.T) {
	f := NewFuture(func() (int, error) {
		panic("at the disco")
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	defer func() {
		r := recover()
		if !strings.HasPrefix(r.(string), "panic: at the disco") {
			t.Fatalf("r: %v", r)
		}
	}()
	f.Result(ctx)
}

func TestFuturePanicIgnored(t *testing.T) {
	panicked := false
	panicHook = func(perr *panicError) {
		t.Logf("perr: %q", perr)
		panicked = strings.HasPrefix(perr.Error(), "panic: at the disco")
	}
	t.Cleanup(func() {
		panicHook = nil
	})
	f := NewFuture(func() (int, error) {
		panic("at the disco")
	}).NonBlocking()
	f.Eager()
	runtime.GC()
	runtime.GC()
	if !panicked {
		t.Fatal("not panicked")
	}
}

func TestFutureDone(t *testing.T) {
	waitCh := make(chan struct{})
	f := NewFuture(func() (int, error) {
		<-waitCh
		return 42, nil
	})
	if f.done != nil {
		t.Fatalf("done: %v", f.done)
	}
	doneCh := f.Done()
	select {
	case <-doneCh:
		t.Fatal("done already closed")
	default:
	}
	waitCh <- struct{}{}
	<-doneCh
	f.Result(context.Background())
}

func TestFutureContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := NewFuture(func() (int, error) {
		cancel()
		return 42, nil
	})
	if f.done != nil {
		t.Errorf("done: %v", f.done)
	}
	res, err := f.Result(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err: %v", err)
	}
	if res != 0 {
		t.Errorf("res: %v", res)
	}
}

func ExampleFuture() {
	ctx := context.Background()
	foo := true
	bar := true

	f1 := NewFuture(func() (int, error) {
		return 42, nil
	}).NonBlocking()

	f2 := NewFuture(func() (string, error) {
		res, err := f1.Result(ctx)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("n=%d", res), nil
	})

	f3 := NewFuture(func() (string, error) {
		return "hello", nil
	}).NonBlocking()

	f4 := NewFuture(func() ([]string, error) {
		if foo && bar {
			f2.Eager()
		}

		var s []string
		if foo {
			r, err := f3.Result(ctx)
			if err != nil {
				return nil, err
			}
			s = append(s, r)
		}
		if bar {
			r, err := f2.Result(ctx)
			if err != nil {
				return nil, err
			}
			s = append(s, r)
		}
		return s, nil
	})

	f4.Result(ctx)
}

func TestFutureResolve(t *testing.T) {
	f := NewFuture(func() (int, error) {
		return 42, nil
	})
	if f.done != nil {
		t.Fatalf("done: %v", f.done)
	}
	err := f.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if f.res != 42 {
		t.Fatal(f.res)
	}
}

func TestFutureNilFunc(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("no panic")
		}
	}()
	NewFuture[int](nil)
}

func TestFutureMultipleCalls(t *testing.T) {
	var called atomic.Int32
	f := NewFuture(func() (int, error) {
		called.Add(1)
		return 42, nil
	})
	n, err := f.Result(context.Background())
	if err != nil || n != 42 {
		t.Fatal(n, err)
	}
	n, err = f.Result(context.Background())
	if err != nil || n != 42 {
		t.Fatal(n, err)
	}
	if c := called.Load(); c != 1 {
		t.Fatal(c)
	}
}
