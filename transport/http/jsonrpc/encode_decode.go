package jsonrpc

import (
	"context"
	"encoding/json"
	"reflect"

	"code.nkcmr.net/go-kit/endpoint"
)

// Server-Side Codec

// EndpointCodec defines a server Endpoint and its associated codecs. An
// EndpointCodecMap holds codecs for methods with different request and response
// types, so EndpointCodec isn't generic; use NewEndpointCodec to build one from
// a typed endpoint and codecs.
type EndpointCodec struct {
	Endpoint endpoint.Endpoint[any, any]
	Decode   DecodeRequestFunc[any]
	Encode   EncodeResponseFunc[any]
}

// NewEndpointCodec builds an EndpointCodec from a typed endpoint and codecs,
// checking at compile time that their types agree.
func NewEndpointCodec[Req, Resp any](
	e endpoint.Endpoint[Req, Resp],
	dec DecodeRequestFunc[Req],
	enc EncodeResponseFunc[Resp],
) EndpointCodec {
	return EndpointCodec{
		Endpoint: endpoint.Erase(e),
		Decode: func(ctx context.Context, msg json.RawMessage) (any, error) {
			return dec(ctx, msg)
		},
		Encode: func(ctx context.Context, response any) (json.RawMessage, error) {
			resp, ok := response.(Resp)
			if !ok && response != nil {
				return nil, &endpoint.TypeError{Value: response, Want: reflect.TypeFor[Resp]()}
			}
			return enc(ctx, resp)
		},
	}
}

// EndpointCodecMap maps the Request.Method to the proper EndpointCodec
type EndpointCodecMap map[string]EndpointCodec

// DecodeRequestFunc extracts a user-domain request object from raw JSON
// It's designed to be used in JSON RPC servers, for server-side endpoints.
// One straightforward DecodeRequestFunc could be something that unmarshals
// JSON from the request body to the concrete request type.
type DecodeRequestFunc[Req any] func(context.Context, json.RawMessage) (request Req, err error)

// EncodeResponseFunc encodes the passed response object to a JSON RPC result.
// It's designed to be used in HTTP servers, for server-side endpoints.
// One straightforward EncodeResponseFunc could be something that JSON encodes
// the object directly.
type EncodeResponseFunc[Resp any] func(context.Context, Resp) (response json.RawMessage, err error)

// Client-Side Codec

// EncodeRequestFunc encodes the given request object to raw JSON.
// It's designed to be used in JSON RPC clients, for client-side
// endpoints. One straightforward EncodeResponseFunc could be something that
// JSON encodes the object directly.
type EncodeRequestFunc[Req any] func(context.Context, Req) (request json.RawMessage, err error)

// DecodeResponseFunc extracts a user-domain response object from an JSON RPC
// response object. It's designed to be used in JSON RPC clients, for
// client-side endpoints. It is the responsibility of this function to decide
// whether any error present in the JSON RPC response should be surfaced to the
// client endpoint.
type DecodeResponseFunc[Resp any] func(context.Context, Response) (response Resp, err error)
