package endpoint_test

import (
	"context"
	"errors"
	"testing"

	"code.nkcmr.net/go-kit/endpoint"
)

type request struct{ N int }

type response struct{ N int }

func (r *response) Failed() error { return errors.New("failed") }

func double(_ context.Context, req request) (*response, error) {
	return &response{N: req.N * 2}, nil
}

func TestEraseTypedRoundTrip(t *testing.T) {
	var calls int
	counter := func(next endpoint.Endpoint[any, any]) endpoint.Endpoint[any, any] {
		return func(ctx context.Context, request any) (any, error) {
			calls++
			return next(ctx, request)
		}
	}

	e := endpoint.Typed[request, *response](counter(endpoint.Erase(double)))
	resp, err := e(context.Background(), request{N: 21})
	if err != nil {
		t.Fatal(err)
	}
	if want, have := 42, resp.N; want != have {
		t.Errorf("want %d, have %d", want, have)
	}
	if want, have := 1, calls; want != have {
		t.Errorf("want %d middleware calls, have %d", want, have)
	}
}

func TestEraseWrongRequestType(t *testing.T) {
	_, err := endpoint.Erase(double)(context.Background(), "not a request")
	var typeErr *endpoint.TypeError
	if !errors.As(err, &typeErr) {
		t.Fatalf("want *endpoint.TypeError, have %v", err)
	}
	if want, have := "not a request", typeErr.Value; want != have {
		t.Errorf("want %v, have %v", want, have)
	}
}

func TestEraseNilRequest(t *testing.T) {
	ptrEndpoint := func(_ context.Context, req *request) (*response, error) {
		if req != nil {
			t.Errorf("want nil request, have %v", req)
		}
		return &response{}, nil
	}
	if _, err := endpoint.Erase(ptrEndpoint)(context.Background(), nil); err != nil {
		t.Errorf("nil request to pointer endpoint: %v", err)
	}

	// A struct can't be nil, so a nil request is a type error.
	_, err := endpoint.Erase(double)(context.Background(), nil)
	var typeErr *endpoint.TypeError
	if !errors.As(err, &typeErr) {
		t.Errorf("want *endpoint.TypeError, have %v", err)
	}
}

func TestEraseNilResponseIsUntypedNil(t *testing.T) {
	wantErr := errors.New("boom")
	failing := func(context.Context, request) (*response, error) { return nil, wantErr }

	resp, err := endpoint.Erase(failing)(context.Background(), request{})
	if err != wantErr {
		t.Errorf("want %v, have %v", wantErr, err)
	}
	if resp != nil {
		t.Errorf("want untyped nil response, have %#v", resp)
	}
	// Middleware commonly checks for Failer; a typed nil *response would match.
	if _, ok := resp.(endpoint.Failer); ok {
		t.Error("nil response must not satisfy endpoint.Failer")
	}
}

func TestTypedWrongResponseType(t *testing.T) {
	e := endpoint.Typed[request, *response](func(context.Context, any) (any, error) {
		return "not a response", nil
	})
	_, err := e(context.Background(), request{})
	var typeErr *endpoint.TypeError
	if !errors.As(err, &typeErr) {
		t.Fatalf("want *endpoint.TypeError, have %v", err)
	}
}

func TestTypedPassesErrorThrough(t *testing.T) {
	wantErr := errors.New("boom")
	e := endpoint.Typed[request, *response](func(context.Context, any) (any, error) {
		return nil, wantErr
	})
	resp, err := e(context.Background(), request{})
	if err != wantErr {
		t.Errorf("want %v, have %v", wantErr, err)
	}
	if resp != nil {
		t.Errorf("want nil response, have %v", resp)
	}
}
