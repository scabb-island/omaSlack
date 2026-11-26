// Package daemon is `omaslack daemon` (task 2.8): the single local source
// of truth that talks to Slack. It runs one sync engine per workspace
// whose credentials are in the keyring and answers the v1 methods and
// events.subscribe on the private socket.
package daemon
