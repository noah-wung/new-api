package usagereporter

import "context"

type healthObserverKey struct{}

// WithHealthObserver reports successful local collector initialization without
// requiring an upstream upload or inventing a usage event on idle devices.
func WithHealthObserver(ctx context.Context, observer func()) context.Context {
	return context.WithValue(ctx, healthObserverKey{}, observer)
}

func observeHealthy(ctx context.Context) {
	if observer, ok := ctx.Value(healthObserverKey{}).(func()); ok {
		observer()
	}
}
