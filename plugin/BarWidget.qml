import QtQuick
import qs.Commons
import qs.Ui

// Bar icon for omaslack. M0 skeleton: the icon dims while the daemon is not
// reachable and the tooltip says so. Badges, the quick-view panel and the
// window land in M4 (spec gui-client).
BarWidget {
  id: root
  moduleName: "ar.scabb.omaslack"

  readonly property var service: bar && bar.shell && typeof bar.shell.serviceFor === "function"
    ? bar.shell.serviceFor(moduleName)
    : null
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
      ? "omaslack: daemon conectado"
      : "omaslack: el daemon no está corriendo (systemctl --user start omaslack)"

    onPressed: function(b) {
      if (root.service) root.service.probe()
    }
  }
}
