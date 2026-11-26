// Package version holds build metadata injected at link time.
package version

import "strconv"

// These are overridden with -ldflags "-X gitlab.com/scabb-island/omaslack/internal/version.Version=..."
// by the Makefile and goreleaser.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Protocol is the major version of the local daemon RPC protocol (spec daemon-api).
// Clients and daemon must agree on it during the hello handshake.
const Protocol = 1

// String returns a one-line human-readable description of the build.
func String() string {
	return "omaslack " + Version + " (commit " + Commit + ", built " + Date + ", protocol " + strconv.Itoa(Protocol) + ")"
}
