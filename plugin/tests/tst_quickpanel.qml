import QtQuick
import QtTest
import "../lib/quickpanel.js" as QP

// The quick-view panel's logic (lib/quickpanel.js): rows, previews, the
// keyboard cursor and the DND choices.
TestCase {
  name: "QuickPanel"

  readonly property var workspaces: [
    { team: "T1", name: "Acme" },
    { team: "T2", name: "Side" }
  ]

  function names(team, conv) { return conv === "D1" ? "Ana" : "#" + conv }

  // ---- rows ---------------------------------------------------------------

  function test_rows_keep_the_service_order_and_shape() {
    var unread = [
      { team: "T1", conversation: { id: "D1", kind: "im", latest: "200.1", mentions: 0, unread: true } },
      { team: "T2", conversation: { id: "C9", kind: "channel", latest: "100.1", mentions: 3, unread: true } }
    ]
    var r = QP.rows(unread, workspaces, names)
    compare(r.length, 2)
    compare(r[0].key, "T1/D1")
    compare(r[0].team, "T1")
    compare(r[0].conv, "D1")
    compare(r[0].name, "Ana")
    compare(r[0].workspace, "Acme")
    compare(r[0].direct, true)
    compare(r[0].latest, "200.1")
    compare(r[1].name, "#C9")
    compare(r[1].workspace, "Side")
    compare(r[1].direct, false)
    compare(r[1].mentions, 3)
  }

  function test_rows_fall_back_to_the_team_id_and_tolerate_missing_fields() {
    var r = QP.rows([{ team: "T7", conversation: { id: "G1", kind: "mpim" } }], [], names)
    compare(r[0].workspace, "T7")
    compare(r[0].direct, true)
    compare(r[0].mentions, 0)
    compare(r[0].latest, "")
    compare(QP.rows(null, null, names).length, 0)
  }

  function test_badge_shows_mentions_only() {
    compare(QP.badge({ mentions: 0 }), "")
    compare(QP.badge({ mentions: 2 }), "2")
    compare(QP.badge({ mentions: 120 }), "99+")
  }

  // ---- previews -------------------------------------------------------------

  function test_plain_text_prefers_html_without_tags_or_entities() {
    compare(QP.plainText({ text: "<@U1> hi", html: "<b>@Ana</b> hi &amp; bye&nbsp;&lt;3&gt; &quot;x&quot; &#39;y&#39;" }),
            "@Ana hi & bye <3> \"x\" 'y'")
    compare(QP.plainText({ text: "line one\n\n  line   two", html: "" }), "line one line two")
    compare(QP.plainText({ text: "a<br>b", html: "a<br>b" }), "a b")
    compare(QP.plainText({}), "")
  }

  function test_shorten_cuts_on_a_word_with_an_ellipsis() {
    compare(QP.shorten("short", 10), "short")
    compare(QP.shorten("hello wonderful world", 12), "hello…")
    compare(QP.shorten("abcdefghijklmnop", 6), "abcde…")
  }

  function test_preview_takes_the_newest_messages_oldest_first() {
    var history = [
      { ts: "300.0", author: "U2", text: "third" },
      { ts: "100.0", author: "U1", text: "first" },
      { ts: "200.0", author: "U1", text: "second" }
    ]
    var p = QP.preview(history, function(u) { return u === "U1" ? "Ana" : "Bo" }, 2, 80)
    compare(p.length, 2)
    compare(p[0].author, "Ana")
    compare(p[0].text, "second")
    compare(p[1].author, "Bo")
    compare(p[1].text, "third")
  }

  // A line keeps the name Slack put on a bot's message, so the panel
  // names it without users.info.
  function test_preview_keeps_a_bots_name() {
    var p = QP.preview([{ ts: "1.0", author: "B1", authorName: "deploybot", text: "deployed" }],
                       function(u) { return u }, 3, 80)
    compare(p[0].author, "B1")
    compare(p[0].authorName, "deploybot")
    compare(QP.preview([{ ts: "1.0", author: "U1", text: "hi" }], function(u) { return u }, 3, 80)[0].authorName, "")
  }

  function test_preview_skips_empty_messages_and_names_unknown_authors() {
    var p = QP.preview([{ ts: "1.0", text: "  " }, { ts: "2.0", text: "file only", author: "" }],
                       function(u) { return u }, 3, 80)
    compare(p.length, 1)
    compare(p[0].author, "")
    compare(QP.preview(null, null, 3, 80).length, 0)
  }

  function test_preview_is_refetched_only_when_latest_changes() {
    var row = { key: "T1/C1", latest: "5.0" }
    var cache = {}
    verify(QP.needsPreview(cache, {}, row))
    cache["T1/C1"] = { latest: "5.0", lines: [] }
    verify(!QP.needsPreview(cache, {}, row))
    verify(QP.needsPreview(cache, {}, { key: "T1/C1", latest: "6.0" }))
    verify(!QP.needsPreview({}, { "T1/C1": "6.0" }, { key: "T1/C1", latest: "6.0" }))
  }

  function test_in_view_is_true_when_any_part_of_the_row_shows() {
    verify(QP.inView(0, 50, 0, 100))
    verify(QP.inView(90, 50, 0, 100))
    verify(!QP.inView(101, 50, 0, 100))
    verify(!QP.inView(0, 50, 60, 100))
  }

  // ---- reply ------------------------------------------------------------------

  function test_can_send_needs_text() {
    verify(!QP.canSend(""))
    verify(!QP.canSend("  \n"))
    verify(!QP.canSend(undefined))
    verify(QP.canSend(" ok "))
  }

  // ---- cursor -----------------------------------------------------------------

  function test_cursor_moves_down_the_rows_and_stops_at_the_end() {
    var c = { section: "rows", index: 0 }
    c = QP.move(c, 0, 1, 4, 3)
    compare(c.index, 1)
    c = QP.move(QP.move(c, 0, 1, 4, 3), 0, 1, 4, 3)
    compare(c.section, "rows")
    compare(c.index, 2)
  }

  function test_cursor_goes_up_from_the_first_row_into_the_dnd_header_and_back() {
    var c = QP.move({ section: "rows", index: 0 }, 0, -1, 4, 3)
    compare(c.section, "dnd")
    compare(c.index, 0)
    c = QP.move(c, 1, 0, 4, 3)
    compare(c.index, 1)
    c = QP.move(c, 9, 0, 4, 3)
    compare(c.index, 3)
    c = QP.move(c, 0, -1, 4, 3)
    compare(c.section, "dnd")
    c = QP.move(c, 0, 1, 4, 3)
    compare(c.section, "rows")
    compare(c.index, 0)
  }

  function test_cursor_without_rows_stays_in_the_header() {
    var c = QP.move({ section: "dnd", index: 0 }, 0, 1, 4, 0)
    compare(c.section, "dnd")
    c = QP.clamp({ section: "rows", index: 5 }, 4, 0)
    compare(c.section, "dnd")
    compare(c.index, 0)
  }

  function test_clamp_keeps_the_cursor_on_a_real_row() {
    var c = QP.clamp({ section: "rows", index: 5 }, 4, 2)
    compare(c.section, "rows")
    compare(c.index, 1)
    c = QP.clamp({ section: "dnd", index: 7 }, 3, 2)
    compare(c.index, 2)
  }

  function test_index_of_key_follows_a_row_that_moved() {
    var rs = [{ key: "a" }, { key: "b" }]
    compare(QP.indexOfKey(rs, "b"), 1)
    compare(QP.indexOfKey(rs, "zz"), -1)
  }

  // ---- DND --------------------------------------------------------------------

  function test_dnd_scopes_offer_all_and_each_workspace_when_several() {
    var s = QP.dndScopes(workspaces)
    compare(s.length, 3)
    compare(s[0].team, "")
    compare(s[0].label, "All workspaces")
    compare(s[1].team, "T1")
    compare(s[2].label, "Side")
    compare(QP.dndScopes([workspaces[0]]).length, 1)
    compare(QP.dndScopes([workspaces[0]])[0].team, "")
  }

  function test_dnd_teams_of_a_scope() {
    compare(QP.dndTeams("", workspaces), ["T1", "T2"])
    compare(QP.dndTeams("T2", workspaces), ["T2"])
  }

  function test_minutes_until_tomorrow_morning() {
    compare(QP.minutesUntilTomorrow(new Date(2026, 10, 27, 22, 30), 9), 630)
    compare(QP.minutesUntilTomorrow(new Date(2026, 10, 27, 1, 0), 9), 32 * 60)
  }

  function test_dnd_choices_map_to_minutes() {
    var c = QP.dndChoices(new Date(2026, 10, 27, 22, 30))
    compare(c.map(function(x) { return x.minutes }), [30, 60, 630])
    compare(c[0].label, "30 min")
    compare(c[2].label, "Tomorrow")
  }

  function test_dnd_status_is_snoozed_when_every_team_is() {
    var dnd = { T1: { snoozed: true, until: 500 }, T2: { snoozed: true, until: 300 } }
    var s = QP.dndStatus(dnd, ["T1", "T2"])
    verify(s.snoozed)
    compare(s.until, 300)
    verify(!QP.dndStatus({ T1: { snoozed: true, until: 5 } }, ["T1", "T2"]).snoozed)
    verify(!QP.dndStatus(null, []).snoozed)
  }

  function test_clock_label_is_local_hours_and_minutes() {
    compare(QP.clockLabel(new Date(2026, 10, 27, 7, 5).getTime() / 1000), "07:05")
  }

  function test_dnd_buttons_cycle_scope_then_choices_then_end_when_snoozed() {
    var scopes = QP.dndScopes(workspaces)
    var choices = QP.dndChoices(new Date(2026, 10, 27, 12, 0))
    var b = QP.dndButtons(scopes, 1, choices, false, false)
    compare(b.map(function(x) { return x.id }), ["scope", "snooze", "snooze", "snooze", "presence"])
    compare(b[0].label, "Acme")
    compare(b[4].label, "Away")
    compare(b[4].away, true)
    b = QP.dndButtons(QP.dndScopes([workspaces[0]]), 0, choices, true, true)
    compare(b.map(function(x) { return x.id }), ["snooze", "snooze", "snooze", "end", "presence"])
    compare(b[3].label, "End")
    compare(b[4].label, "Back")
    compare(b[4].away, false)
  }

  function test_away_status_needs_every_team_of_the_scope_away() {
    var ws = [{ team: "T1", self: "U1" }, { team: "T2", self: "U2" }]
    var presence = { T1: { U1: "away" }, T2: { U2: "away", U9: "active" } }
    verify(QP.awayStatus(presence, ws, ["T1", "T2"]))
    presence.T2.U2 = "active"
    verify(!QP.awayStatus(presence, ws, ["T1", "T2"]))
    verify(QP.awayStatus(presence, ws, ["T1"]))
    verify(!QP.awayStatus(null, ws, ["T1"]))
    verify(!QP.awayStatus(presence, ws, []))
  }

  function test_dnd_calls_for_a_button_cover_every_team_of_the_scope() {
    var calls = QP.dndCalls({ id: "snooze", minutes: 60 }, ["T1", "T2"])
    compare(calls.length, 2)
    compare(calls[0].method, "snooze")
    compare(calls[1].team, "T2")
    compare(calls[1].minutes, 60)
    calls = QP.dndCalls({ id: "end" }, ["T1"])
    compare(calls[0].method, "end")
    compare(QP.dndCalls({ id: "scope" }, ["T1"]).length, 0)
    calls = QP.dndCalls({ id: "presence", away: true }, ["T1", "T2"])
    compare(calls.length, 2)
    compare(calls[1].method, "presence")
    compare(calls[1].away, true)
  }

  function test_next_scope_wraps() {
    compare(QP.nextScope(0, 3), 1)
    compare(QP.nextScope(2, 3), 0)
    compare(QP.nextScope(0, 0), 0)
  }
}
