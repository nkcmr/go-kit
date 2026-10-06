package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync/atomic"

	"code.nkcmr.net/go-kit/endpoint"
	httptransport "code.nkcmr.net/go-kit/transport/http"
)

type clientOptions struct {
	client         httptransport.HTTPClient
	before         []httptransport.RequestFunc
	after          []httptransport.ClientResponseFunc
	finalizer      httptransport.ClientFinalizerFunc
	requestID      RequestIDGenerator
	bufferedStream bool
}

// Client wraps a JSON RPC method and provides a method that implements endpoint.Endpoint.
type Client[Req, Resp any] struct {
	clientOptions

	// JSON RPC endpoint URL
	tgt *url.URL

	// JSON RPC method name.
	method string

	enc EncodeRequestFunc[Req]
	dec DecodeResponseFunc[Resp]
}

type clientRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      interface{}     `json:"id"`
}

// NewClient constructs a usable Client for a single remote method. If enc is
// nil, DefaultRequestEncoder is used. If dec is nil, DefaultResponseDecoder is
// used.
func NewClient[Req, Resp any](
	tgt *url.URL,
	method string,
	enc EncodeRequestFunc[Req],
	dec DecodeResponseFunc[Resp],
	options ...ClientOption,
) *Client[Req, Resp] {
	if enc == nil {
		enc = DefaultRequestEncoder[Req]
	}
	if dec == nil {
		dec = DefaultResponseDecoder[Resp]
	}
	c := &Client[Req, Resp]{
		clientOptions: clientOptions{
			client:         http.DefaultClient,
			before:         []httptransport.RequestFunc{},
			after:          []httptransport.ClientResponseFunc{},
			requestID:      NewAutoIncrementID(0),
			bufferedStream: false,
		},
		method: method,
		tgt:    tgt,
		enc:    enc,
		dec:    dec,
	}
	for _, option := range options {
		option(&c.clientOptions)
	}
	return c
}

// DefaultRequestEncoder marshals the given request to JSON.
func DefaultRequestEncoder[Req any](_ context.Context, req Req) (json.RawMessage, error) {
	return json.Marshal(req)
}

// DefaultResponseDecoder unmarshals the result to a Resp, or returns an
// error, if found.
func DefaultResponseDecoder[Resp any](_ context.Context, res Response) (Resp, error) {
	var result Resp
	if res.Error != nil {
		return result, *res.Error
	}
	err := json.Unmarshal(res.Result, &result)
	if err != nil {
		return result, err
	}
	return result, nil
}

// ClientOption sets an optional parameter for clients.
type ClientOption func(*clientOptions)

// CombineClientOptions returns a ClientOption that applies each of the given
// options in order. It's useful for packages that provide several related
// options as one.
func CombineClientOptions(options ...ClientOption) ClientOption {
	return func(c *clientOptions) {
		for _, option := range options {
			option(c)
		}
	}
}

// SetClient sets the underlying HTTP client used for requests.
// By default, http.DefaultClient is used.
func SetClient(client httptransport.HTTPClient) ClientOption {
	return func(c *clientOptions) { c.client = client }
}

// ClientBefore sets the RequestFuncs that are applied to the outgoing HTTP
// request before it's invoked.
func ClientBefore(before ...httptransport.RequestFunc) ClientOption {
	return func(c *clientOptions) { c.before = append(c.before, before...) }
}

// ClientAfter sets the ClientResponseFuncs applied to the server's HTTP
// response prior to it being decoded. This is useful for obtaining anything
// from the response and adding onto the context prior to decoding.
func ClientAfter(after ...httptransport.ClientResponseFunc) ClientOption {
	return func(c *clientOptions) { c.after = append(c.after, after...) }
}

// ClientFinalizer is executed at the end of every HTTP request.
// By default, no finalizer is registered.
func ClientFinalizer(f httptransport.ClientFinalizerFunc) ClientOption {
	return func(c *clientOptions) { c.finalizer = f }
}

// RequestIDGenerator returns an ID for the request.
type RequestIDGenerator interface {
	Generate() interface{}
}

// ClientRequestIDGenerator is executed before each request to generate an ID
// for the request.
// By default, AutoIncrementRequestID is used.
func ClientRequestIDGenerator(g RequestIDGenerator) ClientOption {
	return func(c *clientOptions) { c.requestID = g }
}

// BufferedStream sets whether the Response.Body is left open, allowing it
// to be read from later. Useful for transporting a file as a buffered stream.
func BufferedStream(buffered bool) ClientOption {
	return func(c *clientOptions) { c.bufferedStream = buffered }
}

// Endpoint returns a usable endpoint that invokes the remote endpoint.
func (c Client[Req, Resp]) Endpoint() endpoint.Endpoint[Req, Resp] {
	return func(ctx context.Context, request Req) (Resp, error) {
		var zero Resp
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		var (
			resp *http.Response
			err  error
		)
		if c.finalizer != nil {
			defer func() {
				if resp != nil {
					ctx = context.WithValue(ctx, httptransport.ContextKeyResponseHeaders, resp.Header)
					ctx = context.WithValue(ctx, httptransport.ContextKeyResponseSize, resp.ContentLength)
				}
				c.finalizer(ctx, err)
			}()
		}

		ctx = context.WithValue(ctx, ContextKeyRequestMethod, c.method)

		var params json.RawMessage
		if params, err = c.enc(ctx, request); err != nil {
			return zero, err
		}
		rpcReq := clientRequest{
			JSONRPC: Version,
			Method:  c.method,
			Params:  params,
			ID:      c.requestID.Generate(),
		}

		req, err := http.NewRequest("POST", c.tgt.String(), nil)
		if err != nil {
			return zero, err
		}

		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		var b bytes.Buffer
		req.Body = ioutil.NopCloser(&b)
		err = json.NewEncoder(&b).Encode(rpcReq)
		if err != nil {
			return zero, err
		}

		for _, f := range c.before {
			ctx = f(ctx, req)
		}

		resp, err = c.client.Do(req.WithContext(ctx))
		if err != nil {
			return zero, err
		}

		if !c.bufferedStream {
			defer resp.Body.Close()
		}

		for _, f := range c.after {
			ctx = f(ctx, resp)
		}

		// Decode the body into an object
		var rpcRes Response
		err = json.NewDecoder(resp.Body).Decode(&rpcRes)
		if err != nil {
			return zero, err
		}

		response, err := c.dec(ctx, rpcRes)
		if err != nil {
			return zero, err
		}

		return response, nil
	}
}

// ClientFinalizerFunc can be used to perform work at the end of a client HTTP
// request, after the response is returned. The principal
// intended use is for error logging. Additional response parameters are
// provided in the context under keys with the ContextKeyResponse prefix.
// Note: err may be nil. There maybe also no additional response parameters
// depending on when an error occurs.
type ClientFinalizerFunc func(ctx context.Context, err error)

// autoIncrementID is a RequestIDGenerator that generates
// auto-incrementing integer IDs.
type autoIncrementID struct {
	v *uint64
}

// NewAutoIncrementID returns an auto-incrementing request ID generator,
// initialised with the given value.
func NewAutoIncrementID(init uint64) RequestIDGenerator {
	// Offset by one so that the first generated value = init.
	v := init - 1
	return &autoIncrementID{v: &v}
}

// Generate satisfies RequestIDGenerator
func (i *autoIncrementID) Generate() interface{} {
	id := atomic.AddUint64(i.v, 1)
	return id
}
