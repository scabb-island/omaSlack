.pragma library

// What the bar icon shows (spec gui-client, "Bar icon with badges"): the
// badges, the state glyph, the tooltip and whether the icon takes space.
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
var SIGN_IN_HINT = "Sign in again with: omaslack auth add"

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

// A badge's label: empty for zero, capped at "99+" so it stays narrow.
function badgeText(n) {
  if (!(n > 0)) return ""
  return n > 99 ? "99+" : String(n)
}

// view turns the icon's inputs into what it draws.
//
// input: { hasService, connectionState, connectionError, everConnected,
//          workspaces, summary, dnd, nowSeconds,
//          showChannelBadge, unreadScope, hideWhenEmpty }
// (everConnected: the Service reached "connected" at least once, so a lost
// connection reads as reconnecting rather than as a daemon never started.)
//
// returns: { status, glyph, dimmed, alert, dot, mainCount, channelCount,
//            mainText, channelText, hidden, tooltip }
function view(input) {
  var daemonState = daemonStatus(input)
  var status = daemonState || workspaceStatus(input)
  var look = STATUSES[status]
  var lost = status === "reconnecting" && daemonState === "reconnecting"

  var badges = daemonState ? { main: 0, channel: 0 } : badgeCounts(input)
  return {
    status: status,
    glyph: look.glyph,
    dimmed: look.dimmed || lost,
    alert: look.alert,
    dot: look.dot && badges.main > 0,
    mainCount: badges.main,
    channelCount: badges.channel,
    mainText: badgeText(badges.main),
    channelText: badgeText(badges.channel),
    hidden: !!input.hideWhenEmpty && look.hideable && !lost && badges.main === 0 && badges.channel === 0,
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

// ---- badges ---------------------------------------------------------------

// Counts from the wire, with anything that is not a non-negative number read
// as zero.
function readCounts(raw) {
  function n(v) { return typeof v === "number" && v > 0 ? Math.floor(v) : 0 }
  raw = raw || {}
  return { dms: n(raw.dms), mentions: n(raw.mentions), channels: n(raw.channels), threads: n(raw.threads) }
}

function badgeCounts(input) {
  var c = readCounts(input.summary ? input.summary.total : null)
  var direct = c.dms + c.mentions
  var rest = c.channels + c.threads
  if (input.unreadScope === "all") return { main: direct + rest, channel: 0 }
  return { main: direct, channel: input.showChannelBadge ? rest : 0 }
}

// ---- layout ---------------------------------------------------------------

// layout places the shown badges beside the glyph, never over it: they
// start a gap after the glyph's right edge and stack top to bottom,
// centered on the bar as a group, so a lone badge sits level with the
// glyph. The slot grows only by the widest badge.
//
// input: { slotWidth, height, iconSize, gap, badges: [{ width, height }] }
// (the button's width and height, the glyph's size, the spacing, and the
// badges that show, top first)
// returns: { width, x, ys } (the icon's width, the badges' left edge and
// each badge's top)
function layout(input) {
  var badges = input.badges || []
  var x = Math.round((input.slotWidth + input.iconSize) / 2 + input.gap)
  if (badges.length === 0) return { width: input.slotWidth, x: x, ys: [] }
  var total = input.gap * (badges.length - 1)
  var widest = 0
  badges.forEach(function(b) { total += b.height; widest = Math.max(widest, b.width) })
  var y = Math.round((input.height - total) / 2)
  var ys = badges.map(function(b) { var top = y; y += b.height + input.gap; return top })
  return { width: Math.max(input.slotWidth, x + widest), x: x, ys: ys }
}

// ---- tooltip --------------------------------------------------------------

function daemonTooltip(status, input) {
  var lines
  if (status === "starting") lines = ["omaslack: connecting to the daemon…"]
  else if (status === "down") lines = ["omaslack: the daemon is not running.", START_HINT]
  else if (status === "reconnecting")
    lines = ["omaslack: lost the daemon, retrying.", "If it stopped: " + START_HINT, "Middle-click to retry now."]
  else lines = ["omaslack: the plugin and the daemon speak different protocol versions.",
                "Update omaslack so both match."]
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
