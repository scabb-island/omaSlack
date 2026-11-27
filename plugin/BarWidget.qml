pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons
import qs.Ui

// The bar entry point: the icon with its badges (BarIcon.qml, task 4.3) and
// the quick-view popup (QuickPanel.qml, task 4.4) under it. Left click
// toggles the popup; right click opens the full window (spec gui-client,
// "Bar icon with badges", scenario "Clicks").
//
// CONTRACT (M4 step 0): this file only wires the pieces together. The icon
// lives in BarIcon.qml and the popup content in QuickPanel.qml, so they can
// change independently.
Panel {
  id: root
  moduleName: "ar.scabb.omaslack"
  ipcTarget: "ar.scabb.omaslack.bar"

  // Overridable for tests (tests/smoke); in the shell it comes from the bar.
  property var service: bar && bar.shell && typeof bar.shell.serviceFor === "function"
    ? bar.shell.serviceFor(moduleName)
    : null

  // Widget settings (manifest barWidget.schema). The shell keeps
  // barWidget.defaults only as registry metadata: `settings` holds just what
  // shell.json sets (omarchy-bar set ar.scabb.omaslack <key> <value>), so
  // these fallbacks must stay equal to the manifest defaults.
  readonly property bool showChannelBadge: setting("showChannelBadge", true)
  readonly property string unreadScope: setting("unreadScope", "dms-and-mentions")
  readonly property string openWith: setting("openWith", "window")
  readonly property bool hideWhenEmpty: setting("hideWhenEmpty", false)

  // hideWhenEmpty: the slot leaves the bar too, as first-party widgets do
  // (SystemUpdate), so the bar keeps no gap for it.
  visible: !icon.hidden
  implicitWidth: icon.implicitWidth
  implicitHeight: icon.implicitHeight

  BarIcon {
    id: icon
    anchors.fill: parent
    bar: root.bar
    service: root.service
    showChannelBadge: root.showChannelBadge
    unreadScope: root.unreadScope
    hideWhenEmpty: root.hideWhenEmpty
    onLeftClicked: root.toggle()
    onRightClicked: if (root.service) root.service.openPreferred(root.openWith, "", "")
  }

  KeyboardPanel {
    id: popup
    anchorItem: icon
    owner: root
    bar: root.bar
    open: root.opened
    focusTarget: popup.loadedContent
    contentWidth: popup.fittedContentWidth(Style.space(420))
    contentHeight: popup.fittedContentHeight(popup.loadedContentHeight, Style.space(640))

    content: Component {
      QuickPanel {
        service: root.service
        bar: root.bar
        openWith: root.openWith
        onCloseRequested: root.close()
      }
    }
  }
}
