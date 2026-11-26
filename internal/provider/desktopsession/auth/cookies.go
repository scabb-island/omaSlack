package auth

import (
	"regexp"
	"strings"
)

// cookie is one cookie of a session, its value as Slack sends it.
type cookie struct{ name, value string }

// cookieName is what RFC 6265 allows in a cookie name (a token).
var cookieName = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

// valid reports whether c can go in a Cookie header as it is: a token
// for a name, and a value with no separator or line break.
func (c cookie) valid() bool {
	return cookieName.MatchString(c.name) && !strings.ContainsAny(c.value, ";\r\n")
}

// parseCookieHeader splits a Cookie header into its pairs, dropping any
// that is not name=value.
func parseCookieHeader(header string) []cookie {
	var out []cookie
	for part := range strings.SplitSeq(header, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if c := (cookie{name, value}); ok && c.valid() {
			out = append(out, c)
		}
	}
	return out
}

// cookieHeader is the Cookie header of a session: its cookies in order,
// with d set to dValue (added at the end when missing), so the header
// always carries the d cookie the credentials name. With no cookies at
// all it is "", which keyring.DesktopSession reads as "d alone".
func cookieHeader(cookies []cookie, dValue string) string {
	if len(cookies) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(cookies)+1)
	seenD := false
	for _, c := range cookies {
		if c.name == "d" {
			c.value, seenD = dValue, true
		}
		pairs = append(pairs, c.name+"="+c.value)
	}
	if !seenD {
		pairs = append(pairs, "d="+dValue)
	}
	return strings.Join(pairs, "; ")
}
