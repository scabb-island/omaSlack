package dbustest

import (
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"
)

const (
	notificationsName  = "org.freedesktop.Notifications"
	notificationsPath  = dbus.ObjectPath("/org/freedesktop/Notifications")
	notificationsIface = "org.freedesktop.Notifications"
)

// Notification is one Notify call as the fake received it.
type Notification struct {
	ID      uint32
	AppName string
	Summary string
	Body    string
	Actions []string // key, label, key, label…
	Hints   map[string]dbus.Variant
	Expire  int32
}

// Notifications is an in-memory org.freedesktop.Notifications that serves
// Notify and emits ActionInvoked and NotificationClosed on request.
type Notifications struct {
	conn *dbus.Conn

	mu   sync.Mutex
	last uint32 // the last ID given
	sent chan Notification
}

// ServeNotifications serves a fake notification server on the bus at
// addr under the org.freedesktop.Notifications name.
func ServeNotifications(t *testing.T, addr string) *Notifications {
	t.Helper()
	n := &Notifications{sent: make(chan Notification, 64)}
	n.conn = Serve(t, addr, notificationsName, Object{Value: n, Path: notificationsPath, Interface: notificationsIface})
	return n
}

// Notify is the spec method; IDs count from 1.
func (n *Notifications) Notify(appName string, _ uint32, _, summary, body string, actions []string,
	hints map[string]dbus.Variant, expire int32) (uint32, *dbus.Error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.last++
	got := Notification{
		ID: n.last, AppName: appName, Summary: summary, Body: body,
		Actions: actions, Hints: hints, Expire: expire,
	}
	n.sent <- got
	return got.ID, nil
}

// Sent is where each notification arrives as it is shown.
func (n *Notifications) Sent() <-chan Notification { return n.sent }

// Invoke emits ActionInvoked for notification id, as a click does.
func (n *Notifications) Invoke(t *testing.T, id uint32, action string) {
	t.Helper()
	if err := n.conn.Emit(notificationsPath, notificationsIface+".ActionInvoked", id, action); err != nil {
		t.Fatal(err)
	}
}

// Close emits NotificationClosed for notification id (reason 2: dismissed).
func (n *Notifications) Close(t *testing.T, id uint32) {
	t.Helper()
	if err := n.conn.Emit(notificationsPath, notificationsIface+".NotificationClosed", id, uint32(2)); err != nil {
		t.Fatal(err)
	}
}
