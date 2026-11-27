// Package client is the daemon connection the TUI programs against. The
// production implementation is *rpcclient.Client; tests use Fake.
package client

import (
	"context"
	"encoding/json"

	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
)

// Client is one connection to the daemon, after hello.
type Client interface {
	// Call makes one method call and returns its raw result. A daemon
	// error is an *rpc.Error.
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
	// Notifications delivers subscribed events in arrival order. It is
	// closed when the connection ends.
	Notifications() <-chan rpcclient.Notification
	// Hello is the daemon's hello result (workspaces, capabilities).
	Hello() json.RawMessage
	// Done is closed when the connection has ended.
	Done() <-chan struct{}
	Close() error
}

var _ Client = (*rpcclient.Client)(nil)
