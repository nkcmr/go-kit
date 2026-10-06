package awslambda

import (
	"context"

	"code.nkcmr.net/go-kit/endpoint"
	"code.nkcmr.net/go-kit/transport"
	"github.com/go-kit/log"
)

// Handler wraps an endpoint.
type Handler[Request, Response any] struct {
	handlerOptions
	e   endpoint.Endpoint[Request, Response]
	dec DecodeRequestFunc[Request]
	enc EncodeResponseFunc[Response]
}

type handlerOptions struct {
	before       []HandlerRequestFunc
	after        []HandlerResponseFunc
	errorEncoder ErrorEncoder
	finalizer    []HandlerFinalizerFunc
	errorHandler transport.ErrorHandler
}

// NewHandler constructs a new handler, which implements
// the AWS lambda.Handler interface.
func NewHandler[Request, Response any](
	e endpoint.Endpoint[Request, Response],
	dec DecodeRequestFunc[Request],
	enc EncodeResponseFunc[Response],
	options ...HandlerOption,
) *Handler[Request, Response] {
	h := &Handler[Request, Response]{
		handlerOptions: handlerOptions{
			errorEncoder: DefaultErrorEncoder,
			errorHandler: transport.NewLogErrorHandler(log.NewNopLogger()),
		},
		e:   e,
		dec: dec,
		enc: enc,
	}
	for _, option := range options {
		option(&h.handlerOptions)
	}
	return h
}

// HandlerOption sets an optional parameter for handlers.
type HandlerOption func(*handlerOptions)

// HandlerBefore functions are executed on the payload byte,
// before the request is decoded.
func HandlerBefore(before ...HandlerRequestFunc) HandlerOption {
	return func(h *handlerOptions) { h.before = append(h.before, before...) }
}

// HandlerAfter functions are only executed after invoking the endpoint
// but prior to returning a response.
func HandlerAfter(after ...HandlerResponseFunc) HandlerOption {
	return func(h *handlerOptions) { h.after = append(h.after, after...) }
}

// HandlerErrorLogger is used to log non-terminal errors.
// By default, no errors are logged.
// Deprecated: Use HandlerErrorHandler instead.
func HandlerErrorLogger(logger log.Logger) HandlerOption {
	return func(h *handlerOptions) { h.errorHandler = transport.NewLogErrorHandler(logger) }
}

// HandlerErrorHandler is used to handle non-terminal errors.
// By default, non-terminal errors are ignored.
func HandlerErrorHandler(errorHandler transport.ErrorHandler) HandlerOption {
	return func(h *handlerOptions) { h.errorHandler = errorHandler }
}

// HandlerErrorEncoder is used to encode errors.
func HandlerErrorEncoder(ee ErrorEncoder) HandlerOption {
	return func(h *handlerOptions) { h.errorEncoder = ee }
}

// HandlerFinalizer sets finalizer which are called at the end of
// request. By default no finalizer is registered.
func HandlerFinalizer(f ...HandlerFinalizerFunc) HandlerOption {
	return func(h *handlerOptions) { h.finalizer = append(h.finalizer, f...) }
}

// DefaultErrorEncoder defines the default behavior of encoding an error response,
// where it returns nil, and the error itself.
func DefaultErrorEncoder(ctx context.Context, err error) ([]byte, error) {
	return nil, err
}

// Invoke represents implementation of the AWS lambda.Handler interface.
func (h *Handler[Request, Response]) Invoke(
	ctx context.Context,
	payload []byte,
) (resp []byte, err error) {
	if len(h.finalizer) > 0 {
		defer func() {
			for _, f := range h.finalizer {
				f(ctx, resp, err)
			}
		}()
	}

	for _, f := range h.before {
		ctx = f(ctx, payload)
	}

	request, err := h.dec(ctx, payload)
	if err != nil {
		h.errorHandler.Handle(ctx, err)
		return h.errorEncoder(ctx, err)
	}

	response, err := h.e(ctx, request)
	if err != nil {
		h.errorHandler.Handle(ctx, err)
		return h.errorEncoder(ctx, err)
	}

	for _, f := range h.after {
		ctx = f(ctx, response)
	}

	if resp, err = h.enc(ctx, response); err != nil {
		h.errorHandler.Handle(ctx, err)
		return h.errorEncoder(ctx, err)
	}

	return resp, err
}
