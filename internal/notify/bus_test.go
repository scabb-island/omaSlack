package notify

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"gitlab.com/scabb-island/omaslack/internal/dbustest"
)

// events records what the bus reports back.
type events struct{ clicked, closed chan uint32 }

func (e events) Clicked(id uint32) { e.clicked <- id }
func (e events) Closed(id uint32)  { e.closed <- id }

func TestBusNotifyAndSignals(t *testing.T) {
	addr := dbustest.StartBus(t)
	srv := dbustest.ServeNotifications(t, addr)
	ev := events{clicked: make(chan uint32, 4), closed: make(chan uint32, 4)}
	bus := NewBus(func() (*dbus.Conn, error) { return dbus.Connect(addr) }, ev, nil)
	t.Cleanup(func() { _ = bus.Close() })

	id, err := bus.Notify(context.Background(), Notification{Summary: "Alice", Body: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	got := <-srv.Sent()
	if got.ID != id || got.AppName != "omaslack" || got.Summary != "Alice" || got.Body != "hi" {
		t.Errorf("sent %+v as %d", got, id)
	}
	if len(got.Actions) != 2 || got.Actions[0] != "default" {
		t.Errorf("actions %v, want the default action", got.Actions)
	}
	if got.Hints["desktop-entry"].Value() != "omaslack" {
		t.Errorf("hints %v", got.Hints)
	}

	srv.Invoke(t, id, "other")
	srv.Invoke(t, id, "default")
	srv.Close(t, id)
	if c := wait(t, ev.clicked); c != id {
		t.Errorf("clicked %d", c)
	}
	if c := wait(t, ev.closed); c != id {
		t.Errorf("closed %d", c)
	}
	select {
	case c := <-ev.clicked:
		t.Errorf("another action counted as a click: %d", c)
	default:
	}
}

func TestBusReconnects(t *testing.T) {
	addr := dbustest.StartBus(t)
	srv := dbustest.ServeNotifications(t, addr)
	var conn *dbus.Conn
	connects := 0
	bus := NewBus(func() (*dbus.Conn, error) {
		connects++
		var err error
		conn, err = dbus.Connect(addr)
		return conn, err
	}, nil, nil)
	t.Cleanup(func() { _ = bus.Close() })
	if _, err := bus.Notify(context.Background(), Notification{Summary: "one"}); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close() // the bus went away
	if _, err := bus.Notify(context.Background(), Notification{Summary: "two"}); err != nil {
		t.Fatal(err)
	}
	<-srv.Sent()
	if (<-srv.Sent()).Summary != "two" || connects != 2 {
		t.Errorf("connected %d times", connects)
	}
}

func TestBusWithoutService(t *testing.T) {
	addr := dbustest.StartBus(t)
	bus := NewBus(func() (*dbus.Conn, error) { return dbus.Connect(addr) }, nil, nil)
	t.Cleanup(func() { _ = bus.Close() })
	if _, err := bus.Notify(context.Background(), Notification{}); err == nil {
		t.Error("no notification server, no error")
	}
	down := NewBus(func() (*dbus.Conn, error) { return nil, errors.New("no session bus") }, nil, nil)
	if _, err := down.Notify(context.Background(), Notification{}); err == nil {
		t.Error("no bus, no error")
	}
	if err := down.Close(); err != nil {
		t.Error(err)
	}
}

func wait(t *testing.T, ch <-chan uint32) uint32 {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("no signal")
		return 0
	}
}
