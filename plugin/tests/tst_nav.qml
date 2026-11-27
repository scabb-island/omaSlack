import QtQuick
import QtTest
import "../lib/nav.js" as N

// The window's navigation logic (lib/nav.js): the conversation finder.
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
}
