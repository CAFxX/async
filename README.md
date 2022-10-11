# async

Tiny futures library for Go. 🔮

- Supports eager and lazy evaluation, both synchronous and asynchronous
- Generics-based
- Propagates panics
- Designed to interoperate with [`context`](https://pkg.go.dev/context)

[![GoDoc](https://pkg.go.dev/badge/github.com/CAFxX/async)](https://pkg.go.dev/github.com/CAFxX/async)

## Usage

Let's start with a simple example:

```go
f := NewFuture(func() ([]byte, error) {
    res, err := http.Get("http://www.example.com")
    if err != nil {
        return nil, err
    }
    defer res.Body.Close()
    return io.ReadAll(res.Body)
})
```

at this point, exeuction of the function wrapped in the `Future` has not
started yet.

There are multiple ways to do it, the simplest being calling `Result`.
If multiple calls to `Result` are done concurrently, only the first
one starts execution of the wrapped function; when the wrapped function
completes the same result is returned to all current (and future) callers
of `Result`:

```go
go func() {
    buf, err := f.Result(ctx1)
    // use buf and err
}()
go func() {
    buf, err := f.Result(ctx2)
    // use buf and err
}()
```

A call to `Result` return immediately if the context is cancelled. This
does not cancel execution of the wrapped function (to cancel execution
of the wrapped function use a context or other cancellation mechanism 
in the wrapped function). So, for example in the code above is `ctx1` is
cancelled the call to `Result` in the first goroutine will return
immediately, but the call in the second goroutine will continue waiting
until the wrapped function returns.

An important feature of the futures provided by this library is that they
propagate panics, so e.g. in the example above if the function wrapped by
the `Future` panicked, the panic would be caught and each call to `Result`
would panic instead (if `Result` is not called and the wrapped function
panics, the panic will be delivered to the go runtime instead, crashing
the process as if the panic had not been recovered).

### A more complex example

The real power of this library lies in its ability to quickly build
lazy evaluation trees that allow performant and efficient concurrent
evaluation of the desired results.

As an example, let's consider the case in which we need to construct
a response based on three subrequests (foo, bar, baz) whose results are
used to construct the two fields in the response (x and y).

```go
ctx, cancel := context.WithCancel(ctx)
defer cancel()

foo := NewFuture(func() (Foo, error) {
    return /* ... */
})
bar := NewFuture(func() (Bar, error) {
    return /* ... */
})
baz := NewFuture(func() (Baz, error) {
    return /* ... */
})

x := NewFuture(func() (string, error) {
    bar.Eager() // start eager evaluation of bar
    res, err := foo.Result(ctx)
    if err != nil {
        return "", err
    }
    res2, err := bar.Result()
    if err != nil {
        return "", err
    }
    return fmt.Sprintf("%v,%v", res, res2), nil
})
y := NewFuture(func() (string, error) {
    baz.Eager() // start eager evaluation of baz
    res, err := foo.Result(ctx) // note: result will be reused
    if err != nil {
        return "", err
    }
    res2, err := baz.Result()
    if err != nil {
        return "", err
    }
    return fmt.Sprintf("%v,%v", res, res2), nil
})
```

We have now built the evaluation trees. Instead of using futures,
we could have simply started eager evaluation of all these functions,
and this would would work in simple cases.

Now consider though what would happen if you did not always need
both x and y to be populated in the response, and instead you needed
to populate them only if requested (or only in some other dynamic
condition). 
Executing all the functions anyway just in case they are needed would
be extremely resource-inefficient, even if you could prune uneeded
functions by selectively cancelling the respective context.

Alternatively, you could execute everything serially, once you are
certain that each function needs to be executed, but this would
potentially be very slow (e.g. in case they involve performing
network requests).

Using this library you can instead do:

```go
if req.needY {
    y.Eager()
}

res := &Response{}
if req.needX {
    r, err := x.Result(ctx)
    if err != nil {
        return nil, err
    }
    res.x = r
}
if req.needY {
    r, err := y.Result(ctx)
    if err != nil {
        return nil, err
    }
    res.y = r
}
return res, nil
```

This will concurrently execute all functions required to satisfy
the request, and none of the functions that are not required, while
maximizing readability and separation of concerns: the resulting
code is linear as all synchronization happens behind the scenes.
Specifically:

- if we have both needX and needY true, all futures defiend above
  are started and execute concurrently
- if we have only needX true, only x, foo and bar are executed
- if we have only needY true, only y, foo and baz are executed

Note that thanks to the context defined above, as soon as any future
returns an error or panics, the context is cancelled and this makes
all futures using that context to return. As such this is an
effective replacement for `errgroup` when it's used to coordinate
the execution of multiple parts of a request.

### Examples

- https://pkg.go.dev/github.com/CAFxX/async#example-Future

## Future plans

Some potential ideas:

- Support also promises
- Adapters for common patterns
