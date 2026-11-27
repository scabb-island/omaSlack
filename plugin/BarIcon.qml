import QtQuick
import qs.Commons
import qs.Commons as Commons
import qs.Ui
import "lib/badges.js" as Badges
import "lib/contrast.js" as C

// The bar icon (task 4.3): follows the bar's color, one badge for DMs and
// mentions aimed at the user and one for channels and threads, the
// connection state (connected, reconnecting, error, auth_expired), DND and a
// per-workspace tooltip. Dims, with a tooltip on how to start the daemon,
// while the daemon does not answer. Takes no space when hideWhenEmpty is on
// and nothing is unread.
//
// Every decision lives in lib/badges.js (tested in tests/tst_badges.qml);
// this file feeds it the Service state and draws what it returns.
Item {
  id: root

  property QtObject bar: null
  property var service: null
  property bool showChannelBadge: true
  // "dms-and-mentions" or "all": what the main badge counts.
  property string unreadScope: "dms-and-mentions"
  property bool hideWhenEmpty: false

  signal leftClicked()
  signal rightClicked()

  readonly property bool available: !!service && service.daemonAvailable
  // Whether the Service has reached the daemon since this icon was built,
  // so a lost connection reads as "reconnecting", not as "never started".
  property bool everConnected: false
  onAvailableChanged: if (available) everConnected = true
  onServiceChanged: everConnected = available
  Component.onCompleted: everConnected = available

  // Wall clock for DND countdowns, ticking only while some DND is set.
  property real nowSeconds: Date.now() / 1000
  Timer {
    interval: 30000
    repeat: true
    running: !!root.service && Object.keys(root.service.dnd || {}).length > 0
    onTriggered: root.nowSeconds = Date.now() / 1000
    onRunningChanged: root.nowSeconds = Date.now() / 1000
  }

  readonly property var view: Badges.view({
    hasService: !!service,
    connectionState: service ? service.connectionState : "unknown",
    connectionError: service ? service.connectionError : "",
    everConnected: everConnected,
    workspaces: service ? service.workspaces : [],
    summary: service ? service.summary : null,
    dnd: service ? service.dnd : ({}),
    nowSeconds: nowSeconds,
    showChannelBadge: showChannelBadge,
    unreadScope: unreadScope,
    hideWhenEmpty: hideWhenEmpty
  })
  readonly property bool hidden: view.hidden

  // Where the badges go (lib/badges.js layout): beside the glyph, stacked
  // and centered on the bar. Which badges show comes from the view, not
  // from the badges' visible, which also follows this icon's own.
  readonly property var shownBadges: [mainBadge, channelBadge].filter(function(b) { return b.label !== "" })
  readonly property var badgeLayout: Badges.layout({
    slotWidth: button.width,
    height: root.height,
    iconSize: Style.bar.iconSize,
    gap: Style.spaceReal(1),
    badges: shownBadges.map(function(b) { return { width: b.width, height: b.height } })
  })

  function badgeY(badge) {
    var i = shownBadges.indexOf(badge)
    return i >= 0 ? badgeLayout.ys[i] : 0
  }

  visible: !hidden
  implicitWidth: hidden ? 0
    : (button.vertical ? button.implicitWidth : Math.max(button.implicitWidth, badgeLayout.width))
  implicitHeight: button.implicitHeight

  BarIconButton {
    id: button
    objectName: "button"
    width: vertical ? parent.width : implicitWidth
    height: parent.height
    bar: root.bar
    text: root.view.glyph
    slotSize: Style.bar.statusSlot
    dimmed: root.view.dimmed
    active: root.view.alert
    tooltipText: root.view.tooltip
    onPressed: function(b) {
      if (b === Qt.RightButton) root.rightClicked()
      else if (b === Qt.MiddleButton) { if (root.service) root.service.reconnect() }
      else root.leftClicked()
    }
  }

  component Badge: Rectangle {
    id: badge
    property string label: ""
    property color fill
    // Background or text color, whichever reads on the fill.
    property color textColor: C.textOn(fill, Commons.Color.background, Commons.Color.foreground)
    property string fontFamily: Style.font.family
    visible: label !== ""
    color: fill
    height: Style.font.caption + Style.space(2)
    width: Math.max(height, badgeLabel.implicitWidth + Style.space(5))
    radius: height / 2

    Text {
      id: badgeLabel
      anchors.centerIn: parent
      textFormat: Text.PlainText
      text: badge.label
      color: badge.textColor
      font.family: badge.fontFamily
      font.pixelSize: Style.font.caption
      font.bold: true
      renderType: Text.NativeRendering
    }
  }

  // DMs and mentions: the bar's urgent color, as first-party widgets use for
  // what needs the user.
  Badge {
    id: mainBadge
    objectName: "mainBadge"
    fontFamily: root.bar ? root.bar.fontFamily : Style.font.family
    label: root.view.mainText
    fill: root.bar ? root.bar.urgent : Commons.Color.urgent
    x: root.badgeLayout.x
    y: root.badgeY(mainBadge)
  }

  // Channels and threads: quieter, in the bar's own foreground.
  Badge {
    id: channelBadge
    objectName: "channelBadge"
    fontFamily: root.bar ? root.bar.fontFamily : Style.font.family
    label: root.view.channelText
    fill: root.bar ? root.bar.barForeground : Commons.Color.foreground
    x: root.badgeLayout.x
    y: root.badgeY(channelBadge)
  }
}
