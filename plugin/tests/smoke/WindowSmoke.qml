import QtQuick
import "../.." as Plugin

// The window opens and closes through the shell lifecycle.
Item {
  id: root
  property var service: null

  Plugin.Window { id: win; service: root.service }

  function run() {
    var failures = []
    win.open("")
    if (!win.opened) failures.push("open() did not show the window")
    win.dismiss()
    if (win.opened) failures.push("dismiss() did not hide the window")
    return failures
  }
}
