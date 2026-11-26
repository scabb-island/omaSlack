package internalapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/provider"
)

// authErrorCodes are the Slack error codes that mean the credentials are no
// longer accepted. Retrying them cannot succeed.
var authErrorCodes = []string{"invalid_auth", "token_revoked", "account_inactive", "not_authed"}

// defaultRetryAfter is how long to wait after a 429 that carries no usable
// Retry-After header.
const defaultRetryAfter = time.Minute

// envelope is the part every Web API response shares. Response types embed
// it, which makes them a response.
type envelope struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

func (e envelope) status() envelope { return e }

// response is a decoded Web API response.
type response interface{ status() envelope }

// apiError turns a Slack error code into an error naming method. Auth codes
// wrap provider.ErrAuthExpired so the sync engine stops retrying.
func apiError(method, code string) error {
	if slices.Contains(authErrorCodes, code) {
		return fmt.Errorf("%s: %s: %w", method, code, provider.ErrAuthExpired)
	}
	return fmt.Errorf("%s: %s", method, code)
}

// retryAfter reads a 429's Retry-After header, in seconds.
func retryAfter(h http.Header) time.Duration {
	secs, err := strconv.Atoi(h.Get("Retry-After"))
	if err != nil {
		return defaultRetryAfter
	}
	return time.Duration(secs) * time.Second
}

// call posts form to the Web API method with the user token and decodes
// the response into out.
func (p *Provider) call(ctx context.Context, method string, form url.Values, out response) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.apiURL+method, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+p.creds.UserToken)

	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		return &provider.RateLimitedError{Method: method, RetryAfter: retryAfter(resp.Header)}
	default:
		return fmt.Errorf("%s: HTTP %s", method, resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: decode response: %w", method, err)
	}
	if env := out.status(); !env.OK {
		return apiError(method, env.Error)
	}
	return nil
}
