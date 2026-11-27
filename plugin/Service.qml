import QtQuick
import Quickshell
import Quickshell.Io
import "lib/rpc.js" as Rpc
import "lib/store.js" as Store
import "lib/backoff.js" as Backoff
import "lib/asks.js" as Asks

// The one connection between omarchy-shell and the omaslack daemon
// (spec gui-client, "Single Service as the daemon client"). The shell mounts
// this once; the bar widget, the quick-view panel and the window get it with
// shell.serviceFor("ar.scabb.omaslack") and only read its properties and
// call its functions. They never open a connection of their own.
//
// The public API below is fixed: the views are built against it, and
// tests/FakeService.qml mirrors it member for member (tst_contract.qml).
// Behind it: a Quickshell.Io Socket with a SplitParser (spike M4.1),
// hello + events.subscribe, request/response correlation in lib/rpc.js,
// state updates in lib/store.js and reconnection with lib/backoff.js
// (1 s to 30 s: every failed attempt leaves a WARN in the shell log).
// Members starting with "_" are private.
//
// State properties hold plain JS values that are replaced, never mutated in
// place, so bindings on them update. Shapes are the daemon's wire shapes
// (internal/wire): Workspace, Conversation, Message, Summary.
Item {
  id: root

  readonly property string pluginId: "ar.scabb.omaslack"
  readonly property string socketPath: (Quickshell.env("XDG_RUNTIME_DIR") || "/tmp") + "/omaslack.sock"
  // The scoped shell API, set by the shell when it mounts the service; used
  // to summon the window. Null outside the shell.
  property var shell: null

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
  // Bumped whenever any cached message list or a user name changes; bind
  // to it and read messages(team, conv) to stay current. messages(),
  // userName() and conversationName() read it, so bindings calling them
  // update on their own.
  property int messagesRevision: 0

  // ---- signals ------------------------------------------------------------

  // Every daemon notification (method and params, as on the wire), emitted
  // after the store has applied it.
  signal daemonEvent(string method, var params)
  // The cached messages of one conversation (or one thread, with threadTS)
  // changed.
  signal messagesUpdated(string team, string conv, string threadTS)
  // ui.focus from any client: someone asked the UI to show this
  // conversation (and message, when ts is set). The echo of this
  // service's own openTui() is not emitted: it is meant for the TUI.
  signal focusRequested(string team, string conv, string ts)

  // ---- reading ------------------------------------------------------------

  // The cached messages of a conversation, oldest first ([] when none are
  // loaded). With threadTS, the replies of that thread, root first.
  function messages(team, conv, threadTS) {
    void root.messagesRevision
    return Store.messages(root._st, team, conv, threadTS || "")
  }
  // Whether older history exists before the oldest cached message.
  function hasMoreHistory(team, conv) {
    void root.messagesRevision
    return Store.hasMore(root._st, team, conv)
  }
  // The conversations with unread messages across all workspaces, DMs and
  // mentions first, then by latest activity: [{ team, conversation }].
  function unreadConversations() {
    void root.conversations
    return Store.unread(root._st)
  }
  // A display name for a user ID; asks users.info once when unknown and
  // returns the ID meanwhile (messagesRevision bumps when it arrives). A
  // bot author (B…) reads "Bot": the daemon has no name for it.
  function userName(team, user) {
    void root.messagesRevision
    var name = Store.userName(root._st, team, user)
    if (name === user) root._wantUser(team, user)
    return name
  }
  // Who wrote message, as views show it: the bot's name Slack sent, else
  // userName() of its author.
  function authorName(team, message) {
    return Store.authorName(message, function(user) { return root.userName(team, user) })
  }
  // A display name for a conversation: #channel, the person of a DM, or the
  // members of a group DM.
  function conversationName(team, conv) {
    void root.messagesRevision
    void root.conversations
    var list = root._st.conversations[team] || []
    for (var i = 0; i < list.length; i++)
      if (list[i].id === conv && list[i].kind === "im" && list[i].user) root.userName(team, list[i].user)
    return Store.conversationName(root._st, team, conv)
  }

  // ---- calls --------------------------------------------------------------
  // Every callback is function(error, result): error is null or
  // { code, message } (the JSON-RPC error), result the method's result.

  // Any daemon method (internal/api): the escape hatch.
  function call(method, params, callback) {
    if (!root.daemonAvailable) {
      root._answer(callback, { code: -32000, message: "not connected" }, null)
      return
    }
    root._send(method, params, callback)
  }
  // conversations.open: first page of history plus the draft, cached.
  function openConversation(team, conv, callback) {
    call("conversations.open", { team: team, conv: conv }, function(err, res) {
      if (!err && res) root._history(team, conv, "", res.messages, false)
      root._answer(callback, err, res)
    })
  }
  // messages.history before the oldest cached message, merged into the cache.
  function loadOlder(team, conv, callback) {
    var cached = Store.messages(root._st, team, conv, "")
    var params = { team: team, conv: conv, limit: root._pageSize }
    if (cached.length > 0) params.before = cached[0].ts
    call("messages.history", params, function(err, res) {
      if (!err && res) root._history(team, conv, "", res, cached.length > 0)
      root._answer(callback, err, res)
    })
  }
  // messages.replies of a thread, cached under threadTS.
  function loadReplies(team, conv, threadTS, callback) {
    call("messages.replies", { team: team, conv: conv, threadTS: threadTS }, function(err, res) {
      if (!err && res) root._history(team, conv, threadTS, res, false)
      root._answer(callback, err, res)
    })
  }
  // messages.send; threadTS empty for the channel itself.
  function send(team, conv, text, threadTS, callback) { call("messages.send", { team: team, conv: conv, text: text, threadTS: threadTS || undefined }, callback) }
  function edit(team, conv, ts, text, callback) { call("messages.edit", { team: team, conv: conv, ts: ts, text: text }, callback) }
  function remove(team, conv, ts, callback) { call("messages.delete", { team: team, conv: conv, ts: ts }, callback) }
  function react(team, conv, ts, emoji, added, callback) { call(added ? "reactions.add" : "reactions.remove", { team: team, conv: conv, ts: ts, emoji: emoji }, callback) }
  // conversations.markRead up to ts (the newest message when empty).
  function markRead(team, conv, ts, callback) { call("conversations.markRead", { team: team, conv: conv, ts: ts || undefined }, callback) }
  function setDraft(team, conv, threadTS, text, callback) { call("drafts.set", { team: team, conv: conv, threadTS: threadTS || undefined, text: text }, callback) }
  function snoozeDND(team, minutes, callback) {
    call("dnd.snooze", { team: team, minutes: minutes }, function(err, res) {
      if (!err) root._publish(Store.applyDND(root._st, team, true, Math.floor(Date.now() / 1000) + minutes * 60))
      root._answer(callback, err, res)
    })
  }
  function endDND(team, callback) {
    call("dnd.end", { team: team }, function(err, res) {
      if (!err) root._publish(Store.applyDND(root._st, team, false, 0))
      root._answer(callback, err, res)
    })
  }
  // files.fetch: the local path of a file's bytes, for Image and Qt.openUrlExternally.
  function fetchFile(team, id, callback) { call("files.fetch", { team: team, fileId: id }, callback) }

  // ---- opening the clients --------------------------------------------------

  // Show the full window (omarchy-shell shell summon), focused on conv when
  // team and conv are set.
  function openWindow(team, conv) {
    var payload = team && conv ? JSON.stringify({ team: team, conv: conv }) : ""
    if (root.shell && typeof root.shell.summon === "function" && root.shell.summon(root.pluginId, payload)) return
    Quickshell.execDetached(["omarchy-shell", "shell", "summon", root.pluginId, payload])
  }
  // Open the TUI (omarchy-launch-or-focus-tui) and ask it, through
  // ui.focus, to show conv when team and conv are set.
  function openTui(team, conv) {
    Quickshell.execDetached(["omarchy-launch-or-focus-tui", "omaslack", "tui"])
    if (team && conv) root._focusTui(team, conv, root._focusTries)
  }
  // Open conv in the client the user picked in the widget settings
  // ("window" or "tui").
  function openPreferred(client, team, conv) { client === "tui" ? openTui(team, conv) : openWindow(team, conv) }

  // Try to connect now, resetting the backoff (the bar's "retry").
  function reconnect() {
    root._delay = 0
    retryTimer.stop()
    root._connect()
  }

  // ---- private ------------------------------------------------------------

  readonly property string _clientName: "omaslack-plugin"
  readonly property int _pageSize: 50 // api.HistoryLimit: a shorter page means no older history
  readonly property int _usersPerCall: 100 // api.UsersInfoMaxIDs
  readonly property int _focusTries: 10 // ui.focus retries while a launched TUI starts (500 ms apart)
  readonly property var _topics: [
    "message.created", "message.updated", "message.deleted",
    "reaction.changed", "conversation.updated", "read.changed",
    "draft.changed", "typing", "presence.changed", "connection.state",
    "unread.summary", "ui.focus"
  ]

  property var _st: Store.empty()
  property var _rpc: Rpc.newClient()
  property int _delay: 0
  // Not properties of interest to bindings; mutated in place.
  property var _askedUsers: Asks.empty() // users.info asks (lib/asks.js)
  property var _userQueue: ({}) // <team>: [user]
  property var _ownFocus: ({}) // "<team>/<conv>": echoes of our ui.focus to skip

  function _answer(callback, err, res) {
    if (!callback) return
    try { callback(err, res) } catch (e) { console.warn("omaslack: callback failed:", e) }
  }

  function _send(method, params, callback) {
    var r = Rpc.request(root._rpc, method, params, callback)
    root._rpc = r.client
    sock.write(r.line)
    sock.flush()
  }

  // _publish makes st the current state and copies the parts that changed
  // into the public properties.
  function _publish(st) {
    root._st = st
    if (root.workspaces !== st.workspaces) root.workspaces = st.workspaces
    if (root.summary !== st.summary) root.summary = st.summary
    if (root.conversations !== st.conversations) root.conversations = st.conversations
    if (root.dnd !== st.dnd) root.dnd = st.dnd
  }

  function _changed(keys) {
    if (keys.length === 0) return
    root.messagesRevision++
    for (var i = 0; i < keys.length; i++) {
      var parts = keys[i].split("/")
      root.messagesUpdated(parts[0], parts[1], parts[2] || "")
    }
  }

  function _history(team, conv, threadTS, list, older) {
    var page = list || []
    root._publish(Store.applyHistory(root._st, team, conv, threadTS, page, older, !threadTS && page.length >= root._pageSize))
    root._changed([team + "/" + conv + (threadTS ? "/" + threadTS : "")])
  }

  // ---- connection lifecycle --------------------------------------------------

  function _connect() {
    sock.connected = false // may schedule a retry; this attempt replaces it
    retryTimer.stop()
    root.connectionState = "connecting"
    sock.connected = true
  }

  function _onConnected() {
    root._send("hello", { client: root._clientName, protocol: Rpc.PROTOCOL }, root._onHello)
  }

  function _onHello(err) {
    if (err && err.code === Rpc.INCOMPATIBLE) {
      root.connectionState = "incompatible"
      root.connectionError = err.message || "incompatible protocol"
      sock.connected = false
      return
    }
    if (err) {
      root.connectionError = "hello failed: " + (err.message || err.code)
      sock.connected = false
      return
    }
    root._delay = 0
    root.connectionError = ""
    root.connectionState = "connected"
    root.messagesRevision++ // names asked while offline are asked again
    root._send("events.subscribe", { topics: root._topics }, root._warnOnError("events.subscribe"))
    root._send("workspaces.list", null, root._onWorkspaces)
    root._send("unread.summary", null, function(e, res) {
      if (!e && res) root._publish(Store.applySummary(root._st, res))
    })
    root._refreshMessages()
  }

  function _onWorkspaces(err, list) {
    if (err || !list) return
    root._publish(Store.applyWorkspaces(root._st, list))
    for (var i = 0; i < list.length; i++) root._loadConversations(list[i].team)
  }

  function _loadConversations(team) {
    root._send("conversations.list", { team: team }, function(err, list) {
      if (err || !list) return
      root._publish(Store.applyConversations(root._st, team, list))
      // Name the unread DMs up front: the panel shows them first.
      for (var i = 0; i < list.length; i++)
        if (list[i].kind === "im" && list[i].user && list[i].unread) root._wantUser(team, list[i].user)
    })
  }

  // _refreshMessages reloads the newest page of every cached list after a
  // reconnection, since events were missed meanwhile.
  function _refreshMessages() {
    for (var key in root._st.messages) {
      var p = key.split("/")
      if (p.length === 3) root.loadReplies(p[0], p[1], p[2], null)
      else root._refreshConversation(p[0], p[1])
    }
  }

  function _refreshConversation(team, conv) {
    root._send("messages.history", { team: team, conv: conv, limit: root._pageSize }, function(err, res) {
      if (!err && res) root._history(team, conv, "", res, false)
    })
  }

  function _warnOnError(what) {
    return function(err) { if (err) console.warn("omaslack:", what, "failed:", err.message) }
  }

  function _onClosed(reason) {
    var failed = Rpc.failAll(root._rpc, { code: -32000, message: "connection closed" })
    root._rpc = failed.client
    root._askedUsers = Asks.empty()
    root._userQueue = ({})
    for (var i = 0; i < failed.callbacks.length; i++) {
      try { failed.callbacks[i]() } catch (e) { console.warn("omaslack: callback failed:", e) }
    }
    if (root.connectionState === "incompatible") return
    if (reason) root.connectionError = reason
    root.connectionState = "unavailable"
    root._scheduleRetry()
  }

  function _scheduleRetry() {
    if (retryTimer.running || root.connectionState === "incompatible") return
    root._delay = Backoff.next(root._delay)
    retryTimer.interval = root._delay
    retryTimer.start()
  }

  function _socketError(code) {
    // QLocalSocket::LocalSocketError
    if (code === 0 || code === 2) return "the omaslack daemon is not running"
    if (code === 1) return "the omaslack daemon closed the connection"
    return "socket error " + code
  }

  function _onLine(line) {
    var r = Rpc.receive(root._rpc, line)
    root._rpc = r.client
    if (r.kind === "response") root._answer(r.callback, r.error, r.result)
    else if (r.kind === "notification") root._onEvent(r.method, r.params)
    else console.warn("omaslack: dropped a line from the daemon:", r.reason)
  }

  function _onEvent(method, params) {
    var r = Store.applyEvent(root._st, method, params)
    root._publish(r.state)
    root._changed(r.changed)
    if (method === "conversation.updated" && params.conversation && params.conversation.kind === "im"
        && params.conversation.unread && params.conversation.user)
      root._wantUser(params.team, params.conversation.user)
    if (method === "ui.focus") root._onFocus(params)
    root.daemonEvent(method, params)
  }

  // ---- users.info batching ----------------------------------------------------

  // Only people are asked: one bot ID (B…) in a batch makes the daemon
  // refuse the whole users.info call, which left every name in it unasked.
  function _wantUser(team, user) {
    if (!team || !Store.isUserID(user)) return
    var key = team + "/" + user
    if (!Asks.shouldAsk(root._askedUsers, key, Date.now())) return
    Asks.asked(root._askedUsers, key)
    var q = root._userQueue[team] || []
    q.push(user)
    root._userQueue[team] = q
    // Not "if (!running)": userName() runs inside bindings, and reading
    // running there makes start() re-trigger the binding being evaluated
    // (a binding loop). start() does nothing while the timer runs.
    usersTimer.start()
  }

  function _flushUsers() {
    if (!root.daemonAvailable) {
      root._askedUsers = Asks.empty()
      root._userQueue = ({})
      return
    }
    var queue = root._userQueue
    root._userQueue = ({})
    for (var team in queue) {
      for (var i = 0; i < queue[team].length; i += root._usersPerCall)
        root._askUsers(team, queue[team].slice(i, i + root._usersPerCall))
    }
  }

  // A failed batch is asked again after a backoff (lib/asks.js):
  // usersRetry re-runs the bindings that call userName() once it passes.
  function _askUsers(team, ids) {
    root._send("users.info", { team: team, ids: ids }, function(err, users) {
      if (err) {
        var keys = ids.map(function(id) { return team + "/" + id })
        usersRetry.interval = Asks.failed(root._askedUsers, keys, Date.now())
        usersRetry.restart()
        return
      }
      if (!users) return
      root._publish(Store.applyUsers(root._st, team, users))
      root.messagesRevision++
    })
  }

  // ---- ui.focus for the TUI -----------------------------------------------------

  // _focusTui sends ui.focus until a client besides this one receives it:
  // a TUI that is just starting subscribes a moment later.
  function _focusTui(team, conv, tries) {
    if (!root.daemonAvailable) return
    var key = team + "/" + conv
    root._ownFocus[key] = (root._ownFocus[key] || 0) + 1
    root._send("ui.focus", { team: team, conv: conv }, function(err, res) {
      if (err || !res || res.clients > 1 || tries <= 1) return
      focusRetry.pending = { team: team, conv: conv, tries: tries - 1 }
      focusRetry.restart()
    })
  }

  function _onFocus(p) {
    var key = p.team + "/" + p.conv
    if (root._ownFocus[key] > 0) {
      root._ownFocus[key]--
      return
    }
    root.focusRequested(p.team, p.conv, p.ts || "")
  }

  Socket {
    id: sock
    path: root.socketPath
    connected: false

    parser: SplitParser {
      splitMarker: "\n"
      onRead: function(data) { root._onLine(data) }
    }

    onConnectionStateChanged: {
      if (connected) root._onConnected()
      else root._onClosed("")
    }
    onError: function(code) {
      root.connectionError = root._socketError(code)
      if (!connected) root._onClosed(root.connectionError)
    }
  }

  Timer {
    id: retryTimer
    onTriggered: root._connect()
  }

  Timer {
    id: usersTimer
    interval: 50
    onTriggered: root._flushUsers()
  }

  Timer {
    id: usersRetry
    onTriggered: root.messagesRevision++
  }

  Timer {
    id: focusRetry
    property var pending: null
    interval: 500
    onTriggered: if (pending) root._focusTui(pending.team, pending.conv, pending.tries)
  }

  Component.onCompleted: root._connect()
}
