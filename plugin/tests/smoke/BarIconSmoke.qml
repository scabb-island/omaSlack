import QtQuick
import "../.." as Plugin

// The bar icon builds with a Service, draws what lib/badges.js decides for
// a few Service states, and reports clicks.
// (BarWidget.qml itself is not loaded here: its KeyboardPanel popup needs a
// Wayland layer-shell backend, which the offscreen platform lacks. qmllint
// and the live test cover its wiring.)
Item {
  id: root
  property var service: null

  Plugin.BarIcon {
    id: icon
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
    var button = child(icon, "button")
    var main = child(icon, "mainBadge")
    var channel = child(icon, "channelBadge")
    check(button && main && channel, "the button or a badge is missing")
    if (failures.length) return failures

    // Connected, nothing unread.
    s.workspaces = [{ team: "T1", name: "Acme", state: "connected" }]
    check(icon.implicitWidth > 0, "the icon has no width")
    check(icon.available, "a connected service is not shown as available")
    check(!main.visible && !channel.visible, "badges show with nothing unread")
    var bare = icon.implicitWidth

    // Unread DMs and channels: both badges, and the icon makes room.
    var c = counts(2, 1, 4, 0)
    s.summary = { workspaces: { T1: c }, total: c }
    check(main.visible && main.label === "3", "main badge: " + main.label)
    check(channel.visible && channel.label === "4", "channel badge: " + channel.label)
    check(icon.implicitWidth > bare, "the badges get no room")
    check(button.tooltipText.indexOf("Acme: 2 DMs, 1 mention, 4 channels") >= 0, "tooltip: " + button.tooltipText)
    icon.showChannelBadge = false
    check(!channel.visible, "the channel badge ignores showChannelBadge")
    icon.showChannelBadge = true

    // A sign-in expired: alert, and never hidden.
    icon.hideWhenEmpty = true
    s.summary = { workspaces: {}, total: counts(0, 0, 0, 0) }
    check(!icon.visible && icon.implicitWidth === 0, "hideWhenEmpty leaves the icon in the bar")
    s.workspaces = [{ team: "T1", name: "Acme", state: "auth_expired" }]
    check(icon.visible && icon.implicitWidth > 0, "auth_expired is hidden")
    check(button.active, "auth_expired is not shown as an alert")
    icon.hideWhenEmpty = false

    // The daemon was never reached: dimmed, with how to start it.
    icon.everConnected = false
    s.connectionError = "connection refused"
    s.connectionState = "unavailable"
    check(button.dimmed, "a missing daemon does not dim the icon")
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
