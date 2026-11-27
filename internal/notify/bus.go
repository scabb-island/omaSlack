package notify

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/godbus/dbus/v5"
)

const (
	busName  = "org.freedesktop.Notifications"
	busPath  = dbus.ObjectPath("/org/freedesktop/Notifications")
	busIface = "org.freedesktop.Notifications"

	// appName is the application name the notification server shows and
	// filters on; desktopEntry names omaslack.desktop.
	appName      = "omaslack"
	desktopEntry = "omaslack"
	// defaultAction is the action a click on the notification invokes.
	defaultAction = "default"
)

// Events is told what happens to the notifications a Bus showed; the
// Notifier is one.
type Events interface {
	Clicked(id uint32)
	Closed(id uint32)
}

// Bus is a Sender over org.freedesktop.Notifications on the session bus.
// It connects on the first notification, and again on the next one after
// the connection broke, so a daemon started before the desktop session
// still notifies once it is up.
type Bus struct {
	connect func() (*dbus.Conn, error)
	events  Events
	log     *slog.Logger

	mu   sync.Mutex
	conn *dbus.Conn
}

// NewBus returns a Bus that connects with connect (dbus.ConnectSessionBus
// in the daemon) and reports clicks and closes to events (nil: nobody).
func NewBus(connect func() (*dbus.Conn, error), events Events, log *slog.Logger) *Bus {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Bus{connect: connect, events: events, log: log}
}

// Notify shows n with omaslack's name and a default action, and returns
// its ID. A call that fails on a broken connection is tried once more on
// a new one.
func (b *Bus) Notify(ctx context.Context, n Notification) (uint32, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id, err := b.notify(ctx, n)
	if errors.Is(err, errBroken) {
		id, err = b.notify(ctx, n)
	}
	return id, err
}

// errBroken marks a call that failed because the connection is gone.
var errBroken = errors.New("notify: the session bus connection broke")

func (b *Bus) notify(ctx context.Context, n Notification) (uint32, error) {
	conn, err := b.connected()
	if err != nil {
		return 0, err
	}
	var id uint32
	err = conn.Object(busName, busPath).CallWithContext(ctx, busIface+".Notify", 0,
		appName, uint32(0), "", n.Summary, n.Body, []string{defaultAction, "Open"},
		map[string]dbus.Variant{"desktop-entry": dbus.MakeVariant(desktopEntry)}, int32(-1)).Store(&id)
	if err != nil && !conn.Connected() {
		b.conn = nil
		return 0, errors.Join(errBroken, err)
	}
	return id, err
}

// connected returns the open connection, opening one (and listening to
// its signals) when there is none or it broke. b.mu is held.
func (b *Bus) connected() (*dbus.Conn, error) {
	if b.conn != nil && b.conn.Connected() {
		return b.conn, nil
	}
	conn, err := b.connect()
	if err != nil {
		return nil, err
	}
	if err := conn.AddMatchSignal(dbus.WithMatchObjectPath(busPath), dbus.WithMatchInterface(busIface)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	signals := make(chan *dbus.Signal, 16)
	conn.Signal(signals)
	go b.listen(signals)
	b.conn = conn
	return conn, nil
}

// listen reports the notification signals until the connection closes,
// which closes signals.
func (b *Bus) listen(signals <-chan *dbus.Signal) {
	for sig := range signals {
		if b.events != nil {
			b.dispatch(sig)
		}
	}
}

func (b *Bus) dispatch(sig *dbus.Signal) {
	switch sig.Name {
	case busIface + ".ActionInvoked":
		var id uint32
		var action string
		if dbus.Store(sig.Body, &id, &action) == nil && action == defaultAction {
			b.events.Clicked(id)
		}
	case busIface + ".NotificationClosed":
		var id, reason uint32
		if dbus.Store(sig.Body, &id, &reason) == nil {
			b.events.Closed(id)
		}
	}
}

// Close closes the connection, if one is open.
func (b *Bus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn == nil {
		return nil
	}
	err := b.conn.Close()
	b.conn = nil
	return err
}
