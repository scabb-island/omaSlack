package provider

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRateLimitedErrorIsFoundThroughWrapping(t *testing.T) {
	err := fmt.Errorf("history: %w", &RateLimitedError{Method: "conversations.history", RetryAfter: 30 * time.Second})

	var rl *RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatal("a wrapped RateLimitedError was not found")
	}
	if rl.Method != "conversations.history" || rl.RetryAfter != 30*time.Second {
		t.Errorf("got %+v", rl)
	}
	if rl.Error() != "rate limited on conversations.history, retry after 30s" {
		t.Errorf("Error() = %q", rl.Error())
	}
}

func TestAuthExpiredIsASentinel(t *testing.T) {
	err := fmt.Errorf("connect: %w", ErrAuthExpired)
	if !errors.Is(err, ErrAuthExpired) {
		t.Error("a wrapped ErrAuthExpired was not recognized")
	}
}
