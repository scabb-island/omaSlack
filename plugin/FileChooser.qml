pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons
import qs.Ui
import "lib/actions.js" as A

// Picks one of a message's files to open (o on a message with several),
// as the TUI's chooser: j/k or ↑/↓ move, Enter or 1–9 opens, any other
// key cancels.
ActionCard {
  id: root

  property var files: []
  property int currentIndex: 0

  signal chosen(var file)

  title: "Open which file?"
  hint: "j/k move · Enter or 1–9 opens · Esc cancels"
  keyHandler: function(event) { return root.handleKey(event.key, event.text) }
  onDismissed: opened = false

  function open(list) {
    files = list
    currentIndex = 0
    opened = true
    forceActiveFocus()
  }

  function choose(i) {
    opened = false
    if (i >= 0 && i < files.length) chosen(files[i])
  }

  function handleKey(key, text) {
    if (key === Qt.Key_Shift || key === Qt.Key_Control || key === Qt.Key_Alt || key === Qt.Key_Meta) return false
    var n = Number(text)
    if (key === Qt.Key_Down || text === "j") currentIndex = Math.min(currentIndex + 1, files.length - 1)
    else if (key === Qt.Key_Up || text === "k") currentIndex = Math.max(currentIndex - 1, 0)
    else if (key === Qt.Key_Return || key === Qt.Key_Enter) choose(currentIndex)
    else if (text !== "" && n >= 1 && n <= 9) choose(n - 1)
    else opened = false
    return true
  }

  Repeater {
    model: root.files

    CursorSurface {
      id: row
      required property var modelData
      required property int index
      width: parent ? parent.width : 0
      implicitHeight: Style.spacing.popupRowHeight
      hasCursor: index === root.currentIndex
      foreground: root.foreground

      Text {
        anchors.verticalCenter: parent.verticalCenter
        anchors.left: parent.left
        anchors.leftMargin: Style.spacing.rowPaddingX
        anchors.right: parent.right
        textFormat: Text.PlainText
        text: (row.index + 1) + "  " + A.fileName(row.modelData)
        color: root.foreground
        font.family: root.fontFamily
        font.pixelSize: Style.font.body
        elide: Text.ElideMiddle
      }

      MouseArea {
        anchors.fill: parent
        hoverEnabled: true
        onEntered: root.currentIndex = row.index
        onClicked: root.choose(row.index)
      }
    }
  }
}
