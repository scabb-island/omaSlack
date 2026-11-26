package provider

import (
	"context"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// Emit hands ev to the consumer of a Connect stream unless ctx has ended.
// The check comes first because a select with both cases ready picks one
// at random. An event already waiting in the select when ctx ends can
// still go through: the guarantee is that the channel closes, not that
// nothing in flight arrives (see Provider.Connect).
func Emit(ctx context.Context, out chan<- domain.Event, ev domain.Event) {
	if ctx.Err() != nil {
		return
	}
	select {
	case out <- ev:
	case <-ctx.Done():
	}
}
