package endpoint

import (
	"context"
	"fmt"
	"reflect"
	"slices"
)

// Endpoint is the fundamental building block of servers and clients.
// It represents a single RPC method.
type Endpoint[Request, Response any] func(ctx context.Context, request Request) (response Response, err error)

func (e Endpoint[Request, Response]) Any() Endpoint[any, any] {
	return Erase(e)
}

// Nop is an endpoint that does nothing and returns a nil error.
// Useful for tests.
func Nop(context.Context, interface{}) (interface{}, error) { return struct{}{}, nil }

// Middleware is a chainable behavior modifier for endpoints.
type Middleware func(Endpoint[any, any]) Endpoint[any, any]

// Chain is a helper function for composing middlewares. Requests will
// traverse them in the order they're declared. That is, the first middleware
// is treated as the outermost middleware.
func Chain(outer Middleware, others ...Middleware) Middleware {
	return func(next Endpoint[any, any]) Endpoint[any, any] {
		for _, other := range slices.Backward(others) { // reverse
			next = other(next)
		}
		return outer(next)
	}
}

// Failer may be implemented by Go kit response types that contain business
// logic error details. If Failed returns a non-nil error, the Go kit transport
// layer may interpret this as a business logic error, and may encode it
// differently than a regular, successful response.
//
// It's not necessary for your response types to implement Failer, but it may
// help for more sophisticated use cases. The addsvc example shows how Failer
// should be used by a complete application.
type Failer interface {
	Failed() error
}

// Erase converts a typed endpoint into an Endpoint[any, any], so that it can
// be wrapped by a Middleware. Use Typed to convert the result back.
//
// If the returned endpoint is called with a request that isn't a Request, it
// returns a *TypeError without calling e. A nil response (e.g. a nil pointer)
// is returned as an untyped nil.
func Erase[Request, Response any](e Endpoint[Request, Response]) Endpoint[any, any] {
	return func(ctx context.Context, request any) (any, error) {
		req, err := assertType[Request](request)
		if err != nil {
			return nil, err
		}
		response, err := e(ctx, req)
		return eraseNil(response), err
	}
}

// Typed converts an Endpoint[any, any], such as one returned by a Middleware,
// into a typed endpoint.
//
// If e returns a non-nil response that isn't a Response, the returned endpoint
// returns a *TypeError. If e returns an error, its response is passed along
// when it is a Response, and the zero value is returned otherwise.
func Typed[Request, Response any](e Endpoint[any, any]) Endpoint[Request, Response] {
	return func(ctx context.Context, request Request) (Response, error) {
		response, err := e(ctx, eraseNil(request))
		if err != nil {
			resp, _ := response.(Response)
			return resp, err
		}
		return assertType[Response](response)
	}
}

// TypeError is returned by the endpoints created by Erase and Typed when a
// request or response doesn't have the expected type.
type TypeError struct {
	Value any          // the value that was received
	Want  reflect.Type // the type that was expected
}

func (e *TypeError) Error() string {
	return fmt.Sprintf("endpoint: got value of type %T, want %v", e.Value, e.Want)
}

// assertType converts v to T. A nil v is accepted when T's zero value is nil.
func assertType[T any](v any) (T, error) {
	if t, ok := v.(T); ok {
		return t, nil
	}
	var zero T
	if v == nil && isNil(zero) {
		return zero, nil
	}
	return zero, &TypeError{Value: v, Want: reflect.TypeFor[T]()}
}

// eraseNil returns v as an any, except that nil pointers, maps, slices and
// similar become an untyped nil, so that checks like v == nil work as expected.
func eraseNil[T any](v T) any {
	if isNil(v) {
		return nil
	}
	return v
}

func isNil[T any](v T) bool {
	rv := reflect.ValueOf(&v).Elem()
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface, reflect.UnsafePointer:
		return rv.IsNil()
	}
	return false
}
