import QtQuick
import "../.." as Plugin

// The quick-view panel content builds with a Service.
Item {
  id: root
  property var service: null

  Plugin.QuickPanel { id: panel; service: root.service }

  function run() {
    var failures = []
    if (panel.implicitHeight <= 0) failures.push("the panel has no height")
    return failures
  }
}
