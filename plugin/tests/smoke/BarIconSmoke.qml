import QtQuick
import qs.Commons
import qs.Commons as Commons
import "../.." as Plugin

// The bar icon builds with a Service, draws what lib/badges.js decides for
// a few Service states (the unread dot, the state glyph, the tooltip), fits
// its slot on horizontal and vertical bars, and reports clicks.
// (BarWidget.qml itself is not loaded here: its KeyboardPanel popup needs a
// Wayland layer-shell backend, which the offscreen platform lacks. qmllint
// and the live test cover its wiring.)
Item {
  id: root
  property var service: null

  // The members of the shell's bar that the icon and BarIconButton read.
  component FakeBar: QtObject {
    property string fontFamily: Style.font.family
    property color barForeground: Commons.Color.bar.text
    property color urgent: Commons.Color.bar.active
    property bool vertical: false
    property int barSize: vertical ? Style.bar.sizeVertical : Style.bar.sizeHorizontal
    property bool foregroundAnimationEnabled: false
    function showTooltip(item, text) {}
    function hideTooltip(item) {}
  }
  FakeBar { id: hbar }
  FakeBar { id: vbar; vertical: true }

  Plugin.BarIcon {
    id: icon
    bar: hbar
    service: root.service
    onLeftClicked: root.clicks.push("left")
    onRightClicked: root.clicks.push("right")
  }

  property var clicks: []

  function child(item, name) {
    if (item.objectName === name) return item
    for (var i = 0; i < item.children.length; i++) {
      var found = child(item.children[i], name)
      if (found) return found
    }
    return null
  }

  function counts(dms, mentions, channels, threads) {
    return { dms: dms, mentions: mentions, channels: channels, threads: threads }
  }

  function run() {
    var failures = []
    function check(ok, message) { if (!ok) failures.push(message) }
    var s = root.service
    var saved = { connectionState: s.connectionState, connectionError: s.connectionError,
                  workspaces: s.workspaces, summary: s.summary, dnd: s.dnd }
    function unread(c) { s.summary = { workspaces: { T1: c }, total: c } }
    var button = child(icon, "button")
    var dot = child(icon, "dot")
    check(button && dot, "the button or the dot is missing")
    check(!child(icon, "mainBadge") && !child(icon, "channelBadge"), "a count pill is still drawn")
    if (failures.length) return failures

    // The bar centers its open-panel mark on the glyph's button
    // (BarWidget.openPanelIndicatorItem).
    check(icon.glyphButton === button, "glyphButton is not the glyph's button")

    // The dot draws inside the button, so it takes its opacity (dimmed,
    // concealed) and its press scale.
    var p = dot.parent
    while (p && p !== button) p = p.parent
    check(p === button, "the dot is not inside the button")
    var dotScale = dot.transform.length ? dot.transform[0] : null
    check(dotScale && dotScale.xScale === button.pressScale && dotScale.yScale === button.pressScale,
          "the dot does not follow the press scale")

    // Connected, nothing unread: no dot.
    s.workspaces = [{ team: "T1", name: "Acme", state: "connected" }]
    check(icon.implicitWidth > 0, "the icon has no width")
    check(icon.available, "a connected service is not shown as available")
    check(!dot.visible, "the dot shows with nothing unread")
    var bare = icon.implicitWidth

    // A DM: the dot, in the urgent color ringed in the bar background, on
    // the glyph's top-right corner; the slot does not grow.
    unread(counts(2, 1, 4, 0))
    check(dot.visible, "no dot for a DM")
    check(icon.implicitWidth === bare, "the dot grows the slot: " + bare + " -> " + icon.implicitWidth)
    check(dot.width === Style.space(6) + 2 && dot.height === dot.width && dot.radius === dot.width / 2,
          "dot size " + dot.width + "x" + dot.height + " r " + dot.radius)
    check(Qt.colorEqual(dot.color, hbar.urgent), "dot color " + dot.color)
    check(dot.border.width === 1 && Qt.colorEqual(dot.border.color, Commons.Color.bar.background), "dot ring")
    var glyphRight = (button.width + Style.bar.iconSize) / 2
    var glyphTop = (button.height - Style.bar.iconSize) / 2
    check(dot.x === Math.round(glyphRight - 0.55 * dot.width) && dot.y === Math.round(glyphTop - 0.35 * dot.width),
          "dot at " + dot.x + "," + dot.y)
    check(dot.x + dot.width <= button.width && dot.y >= 0, "the dot sticks out of the slot")
    check(button.tooltipText.indexOf("Acme: 2 DMs, 1 mention, 4 channels") >= 0, "tooltip: " + button.tooltipText)

    // Scope: with "DMs and mentions", a channel alone does not light it.
    unread(counts(0, 0, 4, 1))
    check(dot.visible, "the default scope (Everything) leaves channels out")
    icon.unreadScope = "dms-and-mentions"
    check(!dot.visible, "a channel lights the dot with the scope DMs and mentions")
    check(button.tooltipText.indexOf("Acme: 4 channels, 1 thread") >= 0, "tooltip: " + button.tooltipText)
    icon.unreadScope = "all"

    // An alert glyph hides the dot; the tooltip keeps the counts.
    unread(counts(2, 0, 0, 0))
    var alerts = ["auth_expired", "connecting", "disconnected"]
    for (var i = 0; i < alerts.length; i++) {
      s.workspaces = [{ team: "T1", name: "Acme", state: alerts[i] }]
      check(!dot.visible, "the dot shows next to the " + alerts[i] + " glyph")
      check(button.tooltipText.indexOf("Acme: 2 DMs") >= 0, alerts[i] + " tooltip: " + button.tooltipText)
    }
    s.workspaces = [{ team: "T1", name: "Acme", state: "connected" }]

    // DND: the dot still shows, and hideWhenEmpty never hides the icon.
    s.dnd = { T1: { snoozed: true, until: 0 } }
    check(dot.visible, "no dot during DND")
    icon.hideWhenEmpty = true
    unread(counts(0, 0, 0, 0))
    check(icon.visible && icon.implicitWidth > 0, "DND vanished with hideWhenEmpty")
    s.dnd = {}
    check(!icon.visible && icon.implicitWidth === 0, "hideWhenEmpty leaves the icon in the bar")

    // A sign-in expired: alert, and never hidden.
    s.workspaces = [{ team: "T1", name: "Acme", state: "auth_expired" }]
    check(icon.visible && icon.implicitWidth > 0, "auth_expired is hidden")
    check(button.active, "auth_expired is not shown as an alert")
    icon.hideWhenEmpty = false
    s.workspaces = [{ team: "T1", name: "Acme", state: "connected" }]

    // A vertical bar: the same corner dot, inside the bar's width.
    unread(counts(3, 0, 0, 0))
    icon.bar = vbar
    check(icon.implicitWidth === vbar.barSize, "vertical icon width " + icon.implicitWidth + ", bar " + vbar.barSize)
    check(dot.visible, "no dot on a vertical bar")
    check(dot.x >= 0 && dot.x + dot.width <= button.width && dot.y >= 0 && dot.y + dot.height <= button.height,
          "vertical dot at " + dot.x + "," + dot.y + " in " + button.width + "x" + button.height)
    icon.bar = hbar

    // The daemon was never reached: dimmed, no dot, with how to start it.
    icon.everConnected = false
    s.connectionError = "connection refused"
    s.connectionState = "unavailable"
    check(button.dimmed, "a missing daemon does not dim the icon")
    check(!dot.visible, "the dot shows without the daemon")
    check(button.tooltipText.indexOf("systemctl --user start omaslack") >= 0, "tooltip: " + button.tooltipText)

    // Middle click asks the Service to reconnect; left and right report.
    s.reset()
    button.pressed(Qt.MiddleButton)
    button.pressed(Qt.LeftButton)
    button.pressed(Qt.RightButton)
    check(s.calls.length === 1 && s.calls[0].method === "reconnect", "middle click did not reconnect")
    check(root.clicks.join(",") === "left,right", "clicks: " + root.clicks.join(","))

    for (var k in saved) s[k] = saved[k]
    return failures
  }
}
