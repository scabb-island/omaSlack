// Package notify is the daemon's notifier (spec notifications-presence;
// tasks 5.1 and 5.2): it turns new messages into one Freedesktop desktop
// notification each, whatever number of clients is connected, and turns
// a click on one into ui.focus.
//
// Which messages notify is decided by Decide, a pure function of the
// user's Config ($XDG_CONFIG_HOME/omaslack/config.toml, [notifications])
// and of what the daemon knows when the message arrives. The Notifier
// keeps that knowledge (when the daemon started, each workspace's Do Not
// Disturb, what each connection reports it is viewing) and sends through
// a Sender, the session bus in the daemon (Bus) and a fake in tests.
package notify
