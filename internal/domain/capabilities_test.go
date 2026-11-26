package domain

import (
	"errors"
	"slices"
	"testing"
)

func TestCapabilitiesHas(t *testing.T) {
	caps := NewCapabilities(Realtime, Search)
	if !caps.Has(Realtime) || !caps.Has(Search) {
		t.Error("declared capabilities are missing")
	}
	if caps.Has(Typing) || caps.Has(Files) {
		t.Error("undeclared capabilities are present")
	}
}

func TestCapabilitiesRequire(t *testing.T) {
	caps := NewCapabilities(Realtime)
	if err := caps.Require(Realtime); err != nil {
		t.Errorf("Require(Realtime) = %v, want nil", err)
	}
	err := caps.Require(Search)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Require(Search) = %v, want ErrUnsupported", err)
	}
	if err.Error() != "capability not supported by this provider: search" {
		t.Errorf("error text = %q", err.Error())
	}
}

func TestCapabilityNamesRoundTrip(t *testing.T) {
	for _, c := range AllCapabilities() {
		got, err := ParseCapability(c.String())
		if err != nil || got != c {
			t.Errorf("ParseCapability(%q) = (%v, %v), want (%v, nil)", c.String(), got, err, c)
		}
	}
	got, err := ParseCapability("teleport")
	if err == nil {
		t.Error("an unknown capability name parsed")
	}
	if slices.Contains(AllCapabilities(), got) {
		t.Errorf("a failed parse returned the real capability %q", got)
	}
}

func TestCapabilityNamesAreTheRPCNames(t *testing.T) {
	want := []string{"realtime", "search", "files", "presenceWrite", "typing", "customEmoji", "sections"}
	all := AllCapabilities()
	if len(all) != len(want) {
		t.Fatalf("got %d capabilities, want %d", len(all), len(want))
	}
	for i, c := range all {
		if c.String() != want[i] {
			t.Errorf("capability %d is %q, want %q", i, c.String(), want[i])
		}
	}
}

func TestCapabilitiesList(t *testing.T) {
	got := NewCapabilities(Typing, Realtime).List()
	if len(got) != 2 || got[0] != Realtime || got[1] != Typing {
		t.Errorf("List() = %v, want [realtime typing] in declaration order", got)
	}
}
