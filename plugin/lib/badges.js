.pragma library

// What the bar icon shows (spec gui-client, "Bar icon with badges"): the
// unread dot, the state glyph, the tooltip and whether the icon takes space.
// Pure: BarIcon.qml hands in the Service state and the widget settings and
// draws what comes back, so every decision here is tested with
// qmltestrunner (tests/tst_badges.qml).

// Material Design glyphs from the Nerd Font the bar already uses.
var GLYPH_SLACK = String.fromCodePoint(0xF04B1)        // md-slack
var GLYPH_RECONNECTING = String.fromCodePoint(0xF04E6) // md-sync
var GLYPH_ERROR = String.fromCodePoint(0xF0028)        // md-alert_circle
var GLYPH_AUTH_EXPIRED = String.fromCodePoint(0xF08EE) // md-lock_alert
var GLYPH_DND = String.fromCodePoint(0xF04B2)          // md-sleep

var START_HINT = "Start it with: systemctl --user start omaslack\n"
  + "or run omaslack daemon in a terminal."
var SIGN_IN_HINT = "Renew it from the window, or sign in again with: omaslack auth add"

// The state glyph and tooltip header of each status. dimmed: the daemon
// does not answer; alert: the user has to act; hideable: hideWhenEmpty may
// remove the icon (never while something is wrong, nor during DND, which
// the user should still see); dot: the unread dot may show (only where no
// connection glyph does, so one signal shows at a time).
var STATUSES = {
  starting: { glyph: GLYPH_SLACK, dimmed: true, alert: false, hideable: true, dot: false },
  down: { glyph: GLYPH_SLACK, dimmed: true, alert: false, hideable: false, dot: false },
  error: { glyph: GLYPH_ERROR, dimmed: false, alert: true, hideable: false, dot: false },
  auth_expired: { glyph: GLYPH_AUTH_EXPIRED, dimmed: false, alert: true, hideable: false, dot: false },
  reconnecting: { glyph: GLYPH_RECONNECTING, dimmed: false, alert: false, hideable: true, dot: false },
  dnd: { glyph: GLYPH_DND, dimmed: false, alert: false, hideable: false, dot: true },
  connected: { glyph: GLYPH_SLACK, dimmed: false, alert: false, hideable: true, dot: true }
}

// view turns the icon's inputs into what it draws.
//
// input: { hasService, connectionState, connectionError, everConnected,
//          workspaces, summary, dnd, nowSeconds,
//          unreadScope, hideWhenEmpty, setup }
// (everConnected: the Service reached "connected" at least once, so a lost
// connection reads as reconnecting rather than as a daemon never started;
// setup: lib/install.js action() for the Service's setupNeed, or null.)
//
// returns: { status, glyph, dimmed, alert, dot, mainCount, channelCount,
//            hidden, tooltip }
function view(input) {
  var daemonState = daemonStatus(input)
  var status = daemonState || workspaceStatus(input)
  var look = STATUSES[status]
  var lost = status === "reconnecting" && daemonState === "reconnecting"

  var counts = daemonState ? { main: 0, channel: 0 } : unreadCounts(input)
  return {
    status: status,
    glyph: look.glyph,
    dimmed: look.dimmed || lost,
    alert: look.alert,
    dot: look.dot && counts.main > 0,
    mainCount: counts.main,
    channelCount: counts.channel,
    hidden: !!input.hideWhenEmpty && look.hideable && !lost && counts.main === 0,
    tooltip: daemonState ? daemonTooltip(daemonState, input) : workspacesTooltip(status, input)
  }
}

// ---- state ----------------------------------------------------------------

// The status when the daemon itself is not usable, or "" when it is.
function daemonStatus(input) {
  if (!input.hasService) return "down"
  var state = input.connectionState
  if (state === "connected") return ""
  if (state === "incompatible") return "error"
  if (input.everConnected && (state === "connecting" || state === "unavailable")) return "reconnecting"
  if (state === "unavailable") return "down"
  return "starting"
}

// The status from the workspaces, for a connected daemon: the one that asks
// most of the user wins.
function workspaceStatus(input) {
  var list = workspaceList(input)
  var states = list.map(function(w) { return w.state })
  if (states.indexOf("auth_expired") >= 0) return "auth_expired"
  if (states.indexOf("connecting") >= 0 || states.indexOf("disconnected") >= 0) return "reconnecting"
  if (list.length > 0 && list.every(function(w) { return dndActive(input, w.team) })) return "dnd"
  return "connected"
}

function workspaceList(input) {
  return Array.isArray(input.workspaces) ? input.workspaces.filter(function(w) { return !!w }) : []
}

// A workspace's DND entry while it is still running, or null.
function dndEntry(input, team) {
  var entry = input.dnd ? input.dnd[team] : null
  if (!entry || !entry.snoozed) return null
  if (entry.until > 0 && entry.until <= input.nowSeconds) return null
  return entry
}

function dndActive(input, team) { return dndEntry(input, team) !== null }

// ---- counts ---------------------------------------------------------------

// Counts from the wire, with anything that is not a non-negative number read
// as zero.
function readCounts(raw) {
  function n(v) { return typeof v === "number" && v > 0 ? Math.floor(v) : 0 }
  raw = raw || {}
  return { dms: n(raw.dms), mentions: n(raw.mentions), channels: n(raw.channels), threads: n(raw.threads) }
}

// main: the unread conversations in scope (what lights the dot); channel:
// the channels and threads left out of it. Any scope but "dms-and-mentions"
// reads as "all", the default.
function unreadCounts(input) {
  var c = readCounts(input.summary ? input.summary.total : null)
  var direct = c.dms + c.mentions
  var rest = c.channels + c.threads
  if (input.unreadScope === "dms-and-mentions") return { main: direct, channel: rest }
  return { main: direct + rest, channel: 0 }
}

// ---- settings -------------------------------------------------------------

// The manifest's barWidget.defaults.unreadScope: the shell hands the widget
// only what shell.json sets, so this must stay equal to it.
var DEFAULT_SCOPE = "all"
var SCOPES = ["all", "dms-and-mentions"]

// scopeSetting reads the unread scope from the widget's shell.json entry.
// A saved scope wins; otherwise a showChannelBadge: false saved by an
// earlier version (the channel pill turned off) maps to "dms-and-mentions",
// which is what that user saw lit.
function scopeSetting(settings) {
  settings = settings || {}
  if (SCOPES.indexOf(settings.unreadScope) >= 0) return settings.unreadScope
  if (settings.showChannelBadge === false) return "dms-and-mentions"
  return DEFAULT_SCOPE
}

// ---- layout ---------------------------------------------------------------

// layout places the unread dot on the glyph's top-right corner, overlapping
// the glyph's box by 55 % of the dot across and 35 % down (the corner
// badge idiom of first-party widgets such as Tailscale), on whole pixels.
// The same on horizontal and vertical bars: the dot stays inside the slot,
// which never grows.
//
// input: { slotWidth, height, iconSize, dotSize }
// (the button's width and height, the glyph's size, and the dot's outer
// diameter, ring included)
// returns: { width, x, y } (the icon's width and the dot's top-left)
function layout(input) {
  var w = input.dotSize
  var glyphRight = (input.slotWidth + input.iconSize) / 2
  var glyphTop = (input.height - input.iconSize) / 2
  var x = Math.round(glyphRight - 0.55 * w)
  var y = Math.round(glyphTop - 0.35 * w)
  return {
    width: input.slotWidth,
    x: Math.max(0, Math.min(x, input.slotWidth - w)),
    y: Math.max(0, Math.min(y, input.height - w))
  }
}

// ---- tooltip --------------------------------------------------------------

// daemonTooltip: with input.setup (lib/install.js action(), from the
// Service's probe), what is wrong and what to do; without it, the generic
// hints. The Service keeps its setupNeed through the "connecting" of each
// retry, so the tooltip does not flicker back to "connecting".
function daemonTooltip(status, input) {
  var lines
  var setup = input.setup
  if (setup && status === "reconnecting")
    lines = ["omaslack: lost the daemon, retrying.", setup.hint, "Middle-click to retry now."]
  else if (setup) lines = ["omaslack: " + setup.title + ".", setup.hint]
  else if (status === "starting") lines = ["omaslack: connecting to the daemon…"]
  else if (status === "down") lines = ["omaslack: the daemon is not running.", START_HINT]
  else if (status === "reconnecting")
    lines = ["omaslack: lost the daemon, retrying.", "If it stopped: " + START_HINT, "Middle-click to retry now."]
  else lines = ["omaslack: the plugin and the daemon speak different protocol versions.",
                "Update omaslack so both match."]
  if (setup && setup.label) lines.push("Open the panel to " + setup.label.charAt(0).toLowerCase() + setup.label.slice(1) + ".")
  if (input.connectionError) lines.push("(" + input.connectionError + ")")
  return lines.join("\n")
}

var HEADERS = {
  connected: "omaslack",
  dnd: "omaslack: do not disturb",
  reconnecting: "omaslack: reconnecting to Slack",
  auth_expired: "omaslack: a sign-in expired. " + SIGN_IN_HINT
}

function workspacesTooltip(status, input) {
  var list = workspaceList(input)
  if (list.length === 0) return "omaslack: no workspace yet. Add one with: omaslack auth add"
  var lines = [HEADERS[status]]
  for (var i = 0; i < list.length; i++) lines.push(workspaceLine(input, list[i]))
  return lines.join("\n")
}

function plural(n, word) { return n + " " + word + (n === 1 ? "" : "s") }

var STATE_NOTES = {
  connecting: "reconnecting",
  disconnected: "disconnected",
  auth_expired: "sign-in expired"
}

function workspaceLine(input, w) {
  var byTeam = input.summary && input.summary.workspaces ? input.summary.workspaces : {}
  var c = readCounts(byTeam[w.team])
  var parts = []
  if (c.dms) parts.push(plural(c.dms, "DM"))
  if (c.mentions) parts.push(plural(c.mentions, "mention"))
  if (c.channels) parts.push(plural(c.channels, "channel"))
  if (c.threads) parts.push(plural(c.threads, "thread"))

  var line = (w.name || w.team) + ": " + (parts.length ? parts.join(", ") : "nothing unread")
  var note = STATE_NOTES[w.state] || dndNote(input, w.team)
  return note ? line + " (" + note + ")" : line
}

function dndNote(input, team) {
  var entry = dndEntry(input, team)
  if (!entry) return ""
  if (!(entry.until > 0)) return "do not disturb"
  return "do not disturb, " + Math.ceil((entry.until - input.nowSeconds) / 60) + " min left"
}
