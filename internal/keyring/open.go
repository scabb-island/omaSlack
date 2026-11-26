package keyring

import (
	"context"
	"errors"
	"io"

	"github.com/godbus/dbus/v5"

	"gitlab.com/scabb-island/omaslack/internal/keyring/secretservice"
)

// ClosableStore is a Store that holds resources until it is closed.
type ClosableStore interface {
	Store
	io.Closer
}

// Open returns the Secret Service store when a Secret Service answers on
// the session bus, and the file store at DefaultFilePath when there is no
// bus or nobody provides the service. Backend says which one it picked. A
// provider that is there but refuses a session is an error, not a reason
// to fall back: the credentials may already be stored in it.
func Open(ctx context.Context) (ClosableStore, error) {
	conn, err := dbus.ConnectSessionBus()
	if err == nil {
		store, err := openSecretService(ctx, conn)
		if err == nil {
			return store, nil
		}
		_ = conn.Close()
		if !errors.Is(err, secretservice.ErrUnavailable) {
			return nil, err
		}
	}
	path, err := DefaultFilePath()
	if err != nil {
		return nil, err
	}
	return NewFileStore(path), nil
}

// openSecretService checks that a session opens on conn, then returns a
// store that opens one per operation.
func openSecretService(ctx context.Context, conn *dbus.Conn) (*SecretServiceStore, error) {
	probe, err := secretservice.Open(ctx, conn)
	if err != nil {
		return nil, err
	}
	_ = probe.Close(ctx)
	return newSecretServiceStore(func(ctx context.Context) (secrets, error) {
		return secretservice.Open(ctx, conn)
	}, conn.Close), nil
}
