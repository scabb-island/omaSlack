package dbustest

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"
)

// Secret is the Secret Service "Secret" struct, signature (oayays).
type Secret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

const (
	secretsName    = "org.freedesktop.secrets"
	secretsPath    = dbus.ObjectPath("/org/freedesktop/secrets")
	defaultAlias   = dbus.ObjectPath("/org/freedesktop/secrets/aliases/default")
	sessionPath    = dbus.ObjectPath("/org/freedesktop/secrets/session/1")
	promptPath     = dbus.ObjectPath("/org/freedesktop/secrets/prompt/1")
	itemPathPrefix = "/org/freedesktop/secrets/collection/login/"
)

// SecretService is an in-memory org.freedesktop.secrets that serves only
// the spec methods omaslack uses. Anything else gets godbus's "unknown
// method" error, so a client that works against it stays inside the spec.
type SecretService struct {
	// Locked makes every item locked: searches list them as locked, and
	// creating or deleting one answers with a prompt.
	Locked bool
	// OpenErr, when set, is how OpenSession refuses.
	OpenErr *dbus.Error

	conn *dbus.Conn

	// godbus runs each method on its own goroutine.
	mu    sync.Mutex
	items map[dbus.ObjectPath]*secretItem
	next  int
	calls []string
}

type secretItem struct {
	svc    *SecretService
	path   dbus.ObjectPath
	attrs  map[string]string
	secret Secret
}

// ServeSecretService serves svc on the bus at addr under the
// org.freedesktop.secrets name.
func ServeSecretService(t *testing.T, addr string, svc *SecretService) {
	t.Helper()
	svc.mu.Lock()
	defer svc.mu.Unlock()
	svc.init()
	svc.conn = Serve(t, addr, secretsName,
		Object{svc, secretsPath, "org.freedesktop.Secret.Service"},
		Object{secretCollection{svc}, defaultAlias, "org.freedesktop.Secret.Collection"},
		Object{secretSession{svc}, sessionPath, "org.freedesktop.Secret.Session"},
	)
	for _, it := range svc.items {
		svc.export(it)
	}
}

func (s *SecretService) init() {
	if s.items == nil {
		s.items = map[dbus.ObjectPath]*secretItem{}
	}
}

// Add stores an item as if another client had created it. It may be
// called before or after ServeSecretService.
func (s *SecretService) Add(attrs map[string]string, value []byte) dbus.ObjectPath {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	return s.store(attrs, Secret{Value: value, ContentType: "text/plain"})
}

// Calls lists the methods called so far, in order, with their key argument.
func (s *SecretService) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

// Items returns every stored item's attributes, for assertions.
func (s *SecretService) Items() []map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]string
	for _, it := range s.items {
		out = append(out, maps.Clone(it.attrs))
	}
	return out
}

func (s *SecretService) record(format string, args ...any) {
	s.calls = append(s.calls, fmt.Sprintf(format, args...))
}

// store must be called with mu held.
func (s *SecretService) store(attrs map[string]string, secret Secret) dbus.ObjectPath {
	s.next++
	it := &secretItem{svc: s, path: dbus.ObjectPath(fmt.Sprintf("%s%d", itemPathPrefix, s.next)), attrs: attrs, secret: secret}
	s.items[it.path] = it
	if s.conn != nil {
		s.export(it)
	}
	return it.path
}

func (s *SecretService) export(it *secretItem) {
	_ = s.conn.Export(it, it.path, "org.freedesktop.Secret.Item")
	_ = s.conn.Export(itemProperties{it}, it.path, "org.freedesktop.DBus.Properties")
}

// match must be called with mu held.
func (s *SecretService) match(attrs map[string]string) []dbus.ObjectPath {
	var out []dbus.ObjectPath
	for path, it := range s.items {
		if containsAll(it.attrs, attrs) {
			out = append(out, path)
		}
	}
	return out
}

func containsAll(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

func (s *SecretService) OpenSession(algorithm string, _ dbus.Variant) (dbus.Variant, dbus.ObjectPath, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record("OpenSession %s", algorithm)
	if s.OpenErr != nil {
		return dbus.MakeVariant(""), "/", s.OpenErr
	}
	return dbus.MakeVariant(""), sessionPath, nil
}

func (s *SecretService) SearchItems(attrs map[string]string) ([]dbus.ObjectPath, []dbus.ObjectPath, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record("SearchItems %s", formatAttrs(attrs))
	found := s.match(attrs)
	if s.Locked {
		return []dbus.ObjectPath{}, found, nil
	}
	return found, []dbus.ObjectPath{}, nil
}

func formatAttrs(attrs map[string]string) string {
	parts := make([]string, 0, len(attrs))
	for k, v := range attrs {
		parts = append(parts, k+"="+v)
	}
	slices.Sort(parts)
	return strings.Join(parts, ",")
}

type secretCollection struct{ svc *SecretService }

func (c secretCollection) CreateItem(props map[string]dbus.Variant, secret Secret, replace bool) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	s := c.svc
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record("CreateItem replace=%t", replace)
	if s.Locked {
		return "/", promptPath, nil
	}
	attrs, _ := props["org.freedesktop.Secret.Item.Attributes"].Value().(map[string]string)
	if replace {
		for _, path := range s.match(attrs) {
			delete(s.items, path)
		}
	}
	return s.store(attrs, secret), "/", nil
}

type secretSession struct{ svc *SecretService }

func (ss secretSession) Close() *dbus.Error {
	ss.svc.mu.Lock()
	defer ss.svc.mu.Unlock()
	ss.svc.record("Session.Close")
	return nil
}

func (it *secretItem) GetSecret(session dbus.ObjectPath) (Secret, *dbus.Error) {
	s := it.svc
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record("Item.GetSecret %s", session)
	if _, ok := s.items[it.path]; !ok {
		return Secret{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownObject", []any{"no such item"})
	}
	secret := it.secret
	secret.Session = session
	return secret, nil
}

func (it *secretItem) Delete() (dbus.ObjectPath, *dbus.Error) {
	s := it.svc
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record("Item.Delete")
	if s.Locked {
		return promptPath, nil
	}
	delete(s.items, it.path)
	return "/", nil
}

type itemProperties struct{ it *secretItem }

func (p itemProperties) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	s := p.it.svc
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record("Item.Get %s", name)
	if iface != "org.freedesktop.Secret.Item" || name != "Attributes" {
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", []any{name})
	}
	return dbus.MakeVariant(p.it.attrs), nil
}
