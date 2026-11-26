package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrInvalidTS reports a string that is not a Slack message timestamp.
var ErrInvalidTS = errors.New("invalid Slack timestamp")

// TS is a Slack message timestamp ("1700000000.123456"). Slack uses it as
// the message ID inside a conversation, so it must round-trip exactly and
// order correctly. The zero value means "no timestamp".
type TS struct {
	sec   int64
	micro int64
}

// ParseTS validates s as "<seconds>.<six digits>".
func ParseTS(s string) (TS, error) {
	secPart, microPart, ok := strings.Cut(s, ".")
	if !ok || !allDigits(secPart) || len(microPart) != 6 || !allDigits(microPart) {
		return TS{}, fmt.Errorf("%w: %q", ErrInvalidTS, s)
	}
	sec, err := strconv.ParseInt(secPart, 10, 64)
	if err != nil {
		return TS{}, fmt.Errorf("%w: %q", ErrInvalidTS, s)
	}
	micro, _ := strconv.ParseInt(microPart, 10, 64) // six validated digits always parse
	return TS{sec: sec, micro: micro}, nil
}

// String formats the timestamp exactly as Slack does. Seconds are padded to
// ten digits, which is what Slack sends for any time after 2001.
func (t TS) String() string { return fmt.Sprintf("%010d.%06d", t.sec, t.micro) }

// OrEmpty is String for an optional timestamp: the zero TS becomes "",
// which is how the Store and the wire format spell "none".
func (t TS) OrEmpty() string {
	if t.IsZero() {
		return ""
	}
	return t.String()
}

// Before reports whether t is strictly earlier than u.
func (t TS) Before(u TS) bool {
	return t.sec < u.sec || (t.sec == u.sec && t.micro < u.micro)
}

// IsZero reports whether t is the zero timestamp.
func (t TS) IsZero() bool { return t == TS{} }

// allDigits reports whether s holds only ASCII digits. An empty s passes;
// callers reject it through its length or strconv.ParseInt.
func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
