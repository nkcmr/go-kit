package sd

import (
	"io"
	"testing"

	"code.nkcmr.net/go-kit/endpoint"
	"github.com/go-kit/log"
)

func BenchmarkEndpoints(b *testing.B) {
	var (
		ca      = make(closer)
		cb      = make(closer)
		cmap    = map[string]io.Closer{"a": ca, "b": cb}
		factory = func(instance string) (endpoint.Endpoint, io.Closer, error) { return endpoint.Nop, cmap[instance], nil }
		c       = newEndpointCache(factory, log.NewNopLogger(), endpointerOptions{})
	)

	b.ReportAllocs()

	c.Update(Event{Instances: []string{"a", "b"}})

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Endpoints()
		}
	})
}
