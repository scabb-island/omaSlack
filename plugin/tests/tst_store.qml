import QtQuick
import QtTest
import "../lib/store.js" as Store

// lib/store.js: the plugin's copy of the daemon state, the QML twin of the
// TUI's app.State. Every function is pure: the input state never changes.
TestCase {
  name: "Store"

  function msg(ts, extra) {
    var m = { ts: ts, author: "U1", text: "m" + ts }
    for (var k in extra) m[k] = extra[k]
    return m
  }
  function conv(id, kind, extra) {
    var c = { id: id, kind: kind, mentions: 0, unread: false }
    for (var k in extra) c[k] = extra[k]
    return c
  }
  function tss(list) { return list.map(function(m) { return m.ts }) }

  // A state with one workspace, two conversations and C1's history loaded.
  function loaded() {
    var s = Store.empty()
    s = Store.applyWorkspaces(s, [{ team: "T1", name: "One", provider: "internal-app", capabilities: [], state: "connected" }])
    s = Store.applyConversations(s, "T1", [conv("C1", "channel", { name: "general", latest: "3.000000" }), conv("D1", "im", { user: "U2" })])
    return Store.applyHistory(s, "T1", "C1", "", [msg("3.000000"), msg("2.000000"), msg("1.000000")], false, true)
  }

  // ---- results of calls ----------------------------------------------------

  function test_empty_state() {
    var s = Store.empty()
    compare(s.workspaces.length, 0)
    compare(s.summary.total.dms, 0)
    compare(Store.messages(s, "T1", "C1", ""), [])
    compare(Store.hasMore(s, "T1", "C1"), false)
    compare(Store.unread(s), [])
  }

  function test_results_replace_their_part_without_mutating_the_input() {
    var s0 = Store.empty()
    var s1 = Store.applyWorkspaces(s0, [{ team: "T1" }])
    var s2 = Store.applySummary(s1, { workspaces: { T1: { dms: 1, mentions: 2, channels: 3, threads: 0 } }, total: { dms: 1, mentions: 2, channels: 3, threads: 0 } })
    var s3 = Store.applyConversations(s2, "T1", [conv("C1", "channel")])
    var s4 = Store.applyDND(s3, "T1", true, 1700000000)
    compare(s0.workspaces.length, 0)
    compare(s1.summary.total.dms, 0)
    compare(s2.conversations.T1, undefined)
    compare(s3.dnd.T1, undefined)
    compare(s4.workspaces[0].team, "T1")
    compare(s4.summary.total.mentions, 2)
    compare(s4.conversations.T1.length, 1)
    compare(s4.dnd.T1.snoozed, true)
    compare(s4.dnd.T1.until, 1700000000)
  }

  // ---- history -------------------------------------------------------------

  function test_history_is_kept_oldest_first_whatever_the_page_order() {
    var s = loaded()
    compare(tss(Store.messages(s, "T1", "C1", "")), ["1.000000", "2.000000", "3.000000"])
    compare(Store.hasMore(s, "T1", "C1"), true)
  }

  function test_older_page_merges_and_deduplicates_by_ts() {
    var s = Store.applyHistory(loaded(), "T1", "C1", "", [msg("1.000000"), msg("0.500000")], true, false)
    compare(tss(Store.messages(s, "T1", "C1", "")), ["0.500000", "1.000000", "2.000000", "3.000000"])
    compare(Store.hasMore(s, "T1", "C1"), false)
  }

  function test_newest_page_overlapping_the_cache_keeps_older_history() {
    var s = Store.applyHistory(loaded(), "T1", "C1", "", [msg("1.000000"), msg("0.500000")], true, false)
    s = Store.applyHistory(s, "T1", "C1", "", [msg("4.000000"), msg("3.000000")], false, true)
    compare(tss(Store.messages(s, "T1", "C1", "")), ["0.500000", "1.000000", "2.000000", "3.000000", "4.000000"])
    compare(Store.hasMore(s, "T1", "C1"), false)
  }

  function test_newest_page_after_a_gap_replaces_the_cache() {
    var s = Store.applyHistory(loaded(), "T1", "C1", "", [msg("9.000000"), msg("8.000000")], false, true)
    compare(tss(Store.messages(s, "T1", "C1", "")), ["8.000000", "9.000000"])
    compare(Store.hasMore(s, "T1", "C1"), true)
  }

  function test_history_does_not_mutate_the_input() {
    var s0 = loaded()
    Store.applyHistory(s0, "T1", "C1", "", [msg("0.100000")], true, false)
    compare(Store.messages(s0, "T1", "C1", "").length, 3)
  }

  function test_thread_replies_are_cached_apart() {
    var s = Store.applyHistory(loaded(), "T1", "C1", "2.000000",
      [msg("2.000000", { threadTS: "2.000000" }), msg("2.500000", { threadTS: "2.000000" })], false, false)
    compare(tss(Store.messages(s, "T1", "C1", "2.000000")), ["2.000000", "2.500000"])
    compare(Store.messages(s, "T1", "C1", "").length, 3)
  }

  function test_conversation_list_hides_replies() {
    var s = Store.applyHistory(Store.empty(), "T1", "C1", "",
      [msg("2.000000", { threadTS: "2.000000" }), msg("2.500000", { threadTS: "2.000000" }), msg("1.000000")], false, false)
    compare(tss(Store.messages(s, "T1", "C1", "")), ["1.000000", "2.000000"])
  }

  // ---- events --------------------------------------------------------------

  function test_message_created_appends_to_a_cached_conversation() {
    var r = Store.applyEvent(loaded(), "message.created", { team: "T1", conv: "C1", message: msg("4.000000") })
    compare(r.changed, ["T1/C1"])
    compare(tss(Store.messages(r.state, "T1", "C1", "")), ["1.000000", "2.000000", "3.000000", "4.000000"])
  }

  function test_message_created_twice_is_kept_once() {
    var r = Store.applyEvent(loaded(), "message.created", { team: "T1", conv: "C1", message: msg("3.000000") })
    compare(Store.messages(r.state, "T1", "C1", "").length, 3)
  }

  function test_message_in_an_uncached_conversation_starts_no_list() {
    var r = Store.applyEvent(loaded(), "message.created", { team: "T1", conv: "C9", message: msg("4.000000") })
    compare(r.changed, [])
    compare(Store.messages(r.state, "T1", "C9", ""), [])
  }

  function test_reply_goes_to_its_cached_thread_only() {
    var s = Store.applyHistory(loaded(), "T1", "C1", "2.000000", [msg("2.000000", { threadTS: "2.000000" })], false, false)
    var r = Store.applyEvent(s, "message.created", { team: "T1", conv: "C1", message: msg("2.700000", { threadTS: "2.000000" }) })
    compare(r.changed, ["T1/C1/2.000000"])
    compare(tss(Store.messages(r.state, "T1", "C1", "2.000000")), ["2.000000", "2.700000"])
    compare(Store.messages(r.state, "T1", "C1", "").length, 3)
  }

  function test_updated_thread_root_changes_both_lists() {
    var s = Store.applyHistory(loaded(), "T1", "C1", "2.000000", [msg("2.000000", { threadTS: "2.000000" })], false, false)
    var r = Store.applyEvent(s, "message.updated", { team: "T1", conv: "C1", message: msg("2.000000", { threadTS: "2.000000", replyCount: 1 }) })
    compare(r.changed.sort(), ["T1/C1", "T1/C1/2.000000"])
    compare(Store.messages(r.state, "T1", "C1", "")[1].replyCount, 1)
  }

  function test_message_updated_keeps_known_reactions() {
    var s = Store.applyHistory(Store.empty(), "T1", "C1", "",
      [msg("1.000000", { reactions: [{ emoji: "wave", users: ["U2"] }] })], false, false)
    var r = Store.applyEvent(s, "message.updated", { team: "T1", conv: "C1", message: msg("1.000000", { text: "edited", edited: "1.500000" }) })
    var m = Store.messages(r.state, "T1", "C1", "")[0]
    compare(m.text, "edited")
    compare(m.reactions.length, 1)
  }

  function test_message_deleted_leaves_every_list_of_the_conversation() {
    var s = Store.applyHistory(loaded(), "T1", "C1", "2.000000",
      [msg("2.000000", { threadTS: "2.000000" }), msg("2.500000", { threadTS: "2.000000" })], false, false)
    var r = Store.applyEvent(s, "message.deleted", { team: "T1", conv: "C1", ts: "2.500000" })
    compare(r.changed, ["T1/C1/2.000000"])
    compare(tss(Store.messages(r.state, "T1", "C1", "2.000000")), ["2.000000"])
    r = Store.applyEvent(r.state, "message.deleted", { team: "T1", conv: "C1", ts: "1.000000" })
    compare(r.changed, ["T1/C1"])
    compare(tss(Store.messages(r.state, "T1", "C1", "")), ["2.000000", "3.000000"])
  }

  function test_reactions_are_added_and_removed() {
    var s = loaded()
    var r = Store.applyEvent(s, "reaction.changed", { team: "T1", conv: "C1", ts: "1.000000", user: "U2", emoji: "wave", added: true })
    compare(r.changed, ["T1/C1"])
    r = Store.applyEvent(r.state, "reaction.changed", { team: "T1", conv: "C1", ts: "1.000000", user: "U3", emoji: "wave", added: true })
    compare(Store.messages(r.state, "T1", "C1", "")[0].reactions, [{ emoji: "wave", users: ["U2", "U3"] }])
    r = Store.applyEvent(r.state, "reaction.changed", { team: "T1", conv: "C1", ts: "1.000000", user: "U2", emoji: "wave", added: false })
    r = Store.applyEvent(r.state, "reaction.changed", { team: "T1", conv: "C1", ts: "1.000000", user: "U3", emoji: "wave", added: false })
    compare(Store.messages(r.state, "T1", "C1", "")[0].reactions, [])
    compare(Store.messages(s, "T1", "C1", "")[0].reactions, undefined)
  }

  function test_reaction_on_an_unknown_message_changes_nothing() {
    var r = Store.applyEvent(loaded(), "reaction.changed", { team: "T1", conv: "C1", ts: "7.000000", user: "U2", emoji: "wave", added: true })
    compare(r.changed, [])
  }

  function test_read_changed_updates_the_unread_flag() {
    var s = Store.applyConversations(Store.empty(), "T1", [conv("C1", "channel", { latest: "3.000000", lastRead: "1.000000", unread: true })])
    var r = Store.applyEvent(s, "read.changed", { team: "T1", conv: "C1", lastRead: "3.000000" })
    compare(r.state.conversations.T1[0].unread, false)
    compare(r.state.conversations.T1[0].lastRead, "3.000000")
    compare(s.conversations.T1[0].unread, true)
  }

  function test_conversation_updated_replaces_or_adds() {
    var s = loaded()
    var r = Store.applyEvent(s, "conversation.updated", { team: "T1", conversation: conv("C1", "channel", { name: "general", unread: true, latest: "5.000000" }) })
    compare(r.state.conversations.T1.length, 2)
    compare(r.state.conversations.T1[0].unread, true)
    r = Store.applyEvent(r.state, "conversation.updated", { team: "T1", conversation: conv("C2", "channel", { name: "new" }) })
    compare(r.state.conversations.T1.length, 3)
  }

  function test_connection_state_updates_the_workspace() {
    var r = Store.applyEvent(loaded(), "connection.state", { team: "T1", state: "auth_expired" })
    compare(r.state.workspaces[0].state, "auth_expired")
  }

  function test_unread_summary_event_replaces_the_summary() {
    var sum = { workspaces: { T1: { dms: 1, mentions: 0, channels: 0, threads: 0 } }, total: { dms: 1, mentions: 0, channels: 0, threads: 0 } }
    var r = Store.applyEvent(loaded(), "unread.summary", sum)
    compare(r.state.summary.total.dms, 1)
  }

  function test_events_without_state_change_nothing() {
    var s = loaded()
    var topics = ["typing", "draft.changed", "ui.focus", "something.new"]
    for (var i = 0; i < topics.length; i++) {
      var r = Store.applyEvent(s, topics[i], { team: "T1", conv: "C1" })
      compare(r.changed, [], topics[i])
      compare(r.state, s, topics[i])
    }
  }

  // ---- own state and presence (task 5.3) -------------------------------------

  function test_dnd_changed_replaces_the_workspaces_snooze() {
    var s = loaded()
    var r = Store.applyEvent(s, "dnd.changed", { team: "T1", snoozed: true, until: 1700003600 })
    compare(r.changed, [])
    compare(r.state.dnd.T1.snoozed, true)
    compare(r.state.dnd.T1.until, 1700003600)
    compare(s.dnd.T1, undefined)
    r = Store.applyEvent(r.state, "dnd.changed", { team: "T1", snoozed: false, until: 0 })
    compare(r.state.dnd.T1.snoozed, false)
    compare(r.state.dnd.T1.until, 0)
  }

  function test_presence_changed_records_each_users_presence() {
    var s = loaded()
    var r = Store.applyEvent(s, "presence.changed", { team: "T1", user: "U2", presence: "away" })
    compare(r.changed, [])
    compare(Store.presence(r.state, "T1", "U2"), "away")
    compare(Store.presence(r.state, "T1", "U3"), "")
    compare(Store.presence(s, "T1", "U2"), "")
    r = Store.applyEvent(r.state, "presence.changed", { team: "T1", user: "U2", presence: "active" })
    compare(Store.presence(r.state, "T1", "U2"), "active")
    compare(r.state.presence.T1.U2, "active")
  }

  function test_apply_presence_is_what_presence_get_answers_for_the_user() {
    var s = Store.applyPresence(loaded(), "T1", "U0", "away")
    compare(Store.presence(s, "T1", "U0"), "away")
    compare(Store.presence(s, "T2", "U0"), "")
  }

  // ---- queries -------------------------------------------------------------

  function test_unread_puts_dms_and_mentions_first_then_latest() {
    var s = Store.applyWorkspaces(Store.empty(), [{ team: "T1" }, { team: "T2" }])
    s = Store.applyConversations(s, "T1", [
      conv("C1", "channel", { unread: true, latest: "9.000000" }),
      conv("C2", "channel", { unread: true, latest: "2.000000", mentions: 1 }),
      conv("C3", "channel", { latest: "10.000000" })
    ])
    s = Store.applyConversations(s, "T2", [
      conv("D1", "im", { unread: true, latest: "3.000000" }),
      conv("C4", "channel", { unread: true, latest: "8.000000" }),
      conv("G1", "mpim", { unread: true, latest: "1.000000" })
    ])
    var got = Store.unread(s).map(function(u) { return u.team + "/" + u.conversation.id })
    compare(got, ["T2/D1", "T1/C2", "T2/G1", "T1/C1", "T2/C4"])
  }

  function test_user_names_come_from_users_info() {
    var s = Store.applyUsers(Store.empty(), "T1", [
      { id: "U1", name: "ana", realName: "Ana Perez", displayName: "anita" },
      { id: "U2", name: "bo", realName: "Bo Li" },
      { id: "U3", name: "cy" }
    ])
    compare(Store.userName(s, "T1", "U1"), "anita")
    compare(Store.userName(s, "T1", "U2"), "Bo Li")
    compare(Store.userName(s, "T1", "U3"), "cy")
    compare(Store.userName(s, "T1", "U4"), "U4")
    compare(Store.userName(s, "T2", "U1"), "U1")
  }

  // users.info names people (U…, W…) only: the daemon refuses a whole
  // batch with any other ID in it, so nothing else may be asked.
  function test_only_user_ids_are_asked_for_a_name() {
    verify(Store.isUserID("U0CM6B9CKJ7"))
    verify(Store.isUserID("W0123ABCD"))
    verify(!Store.isUserID("B0ALMQAKL6Q"))
    verify(!Store.isUserID(""))
    verify(!Store.isUserID(undefined))
  }

  // A bot author (B…) has no users.info name: a neutral label, never its ID.
  function test_bot_authors_read_as_bot() {
    compare(Store.userName(Store.empty(), "T1", "B0ALMQAKL6Q"), "Bot")
  }

  // A message names its author with what Slack put on it (a bot's
  // authorName) before asking userName for the author's ID.
  function test_author_name_prefers_the_name_slack_sent() {
    var names = function(u) { return { U1: "Ana", B1: "Bot" }[u] || u }
    compare(Store.authorName({ author: "B1", authorName: "deploybot" }, names), "deploybot")
    compare(Store.authorName({ author: "U9", authorName: "GitHub" }, names), "GitHub")
    compare(Store.authorName({ author: "U1" }, names), "Ana")
    compare(Store.authorName({ author: "B1", authorName: "" }, names), "Bot")
    compare(Store.authorName({ author: "" }, names), "")
    compare(Store.authorName(null, names), "")
  }

  function test_conversation_names() {
    var s = Store.applyConversations(Store.empty(), "T1", [
      conv("C1", "channel", { name: "general" }),
      conv("C2", "private", { name: "secret" }),
      conv("D1", "im", { user: "U2" }),
      conv("D2", "im", {}),
      conv("G1", "mpim", { name: "mpdm-ana--bo--cy-1" })
    ])
    compare(Store.conversationName(s, "T1", "C1"), "#general")
    compare(Store.conversationName(s, "T1", "C2"), "#secret")
    compare(Store.conversationName(s, "T1", "D1"), "U2")
    s = Store.applyUsers(s, "T1", [{ id: "U2", name: "bo", realName: "Bo Li" }])
    compare(Store.conversationName(s, "T1", "D1"), "Bo Li")
    compare(Store.conversationName(s, "T1", "D2"), "D2")
    compare(Store.conversationName(s, "T1", "G1"), "ana, bo, cy")
    compare(Store.conversationName(s, "T1", "X9"), "X9")
  }
}
