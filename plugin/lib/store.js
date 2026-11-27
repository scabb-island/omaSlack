.pragma library

// The plugin's view of the daemon state, updated by pure functions so it is
// tested with qmltestrunner without a socket (the QML twin of the TUI's
// app.State). Service.qml holds one state and copies its fields into its
// properties after every change.
//
// CONTRACT (M4 step 0): signatures fixed; bodies land with task 4.2.

// empty returns the state before the first hello.
function empty() {
  return {
    workspaces: [],
    summary: { workspaces: ({}), total: { dms: 0, mentions: 0, channels: 0, threads: 0 } },
    conversations: ({}),
    // { <team>/<conv>: { messages: [wire.Message], more: bool } }, and
    // { <team>/<conv>/<threadTS>: ... } for threads.
    messages: ({}),
    // { <team>: { <user>: name } }
    users: ({}),
    dnd: ({})
  }
}

// applyEvent applies one daemon notification (method, params) and returns
// { state, changed: [key] } where changed names the message lists touched
// ("<team>/<conv>" or "<team>/<conv>/<threadTS>").
function applyEvent(state, method, params) {
  return { state: state, changed: [] }
}

// Results of calls, merged into the state.
function applyWorkspaces(state, workspaces) { return state }
function applySummary(state, summary) { return state }
function applyConversations(state, team, conversations) { return state }
// applyHistory merges a page of history (newest first or oldest first, as
// the daemon returns it) into the cached list, deduplicated by ts.
// older: the page is older than what is cached; more: older pages exist.
function applyHistory(state, team, conv, threadTS, messages, older, more) { return state }
function applyUsers(state, team, users) { return state }
function applyDND(state, team, snoozed, until) { return state }

// Queries.
function messages(state, team, conv, threadTS) { return [] }
function hasMore(state, team, conv) { return false }
// unread: conversations with unread messages across workspaces, DMs and
// mentions first, then by latest ts descending: [{ team, conversation }].
function unread(state) { return [] }
function userName(state, team, user) { return user }
function conversationName(state, team, conv) { return conv }
