import QtQuick
import Quickshell
import Quickshell.Io
import "lib/rpc.js" as Rpc
import "lib/store.js" as Store
import "lib/backoff.js" as Backoff

// The one connection between omarchy-shell and the omaslack daemon
// (spec gui-client, "Single Service as the daemon client"). The shell mounts
// this once; the bar widget, the quick-view panel and the window get it with
// shell.serviceFor("ar.scabb.omaslack") and only read its properties and
// call its functions. They never open a connection of their own.
//
// CONTRACT (M4 step 0): the public API below is fixed. The views are built
// against it, and tests/FakeService.qml mirrors it member for member. The
// implementation (task 4.2) lands behind it: a Quickshell.Io Socket with a
// SplitParser (spike M4.1), hello + events.subscribe, request/response
// correlation in lib/rpc.js, state updates in lib/store.js (pure, tested
// with qmltestrunner) and reconnection with lib/backoff.js (1 s to 30 s).
//
// State properties hold plain JS values that are replaced, never mutated in
// place, so bindings on them update. Shapes are the daemon's wire shapes
// (internal/wire): Workspace, Conversation, Message, Summary.
Item {
  id: root

  readonly property string pluginId: "ar.scabb.omaslack"
  readonly property string socketPath: (Quickshell.env("XDG_RUNTIME_DIR") || "/tmp") + "/omaslack.sock"

  // ---- connection ---------------------------------------------------------

  // "unknown" before the first attempt, then "connecting", "connected",
  // "unavailable" (no daemon on the socket; retrying with backoff) or
  // "incompatible" (hello refused our protocol version; not retrying).
  property string connectionState: "unknown"
  readonly property bool daemonAvailable: connectionState === "connected"
  // Why the last attempt failed, for tooltips. Empty when connected.
  property string connectionError: ""

  // ---- state from the daemon ----------------------------------------------

  // [wire.Workspace], in the daemon's order. Each has team, name, self,
  // provider, capabilities and state (the workspace's connection state:
  // "connecting", "connected", "disconnected", "auth_expired").
  property var workspaces: []
  // wire.Summary: { workspaces: { <team>: Counts }, total: Counts }, with
  // Counts = { dms, mentions, channels, threads }.
  property var summary: ({ workspaces: ({}), total: ({ dms: 0, mentions: 0, channels: 0, threads: 0 }) })
  // { <team>: [wire.Conversation] }: every conversation the user is in.
  property var conversations: ({})
  // { <team>: { snoozed: bool, until: epoch seconds } }, as known from this
  // client's own dnd.snooze/dnd.end calls (the daemon sends no DND event).
  property var dnd: ({})
  // Bumped whenever any cached message list changes; bind to it and read
  // messages(team, conv) to stay current.
  property int messagesRevision: 0

  // ---- signals ------------------------------------------------------------

  // Every daemon notification (method and params, as on the wire), emitted
  // after the store has applied it.
  signal daemonEvent(string method, var params)
  // The cached messages of one conversation (or one thread, with threadTS)
  // changed.
  signal messagesUpdated(string team, string conv, string threadTS)
  // ui.focus from any client: someone asked the UI to show this
  // conversation (and message, when ts is set).
  signal focusRequested(string team, string conv, string ts)

  // ---- reading ------------------------------------------------------------

  // The cached messages of a conversation, oldest first ([] when none are
  // loaded). With threadTS, the replies of that thread, root first.
  function messages(team, conv, threadTS) { return [] }
  // Whether older history exists before the oldest cached message.
  function hasMoreHistory(team, conv) { return false }
  // The conversations with unread messages across all workspaces, DMs and
  // mentions first, then by latest activity: [{ team, conversation }].
  function unreadConversations() { return [] }
  // A display name for a user ID; asks users.info once when unknown and
  // returns the ID meanwhile (messagesRevision bumps when it arrives).
  function userName(team, user) { return user }
  // A display name for a conversation: #channel, the person of a DM, or the
  // members of a group DM.
  function conversationName(team, conv) { return conv }

  // ---- calls --------------------------------------------------------------
  // Every callback is function(error, result): error is null or
  // { code, message } (the JSON-RPC error), result the method's result.

  // Any daemon method (internal/api): the escape hatch.
  function call(method, params, callback) {
    if (callback) callback({ code: -32000, message: "not connected" }, null)
  }
  // conversations.open: first page of history plus the draft, cached.
  function openConversation(team, conv, callback) { call("conversations.open", { team: team, conv: conv }, callback) }
  // messages.history before the oldest cached message, merged into the cache.
  function loadOlder(team, conv, callback) { call("messages.history", { team: team, conv: conv }, callback) }
  // messages.replies of a thread, cached under threadTS.
  function loadReplies(team, conv, threadTS, callback) { call("messages.replies", { team: team, conv: conv, threadTS: threadTS }, callback) }
  // messages.send; threadTS empty for the channel itself.
  function send(team, conv, text, threadTS, callback) { call("messages.send", { team: team, conv: conv, text: text, threadTS: threadTS || undefined }, callback) }
  function edit(team, conv, ts, text, callback) { call("messages.edit", { team: team, conv: conv, ts: ts, text: text }, callback) }
  function remove(team, conv, ts, callback) { call("messages.delete", { team: team, conv: conv, ts: ts }, callback) }
  function react(team, conv, ts, emoji, added, callback) { call(added ? "reactions.add" : "reactions.remove", { team: team, conv: conv, ts: ts, emoji: emoji }, callback) }
  // conversations.markRead up to ts (the newest message when empty).
  function markRead(team, conv, ts, callback) { call("conversations.markRead", { team: team, conv: conv, ts: ts || undefined }, callback) }
  function setDraft(team, conv, threadTS, text, callback) { call("drafts.set", { team: team, conv: conv, threadTS: threadTS || undefined, text: text }, callback) }
  function snoozeDND(team, minutes, callback) { call("dnd.snooze", { team: team, minutes: minutes }, callback) }
  function endDND(team, callback) { call("dnd.end", { team: team }, callback) }
  // files.fetch: the local path of a file's bytes, for Image and Qt.openUrlExternally.
  function fetchFile(team, id, callback) { call("files.fetch", { team: team, id: id }, callback) }

  // ---- opening the clients --------------------------------------------------

  // Show the full window (omarchy-shell shell summon), focused on conv when
  // team and conv are set.
  function openWindow(team, conv) {}
  // Open the TUI (omarchy-launch-or-focus-tui) and ask it, through
  // ui.focus, to show conv when team and conv are set.
  function openTui(team, conv) {}
  // Open conv in the client the user picked in the widget settings
  // ("window" or "tui").
  function openPreferred(client, team, conv) { client === "tui" ? openTui(team, conv) : openWindow(team, conv) }

  // Try to connect now, resetting the backoff (the bar's "retry").
  function reconnect() {}
}
