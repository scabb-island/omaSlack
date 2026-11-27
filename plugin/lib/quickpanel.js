.pragma library

// The quick-view panel's logic (task 4.4), pure so qmltestrunner covers it
// (tests/tst_quickpanel.qml). QuickPanel.qml only draws what these return
// and turns user actions into Service calls.

// ---- rows -------------------------------------------------------------------

// isDirect: a DM or a group DM.
function isDirect(kind) { return kind === "im" || kind === "mpim" }

// rows turns service.unreadConversations() ([{ team, conversation }], in
// the Service's order: DMs and mentions first) into the panel's rows.
// nameOf(team, conv) names a conversation (service.conversationName).
function rows(unread, workspaces, nameOf) {
  var names = {}
  var list = workspaces || []
  list.forEach(function(w) { names[w.team] = w.name || w.team })
  return (unread || []).map(function(u) {
    var c = u.conversation || {}
    return {
      key: u.team + "/" + c.id,
      team: u.team,
      conv: c.id,
      name: nameOf(u.team, c.id),
      workspace: names[u.team] || u.team,
      kind: c.kind || "",
      direct: isDirect(c.kind),
      mentions: c.mentions || 0,
      latest: c.latest || ""
    }
  })
}

// badge is the mention count shown on a row ("" for none).
function badge(row) {
  if (!row.mentions) return ""
  return row.mentions > 99 ? "99+" : String(row.mentions)
}

// ---- previews -----------------------------------------------------------------

var ENTITIES = { "&lt;": "<", "&gt;": ">", "&quot;": "\"", "&#39;": "'", "&apos;": "'", "&nbsp;": " " }

function decodeEntities(s) {
  return s
    .replace(/&(lt|gt|quot|#39|apos|nbsp);/g, function(e) { return ENTITIES[e] })
    .replace(/&#(\d+);/g, function(_, n) { return String.fromCharCode(Number(n)) })
    .replace(/&amp;/g, "&")
}

// plainText is a message as one line of plain text: the daemon's rich text
// (html: mentions and emoji already resolved) without its markup when it is
// there, the raw text otherwise.
function plainText(message) {
  var m = message || {}
  var s = m.html
    ? decodeEntities(m.html.replace(/<br\s*\/?>|<\/(p|div|li|pre|blockquote)>/gi, " ").replace(/<[^>]*>/g, ""))
    : (m.text || "")
  return s.replace(/\s+/g, " ").trim()
}

// shorten cuts text to max characters, on a word when it can, ending in "…".
function shorten(text, max) {
  if (text.length <= max) return text
  var cut = text.slice(0, max - 1)
  var space = cut.lastIndexOf(" ")
  return (space > 0 ? cut.slice(0, space) : cut).trim() + "…"
}

// preview picks the newest count messages of a messages.history page (the
// daemon sends it newest first; any order works) and returns them oldest
// first as [{ author, text }], skipping messages with no text.
// authorName(user) names an author (service.userName for the row's team).
function preview(history, authorName, count, max) {
  return (history || [])
    .map(function(m) { return { ts: Number(m.ts) || 0, author: m.author || "", text: plainText(m) } })
    .filter(function(m) { return m.text !== "" })
    .sort(function(a, b) { return a.ts - b.ts })
    .slice(-count)
    .map(function(m) { return { author: m.author ? authorName(m.author) : "", text: shorten(m.text, max) } })
}

// needsPreview: the row's preview is missing or older than its latest
// message, and no fetch for that latest is in flight. cache is
// { <key>: { latest, lines } }, pending { <key>: latest being fetched }.
function needsPreview(cache, pending, row) {
  if (pending[row.key] === row.latest) return false
  var hit = cache[row.key]
  return !hit || hit.latest !== row.latest
}

// inView: a row at top with height shows at least partly in a viewport
// starting at viewTop with viewHeight (previews load for these only).
function inView(top, height, viewTop, viewHeight) {
  return top + height >= viewTop && top <= viewTop + viewHeight
}

// ---- reply ----------------------------------------------------------------------

function canSend(text) { return !!text && text.trim() !== "" }

// ---- keyboard cursor ------------------------------------------------------------
// The cursor is { section: "dnd" | "rows", index }. The DND header is a
// single line of dndCount buttons above rowCount rows.

function clamp(cursor, dndCount, rowCount) {
  if (cursor.section === "rows" && rowCount > 0)
    return { section: "rows", index: Math.max(0, Math.min(cursor.index, rowCount - 1)) }
  var index = cursor.section === "dnd" ? cursor.index : 0
  return { section: "dnd", index: Math.max(0, Math.min(index, dndCount - 1)) }
}

// move applies one arrow key (dx across the header, dy down the list).
function move(cursor, dx, dy, dndCount, rowCount) {
  var c = clamp(cursor, dndCount, rowCount)
  if (c.section === "dnd") {
    if (dy > 0 && rowCount > 0) return { section: "rows", index: 0 }
    return clamp({ section: "dnd", index: c.index + dx }, dndCount, rowCount)
  }
  if (dy < 0 && c.index === 0) return { section: "dnd", index: 0 }
  return clamp({ section: "rows", index: c.index + dy }, dndCount, rowCount)
}

// indexOfKey finds a row by key, so the cursor follows it when the list
// reorders (-1 when it is gone).
function indexOfKey(rows, key) {
  for (var i = 0; i < rows.length; i++) if (rows[i].key === key) return i
  return -1
}

// ---- DND --------------------------------------------------------------------------

// dndScopes: what DND applies to. "All workspaces" (team "") always, then
// each workspace when there are several.
function dndScopes(workspaces) {
  var all = [{ team: "", label: "All workspaces" }]
  var list = workspaces || []
  if (list.length < 2) return all
  return all.concat(list.map(function(w) { return { team: w.team, label: w.name || w.team } }))
}

function nextScope(index, count) { return count > 0 ? (index + 1) % count : 0 }

// dndTeams: the teams a scope covers ("" = every workspace).
function dndTeams(team, workspaces) {
  if (team !== "") return [team]
  return (workspaces || []).map(function(w) { return w.team })
}

// minutesUntilTomorrow: minutes from now to hour:00 on the next calendar day.
function minutesUntilTomorrow(now, hour) {
  var then = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1, hour, 0, 0)
  return Math.round((then.getTime() - now.getTime()) / 60000)
}

var TOMORROW_HOUR = 9

function dndChoices(now) {
  return [
    { label: "30 min", minutes: 30 },
    { label: "1 hour", minutes: 60 },
    { label: "Tomorrow", minutes: minutesUntilTomorrow(now, TOMORROW_HOUR) }
  ]
}

// dndStatus of a scope: snoozed when every team of it is, until the
// earliest end.
function dndStatus(dnd, teams) {
  var d = dnd || {}
  if (teams.length === 0) return { snoozed: false, until: 0 }
  var until = Infinity
  for (var i = 0; i < teams.length; i++) {
    var s = d[teams[i]]
    if (!s || !s.snoozed) return { snoozed: false, until: 0 }
    until = Math.min(until, s.until || 0)
  }
  return { snoozed: true, until: until }
}

function pad2(n) { return (n < 10 ? "0" : "") + n }

// clockLabel: "HH:MM" local time of an epoch in seconds.
function clockLabel(epochSeconds) {
  var d = new Date(epochSeconds * 1000)
  return pad2(d.getHours()) + ":" + pad2(d.getMinutes())
}

// dndButtons: the header's buttons, left to right. The scope button (only
// with several workspaces) cycles the scope; the snooze buttons snooze it;
// "End" (only while snoozed) ends DND.
function dndButtons(scopes, scopeIndex, choices, snoozed) {
  var out = []
  if (scopes.length > 1) out.push({ id: "scope", label: scopes[scopeIndex].label })
  choices.forEach(function(c) { out.push({ id: "snooze", label: c.label, minutes: c.minutes }) })
  if (snoozed) out.push({ id: "end", label: "End" })
  return out
}

// dndCalls: the Service calls a DND button makes, one per team:
// [{ method: "snooze" | "end", team, minutes }].
function dndCalls(button, teams) {
  if (button.id !== "snooze" && button.id !== "end") return []
  return teams.map(function(t) { return { method: button.id, team: t, minutes: button.minutes || 0 } })
}
