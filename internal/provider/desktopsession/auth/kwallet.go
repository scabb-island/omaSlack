// Adapted from github.com/gammons/slk internal/slackdesktop/kwallet_linux.go (MIT, Copyright (c) 2026 Grant Ammons).

package auth

import (
	"context"
	"slices"
	"time"

	"github.com/godbus/dbus/v5"
)

// Chromium's KWallet backend names the folder and entry after the product.
const (
	kwalletFolder = "Slack Keys"
	kwalletEntry  = "Slack Safe Storage"

	// Shown in KWallet's access prompt and kept in its allow list. Slack's
	// own ID would inherit Slack's grant and misname who is asking.
	kwalletAppID = "omaslack"

	// kwalletd can block on a prompt; no call waits longer.
	kwalletCallTimeout = 10 * time.Second
)

// kwalletDaemons lists the bus names in preference order. kwalletd6 also
// owns the kwalletd5 name, so one process can answer twice.
var kwalletDaemons = []struct {
	name string
	path dbus.ObjectPath
}{
	{"org.kde.kwalletd6", "/modules/kwalletd6"},
	{"org.kde.kwalletd5", "/modules/kwalletd5"},
	{"org.kde.kwalletd", "/modules/kwalletd"},
}

// kwalletConn is the part of org.kde.KWallet the lookup uses.
type kwalletConn interface {
	IsEnabled(ctx context.Context) (bool, error)
	NetworkWallet(ctx context.Context) (string, error)
	IsOpen(ctx context.Context, wallet string) (bool, error)
	Open(ctx context.Context, wallet, appID string) (int32, error)
	HasEntry(ctx context.Context, handle int32, folder, key, appID string) (bool, error)
	ReadPassword(ctx context.Context, handle int32, folder, key, appID string) (string, error)
	Close(ctx context.Context, handle int32, appID string) error
}

// kwalletPasswords asks every running kwalletd for the key. A daemon that
// is not running is never called: calling its name would D-Bus-activate
// kwalletd and prompt a user who has the package but no wallet.
func kwalletPasswords(ctx context.Context, conn *dbus.Conn) ([][]byte, error) {
	var running []string
	if err := callKWallet(ctx, conn.BusObject(), "org.freedesktop.DBus.ListNames", &running); err != nil {
		return nil, err
	}
	attempts := []attempt{failed(ErrNoKeyring)}
	for _, d := range kwalletDaemons {
		if slices.Contains(running, d.name) {
			wallet := dbusKWallet{conn.Object(d.name, d.path)}
			attempts = append(attempts, func(ctx context.Context) ([][]byte, error) { return kwalletKey(ctx, wallet) })
		}
	}
	return gather(ctx, attempts)
}

// kwalletKey reads the key without raising a prompt: a closed wallet is
// reported as locked instead of opened, since its unlock dialog over a
// full-screen terminal reads as a hang.
func kwalletKey(ctx context.Context, c kwalletConn) ([][]byte, error) {
	wallet, err := openWallet(ctx, c)
	if err != nil {
		return nil, err
	}
	handle, err := c.Open(ctx, wallet, kwalletAppID)
	if err != nil {
		return nil, err
	}
	// Denied access comes back as a negative handle, not a D-Bus error.
	if handle < 0 {
		return nil, ErrKeyringLocked
	}
	defer func() { _ = c.Close(ctx, handle, kwalletAppID) }()

	// readPassword returns "" for a missing entry too, hence hasEntry.
	has, err := c.HasEntry(ctx, handle, kwalletFolder, kwalletEntry, kwalletAppID)
	if err != nil || !has {
		return nil, notFoundUnless(err)
	}
	pw, err := c.ReadPassword(ctx, handle, kwalletFolder, kwalletEntry, kwalletAppID)
	return single([]byte(pw), err)
}

// openWallet returns the wallet Chromium stores its key in (the network
// wallet) when it is enabled and already open.
func openWallet(ctx context.Context, c kwalletConn) (string, error) {
	enabled, err := c.IsEnabled(ctx)
	if err != nil || !enabled {
		return "", notFoundUnless(err)
	}
	wallet, err := c.NetworkWallet(ctx)
	if err != nil || wallet == "" {
		return "", notFoundUnless(err)
	}
	open, err := c.IsOpen(ctx, wallet)
	if err != nil {
		return "", err
	}
	if !open {
		return "", ErrKeyringLocked
	}
	return wallet, nil
}

// notFoundUnless returns err, or ErrSecretNotFound when the daemon answered
// without the key.
func notFoundUnless(err error) error {
	if err != nil {
		return err
	}
	return ErrSecretNotFound
}

type dbusKWallet struct{ obj dbus.BusObject }

func (d dbusKWallet) IsEnabled(ctx context.Context) (out bool, err error) {
	return out, d.call(ctx, "isEnabled", &out)
}

func (d dbusKWallet) NetworkWallet(ctx context.Context) (out string, err error) {
	return out, d.call(ctx, "networkWallet", &out)
}

func (d dbusKWallet) IsOpen(ctx context.Context, wallet string) (out bool, err error) {
	return out, d.call(ctx, "isOpen", &out, wallet)
}

// Open passes window ID 0: there is no window to parent a prompt to.
func (d dbusKWallet) Open(ctx context.Context, wallet, appID string) (out int32, err error) {
	return out, d.call(ctx, "open", &out, wallet, int64(0), appID)
}

func (d dbusKWallet) HasEntry(ctx context.Context, handle int32, folder, key, appID string) (out bool, err error) {
	return out, d.call(ctx, "hasEntry", &out, handle, folder, key, appID)
}

func (d dbusKWallet) ReadPassword(ctx context.Context, handle int32, folder, key, appID string) (out string, err error) {
	return out, d.call(ctx, "readPassword", &out, handle, folder, key, appID)
}

// Close releases the handle without forcing other apps off the wallet.
func (d dbusKWallet) Close(ctx context.Context, handle int32, appID string) error {
	var out int32
	return d.call(ctx, "close", &out, handle, false, appID)
}

func (d dbusKWallet) call(ctx context.Context, method string, out any, args ...any) error {
	return callKWallet(ctx, d.obj, "org.kde.KWallet."+method, out, args...)
}

func callKWallet(ctx context.Context, obj dbus.BusObject, method string, out any, args ...any) error {
	ctx, cancel := context.WithTimeout(ctx, kwalletCallTimeout)
	defer cancel()
	return obj.CallWithContext(ctx, method, 0, args...).Store(out)
}
