pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons
import qs.Commons as Commons
import qs.Ui
import "lib/actions.js" as A

// The reaction picker (r): a filter line over the emoji names, best match
// first, recent ones first among equals (lib/actions.js filterEmoji).
// Enter picks the selected one, or the typed name when nothing matches
// (Slack checks it); Up/Down (Ctrl+j/k, Ctrl+n/p) move; Esc cancels.
// The caller fills names (standard, then custom from emoji.list) and
// recent, and reacts on picked(name).
ActionCard {
  id: root

  property var names: []
  property var recent: []
  property alias query: input.text
  property int currentIndex: 0
  // How many matches the list shows.
  property int limit: 60

  readonly property var results: opened ? A.filterEmoji(names, recent, query, limit) : []

  signal picked(string name)

  title: "React"
  hint: "Type to filter · ↑/↓ move · Enter reacts (or toggles yours off) · Esc cancels"
  keyHandler: function(event) { return root.handleKey(event.key, event.text, event.modifiers) }
  onDismissed: close()
  onQueryChanged: currentIndex = 0

  function open() {
    query = ""
    currentIndex = 0
    opened = true
    input.forceActiveFocus()
  }

  function close() { opened = false }

  function accept() {
    var name = A.pickEmoji(results, currentIndex, query)
    if (name === "") return
    // picked first: closing hands the keyboard back.
    picked(name)
    opened = false
  }

  function move(d) {
    if (results.length === 0) return
    currentIndex = Math.max(0, Math.min(currentIndex + d, results.length - 1))
    list.positionViewAtIndex(currentIndex, ListView.Contain)
  }

  // handleKey runs the picker's keys; false leaves the key to the filter.
  function handleKey(key, text, modifiers) {
    var ctrl = (modifiers || 0) & Qt.ControlModifier
    if (key === Qt.Key_Escape) close()
    else if (key === Qt.Key_Return || key === Qt.Key_Enter) accept()
    else if (key === Qt.Key_Down || (ctrl && (key === Qt.Key_J || key === Qt.Key_N))) move(1)
    else if (key === Qt.Key_Up || (ctrl && (key === Qt.Key_K || key === Qt.Key_P))) move(-1)
    else return false
    return true
  }

  BorderSurface {
    width: parent.width
    height: input.implicitHeight + Style.spacing.inputPaddingY * 2
    color: Style.controlFill(true, false, root.foreground, Commons.Color.accent)
    borderSpec: Border.controlSpec("focus", root.foreground, Commons.Color.accent)
    radius: Style.cornerRadius

    TextInput {
      id: input
      objectName: "reactionFilter"
      anchors.fill: parent
      anchors.leftMargin: Style.spacing.controlPaddingX
      anchors.rightMargin: Style.spacing.controlPaddingX
      verticalAlignment: TextInput.AlignVCenter
      clip: true
      color: root.foreground
      selectionColor: Style.selectionFillFor(root.foreground, Commons.Color.accent)
      font.family: root.fontFamily
      font.pixelSize: Style.font.body
      Keys.onPressed: function(event) { if (root.handleKey(event.key, event.text, event.modifiers)) event.accepted = true }

      Text {
        visible: input.text === ""
        anchors.verticalCenter: parent.verticalCenter
        textFormat: Text.PlainText
        text: "Emoji name"
        color: Qt.darker(root.foreground, 1.6)
        font: input.font
      }
    }
  }

  ListView {
    id: list
    width: parent.width
    height: Math.min(contentHeight, Style.spacing.popupRowHeight * 9)
    clip: true
    model: root.results
    currentIndex: root.currentIndex
    boundsBehavior: Flickable.StopAtBounds

    delegate: CursorSurface {
      id: row
      required property string modelData
      required property int index
      width: ListView.view.width
      implicitHeight: Style.spacing.popupRowHeight
      hasCursor: index === root.currentIndex
      foreground: root.foreground

      Text {
        anchors.verticalCenter: parent.verticalCenter
        anchors.left: parent.left
        anchors.leftMargin: Style.spacing.rowPaddingX
        anchors.right: parent.right
        textFormat: Text.PlainText
        // Custom emoji are images: their name alone.
        text: (A.emojiGlyph(row.modelData) || "  ") + "  :" + row.modelData + ":"
        color: root.foreground
        font.family: root.fontFamily
        font.pixelSize: Style.font.body
        elide: Text.ElideRight
      }

      MouseArea {
        anchors.fill: parent
        hoverEnabled: true
        onEntered: root.currentIndex = row.index
        onClicked: { root.currentIndex = row.index; root.accept() }
      }
    }
  }

  Text {
    visible: root.results.length === 0
    width: parent.width
    textFormat: Text.PlainText
    text: A.pickEmoji([], 0, root.query) !== "" ? "Enter reacts with :" + A.pickEmoji([], 0, root.query) + ":" : "No emoji match."
    color: Qt.darker(root.foreground, 1.4)
    font.family: root.fontFamily
    font.pixelSize: Style.font.bodySmall
  }
}
