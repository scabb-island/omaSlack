.pragma library

// The full window's navigation logic (task 4.5): the conversation finder,
// pure so qmltestrunner covers it (tests/tst_nav.qml). It follows the
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
