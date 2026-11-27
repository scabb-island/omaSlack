// Package daemon is `omaslack daemon` (task 2.8): the single local source
// of truth that talks to Slack. It runs one sync engine per workspace
// whose credentials are in the keyring and answers the v1 methods
// (api.Methods) and events.subscribe on the private socket.
//
// Workspaces:
//   - Each team in the keyring gets its provider (picked by the kind of
//     its credentials), its SQLite store at <data>/<team>.db, its own rate
//     budget (Slack's tiers, per workspace) and its sync engine, which
//     publishes to the one events hub.
//   - The workspace name and the signed-in user (syncengine.Config.Self)
//     come from Slack's auth.test at start, bounded to 10 s, and are
//     remembered in <data>/<team>.json for when Slack cannot be reached.
//     With nothing remembered, the name is the team ID and Self is zero.
//   - Credentials that cannot be read, or a provider that cannot be built,
//     skip only that workspace; it is logged.
//   - An engine that stops by itself (Slack rejected the credentials) is
//     logged and its workspace stays listed with state auth_expired, still
//     serving its cache. Clients tell the user to sign in again (omaslack
//     auth add) when workspaces.list, hello or a connection.state event
//     says auth_expired.
//
// Notifications (Config.Notifications; tasks 5.1, 5.2): the engines
// publish to the events hub and to a notify.Notifier, which shows one
// desktop notification per new message that passes the rules of
// notify.Decide with the [notifications] settings of Paths.Config. The
// daemon tells it what each connection views (ui.viewing {team, conv,
// threadTS?, focused}; {} clears it; it goes when the connection
// closes) and completes each message from the workspace: its
// conversation, whether the user follows its thread (they started it or
// replied in it, as far as the cache knows) and the names to show. A
// click sends ui.focus to the plugin when "open" is "window" and a client
// said hello as omaslack-plugin; otherwise it starts the TUI
// (Config.LaunchTUI, omarchy-launch-or-focus-tui) and sends ui.focus to
// every omaslack-tui connection, or, when none is connected yet, to the
// first one that subscribes within 30 s.
//
// Hello adds {workspaces: []wire.Workspace, capabilities: {team: []string}}.
//
// Lifecycle (Run):
//   - Start: claim the socket (rpc.ErrSocketInUse if another daemon
//     answers there), read the keyring (a keyring that cannot be opened is
//     fatal), start every workspace, then serve. Clients that connect
//     meanwhile wait in the listen backlog.
//   - Reload (Config.Reload; SIGHUP in `omaslack daemon`): read the
//     notification settings again (a bad file is logged and the previous
//     settings stay) and the keyring; stop the workspaces removed, restart the ones whose
//     credentials changed or whose engine stopped, start the ones added.
//     So `omaslack auth add|remove` followed by `systemctl --user reload
//     omaslack` (or a restart) takes effect. Calls running on a workspace
//     while it restarts may fail.
//   - Shutdown (ctx ends; SIGINT or SIGTERM): stop accepting and close
//     every connection, then stop every engine and close its store, all
//     within Config.ShutdownTimeout (ErrShutdownTimeout past it).
package daemon
