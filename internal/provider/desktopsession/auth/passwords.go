// Adapted from github.com/gammons/slk internal/slackdesktop/password_linux.go (MIT, Copyright (c) 2026 Grant Ammons).

package auth

import (
	"context"
	"errors"

	"github.com/godbus/dbus/v5"

	"gitlab.com/scabb-island/omaslack/internal/keyring/secretservice"
)

// keyringApp names whose "Safe Storage" cookie key to read. The Slack
// desktop app and each Chromium browser store their own key under their own
// name: the Secret Service "application" attribute (Slack spells it
// "Slack", browsers use their lowercase name such as "brave") and the
// KWallet folder and entry ("<Product> Keys" / "<Product> Safe Storage").
type keyringApp struct {
	secretAttr     string // Secret Service "application" attribute
	kwalletProduct string // product name for the KWallet folder and entry
}

// slackApp is the Slack desktop app's key identity, the zero value's
// meaning so SystemPasswords{} keeps reading it.
var slackApp = keyringApp{secretAttr: "Slack", kwalletProduct: "Slack"}

func (a keyringApp) resolved() keyringApp {
	if a.secretAttr == "" {
		return slackApp
	}
	return a
}

func (a keyringApp) kwalletFolder() string { return a.kwalletProduct + " Keys" }
func (a keyringApp) kwalletEntry() string  { return a.kwalletProduct + " Safe Storage" }

// secretQueries are where Chromium's libsecret backend keeps the key, plus
// the QtKeychain schema for setups where kwalletd bridges itself onto
// org.freedesktop.secrets.
func (a keyringApp) secretQueries() []map[string]string {
	return []map[string]string{
		{"xdg:schema": "chrome_libsecret_os_crypt_password_v2", "application": a.secretAttr},
		{"xdg:schema": "chrome_libsecret_os_crypt_password_v1", "application": a.secretAttr},
		{"xdg:schema": "org.qt.keychain", "server": a.kwalletFolder(), "user": a.kwalletEntry()},
	}
}

// SystemPasswords reads a "Safe Storage" key from the session bus. Electron
// and Chromium pick their backend from the desktop session (KWallet on KDE,
// libsecret elsewhere) and KWallet is not reachable over
// org.freedesktop.secrets, so both are asked and every key found is
// returned. The zero value reads the Slack desktop app's key; App names a
// browser's key instead.
type SystemPasswords struct {
	App keyringApp
}

func (p SystemPasswords) Passwords(ctx context.Context) ([][]byte, error) {
	app := p.App.resolved()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, ErrNoKeyring
	}
	defer conn.Close()
	return gather(ctx, []attempt{
		func(ctx context.Context) ([][]byte, error) { return secretServicePasswords(ctx, conn, app) },
		func(ctx context.Context) ([][]byte, error) { return kwalletPasswords(ctx, conn, app) },
	})
}

// attempt is one place the key may be: a keyring, a query, an item.
type attempt func(context.Context) ([][]byte, error)

func failed(err error) attempt {
	return func(context.Context) ([][]byte, error) { return nil, err }
}

// single turns one key into an attempt's result; an empty key would only
// fail later, at decrypt time, as invalid_auth.
func single(key []byte, err error) ([][]byte, error) {
	if err == nil && len(key) == 0 {
		err = ErrSecretNotFound
	}
	if err != nil {
		return nil, err
	}
	return [][]byte{key}, nil
}

// gather runs every attempt and returns all the keys found. When there is
// none it reports the failure the user can act on best.
func gather(ctx context.Context, attempts []attempt) ([][]byte, error) {
	var (
		keys [][]byte
		errs []error
	)
	for _, try := range attempts {
		got, err := try(ctx)
		keys = append(keys, got...)
		errs = append(errs, err)
	}
	if len(keys) > 0 {
		return keys, nil
	}
	return nil, mostActionable(errs)
}

// mostActionable ranks failures: a locked keyring the user can unlock, then
// an unexpected error, then a keyring without the key, then no keyring.
func mostActionable(errs []error) error {
	best := ErrSecretNotFound
	bestRank := -1
	for _, err := range errs {
		if r := rank(err); r > bestRank {
			best, bestRank = err, r
		}
	}
	return best
}

func rank(err error) int {
	switch {
	case errors.Is(err, ErrKeyringLocked):
		return 3
	case errors.Is(err, ErrSecretNotFound):
		return 1
	case errors.Is(err, ErrNoKeyring):
		return 0
	default:
		return 2
	}
}

// items is the part of a Secret Service session the key lookup uses.
type items interface {
	Search(ctx context.Context, attrs map[string]string) (unlocked, locked []dbus.ObjectPath, err error)
	Secret(ctx context.Context, item dbus.ObjectPath) ([]byte, error)
}

func secretServicePasswords(ctx context.Context, conn *dbus.Conn, app keyringApp) ([][]byte, error) {
	session, err := secretservice.Open(ctx, conn)
	if errors.Is(err, secretservice.ErrUnavailable) {
		return nil, ErrNoKeyring
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close(ctx) }()
	return secretServiceKeys(ctx, session, app.secretQueries())
}

// secretServiceKeys reads every unlocked item each query matches. Locked
// matches are only counted: unlocking them would raise a prompt.
func secretServiceKeys(ctx context.Context, s items, secretQueries []map[string]string) ([][]byte, error) {
	queries := make([]attempt, len(secretQueries))
	for i, attrs := range secretQueries {
		queries[i] = func(ctx context.Context) ([][]byte, error) {
			unlocked, locked, err := s.Search(ctx, attrs)
			if err != nil {
				return nil, err
			}
			return gather(ctx, itemAttempts(s, unlocked, locked))
		}
	}
	return gather(ctx, queries)
}

func itemAttempts(s items, unlocked, locked []dbus.ObjectPath) []attempt {
	var attempts []attempt
	for _, item := range unlocked {
		attempts = append(attempts, func(ctx context.Context) ([][]byte, error) { return single(s.Secret(ctx, item)) })
	}
	for range locked {
		attempts = append(attempts, failed(ErrKeyringLocked))
	}
	return attempts
}
