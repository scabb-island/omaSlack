pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Layouts
import qs.Commons
import qs.Commons as Commons
import qs.Ui
import "lib/contrast.js" as C

// A card over the window with a title, a text field and a list under it:
// the base of the finder (Finder.qml) and the search (SearchOverlay.qml).
// It owns the keyboard while it shows: ↑/↓ (Ctrl+P/Ctrl+N) move in the
// list, Enter emits entered(), Esc or a click outside closes it. What a
// row shows is rowContent, a Component with `row` and `selected`
// properties; rows are plain objects with a unique `key`.
FocusScope {
  id: root

  property string title: ""
  property string placeholder: ""
  property var rows: []
  property Component rowContent: null
  // A line under the field ("No messages match", an error); "" for none.
  property string hint: ""
  property int currentIndex: 0
  property color foreground: Commons.Color.popups.text
  property string fontFamily: Style.font.family

  // Secondary text and accent text readable on the card
  // (lib/contrast.js).
  readonly property color dim: C.muted(root.foreground, Commons.Color.popups.background)
  readonly property color accentText: C.readable(Commons.Color.accent, Commons.Color.popups.background, root.foreground)

  readonly property alias field: input
  readonly property alias list: view
  readonly property string query: input.text
  readonly property bool shown: visible

  // Enter in the field.
  signal entered()
  signal closed()

  visible: false

  function show(text) {
    input.text = text || ""
    currentIndex = 0
    visible = true
    input.forceActiveFocus()
  }

  function hide() {
    if (!visible) return
    visible = false
    closed()
  }

  function move(d) {
    if (rows.length === 0) return
    currentIndex = Math.max(0, Math.min(currentIndex + d, rows.length - 1))
    view.positionViewAtIndex(currentIndex, ListView.Contain)
  }

  onRowsChanged: currentIndex = Math.min(Math.max(currentIndex, 0), Math.max(rows.length - 1, 0))
  // A new query starts at the best match.
  onQueryChanged: currentIndex = 0

  // The window behind it, dimmed; a click there closes.
  Rectangle {
    anchors.fill: parent
    color: Commons.Color.menu.scrim
    MouseArea { anchors.fill: parent; onClicked: root.hide() }
  }

  BorderSurface {
    id: card
    anchors.horizontalCenter: parent.horizontalCenter
    y: Math.round(parent.height * 0.12)
    width: Math.min(Style.space(620), parent.width - Style.spacing.huge * 2)
    height: Math.min(column.implicitHeight + padding * 2, parent.height - y - Style.spacing.huge)
    padding: Style.spacing.xl
    color: Commons.Color.popups.background
    radius: Style.cornerRadius
    borderSpec: Border.localOrSurfaceSpec("popups", "border", Commons.Color.popups.border, Commons.Color.popups.border, Math.max(1, Style.space(2)))

    // Clicks on the card stay on it.
    MouseArea { anchors.fill: parent }

    ColumnLayout {
      id: column
      anchors.fill: parent
      anchors.margins: card.padding
      spacing: Style.spacing.md

      Text {
        Layout.fillWidth: true
        textFormat: Text.PlainText
        text: root.title
        color: root.foreground
        font.family: root.fontFamily
        font.pixelSize: Style.font.title
        font.bold: true
        elide: Text.ElideRight
      }

      TextField {
        id: input
        objectName: "overlayField"
        Layout.fillWidth: true
        placeholderText: root.placeholder
        // The shell's darker placeholder reads as typed text on a light theme.
        placeholderTextColor: root.dim
        foreground: root.foreground
        font.family: root.fontFamily
        font.pixelSize: Style.font.body

        Keys.onPressed: function(event) {
          var ctrl = (event.modifiers & Qt.ControlModifier) !== 0
          if (event.key === Qt.Key_Escape) root.hide()
          else if (event.key === Qt.Key_Up || (ctrl && event.key === Qt.Key_P)) root.move(-1)
          else if (event.key === Qt.Key_Down || (ctrl && event.key === Qt.Key_N)) root.move(1)
          else if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) root.entered()
          else return
          event.accepted = true
        }
      }

      Text {
        visible: root.hint !== ""
        Layout.fillWidth: true
        textFormat: Text.PlainText
        text: root.hint
        color: root.dim
        font.family: root.fontFamily
        font.pixelSize: Style.font.bodySmall
        wrapMode: Text.WordWrap
      }

      ListView {
        id: view
        objectName: "overlayList"
        Layout.fillWidth: true
        Layout.fillHeight: true
        Layout.preferredHeight: Math.min(contentHeight, Style.space(420))
        visible: root.rows.length > 0
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        keyNavigationEnabled: false
        currentIndex: root.currentIndex
        model: root.rows

        delegate: CursorSurface {
          id: rowItem
          required property var modelData
          required property int index
          width: ListView.view.width
          implicitHeight: content.implicitHeight + Style.spacing.xs * 2
          hasCursor: root.currentIndex === index
          foreground: root.foreground

          MouseArea {
            anchors.fill: parent
            cursorShape: Qt.PointingHandCursor
            onClicked: {
              root.currentIndex = rowItem.index
              root.entered()
            }
          }

          Loader {
            id: content
            anchors.left: parent.left
            anchors.right: parent.right
            anchors.verticalCenter: parent.verticalCenter
            anchors.leftMargin: Style.spacing.rowPaddingX
            anchors.rightMargin: Style.spacing.rowPaddingX
            sourceComponent: root.rowContent
            onLoaded: {
              item.row = Qt.binding(function() { return rowItem.modelData })
              item.selected = Qt.binding(function() { return rowItem.hasCursor })
            }
          }
        }
      }
    }
  }
}
