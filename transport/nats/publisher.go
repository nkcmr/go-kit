package nats

import (
	"code.nkcmr.net/go-kit/endpoint"
	"context"
	"encoding/json"
	"github.com/nats-io/nats.go"
	"time"
)

// Publisher wraps a URL and provides a method that implements endpoint.Endpoint.
type Publisher[Request, Response any] struct {
	publisherOptions
	publisher *nats.Conn
	subject   string
	enc       EncodeRequestFunc[Request]
	dec       DecodeResponseFunc[Response]
}

type publisherOptions struct {
	before  []RequestFunc
	after   []PublisherResponseFunc
	timeout time.Duration
}

// NewPublisher constructs a usable Publisher for a single remote method.
func NewPublisher[Request, Response any](
	publisher *nats.Conn,
	subject string,
	enc EncodeRequestFunc[Request],
	dec DecodeResponseFunc[Response],
	options ...PublisherOption,
) *Publisher[Request, Response] {
	p := &Publisher[Request, Response]{
		publisherOptions: publisherOptions{
			timeout: 10 * time.Second,
		},
		publisher: publisher,
		subject:   subject,
		enc:       enc,
		dec:       dec,
	}
	for _, option := range options {
		option(&p.publisherOptions)
	}
	return p
}

// PublisherOption sets an optional parameter for clients.
type PublisherOption func(*publisherOptions)

// PublisherBefore sets the RequestFuncs that are applied to the outgoing NATS
// request before it's invoked.
func PublisherBefore(before ...RequestFunc) PublisherOption {
	return func(p *publisherOptions) { p.before = append(p.before, before...) }
}

// PublisherAfter sets the ClientResponseFuncs applied to the incoming NATS
// request prior to it being decoded. This is useful for obtaining anything off
// of the response and adding onto the context prior to decoding.
func PublisherAfter(after ...PublisherResponseFunc) PublisherOption {
	return func(p *publisherOptions) { p.after = append(p.after, after...) }
}

// PublisherTimeout sets the available timeout for NATS request.
func PublisherTimeout(timeout time.Duration) PublisherOption {
	return func(p *publisherOptions) { p.timeout = timeout }
}

// Endpoint returns a usable endpoint that invokes the remote endpoint.
func (p Publisher[Request, Response]) Endpoint() endpoint.Endpoint[Request, Response] {
	return func(ctx context.Context, request Request) (Response, error) {
		var zero Response
		ctx, cancel := context.WithTimeout(ctx, p.timeout)
		defer cancel()

		msg := nats.Msg{Subject: p.subject}

		if err := p.enc(ctx, &msg, request); err != nil {
			return zero, err
		}

		for _, f := range p.before {
			ctx = f(ctx, &msg)
		}

		resp, err := p.publisher.RequestWithContext(ctx, msg.Subject, msg.Data)
		if err != nil {
			return zero, err
		}

		for _, f := range p.after {
			ctx = f(ctx, resp)
		}

		response, err := p.dec(ctx, resp)
		if err != nil {
			return zero, err
		}

		return response, nil
	}
}

// EncodeJSONRequest is an EncodeRequestFunc that serializes the request as a
// JSON object to the Data of the Msg. Many JSON-over-NATS services can use it as
// a sensible default.
func EncodeJSONRequest[Request any](_ context.Context, msg *nats.Msg, request Request) error {
	b, err := json.Marshal(request)
	if err != nil {
		return err
	}

	msg.Data = b

	return nil
}
