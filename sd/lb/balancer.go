package lb

import (
	"errors"

	"code.nkcmr.net/go-kit/endpoint"
)

// Balancer yields endpoints according to some heuristic.
type Balancer[Request, Response any] interface {
	Endpoint() (endpoint.Endpoint[Request, Response], error)
}

// ErrNoEndpoints is returned when no qualifying endpoints are available.
var ErrNoEndpoints = errors.New("no endpoints available")
