pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Layouts
import qs.Commons
import qs.Commons as Commons
import qs.Ui
import "lib/nav.js" as N

// The window's key help (?): every key, in the sections of lib/nav.js
// HELP, laid out in columns. Any key or a click closes it, as in the TUI.
FocusScope {
  id: root

  property color foreground: Commons.Color.popups.text
  property string fontFamily: Style.font.family
  readonly property var sections: N.HELP

  signal closed()

  visible: false

  function show() {
    visible = true
    forceActiveFocus()
  }

  function hide() {
    if (!visible) return
    visible = false
    closed()
  }

  Keys.onPressed: function(event) {
    // A lone modifier is not a key the user meant.
    if (event.key === Qt.Key_Shift || event.key === Qt.Key_Control || event.key === Qt.Key_Alt || event.key === Qt.Key_Meta) return
    event.accepted = true
    root.hide()
  }

  Rectangle {
    anchors.fill: parent
    color: Commons.Color.menu.scrim
    MouseArea { anchors.fill: parent; onClicked: root.hide() }
  }

  BorderSurface {
    id: card
    anchors.centerIn: parent
    // Three columns of sections when they fit.
    width: Math.min(Style.space(330) * 3 + Style.spacing.huge * 2 + padding * 2, parent.width - Style.spacing.huge * 2)
    height: Math.min(column.implicitHeight + padding * 2, parent.height - Style.spacing.huge * 2)
    padding: Style.spacing.xl
    color: Commons.Color.popups.background
    radius: Style.cornerRadius
    borderSpec: Border.localOrSurfaceSpec("popups", "border", Commons.Color.popups.border, Commons.Color.popups.border, Math.max(1, Style.space(2)))
    clip: true

    MouseArea { anchors.fill: parent; onClicked: root.hide() }

    ColumnLayout {
      id: column
      anchors.fill: parent
      anchors.margins: card.padding
      spacing: Style.spacing.lg

      Text {
        textFormat: Text.PlainText
        text: "Keys"
        color: root.foreground
        font.family: root.fontFamily
        font.pixelSize: Style.font.title
        font.bold: true
      }

      Flow {
        id: flow
        objectName: "helpSections"
        Layout.fillWidth: true
        spacing: Style.spacing.huge

        Repeater {
          model: root.sections

          ColumnLayout {
            id: section
            required property var modelData
            width: Style.space(330)
            spacing: Style.spacing.xxs

            PanelSectionHeader {
              text: section.modelData.title
              foreground: root.foreground
              fontFamily: root.fontFamily
            }

            Repeater {
              model: section.modelData.keys

              RowLayout {
                id: keyRow
                required property var modelData
                Layout.fillWidth: true
                spacing: Style.spacing.md

                Text {
                  Layout.preferredWidth: Style.space(90)
                  textFormat: Text.PlainText
                  text: keyRow.modelData[0]
                  color: Commons.Color.accent
                  font.family: root.fontFamily
                  font.pixelSize: Style.font.body
                  font.bold: true
                }
                Text {
                  Layout.fillWidth: true
                  textFormat: Text.PlainText
                  text: keyRow.modelData[1]
                  color: root.foreground
                  font.family: root.fontFamily
                  font.pixelSize: Style.font.body
                  wrapMode: Text.WordWrap
                }
              }
            }
          }
        }
      }

      Text {
        textFormat: Text.PlainText
        text: "Press any key to close."
        color: Qt.darker(root.foreground, 1.4)
        font.family: root.fontFamily
        font.pixelSize: Style.font.caption
      }
    }
  }
}
