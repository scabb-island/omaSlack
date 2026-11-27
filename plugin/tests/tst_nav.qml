import QtQuick
import QtTest
import "../lib/nav.js" as N
import "../lib/actions.js" as A

// The window's navigation logic (lib/nav.js): the conversation finder,
// the compose autocomplete, search results, workspace keys and help.
TestCase {
  name: "Nav"

  function titles(entries) { return entries.map(function(e) { return e.title }) }

  // ---- finder ---------------------------------------------------------------

  function test_fuzzy_score_grades_the_match() {
    compare(N.fuzzyScore("general", ""), 0)
    compare(N.fuzzyScore("general", "General"), N.SCORE_EXACT)
    compare(N.fuzzyScore("general", "gen"), N.SCORE_PREFIX)
    compare(N.fuzzyScore("team-dev", "dev"), N.SCORE_WORD)
    compare(N.fuzzyScore("devteam", "team"), N.SCORE_SUBSTRING)
    // A subsequence loses one point per character skipped between the
    // first and the last match.
    compare(N.fuzzyScore("general", "gnl"), N.SCORE_SUBSEQ - 4)
    compare(N.fuzzyScore("general", "xyz"), -1)
    compare(N.fuzzyScore("ab", "abc"), -1)
  }

  function test_finder_titles_carry_the_kind_sign() {
    compare(N.finderTitle("channel", "#general"), "#general")
    compare(N.finderTitle("channel", "general"), "#general")
    compare(N.finderTitle("private", "#ops"), "~ops")
    compare(N.finderTitle("im", "Ana"), "@Ana")
    compare(N.finderTitle("mpim", "ana, bo"), "&ana, bo")
  }

  readonly property var workspaces: [{ team: "T1", name: "Acme" }, { team: "T2", name: "Side" }]
  readonly property var convs: ({
    T1: [
      { id: "C1", kind: "channel", name: "general", latest: "10.0" },
      { id: "C2", kind: "channel", name: "dev-general", latest: "30.0" },
      { id: "D1", kind: "im", user: "U1", latest: "20.0" }
    ],
    T2: [
      { id: "C9", kind: "channel", name: "gen", unread: true, latest: "5.0" },
      { id: "C8", kind: "private", name: "ops", mentions: 2, latest: "1.0" }
    ]
  })
  function names(team, conv) {
    return { "T1/C1": "#general", "T1/C2": "#dev-general", "T1/D1": "Ana", "T2/C9": "#gen", "T2/C8": "#ops" }[team + "/" + conv]
  }

  function test_finder_entries_cover_every_workspace() {
    var e = N.finderEntries(workspaces, convs, names)
    compare(titles(e), ["#general", "#dev-general", "@Ana", "#gen", "~ops"])
    compare(e[2].team, "T1")
    compare(e[2].conv, "D1")
    compare(e[3].workspace, "Side")
    compare(e[3].unread, true)
    compare(e[4].mentions, 2)
  }

  function test_finder_entries_include_teams_without_a_workspace_entry() {
    var e = N.finderEntries([], { T3: [{ id: "C1", kind: "channel", name: "x" }] }, function() { return "#x" })
    compare(titles(e), ["#x"])
    compare(e[0].workspace, "T3")
  }

  function test_rank_prefers_better_matches_then_attention() {
    var e = N.finderEntries(workspaces, convs, names)
    // gen: exact (#gen, unread) > prefix (#general) > word (#dev-general).
    compare(titles(N.rankConversations(e, "gen")), ["#gen", "#general", "#dev-general"])
  }

  function test_rank_breaks_ties_by_attention_then_recency() {
    var e = N.finderEntries(workspaces, convs, names)
    // Empty query: everything matches with 0. Mentions, then unread, then
    // the most recent first.
    compare(titles(N.rankConversations(e, "")), ["~ops", "#gen", "#dev-general", "@Ana", "#general"])
  }

  function test_rank_attention_never_beats_a_better_match() {
    var e = [
      { title: "#general", latest: "", unread: false, mentions: 0 },
      { title: "#my-gen", latest: "", unread: true, mentions: 9 }
    ]
    compare(titles(N.rankConversations(e, "gen")), ["#general", "#my-gen"])
  }

  function test_rank_a_sign_keeps_only_that_kind() {
    var e = N.finderEntries(workspaces, convs, names)
    compare(titles(N.rankConversations(e, "@")), ["@Ana"])
    compare(titles(N.rankConversations(e, "@an")), ["@Ana"])
    compare(titles(N.rankConversations(e, "~")), ["~ops"])
    compare(titles(N.rankConversations(e, "#ops")), [])
  }

  function test_rank_ties_go_by_name() {
    var e = [
      { title: "#b", latest: "1.0", unread: false, mentions: 0 },
      { title: "#a", latest: "1.0", unread: false, mentions: 0 }
    ]
    compare(titles(N.rankConversations(e, "")), ["#a", "#b"])
  }

  // ---- autocomplete: triggers ---------------------------------------------------

  function trig(text, cursor) {
    var t = N.findTrigger(text, cursor === undefined ? text.length : cursor)
    return t ? [t.sign, t.query, t.start] : null
  }

  function test_trigger_needs_a_sign_starting_a_word() {
    compare(trig("hi @an"), ["@", "an", 3])
    compare(trig("@"), null) // @ needs a letter
    compare(trig("#"), ["#", "", 0]) // # lists every channel
    compare(trig("(#ge"), ["#", "ge", 1])
    compare(trig("me@an"), null)
    compare(trig("at 10:30"), null)
    compare(trig(":t"), null) // : needs two letters
    compare(trig("a :ta"), [":", "ta", 2])
    compare(trig("@ana b"), null) // a space ends the word
  }

  function test_trigger_reads_the_cursor_line_only() {
    compare(trig("@ana\nx #de"), ["#", "de", 7])
    compare(trig("@ana\nbo", 4), ["@", "ana", 0])
    compare(trig("@ana\nbo"), null)
    compare(trig("hi @ana and", 7), ["@", "ana", 3])
  }

  function test_trigger_takes_non_ascii_names() {
    // Intentionally non-ASCII: accented names must complete.
    compare(trig("@josé"), ["@", "josé", 0])
  }

  // ---- autocomplete: popup state ---------------------------------------------

  function test_completion_opens_follows_and_closes() {
    var s = N.completionStep({ trig: null, dismissed: -1 }, N.findTrigger("@a", 2))
    compare(s.close, false)
    compare(s.changed, true)
    compare(s.trig.query, "a")
    var same = N.completionStep(s, N.findTrigger("@a", 2))
    compare(same.changed, false)
    var gone = N.completionStep(s, null)
    compare(gone.close, true)
    compare(gone.trig, null)
  }

  function test_completion_clears_items_when_the_sign_changes() {
    var s = N.completionStep({ trig: null, dismissed: -1 }, N.findTrigger("@a", 2))
    compare(N.completionStep(s, N.findTrigger("@ab", 3)).clearItems, false)
    compare(N.completionStep(s, N.findTrigger("#a", 2)).clearItems, true)
  }

  function test_completion_stays_dismissed_on_the_same_trigger() {
    var s = N.dismiss(N.completionStep({ trig: null, dismissed: -1 }, N.findTrigger("x @a", 4)))
    compare(s.trig, null)
    compare(s.dismissed, 2)
    var typed = N.completionStep(s, N.findTrigger("x @ab", 5))
    compare(typed.close, true)
    compare(typed.dismissed, 2)
    // Leaving the word forgets the dismissal; a new trigger opens again.
    var left = N.completionStep(typed, null)
    compare(left.dismissed, -1)
    var again = N.completionStep(left, N.findTrigger("x @ab @c", 8))
    compare(again.close, false)
    compare(again.trig.start, 6)
  }

  function test_cycle_wraps() {
    compare(N.cycle(0, 3, 1), 1)
    compare(N.cycle(2, 3, 1), 0)
    compare(N.cycle(0, 3, -1), 2)
    compare(N.cycle(0, 0, 1), 0)
  }

  // ---- autocomplete: suggestions -------------------------------------------------

  function test_rank_orders_exact_prefix_word_substring() {
    var names = ["xana", "ana-b", "bo_ana", "ana", "banana"]
    compare(N.rank(names, "ANA").map(function(i) { return names[i] }), ["ana", "ana-b", "bo_ana", "xana", "banana"])
    compare(N.rank(names, "zz"), [])
  }

  function test_people_suggestions_encode_mentions() {
    var users = [
      { id: "U2", name: "bo", realName: "Ana Bo" },
      { id: "U1", name: "ana", realName: "Ana Diaz", displayName: "Ana" }
    ]
    var s = N.peopleSuggestions(users, "an")
    compare(s.map(function(x) { return x.text }), ["@Ana", "@Ana Bo"])
    compare(s[0].wire, "<@U1>")
    compare(s[0].label, "Ana · Ana Diaz")
    compare(s[1].label, "Ana Bo · bo")
  }

  function test_people_suggestions_offer_special_mentions() {
    var s = N.peopleSuggestions([], "h")
    compare(s.length, 1)
    compare(s[0].text, "@here")
    compare(s[0].wire, "<!here>")
    compare(N.peopleSuggestions([], "e")[0].wire, "<!everyone>")
  }

  function test_people_suggestions_are_capped() {
    var users = []
    for (var i = 0; i < 20; i++) users.push({ id: "U" + i, name: "user" + i })
    compare(N.peopleSuggestions(users, "user").length, N.MAX_SUGGESTIONS)
  }

  function test_channel_suggestions_take_channels_only() {
    var convs = [
      { id: "C1", kind: "channel", name: "general" },
      { id: "C2", kind: "private", name: "dev-gen" },
      { id: "D1", kind: "im", name: "gen" }
    ]
    var s = N.channelSuggestions(convs, "gen")
    compare(s.map(function(x) { return x.text }), ["#general", "#dev-gen"])
    compare(s[1].wire, "<#C2>")
  }

  function test_emoji_suggestions_mix_standard_and_custom() {
    var s = N.emojiSuggestions(["tada_parrot", "partyparrot"], "tad")
    compare(s.map(function(x) { return x.text }), [":tada:", ":tada_parrot:"])
    compare(s[0].label, A.emojiGlyph("tada") + " :tada:")
    compare(s[1].label, ":tada_parrot:")
    compare(s[0].wire, "")
  }

  function test_emoji_names_are_the_shared_standard_ones_sorted() {
    compare(N.STANDARD_NAMES.length, A.STANDARD.length)
    compare(N.STANDARD_NAMES.slice().sort(), N.STANDARD_NAMES)
    // A custom emoji named like a standard one is listed once.
    compare(N.emojiSuggestions(["tada"], "tada").length, 1)
  }

  // ---- autocomplete: accepting and wire text -------------------------------------

  function test_accept_replaces_the_word_and_adds_a_space() {
    var t = N.findTrigger("hi @an there", 6)
    var r = N.accept("hi @an there", 6, t, { text: "@Ana", wire: "<@U1>" })
    compare(r.text, "hi @Ana  there")
    compare(r.cursor, 8)
  }

  function test_to_wire_swaps_whole_word_tokens_longest_first() {
    var tokens = { "@Ana": "<@U1>", "@Ana Bo": "<@U2>", "#general": "<#C1>" }
    compare(N.toWire("hi @Ana and @Ana Bo in #general.", tokens), "hi <@U1> and <@U2> in <#C1>.")
    compare(N.toWire("x@Ana @Anabel", tokens), "x@Ana @Anabel")
    compare(N.toWire("plain", {}), "plain")
  }

  function test_to_display_reverses_to_wire() {
    var users = { U1: "Ana" }
    var convs = { C1: "general" }
    var r = N.toDisplay("hi <@U1> in <#C1|general>, <!here> &lt;b&gt; <@U9|zed> <!subteam^S1>",
      function(id) { return users[id] || "" }, function(id) { return convs[id] || "" })
    compare(r.text, "hi @Ana in #general, @here <b> @zed <!subteam^S1>")
    compare(r.tokens["@Ana"], "<@U1>")
    compare(r.tokens["#general"], "<#C1>")
    compare(r.tokens["@here"], "<!here>")
    compare(N.toWire(r.text, r.tokens), "hi <@U1> in <#C1>, <!here> <b> <@U9> <!subteam^S1>")
  }

  // ---- window keys ---------------------------------------------------------------

  function test_nav_keys() {
    compare(N.navKey("\u000b", true), { action: "finder" }) // Ctrl+K
    compare(N.navKey("\u0014", true), { action: "finder" }) // Ctrl+T
    compare(N.navKey("k", true), { action: "finder" })
    compare(N.navKey("/", false), { action: "search" })
    compare(N.navKey("?", false), { action: "help" })
    compare(N.navKey("1", false), { action: "workspace", index: 0 })
    compare(N.navKey("9", false), { action: "workspace", index: 8 })
    compare(N.navKey("0", false), null)
    compare(N.navKey("1", true), null)
    compare(N.navKey("x", false), null)
  }

  readonly property var rows: [
    { type: "workspace", team: "T1" }, { type: "section" }, { type: "conv", team: "T1", conv: "C1" },
    { type: "workspace", team: "T2" }, { type: "section" }, { type: "conv", team: "T2", conv: "C9" },
    { type: "workspace", team: "T3" }
  ]

  function test_workspace_jump_lands_on_its_first_conversation() {
    compare(N.workspaceJump(rows, 0), { index: 2, error: "" })
    compare(N.workspaceJump(rows, 1), { index: 5, error: "" })
    compare(N.workspaceJump(rows, 2), { index: -1, error: "Workspace 3 has no conversations yet." })
    compare(N.workspaceJump(rows, 3), { index: -1, error: "There is no workspace 4." })
  }

  // ---- search -------------------------------------------------------------------

  function test_search_rows_say_where_who_and_when() {
    var hits = [
      { conv: "C1", message: { ts: "1700000000.000100", author: "U1", text: "a *b*", html: "a <b>b</b>" } },
      { conv: "D1", message: { ts: "1700000500.000200", threadTS: "1700000400.000100", author: "U2", text: "x" } },
      { conv: "C7", message: { ts: "1700000600.000100", author: "U3", text: "y" } }
    ]
    var convs = [{ id: "C1", kind: "channel" }, { id: "D1", kind: "im" }]
    var r = N.searchRows(hits, convs,
      function(conv) { return { C1: "#general", D1: "Ana" }[conv] || conv },
      function(user) { return { U1: "Ana", U2: "Bo" }[user] || user })
    compare(r.length, 3)
    compare(r[0].where, "#general")
    compare(r[0].who, "Ana")
    compare(r[0].seconds, 1700000000)
    compare(r[0].html, "a <b>b</b>")
    compare(r[0].root, "1700000000.000100")
    compare(r[1].where, "@Ana")
    compare(r[1].root, "1700000400.000100")
    compare(r[1].ts, "1700000500.000200")
    compare(r[1].html, "x") // plain text when the daemon sent no html
    compare(r[2].where, "#C7")
    compare(r[2].who, "U3")
    verify(r[0].key !== r[1].key)
  }

  function test_html_from_plain_text_is_escaped() {
    compare(N.searchRows([{ conv: "C1", message: { ts: "1.0", author: "U1", text: "a<b>&" } }], [],
      function(c) { return c }, function(u) { return u })[0].html, "a&lt;b&gt;&amp;")
  }

  function test_contains_ts() {
    var msgs = [{ ts: "1.0" }, { ts: "2.0" }]
    verify(N.containsTS(msgs, "2.0"))
    verify(!N.containsTS(msgs, "3.0"))
  }

  // ---- help -----------------------------------------------------------------------

  function test_help_lists_every_window_key() {
    var keys = []
    N.HELP.forEach(function(section) {
      verify(section.title !== "")
      section.keys.forEach(function(k) { verify(k[1] !== "", k[0]); keys.push(k[0]) })
    })
    ;["j/↓", "k/↑", "g", "G", "h/←", "l/→", "Tab", "Shift+Tab", "Enter", "1–9", "Ctrl+K", "i", "R",
      "e", "D/x/Del", "r", "o", ":attach", "/", "?", "Esc", "Shift+Enter", "@ # :", "Tab/Enter"].forEach(function(k) {
      verify(keys.indexOf(k) >= 0, k)
    })
  }
}
