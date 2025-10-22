package web

import "go.opentelemetry.io/otel/trace"

type baseOptions struct {
	// tracer is used to create + export otel traces.
	tracer trace.Tracer
}

// Option configures how we set up the web service.
// Cribbed from https://github.com/grpc/grpc-go/blob/aff571cc86e6e7e740130dbbb32a9741558db805/dialoptions.go#L41
type Option interface {
	apply(*options)
}

// funcOption wraps a function that modifies options into an
// implementation of the Option interface.
type funcOption struct {
	f func(*options)
}

func (fdo *funcOption) apply(do *options) {
	fdo.f(do)
}

func newFuncOption(f func(*options)) *funcOption {
	return &funcOption{
		f: f,
	}
}

// WithTracer returns an Option which sets an otel trace provider.
func WithTracer(tracer trace.Tracer) Option {
	return newFuncOption(func(o *options) {
		o.tracer = tracer
	})
}
