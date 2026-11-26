// Package secretservice is a small client for the freedesktop Secret
// Service (org.freedesktop.secrets) over D-Bus. It calls only methods and
// properties in the spec, so spec-only providers such as oo7-daemon work as
// well as gnome-keyring and KeePassXC. It does not drive unlock prompts: a
// locked keyring is reported as ErrLocked.
//
// Adapted from github.com/gammons/slk internal/slackdesktop/password_linux.go (MIT, Copyright (c) 2026 Grant Ammons).
package secretservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

var (
	// ErrUnavailable reports that nothing provides org.freedesktop.secrets
	// on the bus.
	ErrUnavailable = errors.New("no Secret Service on the session bus")
	// ErrLocked reports a keyring that needs an unlock prompt first.
	ErrLocked = errors.New("the keyring is locked: unlock it and try again")
)

const (
	busName       = "org.freedesktop.secrets"
	servicePath   = dbus.ObjectPath("/org/freedesktop/secrets")
	defaultAlias  = dbus.ObjectPath("/org/freedesktop/secrets/aliases/default")
	serviceIface  = "org.freedesktop.Secret.Service"
	sessionIface  = "org.freedesktop.Secret.Session"
	itemIface     = "org.freedesktop.Secret.Item"
	collectIface  = "org.freedesktop.Secret.Collection"
	noPrompt      = dbus.ObjectPath("/")
	attributesKey = itemIface + ".Attributes"

	// A provider can block on an unlock prompt; no call waits longer.
	callTimeout = 10 * time.Second
)

// secret is the Secret Service "Secret" struct, signature (oayays).
type secret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

// Session is an open session with the "plain" algorithm: secrets cross the
// bus unencrypted, which the bus's own access control already allows.
type Session struct {
	conn *dbus.Conn
	path dbus.ObjectPath
}

// Open opens a session on conn. When no provider is on the bus, the error
// wraps ErrUnavailable; a provider that refuses keeps its bus error.
func Open(ctx context.Context, conn *dbus.Conn) (*Session, error) {
	s := &Session{conn: conn}
	var output dbus.Variant
	err := call(ctx, s.service(), serviceIface+".OpenSession",
		[]any{&output, &s.path}, "plain", dbus.MakeVariant(""))
	switch {
	case err == nil:
		return s, nil
	case absent(err):
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	default:
		return nil, fmt.Errorf("secret service: %w", err)
	}
}

// absent reports whether err says nobody provides org.freedesktop.secrets:
// no owner and nothing to activate, or an activation that failed. The bus's
// error name is the signal; its text varies between bus implementations.
func absent(err error) bool {
	var dbusErr dbus.Error
	if !errors.As(err, &dbusErr) {
		return false
	}
	switch dbusErr.Name {
	case "org.freedesktop.DBus.Error.ServiceUnknown",
		"org.freedesktop.DBus.Error.NameHasNoOwner":
		return true
	}
	return strings.HasPrefix(dbusErr.Name, "org.freedesktop.DBus.Error.Spawn.")
}

// Close ends the session.
func (s *Session) Close(ctx context.Context) error {
	return call(ctx, s.conn.Object(busName, s.path), sessionIface+".Close", nil)
}

// Search lists the items whose attributes include attrs, split into
// unlocked ones and locked ones.
func (s *Session) Search(ctx context.Context, attrs map[string]string) (unlocked, locked []dbus.ObjectPath, err error) {
	err = call(ctx, s.service(), serviceIface+".SearchItems", []any{&unlocked, &locked}, attrs)
	return unlocked, locked, err
}

// Secret reads an unlocked item's secret.
func (s *Session) Secret(ctx context.Context, item dbus.ObjectPath) ([]byte, error) {
	var out secret
	err := call(ctx, s.conn.Object(busName, item), itemIface+".GetSecret", []any{&out}, s.path)
	return out.Value, err
}

// Attributes reads an item's lookup attributes.
func (s *Session) Attributes(ctx context.Context, item dbus.ObjectPath) (map[string]string, error) {
	var attrs map[string]string
	err := call(ctx, s.conn.Object(busName, item), "org.freedesktop.DBus.Properties.Get",
		[]any{&attrs}, itemIface, "Attributes")
	return attrs, err
}

// Create stores value in the default collection under attrs, replacing any
// item with the same attributes.
func (s *Session) Create(ctx context.Context, label string, attrs map[string]string, value []byte, contentType string) error {
	props := map[string]dbus.Variant{
		itemIface + ".Label": dbus.MakeVariant(label),
		attributesKey:        dbus.MakeVariant(attrs),
	}
	var item, prompt dbus.ObjectPath
	err := call(ctx, s.conn.Object(busName, defaultAlias), collectIface+".CreateItem",
		[]any{&item, &prompt}, props, secret{Session: s.path, Value: value, ContentType: contentType}, true)
	return promptless(prompt, err)
}

// Delete removes an item.
func (s *Session) Delete(ctx context.Context, item dbus.ObjectPath) error {
	var prompt dbus.ObjectPath
	err := call(ctx, s.conn.Object(busName, item), itemIface+".Delete", []any{&prompt})
	return promptless(prompt, err)
}

// promptless turns a call that needs an unlock prompt into ErrLocked.
func promptless(prompt dbus.ObjectPath, err error) error {
	if err == nil && prompt != noPrompt {
		return ErrLocked
	}
	return err
}

func (s *Session) service() dbus.BusObject {
	return s.conn.Object(busName, servicePath)
}

func call(ctx context.Context, obj dbus.BusObject, method string, out []any, args ...any) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	c := obj.CallWithContext(ctx, method, 0, args...)
	if c.Err != nil {
		return c.Err
	}
	return c.Store(out...)
}
