package slackwire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
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

// IsAuthError reports whether a Slack error code rejects the credentials.
func IsAuthError(code string) bool { return slices.Contains(authErrorCodes, code) }

// defaultRetryAfter is how long to wait after a 429 that carries no usable
// Retry-After header.
const defaultRetryAfter = time.Minute

// Envelope is the part every Web API response shares. Response types embed
// it, which makes them a Response.
type Envelope struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

func (e Envelope) status() Envelope { return e }

// Response is a decoded Web API response.
type Response interface{ status() Envelope }

// APIError is the error code a Web API method answered with (ok:false).
// Codes that reject the credentials also match provider.ErrAuthExpired,
// so the sync engine stops retrying.
type APIError struct {
	Method string
	Code   string
}

func (e *APIError) Error() string {
	if IsAuthError(e.Code) {
		return fmt.Sprintf("%s: %s: %v", e.Method, e.Code, provider.ErrAuthExpired)
	}
	return e.Method + ": " + e.Code
}

// Is makes errors.Is(err, provider.ErrAuthExpired) true for auth codes.
func (e *APIError) Is(target error) bool {
	return target == provider.ErrAuthExpired && IsAuthError(e.Code)
}

// HasCode reports whether err is, or wraps, a Slack error with code.
func HasCode(err error, code string) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == code
}

// retryAfter reads a 429's Retry-After header, in seconds.
func retryAfter(h http.Header) time.Duration {
	secs, err := strconv.Atoi(h.Get("Retry-After"))
	if err != nil {
		return defaultRetryAfter
	}
	return time.Duration(secs) * time.Second
}

// Signer adds a provider's credentials to a Web API request, in its form
// or in its headers. It may change both freely: each request gets its own.
type Signer func(form url.Values, h http.Header)

// WebAPI calls Slack's Web API for one workspace. Create it with
// NewWebAPI; it is safe for concurrent use.
type WebAPI struct {
	url  string
	http *http.Client
	sign Signer
	log  *slog.Logger
	now  func() time.Time // the local clock; tests replace it
}

// NewWebAPI returns a Web API client that posts to base (which ends in
// "/"; method names are appended to it) through client, signs every
// request with sign, and logs the messages it drops to log.
func NewWebAPI(base string, client *http.Client, sign Signer, log *slog.Logger) *WebAPI {
	return &WebAPI{url: base, http: client, sign: sign, log: log, now: time.Now}
}

// Call posts form to the Web API method and decodes the response into
// out. An HTTP 429 is a *provider.RateLimitedError; a rejected credential
// wraps provider.ErrAuthExpired.
func (a *WebAPI) Call(ctx context.Context, method string, form url.Values, out Response) error {
	form = maps.Clone(form)
	header := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}
	a.sign(form, header)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url+method, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	req.Header = header

	resp, err := a.http.Do(req)
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
		return &APIError{Method: method, Code: env.Error}
	}
	return nil
}
