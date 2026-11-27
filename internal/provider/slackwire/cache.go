package slackwire

import (
	"sync"
	"time"
)

// cached is a value loaded from Slack and kept for a while: the user
// directory, the custom emoji. Its mutex is held during a load, so
// concurrent callers share one. A failed load is not kept.
type cached[T any] struct {
	mu     sync.Mutex
	value  T
	loaded time.Time // zero until the first successful load
}

// get returns the value, loading it first when it is missing or older
// than ttl at now.
func (c *cached[T]) get(now time.Time, ttl time.Duration, load func() (T, error)) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded.IsZero() && now.Sub(c.loaded) < ttl {
		return c.value, nil
	}
	v, err := load()
	if err != nil {
		return v, err
	}
	c.value, c.loaded = v, now
	return v, nil
}
