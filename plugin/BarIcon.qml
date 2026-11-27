import QtQuick
import qs.Commons
import qs.Ui

// The bar icon (task 4.3): follows the bar's color, one badge for DMs and
// mentions aimed at the user and one for channels and threads, the
// connection state (connected, reconnecting, error, auth_expired), DND and a
// per-workspace tooltip. Dims, with a tooltip on how to start the daemon,
// while the daemon does not answer. Takes no space when hideWhenEmpty is on
// and nothing is unread.
//
// CONTRACT (M4 step 0): inputs and signals fixed; the M0 look stays until
// task 4.3 replaces it.
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

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  BarIconButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    text: "󰒱"
    slotSize: Style.bar.statusSlot
    opacity: root.available ? 1 : 0.45
    tooltipText: root.available
      ? "omaslack: daemon connected"
      : "omaslack: the daemon is not running (systemctl --user start omaslack)"
    onPressed: function(b) {
      if (b === Qt.RightButton) root.rightClicked()
      else root.leftClicked()
    }
  }
}
