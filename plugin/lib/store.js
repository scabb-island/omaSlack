.pragma library

// The plugin's view of the daemon state, updated by pure functions so it is
// tested with qmltestrunner without a socket (the QML twin of the TUI's
// app.State). Service.qml holds one state and copies its fields into its
// properties after every change.
//
// Every function returns a new state and never changes the one it got:
// the parts it touches are copied, the rest is shared. Timestamps compare
// as strings, because the daemon always sends Slack's canonical
// "%010d.%06d" form, which sorts lexically.

// empty returns the state before the first hello.
function empty() {
  return {
    workspaces: [],
    summary: { workspaces: ({}), total: { dms: 0, mentions: 0, channels: 0, threads: 0 } },
    conversations: ({}),
    // { <team>/<conv>: { messages: [wire.Message], more: bool } }, and
    // { <team>/<conv>/<threadTS>: ... } for threads. Oldest first.
    messages: ({}),
    // { <team>: { <user>: name } }
    users: ({}),
    dnd: ({})
  }
}

// ---- helpers ----------------------------------------------------------------

function copy(obj) {
  var out = ({})
  for (var k in obj) out[k] = obj[k]
  return out
}

// withField returns state with field set to value.
function withField(state, field, value) {
  var out = copy(state)
  out[field] = value
  return out
}

// withKey returns state with state[field][key] set to value.
function withKey(state, field, key, value) {
  var inner = copy(state[field])
  inner[key] = value
  return withField(state, field, inner)
}

function listKey(team, conv, threadTS) {
  return team + "/" + conv + (threadTS ? "/" + threadTS : "")
}

function byTS(a, b) { return a.ts < b.ts ? -1 : a.ts > b.ts ? 1 : 0 }

function isTopLevel(m) { return !m.threadTS || m.threadTS === m.ts }

// merge returns list plus msgs, one per ts, oldest first. A message without
// reactions keeps the ones already known: message events never carry them
// (reaction.changed does).
function merge(list, msgs) {
  var index = ({})
  for (var i = 0; i < list.length; i++) index[list[i].ts] = list[i]
  for (var j = 0; j < msgs.length; j++) {
    var m = msgs[j], old = index[m.ts]
    if (old && m.reactions === undefined && old.reactions !== undefined) {
      m = copy(m)
      m.reactions = old.reactions
    }
    index[m.ts] = m
  }
  var out = []
  for (var ts in index) out.push(index[ts])
  return out.sort(byTS)
}

// ---- results of calls -------------------------------------------------------

function applyWorkspaces(state, workspaces) { return withField(state, "workspaces", workspaces || []) }
function applySummary(state, summary) { return withField(state, "summary", summary) }
function applyConversations(state, team, conversations) { return withKey(state, "conversations", team, conversations || []) }

// applyHistory merges a page of history (newest first or oldest first, as
// the daemon returns it) into the cached list, deduplicated by ts.
// older: the page is older than what is cached; more: older pages exist.
// A newest page that does not reach the cached messages (some arrived
// while the conversation was not followed) replaces them, so the list has
// no gap; one that overlaps keeps the older history and what is known
// about it.
function applyHistory(state, team, conv, threadTS, messages, older, more) {
  var key = listKey(team, conv, threadTS)
  var cached = state.messages[key]
  var page = (messages || []).slice().sort(byTS)
  var entry
  if (!cached || older)
    entry = { messages: merge(cached ? cached.messages : [], page), more: !!more }
  else if (overlaps(cached.messages, page))
    entry = { messages: merge(cached.messages, page), more: cached.more }
  else
    entry = { messages: page, more: !!more }
  return withKey(state, "messages", key, entry)
}

function overlaps(cached, page) {
  if (cached.length === 0 || page.length === 0) return true
  return page[0].ts <= cached[cached.length - 1].ts
}

// applyUsers records users.info answers: display name, real name or
// handle, the first one set.
function applyUsers(state, team, users) {
  var names = copy(state.users[team] || ({}))
  for (var i = 0; i < (users || []).length; i++) {
    var u = users[i]
    names[u.id] = u.displayName || u.realName || u.name || u.id
  }
  return withKey(state, "users", team, names)
}

function applyDND(state, team, snoozed, until) {
  return withKey(state, "dnd", team, { snoozed: !!snoozed, until: until || 0 })
}

// ---- events -----------------------------------------------------------------

// applyEvent applies one daemon notification (method, params) and returns
// { state, changed: [key] } where changed names the message lists touched
// ("<team>/<conv>" or "<team>/<conv>/<threadTS>"). Topics with no state
// here (typing, presence.changed, draft.changed, ui.focus, unknown ones)
// return the same state.
function applyEvent(state, method, params) {
  var handler = eventHandlers[method]
  if (!handler || !params) return { state: state, changed: [] }
  return handler(state, params)
}

var eventHandlers = {
  "message.created": messagePut,
  "message.updated": messagePut,
  "message.deleted": messageDeleted,
  "reaction.changed": reactionChanged,
  "read.changed": readChanged,
  "conversation.updated": conversationUpdated,
  "connection.state": connectionChanged,
  "unread.summary": function(state, p) { return unchangedLists(applySummary(state, p)) }
}

function unchangedLists(state) { return { state: state, changed: [] } }

// editLists applies edit(messages) to each cached list of team/conv that
// keep(key) accepts and returns the lists whose messages it changed (edit
// returns null for "no change").
function editLists(state, team, conv, keep, edit) {
  var prefix = team + "/" + conv
  var lists = null, changed = []
  for (var key in state.messages) {
    if (key !== prefix && key.indexOf(prefix + "/") !== 0) continue
    if (!keep(key)) continue
    var next = edit(state.messages[key].messages)
    if (next === null) continue
    lists = lists || copy(state.messages)
    lists[key] = { messages: next, more: state.messages[key].more }
    changed.push(key)
  }
  return { state: lists ? withField(state, "messages", lists) : state, changed: changed }
}

// messagePut adds or replaces a message in the cached lists it belongs to:
// the conversation's for a top-level message (thread roots included) and
// its thread's. A list that is not cached yet is not started: the client
// opens it with history first.
function messagePut(state, p) {
  var m = p.message
  var team = p.team, conv = p.conv
  var keep = function(key) {
    return (key === listKey(team, conv) && isTopLevel(m))
      || (!!m.threadTS && key === listKey(team, conv, m.threadTS))
  }
  return editLists(state, team, conv, keep, function(list) { return merge(list, [m]) })
}

function messageDeleted(state, p) {
  return editLists(state, p.team, p.conv, function() { return true }, function(list) {
    var out = list.filter(function(m) { return m.ts !== p.ts })
    return out.length === list.length ? null : out
  })
}

function reactionChanged(state, p) {
  return editLists(state, p.team, p.conv, function() { return true }, function(list) {
    var at = indexOfTS(list, p.ts)
    if (at < 0) return null
    var out = list.slice()
    out[at] = copy(list[at])
    out[at].reactions = react(list[at].reactions || [], p.emoji, p.user, p.added)
    return out
  })
}

function indexOfTS(list, ts) {
  for (var i = 0; i < list.length; i++) if (list[i].ts === ts) return i
  return -1
}

// react returns reactions with user's emoji added or removed. Reactions
// keep their order; one left without users goes away.
function react(reactions, emoji, user, added) {
  var out = [], found = false
  for (var i = 0; i < reactions.length; i++) {
    var r = reactions[i]
    if (r.emoji === emoji) {
      found = true
      r = { emoji: r.emoji, users: withUser(r.users || [], user, added) }
    }
    if (r.users.length > 0) out.push(r)
  }
  if (!found && added) out.push({ emoji: emoji, users: [user] })
  return out
}

function withUser(users, user, add) {
  var out = users.filter(function(u) { return u !== user })
  if (add) out.push(user)
  return out
}

// editConversation replaces conversation id of team with edit(it); edit
// gets null when it is not listed. Returns the state unchanged when edit
// returns null.
function editConversation(state, team, id, edit) {
  var list = state.conversations[team] || []
  var at = -1
  for (var i = 0; i < list.length; i++) if (list[i].id === id) at = i
  var next = edit(at < 0 ? null : list[at])
  if (!next) return state
  var out = list.slice()
  if (at < 0) out.push(next); else out[at] = next
  return applyConversations(state, team, out)
}

function readChanged(state, p) {
  return unchangedLists(editConversation(state, p.team, p.conv, function(c) {
    if (!c) return null
    var out = copy(c)
    out.lastRead = p.lastRead
    out.unread = (c.latest || "") > (p.lastRead || "")
    return out
  }))
}

function conversationUpdated(state, p) {
  var c = p.conversation
  if (!c) return unchangedLists(state)
  return unchangedLists(editConversation(state, p.team, c.id, function() { return c }))
}

function connectionChanged(state, p) {
  var changed = false
  var out = state.workspaces.map(function(w) {
    if (w.team !== p.team) return w
    changed = true
    var nw = copy(w)
    nw.state = p.state
    return nw
  })
  return unchangedLists(changed ? applyWorkspaces(state, out) : state)
}

// ---- queries ----------------------------------------------------------------

// messages: a conversation's top-level messages, oldest first, or with
// threadTS a thread's root and replies.
function messages(state, team, conv, threadTS) {
  var entry = state.messages[listKey(team, conv, threadTS)]
  if (!entry) return []
  return threadTS ? entry.messages : entry.messages.filter(isTopLevel)
}

function hasMore(state, team, conv) {
  var entry = state.messages[listKey(team, conv)]
  return !!entry && entry.more
}

// teams lists the workspaces in the daemon's order, then any other team
// with conversations.
function teams(state) {
  var out = state.workspaces.map(function(w) { return w.team })
  for (var t in state.conversations) if (out.indexOf(t) < 0) out.push(t)
  return out
}

function needsYou(c) { return c.kind === "im" || c.kind === "mpim" || c.mentions > 0 }

// unread: conversations with unread messages across workspaces, DMs and
// mentions first, then by latest ts descending: [{ team, conversation }].
function unread(state) {
  var out = []
  teams(state).forEach(function(team) {
    (state.conversations[team] || []).forEach(function(c) {
      if (c.unread || c.mentions > 0) out.push({ team: team, conversation: c })
    })
  })
  return out.sort(function(a, b) {
    var pa = needsYou(a.conversation), pb = needsYou(b.conversation)
    if (pa !== pb) return pa ? -1 : 1
    return byTS({ ts: b.conversation.latest || "" }, { ts: a.conversation.latest || "" })
  })
}

function userName(state, team, user) {
  var names = state.users[team]
  return (names && names[user]) || user
}

// conversationName: #channel (public or private), the person of a DM, or
// the members of a group DM ("mpdm-ana--bo--cy-1" is "ana, bo, cy").
function conversationName(state, team, conv) {
  var list = state.conversations[team] || []
  for (var i = 0; i < list.length; i++) {
    var c = list[i]
    if (c.id !== conv) continue
    if (c.kind === "im") return c.user ? userName(state, team, c.user) : conv
    if (c.kind === "mpim") return groupName(c.name) || conv
    return c.name ? "#" + c.name : conv
  }
  return conv
}

function groupName(name) {
  var n = (name || "").replace(/^mpdm-/, "")
  var dash = n.lastIndexOf("-")
  if (dash > 0) n = n.slice(0, dash)
  return n.split("--").join(", ")
}
