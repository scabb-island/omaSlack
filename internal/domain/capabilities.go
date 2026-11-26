package domain

import (
	"errors"
	"fmt"
)

// ErrUnsupported reports an operation the workspace's provider does not
// support. The daemon maps it to RPC error -32002 unsupported_capability.
var ErrUnsupported = errors.New("capability not supported by this provider")

// Capability is one optional feature a provider may support (spec
// slack-connection, "Provider capability advertisement").
type Capability uint8

// The order here is the order capabilities are listed in. The zero value is
// not a capability, so a failed parse can never be mistaken for one.
const (
	_ Capability = iota
	Realtime
	Search
	Files
	PresenceWrite
	Typing
	CustomEmoji
	Sections
	capabilityEnd
)

// capabilityNames are the names used on the RPC wire.
var capabilityNames = [capabilityEnd]string{
	Realtime:      "realtime",
	Search:        "search",
	Files:         "files",
	PresenceWrite: "presenceWrite",
	Typing:        "typing",
	CustomEmoji:   "customEmoji",
	Sections:      "sections",
}

func (c Capability) String() string { return capabilityNames[c] }

// AllCapabilities lists every capability in declaration order.
func AllCapabilities() []Capability {
	var all []Capability
	for c := Realtime; c < capabilityEnd; c++ {
		all = append(all, c)
	}
	return all
}

// ParseCapability maps an RPC name back to its capability.
func ParseCapability(name string) (Capability, error) {
	for _, c := range AllCapabilities() {
		if c.String() == name {
			return c, nil
		}
	}
	return 0, fmt.Errorf("unknown capability %q", name)
}

// Capabilities is the set a provider declares. It is a value: copy it freely.
type Capabilities struct{ bits uint16 }

// NewCapabilities builds the set of the given capabilities.
func NewCapabilities(cs ...Capability) Capabilities {
	var set Capabilities
	for _, c := range cs {
		set.bits |= 1 << c
	}
	return set
}

// Has reports whether c is in the set.
func (s Capabilities) Has(c Capability) bool { return s.bits&(1<<c) != 0 }

// Require returns an ErrUnsupported error naming c when c is not in the set.
func (s Capabilities) Require(c Capability) error {
	if s.Has(c) {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrUnsupported, c)
}

// List returns the capabilities in the set, in declaration order.
func (s Capabilities) List() []Capability {
	var out []Capability
	for _, c := range AllCapabilities() {
		if s.Has(c) {
			out = append(out, c)
		}
	}
	return out
}
