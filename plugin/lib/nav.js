.pragma library
.import "actions.js" as A
.import "store.js" as Store

// The full window's navigation logic (task 4.5): the conversation finder
// and the compose autocomplete, pure so qmltestrunner covers it (tests/tst_nav.qml). It follows the
// TUI (internal/tui/app) rule for rule, so both clients rank and complete
// the same way.

// ---- finder ---------------------------------------------------------------

// Fuzzy match scores, best first (the TUI's fuzzy.go). A subsequence
// scores below substring matches, less for every character it skips, and
// never below 1.
var SCORE_EXACT = 1000
var SCORE_PREFIX = 800
var SCORE_WORD = 600 // the query starts a word: "dev" in "team-dev"
var SCORE_SUBSTRING = 400
var SCORE_SUBSEQ = 200

// What needs attention ranks higher: an unread conversation gains
// FINDER_UNREAD, one with mentions FINDER_MENTIONS more. Both stay below
// the gap between two match kinds, so a better match always wins and
// attention breaks ties among equal ones.
var FINDER_UNREAD = 100
var FINDER_MENTIONS = 50

// fuzzyScore: how well query matches name, ignoring case (-1: no match).
// An empty query matches everything with 0.
function fuzzyScore(name, query) {
  name = name.toLowerCase()
  query = query.toLowerCase()
  if (query === "") return 0
  if (name === query) return SCORE_EXACT
  var i = name.indexOf(query)
  if (i === 0) return SCORE_PREFIX
  if (i > 0) return " -_.,".indexOf(name[i - 1]) >= 0 ? SCORE_WORD : SCORE_SUBSTRING
  return subsequence(name, query)
}

// subsequence matches query's characters in order inside name, scoring
// SCORE_SUBSEQ less one per character skipped between the first and the
// last match.
function subsequence(name, query) {
  var q = 0, gaps = 0
  for (var i = 0; i < name.length && q < query.length; i++) {
    if (name[i] === query[q]) q++
    else if (q > 0) gaps++
  }
  return q === query.length ? Math.max(SCORE_SUBSEQ - gaps, 1) : -1
}

// finderTitle: a conversation as the TUI's sidebar names it, with its kind
// as a sign: #channel, ~private, @person, &group. name is
// service.conversationName (which puts a # on channels).
function finderTitle(kind, name) {
  var bare = (name || "").replace(/^#/, "")
  var sign = { im: "@", mpim: "&", "private": "~" }[kind] || "#"
  return sign + bare
}

// finderEntries: every conversation of every workspace (in the daemon's
// order, then any other team with conversations), for rankConversations.
// nameOf(team, conv) is service.conversationName.
function finderEntries(workspaces, conversations, nameOf) {
  var list = (workspaces || []).slice()
  var convs = conversations || {}
  var known = list.map(function(w) { return w.team })
  for (var t in convs) if (known.indexOf(t) < 0) list.push({ team: t, name: t })
  var out = []
  list.forEach(function(w) {
    (convs[w.team] || []).forEach(function(c) {
      out.push({
        key: w.team + "/" + c.id,
        team: w.team,
        conv: c.id,
        title: finderTitle(c.kind, nameOf(w.team, c.id)),
        workspace: w.name || w.team,
        unread: !!c.unread || c.mentions > 0,
        mentions: c.mentions || 0,
        latest: c.latest || ""
      })
    })
  })
  return out
}

function attention(e) {
  var boost = 0
  if (e.unread || e.mentions > 0) boost += FINDER_UNREAD
  if (e.mentions > 0) boost += FINDER_MENTIONS
  return boost
}

// rankConversations keeps the entries matching query, best first:
//   - a query starting with #, ~, @ or & keeps only that kind; the rest is
//     matched against the title without its sign;
//   - score: the match (fuzzyScore) plus the attention boost;
//   - ties: the most recent message first, then by title, then in the
//     order given (workspaces in the sidebar's order).
function rankConversations(entries, query) {
  var sign = query !== "" && "#~@&".indexOf(query[0]) >= 0 ? query[0] : ""
  var rest = sign ? query.slice(1) : query
  var scored = []
  entries.forEach(function(e, i) {
    if (sign && e.title[0] !== sign) return
    var s = fuzzyScore(e.title.slice(1), rest)
    if (s >= 0) scored.push({ e: e, score: s + attention(e), i: i })
  })
  scored.sort(function(a, b) {
    if (a.score !== b.score) return b.score - a.score
    if (a.e.latest !== b.e.latest) return a.e.latest > b.e.latest ? -1 : 1
    if (a.e.title !== b.e.title) return a.e.title < b.e.title ? -1 : 1
    return a.i - b.i
  })
  return scored.map(function(s) { return s.e })
}

// ---- compose autocomplete -------------------------------------------------------
//
// Mention tokens, as in the TUI (internal/tui/app/suggest.go). The
// composer shows people and channels the way people read them ("@Ana",
// "#general") and sends them the way Slack wants them ("<@U1>", "<#C1>").
// Accepting a suggestion records its display token and wire token in a map
// the window keeps for the session; sending runs toWire, which swaps every
// display token standing as a whole word for its wire token. The field
// stays plain text, with no hidden markup to keep in step with the cursor.
// Drafts keep the display text. Editing a message does the reverse with
// toDisplay, which also returns the tokens it found.

// How many suggestions the popup shows.
var MAX_SUGGESTIONS = 8

// How much a sign needs typed after it before suggesting: users.search
// needs a letter, and two letters keep ":" in "10:30" or a smiley from
// opening the emoji list.
var MIN_QUERY = { "@": 1, "#": 0, ":": 2 }

// Titles of the popup, by sign.
var POPUP_TITLES = { "@": "People", "#": "Channels", ":": "Emoji" }

// isNameChar: c can be part of a name, a channel or an emoji code. Any
// non-ASCII character but a space counts as a letter (the JS engine has no
// Unicode classes).
function isNameChar(c) {
  return /[A-Za-z0-9_\-.+']/.test(c) || (c > "\u007f" && !/\s/.test(c))
}

function opensWord(c) { return /\s/.test(c) || "([{\"'".indexOf(c) >= 0 }

// findTrigger: the word being completed at cursor in text, on the
// cursor's line: { sign ("@" people, "#" channels, ":" emoji), query
// (what follows the sign), start (the sign's index in text) }, or null.
// The sign must start a word: "me@an" is not a mention.
function findTrigger(text, cursor) {
  var lineStart = text.lastIndexOf("\n", cursor - 1) + 1
  var i = cursor
  while (i > lineStart && isNameChar(text[i - 1])) i--
  if (i === lineStart) return null
  var start = i - 1
  var sign = text[start]
  var query = text.slice(i, cursor)
  if (!(sign in MIN_QUERY) || query.length < MIN_QUERY[sign]) return null
  if (start > lineStart && !opensWord(text[start - 1])) return null
  return { sign: sign, query: query, start: start }
}

function sameTrigger(a, b) {
  return !!a && !!b && a.sign === b.sign && a.query === b.query && a.start === b.start
}

// completionStep follows the composer after an edit or a cursor move.
// state is { trig, dismissed } (dismissed: the start of the trigger Esc
// closed the popup on, -1 for none); found is findTrigger's answer. It
// returns the next state plus what to do: close (hide the popup), changed
// (a new trigger: suggest for it) and clearItems (another kind of list:
// the old one would mislead).
function completionStep(state, found) {
  var dismissed = state.dismissed
  if (dismissed >= 0 && (!found || found.start !== dismissed)) dismissed = -1
  if (!found || dismissed >= 0)
    return { trig: null, dismissed: dismissed, close: true, changed: false, clearItems: true }
  var changed = !sameTrigger(found, state.trig)
  return {
    trig: found,
    dismissed: -1,
    close: false,
    changed: changed,
    clearItems: changed && (!state.trig || state.trig.sign !== found.sign)
  }
}

// dismiss: Esc closed the popup; it stays closed on this trigger.
function dismiss(state) {
  return { trig: null, dismissed: state.trig ? state.trig.start : -1, close: true, changed: false, clearItems: true }
}

// cycle moves a selection by d over n items, wrapping.
function cycle(sel, n, d) { return n > 0 ? ((sel + d) % n + n) % n : 0 }

// score grades how name matches q, both lower case: lower is better, -1
// for no match.
function score(name, q) {
  if (name === q) return 0
  if (name.indexOf(q) === 0) return 1
  for (var i = 0; i < name.length; i++)
    if (" _-.".indexOf(name[i]) >= 0 && name.indexOf(q, i + 1) === i + 1) return 2
  return name.indexOf(q) >= 0 ? 3 : -1
}

// rank: the indexes of the names matching q, best first: exact, prefix, a
// word inside the name, anywhere; shorter names first within each, then
// by name. Case does not matter.
function rank(names, q) {
  q = q.toLowerCase()
  var hits = []
  names.forEach(function(n, i) {
    var s = score(n.toLowerCase(), q)
    if (s >= 0) hits.push({ i: i, s: s })
  })
  hits.sort(function(a, b) {
    if (a.s !== b.s) return a.s - b.s
    var na = names[a.i], nb = names[b.i]
    if (na.length !== nb.length) return na.length - nb.length
    if (na !== nb) return na < nb ? -1 : 1
    return a.i - b.i
  })
  return hits.map(function(h) { return h.i })
}

function capped(list) { return list.slice(0, MAX_SUGGESTIONS) }

// The mentions that notify groups, with what they do.
var SPECIAL_MENTIONS = [
  ["here", "notify the active members"],
  ["channel", "notify everyone in the channel"],
  ["everyone", "notify the whole workspace"]
]

// userLabel: how a wire.User is called (as the TUI's State.UserName).
function userLabel(u) { return u.displayName || u.realName || u.name || u.id }

// peopleSuggestions turns users.search's answer into suggestions
// ({ label, text, wire }): the best local matches first, then the rest in
// the daemon's order (it may match on what the window does not see, like
// an email), then @here, @channel and @everyone when q starts them.
function peopleSuggestions(users, q) {
  var lq = q.toLowerCase()
  function best(u) {
    var s = 4
    ;[u.displayName, u.realName, u.name].forEach(function(n) {
      var g = n ? score(n.toLowerCase(), lq) : -1
      if (g >= 0) s = Math.min(s, g)
    })
    return s
  }
  var sorted = (users || []).map(function(u, i) { return { u: u, i: i, s: best(u) } })
  sorted.sort(function(a, b) {
    if (a.s !== b.s) return a.s - b.s
    var la = userLabel(a.u).length, lb = userLabel(b.u).length
    return la !== lb ? la - lb : a.i - b.i
  })
  var out = sorted.map(function(x) { return personSuggestion(x.u) })
  SPECIAL_MENTIONS.forEach(function(sp) {
    if (sp[0].indexOf(lq) === 0) out.push({ label: "@" + sp[0] + " · " + sp[1], text: "@" + sp[0], wire: "<!" + sp[0] + ">" })
  })
  return capped(out)
}

// personSuggestion shows the name the composer will get and, to tell
// people apart, the first other name they have.
function personSuggestion(u) {
  var name = userLabel(u)
  var label = name
  var others = [u.realName, u.name]
  for (var i = 0; i < others.length; i++) {
    if (others[i] && others[i] !== name) { label += " · " + others[i]; break }
  }
  return { label: label, text: "@" + name, wire: "<@" + u.id + ">" }
}

// channelSuggestions offers the workspace's channels, public and private.
function channelSuggestions(convs, q) {
  var chans = (convs || []).filter(function(c) { return c.kind === "channel" || c.kind === "private" })
  var names = chans.map(function(c) { return c.name || c.id })
  return capped(rank(names, q)).map(function(i) {
    return { label: "#" + names[i], text: "#" + names[i], wire: "<#" + chans[i].id + ">" }
  })
}

// emojiSuggestions offers the standard emoji (with their glyph) and the
// workspace's custom ones (custom: their names, from emoji.list) matching
// q. The text is the shortcode, already what Slack wants.
// The standard ones are lib/actions.js STANDARD (shared with the reaction
// picker), sorted by name as the TUI's StandardEmojiNames.
var STANDARD_NAMES = A.STANDARD.map(function(e) { return e[0] }).sort()

function emojiSuggestions(custom, q) {
  var names = STANDARD_NAMES.concat((custom || []).filter(function(n) { return STANDARD_NAMES.indexOf(n) < 0 }))
  var standard = STANDARD_NAMES.length
  return capped(rank(names, q)).map(function(i) {
    var code = ":" + names[i] + ":"
    return { label: i < standard ? A.emojiGlyph(names[i]) + " " + code : code, text: code, wire: "" }
  })
}

// accept replaces the trigger's word (its sign up to cursor) with the
// suggestion's text and a space: { text, cursor }.
function accept(text, cursor, trig, suggestion) {
  var insert = suggestion.text + " "
  return { text: text.slice(0, trig.start) + insert + text.slice(cursor), cursor: trig.start + insert.length }
}

// withToken: tokens plus the suggestion's display -> wire pair (a copy).
function withToken(tokens, suggestion) {
  var out = Object.assign({}, tokens || {})
  if (suggestion.wire) out[suggestion.text] = suggestion.wire
  return out
}

function isWordChar(c) { return c !== undefined && c !== "" && (/[A-Za-z0-9_]/.test(c) || (c > "\u007f" && !/\s/.test(c))) }

// toWire swaps every display token in text that stands as a whole word for
// its wire token, longest first, so "@Ana Zed" wins over "@Ana".
function toWire(text, tokens) {
  var keys = Object.keys(tokens || {}).sort(function(a, b) { return b.length - a.length })
  if (keys.length === 0) return text
  var out = ""
  var i = 0
  while (i < text.length) {
    var k = i > 0 && isWordChar(text[i - 1]) ? null : tokenAt(text, i, keys)
    if (k) { out += tokens[k]; i += k.length; continue }
    out += text[i]
    i++
  }
  return out
}

function tokenAt(text, i, keys) {
  for (var j = 0; j < keys.length; j++) {
    var k = keys[j]
    if (text.substr(i, k.length) === k && !isWordChar(text[i + k.length])) return k
  }
  return null
}

// toDisplay is toWire's reverse, for loading a message into the composer:
// the mentions of people and channels it can name become display tokens,
// and Slack's escapes become the characters they stand for (after the
// mentions, so "&lt;@U1&gt;" is not taken for one). A mention it cannot
// name stays as Slack wrote it. userName(id) and convName(id) answer ""
// when they do not know. Returns { text, tokens }.
function toDisplay(text, userName, convName) {
  var tokens = {}
  var out = text.replace(/<([@#!])([^>|]*)(?:\|([^>]*))?>/g, function(match, kind, id, label) {
    var sign = kind === "#" ? "#" : "@"
    var name = kind === "@" ? (userName(id) || label || "")
      : kind === "#" ? (convName(id) || label || "")
      : specialName(id)
    if (!name) return match
    tokens[sign + name] = "<" + kind + id + ">"
    return sign + name
  })
  out = out.replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&")
  return { text: out, tokens: tokens }
}

function specialName(id) {
  for (var i = 0; i < SPECIAL_MENTIONS.length; i++) if (SPECIAL_MENTIONS[i][0] === id) return id
  return ""
}

// ---- window keys ------------------------------------------------------------------

// navKey: the window key (key a Qt.Key_*, t its text, ctrl whether
// Control is held) as one of this file's actions, or null: Ctrl+K or
// Ctrl+T the finder (as the TUI), / search, ? help, 1–9 a workspace
// (index 0–8). The chord is told by key, since its text is a control
// character on some keyboards and the plain letter on others.
function navKey(key, t, ctrl) {
  if (ctrl) return key === Qt.Key_K || key === Qt.Key_T ? { action: "finder" } : null
  if (t === "/") return { action: "search" }
  if (t === "?") return { action: "help" }
  if (t >= "1" && t <= "9" && t.length === 1) return { action: "workspace", index: t.charCodeAt(0) - 49 }
  return null
}

// workspaceJump: where 1–9 puts the sidebar cursor: the first conversation
// of the index-th workspace in the sidebar's rows (lib/window.js
// sidebarRows), or index -1 and why not.
function workspaceJump(rows, index) {
  var seen = -1
  for (var i = 0; i < rows.length; i++) {
    if (rows[i].type === "workspace") seen++
    else if (seen === index && rows[i].type === "conv") return { index: i, error: "" }
    if (seen > index) break
  }
  var error = seen >= index ? "Workspace " + (index + 1) + " has no conversations yet." : "There is no workspace " + (index + 1) + "."
  return { index: -1, error: error }
}

// ---- search -------------------------------------------------------------------------

function escapeHtml(s) { return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;") }

// searchRows: search.messages's hits ([wire.SearchHit]) of one workspace as
// the result list shows them: { key, conv, ts, root (the top-level
// message: the thread's root for a reply), where (the conversation, as
// the finder names it), who (the bot's name Slack sent, else
// userName), seconds (epoch, for the date), html }.
// convs are the workspace's conversations; nameOf(conv) and userName(user)
// are the Service's, bound to the workspace.
function searchRows(hits, convs, nameOf, userName) {
  var kinds = {}
  ;(convs || []).forEach(function(c) { kinds[c.id] = c.kind })
  return (hits || []).map(function(h, i) {
    var m = h.message || {}
    return {
      key: h.conv + "/" + m.ts + "/" + i,
      conv: h.conv,
      ts: m.ts,
      root: m.threadTS || m.ts,
      where: finderTitle(kinds[h.conv] || "channel", nameOf(h.conv)),
      who: Store.authorName(m, userName) || m.author || "",
      seconds: parseInt(m.ts, 10) || 0,
      html: m.html || escapeHtml(m.text || "")
    }
  })
}

// containsTS: whether messages ([wire.Message]) hold the message ts.
function containsTS(messages, ts) {
  for (var i = 0; i < messages.length; i++) if (messages[i].ts === ts) return true
  return false
}

// ---- help ---------------------------------------------------------------------------

// HELP: every window key, in sections, for the help overlay (?). Keep it
// in step with Window.qml's keys.
var HELP = [
  { title: "Move", keys: [
    ["j/↓", "move down (newer)"],
    ["k/↑", "move up (older; loads history at the top)"],
    ["g", "go to the oldest message"],
    ["G", "go to the newest"],
    ["h/←", "pane to the left"],
    ["l/→", "pane to the right"],
    ["Tab", "next pane"],
    ["Shift+Tab", "previous pane"]
  ] },
  { title: "Conversations", keys: [
    ["Enter", "open the conversation / the message's thread"],
    ["1–9", "switch workspace"],
    ["Ctrl+K", "find a conversation in any workspace (Ctrl+T too)"],
    ["/", "search messages in this workspace"]
  ] },
  { title: "Messages", keys: [
    ["i", "write a message"],
    ["R", "reply in the message's thread"],
    ["e", "edit your message"],
    ["D/x/Del", "delete your message (y confirms)"],
    ["r", "react to the message"],
    ["o", "open the message's file (1–9 picks one of several)"]
  ] },
  { title: "Compose", keys: [
    ["Enter", "send"],
    ["Shift+Enter", "new line"],
    [":attach", ":attach <path> [caption] uploads a file here"],
    ["Esc", "leave the composer"]
  ] },
  { title: "Autocomplete", keys: [
    ["@ # :", "suggest people, channels, emoji"],
    ["Tab/Enter", "pick the suggestion"],
    ["↑/↓", "previous / next suggestion"],
    ["Esc", "close the suggestions"]
  ] },
  { title: "Lists (finder, search)", keys: [
    ["↑/↓", "previous / next (Ctrl+P / Ctrl+N too)"],
    ["Enter", "open"],
    ["Esc", "close"]
  ] },
  { title: "Window", keys: [
    ["?", "this help"],
    ["Esc", "leave the composer, close the thread, then the window"]
  ] }
]
