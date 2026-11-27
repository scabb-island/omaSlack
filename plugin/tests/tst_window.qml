import QtQuick
import QtTest
import "../lib/window.js" as W

// The full window's logic (lib/window.js): the sidebar, message rows with
// day separators and author grouping, list syncing, links, drafts, marking
// read and the keyboard focus.
TestCase {
  name: "Window"

  function names(team, conv) { return { C1: "#general", C2: "#random", C3: "#ops", D1: "Ana", D2: "Bo", G1: "ana, bo" }[conv] || conv }

  function keys(rows) { return rows.map(function(r) { return r.key }) }

  // ---- sidebar --------------------------------------------------------------

  readonly property var workspaces: [
    { team: "T1", name: "Acme", state: "connected" },
    { team: "T2", name: "Side", state: "auth_expired" }
  ]

  function test_sidebar_lists_each_workspace_with_its_sections() {
    var convs = {
      T1: [
        { id: "C2", kind: "channel", name: "random" },
        { id: "D2", kind: "im", user: "U2" },
        { id: "C1", kind: "channel", name: "general", unread: true, latest: "10.0" },
        { id: "D1", kind: "im", user: "U1", unread: true, latest: "20.0" },
        { id: "C3", kind: "private", name: "ops", mentions: 2, unread: true, latest: "5.0" },
        { id: "G1", kind: "mpim", name: "mpdm-ana--bo-1" }
      ],
      T2: [{ id: "C1", kind: "channel", name: "general" }]
    }
    var summary = { workspaces: { T1: { dms: 1, mentions: 2, channels: 1, threads: 0 } }, total: {} }
    var rows = W.sidebarRows(workspaces, convs, summary, names)
    compare(keys(rows), [
      "T1", "T1#Unread", "T1/C3", "T1/D1", "T1/C1",
      "T1#Channels", "T1/C2",
      "T1#Direct messages", "T1/G1", "T1/D2",
      "T2", "T2#Channels", "T2/C1"
    ])
    compare(rows[0].type, "workspace")
    compare(rows[0].name, "Acme")
    compare(rows[0].badge, "3") // DMs + mentions, as the bar counts them
    compare(rows[1].type, "section")
    compare(rows[1].title, "Unread")
    var ops = rows[2]
    compare(ops.type, "conv")
    compare(ops.team, "T1")
    compare(ops.conv, "C3")
    compare(ops.name, "#ops")
    compare(ops.unread, true)
    compare(ops.badge, "2")
    compare(ops.direct, false)
    compare(rows[3].direct, true)
    compare(rows[3].badge, "")
    compare(rows[10].state, "auth_expired")
    compare(rows[10].badge, "")
  }

  function test_sidebar_adds_teams_that_only_have_conversations_and_tolerates_nothing() {
    var rows = W.sidebarRows([], { T9: [{ id: "C1", kind: "channel", name: "general" }] }, null, names)
    compare(keys(rows), ["T9", "T9#Channels", "T9/C1"])
    compare(rows[0].name, "T9")
    compare(W.sidebarRows(null, null, null, names).length, 0)
  }

  function test_step_skips_headers_and_stops_at_the_ends() {
    var rows = [
      { type: "workspace" }, { type: "section" }, { type: "conv" }, { type: "conv" },
      { type: "section" }, { type: "conv" }
    ]
    compare(W.step(rows, 2, 1), 3)
    compare(W.step(rows, 3, 1), 5)
    compare(W.step(rows, 5, 1), 5)
    compare(W.step(rows, 5, -1), 3)
    compare(W.step(rows, 2, -1), 2)
    compare(W.step(rows, -1, 1), 2) // no cursor yet: the first conversation
    compare(W.step([{ type: "workspace" }], -1, 1), -1)
  }

  // ---- messages ---------------------------------------------------------------

  // Local times, so day labels do not depend on the machine's time zone.
  function ts(y, mo, d, h, mi) { return (new Date(y, mo - 1, d, h, mi).getTime() / 1000).toFixed(0) + ".000100" }

  function test_message_rows_mark_new_days_and_group_by_author() {
    var now = new Date(2026, 10, 27, 12, 0)
    var msgs = [
      { ts: ts(2026, 11, 25, 9, 0), author: "U1", text: "a" },
      { ts: ts(2026, 11, 26, 9, 0), author: "U1", text: "b" },
      { ts: ts(2026, 11, 26, 9, 3), author: "U1", text: "c" },
      { ts: ts(2026, 11, 26, 9, 4), author: "U2", text: "d" },
      { ts: ts(2026, 11, 26, 9, 30), author: "U2", text: "e" },
      { ts: ts(2026, 11, 27, 8, 0), author: "U2", text: "f" }
    ]
    var rows = W.messageRows(msgs, now)
    compare(keys(rows), msgs.map(function(m) { return m.ts }))
    compare(rows.map(function(r) { return r.day }), ["Wednesday, November 25", "Yesterday", "", "", "", "Today"])
    compare(rows.map(function(r) { return r.grouped }), [false, false, true, false, false, false])
    compare(rows[0].time, "09:00")
    verify(rows[2].message === msgs[2], "rows carry the message itself")
  }

  function test_day_label_adds_the_year_when_it_is_not_this_one() {
    var now = new Date(2026, 0, 2, 10, 0)
    compare(W.dayLabel(new Date(2025, 11, 30, 10, 0), now), "Tuesday, December 30, 2025")
    compare(W.dayLabel(new Date(2026, 0, 1, 23, 59), now), "Yesterday")
  }

  function test_thread_label_counts_replies() {
    compare(W.threadLabel({}), "")
    compare(W.threadLabel({ replyCount: 1 }), "↳ 1 reply")
    compare(W.threadLabel({ replyCount: 12 }), "↳ 12 replies")
  }

  function test_reactions_count_users_and_mark_your_own() {
    var chips = W.reactionChips([{ emoji: "+1", users: ["U1", "U2"] }, { emoji: "tada", users: ["U3"] }], "U3")
    compare(chips.length, 2)
    compare(chips[0].label, ":+1: 2")
    compare(chips[0].mine, false)
    compare(chips[1].label, ":tada: 1")
    compare(chips[1].mine, true)
    compare(W.reactionChips(undefined, "U1").length, 0)
  }

  function test_file_label_names_the_file_with_its_size() {
    compare(W.fileLabel({ name: "a.txt", size: 0 }), "a.txt")
    compare(W.fileLabel({ name: "a.txt", size: 900 }), "a.txt (900 B)")
    compare(W.fileLabel({ name: "a.png", title: "Screenshot", size: 2048 }), "Screenshot (2.0 KB)")
    compare(W.fileLabel({ name: "v.mp4", size: 5 * 1024 * 1024 }), "v.mp4 (5.0 MB)")
  }

  function test_body_prefers_the_daemons_rich_text() {
    compare(W.body({ text: "*hi*", html: "<b>hi</b>" }), { rich: true, text: "<b>hi</b>" })
    compare(W.body({ text: "*hi*" }), { rich: false, text: "*hi*" })
    compare(W.body({}), { rich: false, text: "" })
  }

  // ---- links ------------------------------------------------------------------

  function test_links_open_conversations_or_the_browser() {
    compare(W.linkAction("omaslack://conv/T1/C9"), { kind: "conv", team: "T1", conv: "C9" })
    compare(W.linkAction("omaslack://user/T1/U1"), { kind: "user", team: "T1", user: "U1" })
    compare(W.linkAction("https://example.com/a?b"), { kind: "external", url: "https://example.com/a?b" })
    compare(W.linkAction("http://x"), { kind: "external", url: "http://x" })
    compare(W.linkAction("mailto:a@b.c"), { kind: "external", url: "mailto:a@b.c" })
    compare(W.linkAction("file:///etc/passwd"), { kind: "ignore" })
    compare(W.linkAction("omaslack://conv/T1"), { kind: "ignore" })
    compare(W.linkAction(""), { kind: "ignore" })
  }

  // ---- list syncing -------------------------------------------------------------

  // apply runs ops on a plain array the way MessageList runs them on its
  // ListModel.
  function apply(list, ops) {
    var out = list.slice()
    ops.forEach(function(op) {
      if (op.op === "remove") out.splice(op.index, 1)
      else out.splice(op.index, 0, op.key)
    })
    return out
  }

  function test_sync_ops_turn_the_old_keys_into_the_new_ones() {
    var cases = [
      [[], ["1", "2"]],
      [["3", "4"], ["1", "2", "3", "4"]],     // older page
      [["1", "2"], ["1", "2", "3"]],           // new message
      [["1", "2", "3"], ["1", "3"]],           // deleted
      [["1", "2", "3"], []],
      [["2", "4", "6"], ["1", "2", "5", "6", "7"]]
    ]
    cases.forEach(function(c) { compare(apply(c[0], W.syncOps(c[0], c[1])), c[1], JSON.stringify(c)) })
    compare(W.syncOps(["1", "2"], ["1", "2"]).length, 0)
    // A prepended page inserts before the rows on screen and touches none of them.
    compare(W.syncOps(["3"], ["1", "2", "3"]), [{ op: "insert", index: 0, key: "1" }, { op: "insert", index: 1, key: "2" }])
  }

  function test_index_for_ts_finds_the_message_or_the_next_one() {
    var k = ["100.0", "200.0", "300.0"]
    compare(W.indexForTS(k, "200.0"), 1)
    compare(W.indexForTS(k, "150.0"), 1)
    compare(W.indexForTS(k, "050.0"), 0)
    compare(W.indexForTS(k, "999.0"), 2)
    compare(W.indexForTS([], "1.0"), -1)
  }

  // ---- marking read -------------------------------------------------------------

  function test_mark_read_only_for_a_newer_message_not_yet_marked() {
    compare(W.readUpTo([{ ts: "1.0" }, { ts: "2.0" }], "1.0", ""), "2.0")
    compare(W.readUpTo([{ ts: "1.0" }, { ts: "2.0" }], "2.0", ""), "")
    compare(W.readUpTo([{ ts: "1.0" }, { ts: "2.0" }], "1.0", "2.0"), "")
    compare(W.readUpTo([], "", ""), "")
  }

  // ---- payload ------------------------------------------------------------------

  function test_payload_parses_or_gives_nothing() {
    compare(W.parsePayload("{\"team\":\"T1\",\"conv\":\"C1\",\"ts\":\"1.0\"}"), { team: "T1", conv: "C1", ts: "1.0" })
    compare(W.parsePayload("{\"team\":\"T1\",\"conv\":\"C1\"}"), { team: "T1", conv: "C1", ts: "" })
    compare(W.parsePayload("{\"team\":\"T1\"}"), null)
    compare(W.parsePayload("not json"), null)
    compare(W.parsePayload(""), null)
    compare(W.parsePayload(undefined), null)
  }

  // ---- keyboard focus -------------------------------------------------------------

  function test_panes_move_left_and_right_and_tab_wraps() {
    compare(W.movePane("sidebar", 1, false, false), "messages")
    compare(W.movePane("messages", 1, false, false), "messages")
    compare(W.movePane("messages", 1, true, false), "thread")
    compare(W.movePane("thread", 1, true, false), "thread")
    compare(W.movePane("sidebar", -1, true, false), "sidebar")
    compare(W.movePane("messages", 1, false, true), "sidebar")
    compare(W.movePane("thread", 1, true, true), "sidebar")
    compare(W.movePane("sidebar", -1, true, true), "thread")
    compare(W.movePane("thread", 0, false, false), "messages") // a closed thread hands back
  }

  function test_escape_steps_back_one_level_at_a_time() {
    var f = { pane: "thread", composing: true, threadOpen: true }
    f = W.escape(f)
    compare(f, { pane: "thread", composing: false, threadOpen: true, dismiss: false })
    f = W.escape(f)
    compare(f, { pane: "messages", composing: false, threadOpen: false, dismiss: false })
    f = W.escape(f)
    compare(f.dismiss, true)
    compare(W.escape({ pane: "sidebar", composing: false, threadOpen: true }),
            { pane: "sidebar", composing: false, threadOpen: false, dismiss: false })
  }
}
