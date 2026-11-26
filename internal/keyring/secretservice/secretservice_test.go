package secretservice

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/godbus/dbus/v5"

	"gitlab.com/scabb-island/omaslack/internal/dbustest"
)

func serve(t *testing.T, svc *dbustest.SecretService) *dbus.Conn {
	t.Helper()
	addr := dbustest.StartBus(t)
	dbustest.ServeSecretService(t, addr, svc)
	return dbustest.Connect(t, addr)
}

func open(t *testing.T, conn *dbus.Conn) *Session {
	t.Helper()
	s, err := Open(context.Background(), conn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

// Nobody owns org.freedesktop.secrets and nothing can activate it.
func TestOpenReportsNoProvider(t *testing.T) {
	conn := dbustest.Connect(t, dbustest.StartBus(t))
	if _, err := Open(context.Background(), conn); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Open error = %v, want ErrUnavailable", err)
	}
}

// A provider that is there but refuses the session is not "no Secret
// Service": the bus error reaches the caller.
func TestOpenKeepsAProviderRefusal(t *testing.T) {
	svc := &dbustest.SecretService{OpenErr: dbus.NewError("org.freedesktop.DBus.Error.NotSupported", []any{"no plain"})}
	_, err := Open(context.Background(), serve(t, svc))
	var dbusErr dbus.Error
	if errors.Is(err, ErrUnavailable) || !errors.As(err, &dbusErr) || dbusErr.Name != "org.freedesktop.DBus.Error.NotSupported" {
		t.Fatalf("Open error = %#v, want NotSupported from the bus", err)
	}
}

func TestStoreFindReadAndDeleteAnItem(t *testing.T) {
	ctx := context.Background()
	svc := &dbustest.SecretService{}
	s := open(t, serve(t, svc))
	attrs := map[string]string{"service": "omaslack", "team": "T1A"}

	if err := s.Create(ctx, "omaslack T1A", attrs, []byte(`{"a":1}`), "application/json"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Create(ctx, "omaslack T1A", attrs, []byte(`{"a":2}`), "application/json"); err != nil {
		t.Fatalf("Create again: %v", err)
	}
	unlocked, locked, err := s.Search(ctx, map[string]string{"service": "omaslack"})
	if err != nil || len(unlocked) != 1 || len(locked) != 0 {
		t.Fatalf("Search = %v, %v, %v; want one unlocked item", unlocked, locked, err)
	}
	secret, err := s.Secret(ctx, unlocked[0])
	if err != nil || string(secret) != `{"a":2}` {
		t.Fatalf("Secret = %s, %v; want the replacement", secret, err)
	}
	got, err := s.Attributes(ctx, unlocked[0])
	if err != nil || !reflect.DeepEqual(got, attrs) {
		t.Fatalf("Attributes = %v, %v; want %v", got, err, attrs)
	}
	if err := s.Delete(ctx, unlocked[0]); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if unlocked, _, _ := s.Search(ctx, attrs); len(unlocked) != 0 {
		t.Fatalf("Search after Delete = %v", unlocked)
	}
	if err := s.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	want := []string{
		"OpenSession plain",
		"CreateItem replace=true",
		"CreateItem replace=true",
		"SearchItems service=omaslack",
		"Item.GetSecret /org/freedesktop/secrets/session/1",
		"Item.Get Attributes",
		"Item.Delete",
		"SearchItems service=omaslack,team=T1A",
		"Session.Close",
	}
	if calls := svc.Calls(); !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %q\nwant    %q", calls, want)
	}
}

// Unlocking raises a prompt; omaslack does not drive prompts, so a locked
// keyring is reported instead.
func TestLockedKeyringIsReported(t *testing.T) {
	ctx := context.Background()
	svc := &dbustest.SecretService{Locked: true}
	item := svc.Add(map[string]string{"service": "omaslack"}, []byte("x"))
	s := open(t, serve(t, svc))

	if err := s.Create(ctx, "label", map[string]string{"service": "omaslack"}, []byte("x"), "text/plain"); !errors.Is(err, ErrLocked) {
		t.Errorf("Create error = %v, want ErrLocked", err)
	}
	if err := s.Delete(ctx, item); !errors.Is(err, ErrLocked) {
		t.Errorf("Delete error = %v, want ErrLocked", err)
	}
	unlocked, locked, err := s.Search(ctx, map[string]string{"service": "omaslack"})
	if err != nil || len(unlocked) != 0 || !reflect.DeepEqual(locked, []dbus.ObjectPath{item}) {
		t.Errorf("Search = %v, %v, %v; want the item as locked", unlocked, locked, err)
	}
}

func TestCallsOnAMissingItemFail(t *testing.T) {
	ctx := context.Background()
	s := open(t, serve(t, &dbustest.SecretService{}))
	missing := dbus.ObjectPath("/org/freedesktop/secrets/collection/login/404")
	if _, err := s.Secret(ctx, missing); err == nil {
		t.Error("Secret of a missing item succeeded")
	}
	if _, err := s.Attributes(ctx, missing); err == nil {
		t.Error("Attributes of a missing item succeeded")
	}
	if err := s.Delete(ctx, missing); err == nil {
		t.Error("Delete of a missing item succeeded")
	}
}

func TestAbsent(t *testing.T) {
	for _, tt := range []struct {
		err  error
		want bool
	}{
		{dbus.Error{Name: "org.freedesktop.DBus.Error.ServiceUnknown"}, true},
		{dbus.Error{Name: "org.freedesktop.DBus.Error.NameHasNoOwner"}, true},
		{dbus.Error{Name: "org.freedesktop.DBus.Error.Spawn.ChildExited"}, true},
		{dbus.Error{Name: "org.freedesktop.DBus.Error.NotSupported"}, false},
		{errors.New("connection reset"), false},
	} {
		if got := absent(tt.err); got != tt.want {
			t.Errorf("absent(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}
