package provider

import (
	"errors"
	"fmt"
	"time"
)

// ErrAuthExpired reports credentials Slack rejected (invalid_auth,
// token_revoked, …). The sync engine stops retrying and marks the
// workspace auth_expired (spec slack-connection, "Expired credentials").
var ErrAuthExpired = errors.New("slack rejected the credentials")

// RateLimitedError reports an HTTP 429 from Slack. The caller feeds
// RetryAfter to its rate budget (ratebudget.Budget.Penalize).
type RateLimitedError struct {
	Method     string
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("rate limited on %s, retry after %s", e.Method, e.RetryAfter)
}

// ErrFileTooLarge reports a file over the size limit of an upload or a
// download.
var ErrFileTooLarge = errors.New("file is larger than the limit")

// ErrNotDownloadable reports a file Slack offers no download for (an
// external file, such as a document linked from another service).
var ErrNotDownloadable = errors.New("file cannot be downloaded")
