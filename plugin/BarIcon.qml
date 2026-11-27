import QtQuick
import qs.Commons
import qs.Commons as Commons
import qs.Ui
import "lib/badges.js" as Badges

// The bar icon (task 4.3): follows the bar's color, one unread dot at the
// glyph's corner for anything unread in scope (no counts: those are in the
// per-workspace tooltip and the quick panel), the connection state
// (connected, reconnecting, error, auth_expired) and DND. Dims, with a
// tooltip on how to start the daemon, while the daemon does not answer.
// Takes no space when hideWhenEmpty is on and nothing is unread (never
// during DND).
//
// Every decision lives in lib/badges.js (tested in tests/tst_badges.qml);
// this file feeds it the Service state and draws what it returns.
Item {
  id: root

  property QtObject bar: null
  property var service: null
  // "all" or "dms-and-mentions": what lights the unread dot.
  property string unreadScope: Badges.DEFAULT_SCOPE
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
    unreadScope: unreadScope,
    hideWhenEmpty: hideWhenEmpty
  })
  readonly property bool hidden: view.hidden
  // The glyph's button, where the bar centers its open-panel mark
  // (BarWidget.openPanelIndicatorItem).
  readonly property Item glyphButton: button

  // Where the dot goes (lib/badges.js layout): the glyph's top-right
  // corner, inside the slot on horizontal and vertical bars.
  readonly property var dotLayout: Badges.layout({
    slotWidth: button.width,
    height: button.height,
    iconSize: Style.bar.iconSize,
    dotSize: dot.width
  })

  visible: !hidden
  implicitWidth: hidden ? 0 : button.implicitWidth
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

    // The unread dot: the bar's urgent color, ringed 1 px in the bar
    // background so it reads on any theme and parts from the glyph's
    // strokes. Inside the button, so it takes the button's opacity (dimmed,
    // concealed); scaled about the slot's center like the glyph when
    // pressed. No shadow, no animation: it changes often.
    Rectangle {
      id: dot
      objectName: "dot"
      visible: root.view.dot
      width: Style.space(6) + 2
      height: width
      radius: width / 2
      x: root.dotLayout.x
      y: root.dotLayout.y
      color: root.bar ? root.bar.urgent : Commons.Color.urgent
      border.width: 1
      border.color: Commons.Color.bar.background
      transform: Scale {
        origin.x: button.width / 2 - dot.x
        origin.y: button.height / 2 - dot.y
        xScale: button.pressScale
        yScale: button.pressScale
      }
    }
  }
}
