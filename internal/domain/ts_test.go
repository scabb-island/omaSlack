package domain

import (
	"errors"
	"testing"
)

func TestParseTSRoundTrips(t *testing.T) {
	for _, s := range []string{"1700000009.999999", "1700000000.123456", "1700000000.000001", "0000000001.000000"} {
		ts, err := ParseTS(s)
		if err != nil {
			t.Fatalf("ParseTS(%q): %v", s, err)
		}
		if ts.String() != s {
			t.Errorf("ParseTS(%q).String() = %q", s, ts.String())
		}
	}
}

func TestParseTSRejectsOtherShapes(t *testing.T) {
	bad := []string{"", ".123456", "99999999999999999999.000000", "1700000000", "1700000000.", "1700000000.12345", "1700000000.1234567", "abc.123456", "1700000000,123456", "-1.123456"}
	for _, s := range bad {
		if _, err := ParseTS(s); !errors.Is(err, ErrInvalidTS) {
			t.Errorf("ParseTS(%q) err = %v, want ErrInvalidTS", s, err)
		}
	}
}

func TestTSOrdering(t *testing.T) {
	a, _ := ParseTS("1700000000.000009")
	b, _ := ParseTS("1700000000.000010")
	c, _ := ParseTS("1700000001.000000")
	if !a.Before(b) || !b.Before(c) || !a.Before(c) {
		t.Error("timestamps are not ordered by seconds, then microseconds")
	}
	if b.Before(a) || a.Before(a) {
		t.Error("Before is not strict")
	}
}

func TestZeroTS(t *testing.T) {
	var zero TS
	if !zero.IsZero() {
		t.Error("the zero TS is not zero")
	}
	ts, _ := ParseTS("1700000000.123456")
	if ts.IsZero() {
		t.Error("a parsed TS reports zero")
	}
}
