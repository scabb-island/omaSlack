import QtQuick
import QtQuick.Layouts
import qs.Commons
import qs.Commons as Commons
import qs.Ui
import "lib/actions.js" as A

// The inline "Delete this message? y/n" bar at the bottom of the window,
// as the TUI asks in its status line: y (or the Delete button) confirms,
// any other key (or Keep) keeps the message. Nothing is deleted until
// then.
FocusScope {
  id: root

  property bool opened: false
  property color foreground: Commons.Color.foreground
  property string fontFamily: Style.font.family

  signal confirmed()
  signal canceled()

  visible: opened
  height: visible ? bar.implicitHeight : 0

  function open() {
    opened = true
    forceActiveFocus()
  }

  function answer(yes) {
    opened = false
    if (yes) confirmed()
    else canceled()
  }

  // handleKey takes the answer; modifier keys alone wait for the next.
  function handleKey(key, text) {
    if (key === Qt.Key_Shift || key === Qt.Key_Control || key === Qt.Key_Alt || key === Qt.Key_Meta) return false
    answer(A.confirmsDelete(text))
    return true
  }

  Keys.onPressed: function(event) { if (root.handleKey(event.key, event.text)) event.accepted = true }

  BorderSurface {
    id: bar
    anchors.fill: parent
    implicitHeight: row.implicitHeight + Style.spacing.md * 2
    color: Commons.Color.popups.background
    borderSpec: Border.controlSpec("focus", root.foreground, Commons.Color.urgent)

    RowLayout {
      id: row
      anchors.fill: parent
      anchors.leftMargin: Style.spacing.rowPaddingX
      anchors.rightMargin: Style.spacing.rowPaddingX
      spacing: Style.spacing.controlGap

      Text {
        Layout.fillWidth: true
        textFormat: Text.PlainText
        text: "Delete this message? y/n"
        color: root.foreground
        font.family: root.fontFamily
        font.pixelSize: Style.font.body
        font.bold: true
        elide: Text.ElideRight
      }

      Button {
        text: "Delete"
        foreground: Commons.Color.urgent
        bordered: true
        fontFamily: root.fontFamily
        onClicked: root.answer(true)
      }

      Button {
        text: "Keep"
        bordered: true
        foreground: root.foreground
        fontFamily: root.fontFamily
        onClicked: root.answer(false)
      }
    }
  }
}
