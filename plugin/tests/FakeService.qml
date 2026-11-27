import QtQuick

// A stand-in for Service.qml in qmltestrunner tests of the views: the same
// public API, with state the test sets directly and every call recorded.
// Keep it member for member with Service.qml (tst_contract.qml checks).
Item {
  id: root

  readonly property string pluginId: "ar.scabb.omaslack"
  readonly property string socketPath: "/tmp/omaslack-test.sock"
  property var shell: null

  property string connectionState: "connected"
  readonly property bool daemonAvailable: connectionState === "connected"
  property string connectionError: ""

  property var workspaces: []
  property var summary: ({ workspaces: ({}), total: ({ dms: 0, mentions: 0, channels: 0, threads: 0 }) })
  property var conversations: ({})
  property var dnd: ({})
  property int messagesRevision: 0

  signal daemonEvent(string method, var params)
  signal messagesUpdated(string team, string conv, string threadTS)
  signal focusRequested(string team, string conv, string ts)

  // ---- test controls ------------------------------------------------------

  // { "<team>/<conv>": [wire.Message] } and "<team>/<conv>/<threadTS>".
  property var messageLists: ({})
  // { "<team>/<conv>": true } where hasMoreHistory() says older pages exist.
  property var moreHistory: ({})
  // [{ team, conversation }], returned as is by unreadConversations().
  property var unreadList: []
  // { "<team>/<user>": name }
  property var userNames: ({})
  // { <method>: { error, result } }: what call() answers; default ok/{}.
  property var answers: ({})
  // Every call, in order: { method, params } (openWindow/openTui too, as
  // "openWindow"/"openTui" with { team, conv }).
  property var calls: []

  function reset() { calls = [] }
  function record(method, params) { var c = calls.slice(); c.push({ method: method, params: params }); calls = c }

  // ---- the Service API ------------------------------------------------------

  function messages(team, conv, threadTS) {
    var key = team + "/" + conv + (threadTS ? "/" + threadTS : "")
    return messageLists[key] || []
  }
  function hasMoreHistory(team, conv) { return !!moreHistory[team + "/" + conv] }
  function unreadConversations() { return unreadList }
  function userName(team, user) { return userNames[team + "/" + user] || user }
  function conversationName(team, conv) {
    var list = conversations[team] || []
    for (var i = 0; i < list.length; i++) if (list[i].id === conv) return list[i].name || conv
    return conv
  }

  function call(method, params, callback) {
    record(method, params)
    var a = answers[method] || { error: null, result: ({}) }
    if (callback) callback(a.error || null, a.error ? null : a.result)
  }
  function openConversation(team, conv, callback) { call("conversations.open", { team: team, conv: conv }, callback) }
  function loadOlder(team, conv, callback) { call("messages.history", { team: team, conv: conv }, callback) }
  function loadReplies(team, conv, threadTS, callback) { call("messages.replies", { team: team, conv: conv, threadTS: threadTS }, callback) }
  function send(team, conv, text, threadTS, callback) { call("messages.send", { team: team, conv: conv, text: text, threadTS: threadTS || undefined }, callback) }
  function edit(team, conv, ts, text, callback) { call("messages.edit", { team: team, conv: conv, ts: ts, text: text }, callback) }
  function remove(team, conv, ts, callback) { call("messages.delete", { team: team, conv: conv, ts: ts }, callback) }
  function react(team, conv, ts, emoji, added, callback) { call(added ? "reactions.add" : "reactions.remove", { team: team, conv: conv, ts: ts, emoji: emoji }, callback) }
  function markRead(team, conv, ts, callback) { call("conversations.markRead", { team: team, conv: conv, ts: ts || undefined }, callback) }
  function setDraft(team, conv, threadTS, text, callback) { call("drafts.set", { team: team, conv: conv, threadTS: threadTS || undefined, text: text }, callback) }
  function snoozeDND(team, minutes, callback) { call("dnd.snooze", { team: team, minutes: minutes }, callback) }
  function endDND(team, callback) { call("dnd.end", { team: team }, callback) }
  function fetchFile(team, id, callback) { call("files.fetch", { team: team, fileId: id }, callback) }

  function openWindow(team, conv) { record("openWindow", { team: team, conv: conv }) }
  function openTui(team, conv) { record("openTui", { team: team, conv: conv }) }
  function openPreferred(client, team, conv) { client === "tui" ? openTui(team, conv) : openWindow(team, conv) }

  function reconnect() { record("reconnect", {}) }
}
