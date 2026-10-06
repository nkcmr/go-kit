package amqp

import (
	"context"
	"time"

	"code.nkcmr.net/go-kit/endpoint"
	amqp "github.com/rabbitmq/amqp091-go"
)

// The golang AMQP implementation requires the []byte representation of
// correlation id strings to have a maximum length of 255 bytes.
const maxCorrelationIdLength = 255

// Publisher wraps an AMQP channel and queue, and provides a method that
// implements endpoint.Endpoint.
type Publisher[Request, Response any] struct {
	publisherOptions
	ch  Channel
	q   *amqp.Queue
	enc EncodeRequestFunc[Request]
	dec DecodeResponseFunc[Response]
}

type publisherOptions struct {
	before    []RequestFunc
	after     []PublisherResponseFunc
	deliverer Deliverer
	timeout   time.Duration
}

// NewPublisher constructs a usable Publisher for a single remote method.
func NewPublisher[Request, Response any](
	ch Channel,
	q *amqp.Queue,
	enc EncodeRequestFunc[Request],
	dec DecodeResponseFunc[Response],
	options ...PublisherOption,
) *Publisher[Request, Response] {
	p := &Publisher[Request, Response]{
		publisherOptions: publisherOptions{
			deliverer: DefaultDeliverer,
			timeout:   10 * time.Second,
		},
		ch:  ch,
		q:   q,
		enc: enc,
		dec: dec,
	}
	for _, option := range options {
		option(&p.publisherOptions)
	}
	return p
}

// PublisherOption sets an optional parameter for clients.
type PublisherOption func(*publisherOptions)

// PublisherBefore sets the RequestFuncs that are applied to the outgoing AMQP
// request before it's invoked.
func PublisherBefore(before ...RequestFunc) PublisherOption {
	return func(p *publisherOptions) { p.before = append(p.before, before...) }
}

// PublisherAfter sets the ClientResponseFuncs applied to the incoming AMQP
// request prior to it being decoded. This is useful for obtaining anything off
// of the response and adding onto the context prior to decoding.
func PublisherAfter(after ...PublisherResponseFunc) PublisherOption {
	return func(p *publisherOptions) { p.after = append(p.after, after...) }
}

// PublisherDeliverer sets the deliverer function that the Publisher invokes.
func PublisherDeliverer(deliverer Deliverer) PublisherOption {
	return func(p *publisherOptions) { p.deliverer = deliverer }
}

// PublisherTimeout sets the available timeout for an AMQP request.
func PublisherTimeout(timeout time.Duration) PublisherOption {
	return func(p *publisherOptions) { p.timeout = timeout }
}

// Endpoint returns a usable endpoint that invokes the remote endpoint.
func (p Publisher[Request, Response]) Endpoint() endpoint.Endpoint[Request, Response] {
	return func(ctx context.Context, request Request) (Response, error) {
		var zero Response
		ctx, cancel := context.WithTimeout(ctx, p.timeout)
		defer cancel()

		pub := amqp.Publishing{
			ReplyTo:       p.q.Name,
			CorrelationId: randomString(randInt(5, maxCorrelationIdLength)),
		}

		if err := p.enc(ctx, &pub, request); err != nil {
			return zero, err
		}

		for _, f := range p.before {
			// Affect only amqp.Publishing
			ctx = f(ctx, &pub, nil)
		}

		deliv, err := p.deliverer(ctx, p.ch, p.q, &pub)
		if err != nil {
			return zero, err
		}

		for _, f := range p.after {
			ctx = f(ctx, deliv)
		}
		response, err := p.dec(ctx, deliv)
		if err != nil {
			return zero, err
		}

		return response, nil
	}
}

// Deliverer is invoked by the Publisher to publish the specified Publishing on
// the Publisher's channel, and to retrieve the appropriate response Delivery
// object from its reply queue.
type Deliverer func(
	ctx context.Context,
	ch Channel,
	q *amqp.Queue,
	pub *amqp.Publishing,
) (*amqp.Delivery, error)

// DefaultDeliverer is a deliverer that publishes the specified Publishing
// and returns the first Delivery object with the matching correlationId.
// If the context times out while waiting for a reply, an error will be returned.
func DefaultDeliverer(
	ctx context.Context,
	ch Channel,
	q *amqp.Queue,
	pub *amqp.Publishing,
) (*amqp.Delivery, error) {
	err := ch.Publish(
		getPublishExchange(ctx),
		getPublishKey(ctx),
		false, //mandatory
		false, //immediate
		*pub,
	)
	if err != nil {
		return nil, err
	}
	autoAck := getConsumeAutoAck(ctx)

	msg, err := ch.Consume(
		q.Name,
		"", //consumer
		autoAck,
		false, //exclusive
		false, //noLocal
		false, //noWait
		getConsumeArgs(ctx),
	)
	if err != nil {
		return nil, err
	}

	for {
		select {
		case d := <-msg:
			if d.CorrelationId == pub.CorrelationId {
				if !autoAck {
					d.Ack(false) //multiple
				}
				return &d, nil
			}

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

}

// SendAndForgetDeliverer delivers the supplied publishing and
// returns a nil response.
// When using this deliverer please ensure that the supplied DecodeResponseFunc and
// PublisherResponseFunc are able to handle nil-type responses.
func SendAndForgetDeliverer(
	ctx context.Context,
	ch Channel,
	_ *amqp.Queue,
	pub *amqp.Publishing,
) (*amqp.Delivery, error) {
	err := ch.Publish(
		getPublishExchange(ctx),
		getPublishKey(ctx),
		false, //mandatory
		false, //immediate
		*pub,
	)
	return nil, err
}
