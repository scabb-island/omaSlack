.pragma library
.import "quickpanel.js" as QP
.import "actions.js" as A

// The full window's logic (task 4.5), pure so qmltestrunner covers it
// (tests/tst_window.qml). Window.qml and its parts only draw what these
// return and turn user actions into Service calls.
//
// Timestamps compare as strings: the daemon always sends Slack's canonical
// "%010d.%06d" form, which sorts lexically.

// ---- sidebar ------------------------------------------------------------------

// The sections of a workspace, as the TUI lays them out (internal/tui/app,
// Sidebar): what needs attention on top, then the rest by name.
var SECTIONS = ["Unread", "Channels", "Direct messages"]

function sectionOf(c) {
  if (c.unread || c.mentions > 0) return 0
  return QP.isDirect(c.kind) ? 2 : 1
}

// sortName drops the "#" of channel names, so #b and Ana sort together.
function sortName(row) { return row.name.replace(/^#/, "").toLowerCase() }

function byName(a, b) { return sortName(a) < sortName(b) ? -1 : sortName(a) > sortName(b) ? 1 : 0 }

// byAttention: most mentions first, then the most recent, then by name.
function byAttention(a, b) {
  if (a.mentions !== b.mentions) return b.mentions - a.mentions
  if (a.latest !== b.latest) return a.latest > b.latest ? -1 : 1
  return byName(a, b)
}

function convRow(team, c, nameOf, presence) {
  return {
    type: "conv",
    key: team + "/" + c.id,
    team: team,
    conv: c.id,
    name: nameOf(team, c.id),
    kind: c.kind || "",
    direct: QP.isDirect(c.kind),
    unread: !!c.unread || c.mentions > 0,
    mentions: c.mentions || 0,
    latest: c.latest || "",
    badge: QP.badge({ mentions: c.mentions || 0 }),
    // A DM's other person: "active", "away", or "" while unknown.
    presence: (c.kind === "im" && c.user && presence[c.user]) || ""
  }
}

// workspaceRows: the workspace header, then its non-empty sections.
function workspaceRows(w, convs, counts, nameOf, presence) {
  var buckets = [[], [], []]
  convs.forEach(function(c) { buckets[sectionOf(c)].push(convRow(w.team, c, nameOf, presence || {})) })
  buckets[0].sort(byAttention)
  buckets[1].sort(byName)
  buckets[2].sort(byName)
  var c = counts || {}
  var out = [{
    type: "workspace", key: w.team, team: w.team, name: w.name || w.team, state: w.state || "",
    badge: QP.badge({ mentions: (c.dms || 0) + (c.mentions || 0) })
  }]
  buckets.forEach(function(rows, i) {
    if (rows.length === 0) return
    out.push({ type: "section", key: w.team + "#" + SECTIONS[i], team: w.team, title: SECTIONS[i] })
    out = out.concat(rows)
  })
  return out
}

// sidebarRows: one flat list for the sidebar's ListView: each workspace (in
// the daemon's order, then any other team with conversations) with its
// Unread, Channels and Direct messages sections. Rows have type
// "workspace", "section" or "conv"; only "conv" rows take the cursor.
// nameOf(team, conv) is service.conversationName; presence is
// service.presence (optional), for the dot of DM rows.
function sidebarRows(workspaces, conversations, summary, nameOf, presence) {
  var list = (workspaces || []).slice()
  var convs = conversations || {}
  var known = list.map(function(w) { return w.team })
  for (var t in convs) if (known.indexOf(t) < 0) list.push({ team: t, name: t })
  var counts = (summary && summary.workspaces) || {}
  var out = []
  var who = presence || {}
  list.forEach(function(w) { out = out.concat(workspaceRows(w, convs[w.team] || [], counts[w.team], nameOf, who[w.team])) })
  return out
}

// step moves a cursor index by dir (+1 down, -1 up) to the next row that
// takes it; it stays put at either end. From -1 (no cursor) it goes to the
// first such row; -1 when there is none.
function step(rows, index, dir) {
  var from = index < 0 ? -1 : index
  var d = index < 0 ? 1 : dir
  for (var i = from + d; i >= 0 && i < rows.length; i += d)
    if (rows[i].type === "conv") return i
  return index < 0 ? -1 : index
}

// ---- messages -----------------------------------------------------------------

// Consecutive messages of one author closer than this share one header.
var GROUP_SECONDS = 5 * 60

var DAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"]
var MONTHS = ["January", "February", "March", "April", "May", "June", "July",
              "August", "September", "October", "November", "December"]

function tsSeconds(ts) { return Number(String(ts).split(".")[0]) || 0 }
function tsDate(ts) { return new Date(tsSeconds(ts) * 1000) }

function sameDay(a, b) {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
}

// dayLabel names the day of date as seen from now: "Today", "Yesterday",
// "Wednesday, November 25", with the year when it is not this one.
function dayLabel(date, now) {
  if (sameDay(date, now)) return "Today"
  var yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
  if (sameDay(date, yesterday)) return "Yesterday"
  var label = DAYS[date.getDay()] + ", " + MONTHS[date.getMonth()] + " " + date.getDate()
  return date.getFullYear() === now.getFullYear() ? label : label + ", " + date.getFullYear()
}

function pad2(n) { return (n < 10 ? "0" : "") + n }

function timeLabel(ts) {
  var d = tsDate(ts)
  return pad2(d.getHours()) + ":" + pad2(d.getMinutes())
}

// messageRows turns a message list (oldest first) into the rows the list
// draws: { key: ts, message, day, grouped, time }. day is the separator
// label above the row ("" for none: same day as the row before); grouped
// says the row continues the previous one's author (no header).
function messageRows(messages, now) {
  var out = []
  var prev = null
  ;(messages || []).forEach(function(m) {
    var date = tsDate(m.ts)
    var newDay = !prev || !sameDay(date, tsDate(prev.ts))
    out.push({
      key: m.ts,
      message: m,
      day: newDay ? dayLabel(date, now) : "",
      grouped: !newDay && !!m.author && m.author === prev.author && tsSeconds(m.ts) - tsSeconds(prev.ts) < GROUP_SECONDS,
      time: timeLabel(m.ts)
    })
    prev = m
  })
  return out
}

// threadLabel: "↳ N replies" under a thread root, "" for none.
function threadLabel(m) {
  var n = m.replyCount || 0
  if (n === 0) return ""
  return "↳ " + n + (n === 1 ? " reply" : " replies")
}

// reactionChips: [{ emoji, count, mine, label }] for a message's reactions;
// mine when self is among the users. The label shows a standard emoji as
// its character and any other (custom) one as :name:.
function reactionChips(reactions, self) {
  return (reactions || []).map(function(r) {
    var users = r.users || []
    var glyph = A.emojiGlyph(r.emoji) || ":" + r.emoji + ":"
    return { emoji: r.emoji, count: users.length, mine: users.indexOf(self) >= 0, label: glyph + " " + users.length }
  })
}

function sizeLabel(bytes) {
  if (bytes < 1024) return bytes + " B"
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + " KB"
  return (bytes / (1024 * 1024)).toFixed(1) + " MB"
}

// fileLabel: a file's title (or name) and size.
function fileLabel(f) {
  var name = f.title || f.name || f.id || ""
  return f.size > 0 ? name + " (" + sizeLabel(f.size) + ")" : name
}

// body: what a message's Text shows: the daemon's rich text (html: mrkdwn
// already parsed) when it is there, the raw text as plain text otherwise.
// Text.RichText ignores Text.linkColor, so links get linkColor (the
// theme's accent, as "#rrggbb") through a style sheet.
function body(m, linkColor) {
  if (m.html) return { rich: true, text: (linkColor ? "<style>a { color: " + linkColor + "; }</style>" : "") + m.html }
  return { rich: false, text: m.text || "" }
}

// ---- links --------------------------------------------------------------------

// linkAction: what clicking a link in a message does. The daemon's rich
// text links to omaslack://conv/<team>/<conv> (a channel mention),
// omaslack://user/<team>/<user> (a person) and http(s)/mailto URLs;
// anything else is ignored.
function linkAction(url) {
  var u = String(url || "")
  var m = u.match(/^omaslack:\/\/(conv|user)\/([^/]+)\/([^/?#]+)$/)
  if (m && m[1] === "conv") return { kind: "conv", team: m[2], conv: m[3] }
  if (m) return { kind: "user", team: m[2], user: m[3] }
  if (/^(https?:\/\/|mailto:)/i.test(u)) return { kind: "external", url: u }
  return { kind: "ignore" }
}

// ---- list syncing -------------------------------------------------------------

// syncOps: the removes, inserts and moves that turn a ListModel holding
// oldKeys into newKeys, applied in order ({ op: "remove", index },
// { op: "insert", index, key }, { op: "move", from, to }). Rows kept in
// place are not touched, which is what lets a ListView keep its scroll
// position when an older page arrives above what is on screen, and keeps
// the sidebar's delegates when only their data changes. Keys are unique.
function syncOps(oldKeys, newKeys) {
  var ops = []
  var wanted = ({})
  newKeys.forEach(function(k) { wanted[k] = true })
  var cur = []
  for (var r = oldKeys.length - 1; r >= 0; r--) {
    if (wanted[oldKeys[r]]) cur.unshift(oldKeys[r])
    else ops.push({ op: "remove", index: r })
  }
  for (var i = 0; i < newKeys.length; i++) {
    if (cur[i] === newKeys[i]) continue
    var from = cur.indexOf(newKeys[i], i + 1)
    if (from >= 0) {
      cur.splice(i, 0, cur.splice(from, 1)[0])
      ops.push({ op: "move", from: from, to: i })
    } else {
      cur.splice(i, 0, newKeys[i])
      ops.push({ op: "insert", index: i, key: newKeys[i] })
    }
  }
  return ops
}

// indexForTS: the index of ts in keys (sorted), or of the first message
// after it, or the last one; -1 for no keys.
function indexForTS(keys, ts) {
  for (var i = 0; i < keys.length; i++) if (keys[i] >= ts) return i
  return keys.length - 1
}

// ---- marking read ---------------------------------------------------------------

// readUpTo: the ts to mark the open conversation read up to, while the view
// follows its newest message: that message, when it is newer than the
// conversation's lastRead and than what was already marked; "" for nothing
// to do.
function readUpTo(messages, lastRead, marked) {
  if (!messages || messages.length === 0) return ""
  var newest = messages[messages.length - 1].ts
  if (newest <= (lastRead || "") || newest <= (marked || "")) return ""
  return newest
}

// ---- payload ------------------------------------------------------------------

// parsePayload reads the shell's summon payload { team, conv, ts? };
// null when it does not name a conversation.
function parsePayload(json) {
  if (!json) return null
  try {
    var p = JSON.parse(String(json))
    if (!p || !p.team || !p.conv) return null
    return { team: String(p.team), conv: String(p.conv), ts: p.ts ? String(p.ts) : "" }
  } catch (e) {
    return null
  }
}

// ---- keyboard focus -------------------------------------------------------------
// The window's focus is { pane, composing, threadOpen }: the pane with the
// keyboard ("sidebar", "messages", "thread"), whether its composer has it,
// and whether the thread pane is open.

function panes(threadOpen) { return threadOpen ? ["sidebar", "messages", "thread"] : ["sidebar", "messages"] }

// movePane: the pane dir steps away from pane (h/l without wrapping, Tab
// and Shift+Tab with wrap). A pane that is gone (the thread, closed) hands
// the keyboard to the messages.
function movePane(pane, dir, threadOpen, wrap) {
  var list = panes(threadOpen)
  var at = list.indexOf(pane)
  if (at < 0) return "messages"
  var next = at + dir
  if (wrap) return list[(next + list.length) % list.length]
  return list[Math.max(0, Math.min(next, list.length - 1))]
}

// escape steps back one level: out of the composer, then closing the
// thread, then (dismiss: true) closing the window.
function escape(focus) {
  var out = { pane: focus.pane, composing: false, threadOpen: focus.threadOpen, dismiss: false }
  if (focus.composing) return out
  if (focus.threadOpen) {
    out.threadOpen = false
    if (out.pane === "thread") out.pane = "messages"
    return out
  }
  out.dismiss = true
  return out
}

// ---- ui.viewing -------------------------------------------------------------------

// viewing is ui.viewing's params for what the window shows (task 5.1), so
// the daemon does not notify messages on screen: the open conversation
// and thread while the window is open, focused while it has the keyboard;
// {} when it shows nothing.
function viewing(opened, active, team, conv, threadTS) {
  if (!opened || !team || !conv) return {}
  var p = { team: team, conv: conv, focused: !!active }
  if (threadTS) p.threadTS = threadTS
  return p
}
