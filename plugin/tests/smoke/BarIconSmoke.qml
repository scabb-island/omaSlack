import QtQuick
import "../.." as Plugin

// The bar icon builds with a Service and reports clicks.
// (BarWidget.qml itself is not loaded here: its KeyboardPanel popup needs a
// Wayland layer-shell backend, which the offscreen platform lacks. qmllint
// and the live test cover its wiring.)
Item {
  id: root
  property var service: null

  Plugin.BarIcon { id: icon; service: root.service }

  function run() {
    var failures = []
    if (icon.implicitWidth <= 0) failures.push("the icon has no width")
    if (!icon.available) failures.push("a connected service is not shown as available")
    return failures
  }
}
