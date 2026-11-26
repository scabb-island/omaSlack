package dbustest

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"

	"gitlab.com/scabb-island/omaslack/internal/keyring/secretservice"
)

func TestStartBusServesAPrivateBus(t *testing.T) {
	addr := StartBus(t)
	if !strings.HasPrefix(addr, "unix:") {
		t.Fatalf("address = %q, want a unix socket", addr)
	}
	conn := Connect(t, addr)
	var names []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		t.Fatalf("ListNames: %v", err)
	}
	for _, n := range names {
		if n == "org.freedesktop.secrets" || strings.HasPrefix(n, "org.kde.") {
			t.Errorf("the private bus has %s on it", n)
		}
	}
}

// Skipping is only for machines without dbus-daemon; where it is installed
// a skip would silently drop every D-Bus test.
func TestStartBusDoesNotSkipWhereTheDaemonIsInstalled(t *testing.T) {
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("dbus-daemon not installed")
	}
	started := false
	t.Run("start", func(t *testing.T) {
		StartBus(t)
		started = true
	})
	if !started {
		t.Error("StartBus skipped although dbus-daemon is installed")
	}
}

func TestStartDaemonReportsADaemonThatDoesNotRun(t *testing.T) {
	if _, err := startDaemon(t, filepath.Join(t.TempDir(), "no-such-daemon")); err == nil {
		t.Error("a missing binary started")
	}
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no true binary")
	}
	if _, err := startDaemon(t, truePath); err == nil {
		t.Error("a daemon that printed no address was accepted")
	}
}

func TestServeTakesTheNameOnce(t *testing.T) {
	addr := StartBus(t)
	Serve(t, addr, "org.example.Test")
	if err := serve(Connect(t, addr), "org.example.Test"); err == nil {
		t.Error("a second owner took the name")
	}
	bad := Object{Value: struct{}{}, Path: "not a path", Interface: "org.example.Test"}
	if err := serve(Connect(t, addr), "org.example.Other", bad); err == nil {
		t.Error("an object with an invalid path was exported")
	}
}

func openSession(t *testing.T, svc *SecretService) (*secretservice.Session, *dbus.Conn) {
	t.Helper()
	addr := StartBus(t)
	ServeSecretService(t, addr, svc)
	conn := Connect(t, addr)
	s, err := secretservice.Open(context.Background(), conn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, conn
}

func TestSecretServiceStoresItems(t *testing.T) {
	ctx := context.Background()
	svc := &SecretService{}
	seeded := svc.Add(map[string]string{"app": "seed"}, []byte("seeded"))
	s, _ := openSession(t, svc)
	attrs := map[string]string{"app": "x", "team": "T1"}

	for _, v := range []string{"one", "two"} {
		if err := s.Create(ctx, "label", attrs, []byte(v), "text/plain"); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	if err := s.Create(ctx, "label", map[string]string{"app": "x", "team": "T2"}, []byte("other"), "text/plain"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	unlocked, locked, err := s.Search(ctx, attrs)
	if err != nil || len(unlocked) != 1 || len(locked) != 0 {
		t.Fatalf("Search = %v, %v, %v; want the one replacement", unlocked, locked, err)
	}
	if got, err := s.Secret(ctx, unlocked[0]); err != nil || string(got) != "two" {
		t.Errorf("Secret = %q, %v; want the replacement", got, err)
	}
	if got, err := s.Attributes(ctx, unlocked[0]); err != nil || !reflect.DeepEqual(got, attrs) {
		t.Errorf("Attributes = %v, %v", got, err)
	}
	if got, err := s.Secret(ctx, seeded); err != nil || string(got) != "seeded" {
		t.Errorf("Secret of an item added before serving = %q, %v", got, err)
	}
	if all, _, _ := s.Search(ctx, map[string]string{"app": "x"}); len(all) != 2 {
		t.Errorf("Search by a subset of attributes found %d items, want 2", len(all))
	}

	if err := s.Delete(ctx, unlocked[0]); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Secret(ctx, unlocked[0]); err == nil {
		t.Error("Secret of a deleted item succeeded")
	}
	if err := s.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	wantItems := []map[string]string{{"app": "seed"}, {"app": "x", "team": "T2"}}
	items := svc.Items()
	if !reflect.DeepEqual(items, wantItems) {
		t.Errorf("Items() = %v, want %v", items, wantItems)
	}
	wantCalls := []string{
		"OpenSession plain",
		"CreateItem replace=true",
		"CreateItem replace=true",
		"CreateItem replace=true",
		"SearchItems app=x,team=T1",
		"Item.GetSecret /org/freedesktop/secrets/session/1",
		"Item.Get Attributes",
		"Item.GetSecret /org/freedesktop/secrets/session/1",
		"SearchItems app=x",
		"Item.Delete",
		"Item.GetSecret /org/freedesktop/secrets/session/1",
		"Session.Close",
	}
	if calls := svc.Calls(); !reflect.DeepEqual(calls, wantCalls) {
		t.Errorf("Calls() = %q\nwant      %q", calls, wantCalls)
	}
}

// CreateItem with replace=false keeps the older item.
func TestSecretServiceCreateWithoutReplace(t *testing.T) {
	svc := &SecretService{}
	_, conn := openSession(t, svc)
	attrs := map[string]string{"app": "x"}
	props := map[string]dbus.Variant{"org.freedesktop.Secret.Item.Attributes": dbus.MakeVariant(attrs)}
	collection := conn.Object("org.freedesktop.secrets", "/org/freedesktop/secrets/aliases/default")
	for range 2 {
		var item, prompt dbus.ObjectPath
		call := collection.Call("org.freedesktop.Secret.Collection.CreateItem", 0, props, Secret{Session: "/", Value: []byte("v")}, false)
		if err := call.Store(&item, &prompt); err != nil || prompt != "/" {
			t.Fatalf("CreateItem = %s, %s, %v", item, prompt, err)
		}
	}
	if n := len(svc.Items()); n != 2 {
		t.Errorf("%d items, want both kept", n)
	}
}

func TestSecretServiceLockedAsksForAPrompt(t *testing.T) {
	ctx := context.Background()
	svc := &SecretService{Locked: true}
	item := svc.Add(map[string]string{"app": "x"}, []byte("v"))
	s, _ := openSession(t, svc)
	if err := s.Create(ctx, "label", map[string]string{"app": "y"}, []byte("v"), "text/plain"); !errors.Is(err, secretservice.ErrLocked) {
		t.Errorf("Create error = %v, want ErrLocked", err)
	}
	if err := s.Delete(ctx, item); !errors.Is(err, secretservice.ErrLocked) {
		t.Errorf("Delete error = %v, want ErrLocked", err)
	}
	unlocked, locked, err := s.Search(ctx, map[string]string{"app": "x"})
	if err != nil || len(unlocked) != 0 || !reflect.DeepEqual(locked, []dbus.ObjectPath{item}) {
		t.Errorf("Search = %v, %v, %v; want the item locked", unlocked, locked, err)
	}
	if n := len(svc.Items()); n != 1 {
		t.Errorf("%d items after a locked create and delete, want 1", n)
	}
}

func TestSecretServiceRefusesWhatItIsToldTo(t *testing.T) {
	addr := StartBus(t)
	ServeSecretService(t, addr, &SecretService{OpenErr: dbus.NewError("org.freedesktop.DBus.Error.NotSupported", nil)})
	_, err := secretservice.Open(context.Background(), Connect(t, addr))
	var dbusErr dbus.Error
	if !errors.As(err, &dbusErr) || dbusErr.Name != "org.freedesktop.DBus.Error.NotSupported" {
		t.Errorf("Open error = %v, want NotSupported", err)
	}
}

// Only the spec's Attributes property is served.
func TestSecretServiceServesOnlyTheAttributesProperty(t *testing.T) {
	svc := &SecretService{}
	item := svc.Add(map[string]string{"app": "x"}, []byte("v"))
	_, conn := openSession(t, svc)
	obj := conn.Object("org.freedesktop.secrets", item)
	for _, prop := range [][2]string{
		{"org.freedesktop.Secret.Item", "Type"},
		{"org.example.Other", "Attributes"},
	} {
		var v dbus.Variant
		err := obj.Call("org.freedesktop.DBus.Properties.Get", 0, prop[0], prop[1]).Store(&v)
		var dbusErr dbus.Error
		if !errors.As(err, &dbusErr) || dbusErr.Name != "org.freedesktop.DBus.Error.UnknownProperty" {
			t.Errorf("Get %s.%s error = %v, want UnknownProperty", prop[0], prop[1], err)
		}
	}
}
