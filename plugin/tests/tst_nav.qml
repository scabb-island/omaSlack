import QtQuick
import QtTest
import "../lib/nav.js" as N
import "../lib/emoji.js" as E

// The window's navigation logic (lib/nav.js): the conversation finder
// and the compose autocomplete.
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
    compare(s[0].label, E.GLYPHS["tada"] + " :tada:")
    compare(s[1].label, ":tada_parrot:")
    compare(s[0].wire, "")
  }

  function test_emoji_table_skips_skin_tones() {
    verify(E.NAMES.indexOf("+1") >= 0)
    verify(E.NAMES.indexOf("skin-tone-2") < 0)
    compare(E.NAMES.slice().sort(), E.NAMES)
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
}
