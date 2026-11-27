import QtQuick
import QtTest
import "../lib/badges.js" as Badges

// lib/badges.js: what the bar icon shows (unread dot, state, tooltip, hidden)
// for a given Service state and widget settings.
TestCase {
  name: "Badges"

  function ws(team, name, state) { return { team: team, name: name, state: state || "connected" } }
  function counts(dms, mentions, channels, threads) {
    return { dms: dms, mentions: mentions, channels: channels, threads: threads }
  }

  // A connected daemon with one workspace and nothing unread, overridable
  // field by field.
  function input(over) {
    var base = {
      hasService: true,
      connectionState: "connected",
      connectionError: "",
      everConnected: true,
      workspaces: [ws("T1", "Acme")],
      summary: { workspaces: { T1: counts(0, 0, 0, 0) }, total: counts(0, 0, 0, 0) },
      dnd: {},
      nowSeconds: 1000,
      unreadScope: "dms-and-mentions",
      hideWhenEmpty: false
    }
    for (var k in over) base[k] = over[k]
    return base
  }

  function withCounts(c) {
    return { summary: { workspaces: { T1: c }, total: c } }
  }

  function view(over) { return Badges.view(input(over)) }

  // ---- counts -------------------------------------------------------------

  function test_scope_dms_and_mentions_counts_dms_and_mentions() {
    var v = view(withCounts(counts(2, 1, 5, 3)))
    compare(v.mainCount, 3)
    compare(v.channelCount, 8)
  }

  function test_scope_all_counts_everything() {
    var o = withCounts(counts(2, 1, 5, 3))
    o.unreadScope = "all"
    var v = view(o)
    compare(v.mainCount, 11)
    compare(v.channelCount, 0, "nothing is left out of the scope")
  }

  function test_scope_dms_and_mentions_leaves_channels_out_of_the_dot() {
    var v = view(withCounts(counts(0, 0, 5, 3)))
    verify(!v.dot, "channels lit the dot")
    compare(v.channelCount, 8, "still counted, for the tooltip and panel")
  }

  function test_scope_all_lights_the_dot_for_channels_and_threads() {
    var o = withCounts(counts(0, 0, 1, 0))
    o.unreadScope = "all"
    verify(view(o).dot, "a channel")
    o = withCounts(counts(0, 0, 0, 1))
    o.unreadScope = "all"
    verify(view(o).dot, "a thread")
  }

  function test_unknown_scope_reads_as_everything() {
    var o = withCounts(counts(0, 0, 1, 0))
    o.unreadScope = "bogus"
    verify(view(o).dot)
  }

  // ---- settings -------------------------------------------------------------

  function test_scope_setting_defaults_to_everything() {
    compare(Badges.DEFAULT_SCOPE, "all")
    compare(Badges.scopeSetting({}), "all")
    compare(Badges.scopeSetting(null), "all")
    compare(Badges.scopeSetting({ unreadScope: null }), "all")
  }

  function test_scope_setting_keeps_a_saved_scope() {
    compare(Badges.scopeSetting({ unreadScope: "dms-and-mentions" }), "dms-and-mentions")
    compare(Badges.scopeSetting({ unreadScope: "all" }), "all")
    compare(Badges.scopeSetting({ unreadScope: "bogus" }), "all")
  }

  function test_scope_setting_maps_a_retired_channel_badge_off() {
    compare(Badges.scopeSetting({ showChannelBadge: false }), "dms-and-mentions")
    compare(Badges.scopeSetting({ showChannelBadge: true }), "all")
    compare(Badges.scopeSetting({ showChannelBadge: false, unreadScope: "all" }), "all",
            "an explicit scope wins over the retired setting")
  }

  function test_malformed_summary_counts_as_nothing_unread() {
    var v = view({ summary: { total: { dms: "x", mentions: -2 } } })
    compare(v.mainCount, 0)
    compare(v.channelCount, 0)
    compare(view({ summary: null }).mainCount, 0)
  }

  function test_no_counts_without_the_daemon() {
    var o = withCounts(counts(2, 1, 5, 3))
    o.connectionState = "unavailable"
    var v = view(o)
    compare(v.mainCount, 0)
    compare(v.channelCount, 0)
  }

  // ---- dot ----------------------------------------------------------------

  function test_dot_off_with_nothing_unread() {
    verify(!view({}).dot)
  }

  function test_dot_on_for_a_dm_or_a_mention() {
    verify(view(withCounts(counts(1, 0, 0, 0))).dot, "a DM")
    verify(view(withCounts(counts(0, 1, 0, 0))).dot, "a mention")
  }

  function test_dot_shows_while_dnd() {
    var o = withCounts(counts(1, 0, 0, 0))
    o.dnd = { T1: { snoozed: true, until: 0 } }
    var v = view(o)
    compare(v.status, "dnd")
    verify(v.dot, "DND is not an alert")
  }

  function test_alert_glyph_hides_the_dot() {
    var base = withCounts(counts(2, 1, 5, 3))
    var cases = [
      { workspaces: [ws("T1", "Acme", "auth_expired")] },
      { workspaces: [ws("T1", "Acme", "connecting")] },
      { workspaces: [ws("T1", "Acme", "disconnected")] },
      { connectionState: "incompatible" },
      { connectionState: "unavailable", everConnected: true },
      { connectionState: "unavailable", everConnected: false },
      { connectionState: "connecting", everConnected: false },
      { hasService: false }
    ]
    for (var i = 0; i < cases.length; i++) {
      var o = { summary: base.summary, unreadScope: "all" }
      for (var k in cases[i]) o[k] = cases[i][k]
      var v = view(o)
      verify(!v.dot, "dot shows with status " + v.status)
    }
  }

  // ---- state --------------------------------------------------------------

  function test_connected() {
    var v = view({})
    compare(v.status, "connected")
    compare(v.glyph, Badges.GLYPH_SLACK)
    verify(!v.dimmed)
    verify(!v.alert)
  }

  function test_no_service_is_down() {
    var v = view({ hasService: false, connectionState: "unknown", everConnected: false })
    compare(v.status, "down")
    verify(v.dimmed)
  }

  function test_daemon_never_reached_is_down_and_says_how_to_start_it() {
    var v = view({ connectionState: "unavailable", everConnected: false, connectionError: "connection refused" })
    compare(v.status, "down")
    verify(v.dimmed)
    verify(v.tooltip.indexOf("systemctl --user start omaslack") >= 0, v.tooltip)
    verify(v.tooltip.indexOf("omaslack daemon") >= 0, v.tooltip)
    verify(v.tooltip.indexOf("connection refused") >= 0, v.tooltip)
  }

  function test_first_attempt_is_starting() {
    compare(view({ connectionState: "unknown", everConnected: false }).status, "starting")
    var v = view({ connectionState: "connecting", everConnected: false })
    compare(v.status, "starting")
    verify(v.dimmed)
  }

  function test_losing_the_daemon_is_reconnecting_dimmed() {
    var states = ["connecting", "unavailable"]
    for (var i = 0; i < states.length; i++) {
      var v = view({ connectionState: states[i], everConnected: true })
      compare(v.status, "reconnecting", states[i])
      compare(v.glyph, Badges.GLYPH_RECONNECTING)
      verify(v.dimmed, states[i])
      verify(v.tooltip.indexOf("systemctl --user start omaslack") >= 0, v.tooltip)
    }
  }

  function test_incompatible_is_an_error_that_says_to_update() {
    var v = view({ connectionState: "incompatible", connectionError: "protocol 2 not supported" })
    compare(v.status, "error")
    compare(v.glyph, Badges.GLYPH_ERROR)
    verify(v.alert)
    verify(v.tooltip.indexOf("Update omaslack") >= 0, v.tooltip)
    verify(v.tooltip.indexOf("protocol 2 not supported") >= 0, v.tooltip)
  }

  function test_workspace_reconnecting_keeps_the_counts() {
    var states = ["connecting", "disconnected"]
    for (var i = 0; i < states.length; i++) {
      var o = withCounts(counts(1, 0, 0, 0))
      o.workspaces = [ws("T1", "Acme", states[i])]
      var v = view(o)
      compare(v.status, "reconnecting", states[i])
      verify(!v.dimmed, "the daemon still answers")
      compare(v.mainCount, 1)
    }
  }

  function test_auth_expired_wins_over_reconnecting() {
    var v = view({ workspaces: [ws("T1", "Acme", "disconnected"), ws("T2", "Beta", "auth_expired")] })
    compare(v.status, "auth_expired")
    compare(v.glyph, Badges.GLYPH_AUTH_EXPIRED)
    verify(v.alert)
    verify(v.tooltip.indexOf("omaslack auth add") >= 0, v.tooltip)
  }

  function test_dnd_when_every_workspace_is_snoozed() {
    var v = view({ dnd: { T1: { snoozed: true, until: 2500 } } })
    compare(v.status, "dnd")
    compare(v.glyph, Badges.GLYPH_DND)
    verify(v.tooltip.indexOf("Acme: nothing unread (do not disturb, 25 min left)") >= 0, v.tooltip)
  }

  function test_dnd_on_some_workspaces_only_shows_in_the_tooltip() {
    var v = view({
      workspaces: [ws("T1", "Acme"), ws("T2", "Beta")],
      dnd: { T2: { snoozed: true, until: 0 } }
    })
    compare(v.status, "connected")
    verify(v.tooltip.indexOf("Beta: nothing unread (do not disturb)") >= 0, v.tooltip)
  }

  function test_expired_dnd_is_ignored() {
    compare(view({ dnd: { T1: { snoozed: true, until: 900 } } }).status, "connected")
    compare(view({ dnd: { T1: { snoozed: false, until: 2500 } } }).status, "connected")
  }

  function test_reconnecting_wins_over_dnd() {
    var v = view({ workspaces: [ws("T1", "Acme", "connecting")], dnd: { T1: { snoozed: true, until: 0 } } })
    compare(v.status, "reconnecting")
  }

  // ---- tooltip ------------------------------------------------------------

  function test_tooltip_summarizes_each_workspace() {
    var v = view({
      workspaces: [ws("T1", "Acme"), ws("T2", "", "auth_expired")],
      summary: {
        workspaces: { T1: counts(2, 1, 4, 1), T2: counts(0, 0, 0, 0) },
        total: counts(2, 1, 4, 1)
      }
    })
    var lines = v.tooltip.split("\n")
    verify(lines.indexOf("Acme: 2 DMs, 1 mention, 4 channels, 1 thread") >= 0, v.tooltip)
    verify(lines.indexOf("T2: nothing unread (sign-in expired)") >= 0, v.tooltip)
  }

  function test_tooltip_names_the_workspace_state() {
    var v = view({ workspaces: [ws("T1", "Acme", "connecting")] })
    verify(v.tooltip.indexOf("Acme: nothing unread (reconnecting)") >= 0, v.tooltip)
  }

  function test_tooltip_without_workspaces_says_how_to_add_one() {
    var v = view({ workspaces: [], summary: { workspaces: {}, total: counts(0, 0, 0, 0) } })
    compare(v.status, "connected")
    verify(v.tooltip.indexOf("omaslack auth add") >= 0, v.tooltip)
  }

  function test_tooltip_has_no_workspaces_while_the_daemon_is_away() {
    var v = view({ connectionState: "unavailable", everConnected: false })
    verify(v.tooltip.indexOf("Acme") < 0, v.tooltip)
  }

  // ---- hideWhenEmpty ------------------------------------------------------

  function test_hidden_only_when_on_and_nothing_in_scope() {
    verify(!view({}).hidden, "off by default")
    verify(view({ hideWhenEmpty: true }).hidden)
    var o = withCounts(counts(0, 0, 1, 0))
    o.hideWhenEmpty = true
    verify(view(o).hidden, "a channel is outside the scope DMs and mentions")
    o.unreadScope = "all"
    verify(!view(o).hidden, "with the scope Everything a channel is something to show")
  }

  function test_never_hidden_on_errors() {
    var cases = [
      { connectionState: "unavailable", everConnected: false },
      { connectionState: "unavailable", everConnected: true },
      { connectionState: "incompatible" },
      { workspaces: [ws("T1", "Acme", "auth_expired")] },
      { hasService: false }
    ]
    for (var i = 0; i < cases.length; i++) {
      cases[i].hideWhenEmpty = true
      var v = view(cases[i])
      verify(!v.hidden, v.status)
    }
  }

  function test_quiet_states_hide() {
    var cases = [
      { connectionState: "connecting", everConnected: false },
      { workspaces: [ws("T1", "Acme", "connecting")] }
    ]
    for (var i = 0; i < cases.length; i++) {
      cases[i].hideWhenEmpty = true
      var v = view(cases[i])
      verify(v.hidden, v.status)
    }
  }

  function test_dnd_keeps_the_icon_with_hide_when_empty() {
    var v = view({ hideWhenEmpty: true, dnd: { T1: { snoozed: true, until: 0 } } })
    compare(v.status, "dnd")
    verify(!v.hidden, "DND would vanish from the bar")
  }

  // ---- layout ---------------------------------------------------------------

  // A horizontal bar slot as the shell sizes it at the default scale: a
  // 21 px slot, 26 px tall, a 12 px glyph in its middle, and an 8 px dot
  // (6 px plus its 1 px ring).
  function place(over) {
    var input = { slotWidth: 21, height: 26, iconSize: 12, dotSize: 8 }
    for (var k in over) input[k] = over[k]
    return Badges.layout(input)
  }

  function test_layout_puts_the_dot_on_the_glyphs_top_right_corner() {
    var l = place({})
    // The glyph spans 4.5..16.5 across and 7..19 down; the dot overlaps
    // it by 55 % across and 35 % down, on whole pixels:
    // x = round(16.5 - 4.4) = 12, y = round(7 - 2.8) = 4.
    compare(l.x, 12)
    compare(l.y, 4)
  }

  function test_layout_never_grows_the_slot() {
    compare(place({}).width, 21)
    compare(place({ slotWidth: 40 }).width, 40)
  }

  function test_layout_vertical_stays_inside_the_slot() {
    // Vertical bars at 1.0, 1.2 and 2.0, and a slot barely wider than the
    // glyph: the dot never sticks out.
    var slots = [
      { slotWidth: 26, height: 21, iconSize: 12, dotSize: 8 },
      { slotWidth: 31, height: 25, iconSize: 14, dotSize: 9 },
      { slotWidth: 52, height: 42, iconSize: 24, dotSize: 14 },
      { slotWidth: 14, height: 12, iconSize: 12, dotSize: 8 }
    ]
    for (var i = 0; i < slots.length; i++) {
      var l = Badges.layout(slots[i])
      var tag = JSON.stringify(slots[i]) + " -> " + JSON.stringify(l)
      verify(l.x >= 0 && l.x + slots[i].dotSize <= slots[i].slotWidth, tag)
      verify(l.y >= 0 && l.y + slots[i].dotSize <= slots[i].height, tag)
      compare(l.width, slots[i].slotWidth, tag)
    }
  }
}
