pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Layouts
import qs.Commons
import qs.Commons as Commons
import qs.Ui
import "lib/window.js" as W
import "lib/actions.js" as A
import "lib/contrast.js" as C

// One message in a MessageList: the day separator above it when the day
// changes, the author and time (unless it continues the previous
// message's author), the text (the daemon's rich text, or the plain text
// until it is there), "(edited)", files, reactions and "↳ N replies".
// It draws and reports; MessageList owns the cursor.
//
// Image files also show a thumbnail (FileThumb). Clicking a reaction
// chip (toggle) or a file (open) is reported through reactionClicked and
// fileClicked; message actions (edit, delete) act on the list's
// currentMessage(), not on this item.
Column {
  id: root

  // A row of W.messageRows(): { key, message, day, grouped, time }.
  property var row: ({ message: ({}) })
  property string team: ""
  property var service: null
  // The user's own ID in this workspace, to mark their reactions.
  property string selfUser: ""
  property bool hasCursor: false
  // Whether to show "↳ N replies" (not inside the thread pane).
  property bool showThread: true
  property color foreground: Commons.Color.foreground
  property string fontFamily: Style.font.family

  signal pointed()
  signal clicked()
  signal threadRequested()
  signal linkActivated(string url)
  signal reactionClicked(string emoji)
  signal fileClicked(var file)

  readonly property var message: row && row.message ? row.message : ({})
  // Secondary text and accent text (links, "↳ N replies") readable on the
  // window's background on light and dark themes (lib/contrast.js).
  readonly property color dim: C.muted(root.foreground, Commons.Color.background)
  readonly property color accentText: C.readable(Commons.Color.accent, Commons.Color.background, root.foreground)
  readonly property var content: W.body(message, String(accentText))
  readonly property string threadText: showThread ? W.threadLabel(message) : ""
  readonly property var chips: W.reactionChips(message.reactions, selfUser)

  spacing: 0

  // ---- day separator ----------------------------------------------------------

  Item {
    visible: !!root.row.day
    width: root.width
    height: visible ? dayText.implicitHeight + Style.spacing.lg * 2 : 0

    Rectangle {
      anchors.verticalCenter: parent.verticalCenter
      width: parent.width
      height: Style.spacing.hairline
      color: Style.normalBorderColor
    }

    Rectangle {
      anchors.centerIn: parent
      width: dayText.implicitWidth + Style.spacing.lg * 2
      height: dayText.implicitHeight
      color: Commons.Color.background

      Text {
        id: dayText
        anchors.centerIn: parent
        textFormat: Text.PlainText
        text: root.row.day || ""
        color: root.dim
        font.family: root.fontFamily
        font.pixelSize: Style.font.caption
        font.bold: true
      }
    }
  }

  // ---- the message ------------------------------------------------------------

  CursorSurface {
    width: root.width
    implicitHeight: body.implicitHeight + Style.spacing.xs * 2
    hasCursor: root.hasCursor
    foreground: root.foreground

    MouseArea {
      anchors.fill: parent
      hoverEnabled: true
      acceptedButtons: Qt.LeftButton
      onEntered: root.pointed()
      onClicked: root.clicked()
    }

    ColumnLayout {
      id: body
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.top: parent.top
      anchors.topMargin: Style.spacing.xs
      anchors.leftMargin: Style.spacing.rowPaddingX
      anchors.rightMargin: Style.spacing.rowPaddingX
      spacing: Style.spacing.xxs

      RowLayout {
        visible: !root.row.grouped
        Layout.fillWidth: true
        Layout.topMargin: Style.spacing.sm
        spacing: Style.spacing.md

        Text {
          objectName: "author"
          textFormat: Text.PlainText
          text: root.message.author && root.service ? root.service.userName(root.team, root.message.author) : ""
          color: root.foreground
          font.family: root.fontFamily
          font.pixelSize: Style.font.body
          font.bold: true
          elide: Text.ElideRight
          Layout.maximumWidth: body.width * 0.7
        }

        Text {
          textFormat: Text.PlainText
          text: root.row.time || ""
          color: root.dim
          font.family: root.fontFamily
          font.pixelSize: Style.font.caption
        }

        Item { Layout.fillWidth: true }
      }

      Text {
        objectName: "body"
        Layout.fillWidth: true
        visible: text !== ""
        textFormat: root.content.rich ? Text.RichText : Text.PlainText
        text: root.content.text
        color: root.foreground
        font.family: root.fontFamily
        font.pixelSize: Style.font.body
        wrapMode: Text.Wrap
        onLinkActivated: function(url) { root.linkActivated(url) }

        HoverHandler {
          cursorShape: parent.hoveredLink !== "" ? Qt.PointingHandCursor : Qt.ArrowCursor
        }
      }

      Text {
        visible: !!root.message.edited
        textFormat: Text.PlainText
        text: "(edited)"
        color: root.dim
        font.family: root.fontFamily
        font.pixelSize: Style.font.caption
      }

      Repeater {
        model: root.message.files || []

        Text {
          required property var modelData
          Layout.fillWidth: true
          textFormat: Text.PlainText
          text: "󰈙 " + W.fileLabel(modelData)
          color: root.dim
          font.family: root.fontFamily
          font.pixelSize: Style.font.bodySmall
          elide: Text.ElideMiddle

          MouseArea {
            anchors.fill: parent
            cursorShape: Qt.PointingHandCursor
            onClicked: root.fileClicked(parent.modelData)
          }
        }
      }

      Repeater {
        model: (root.message.files || []).filter(A.isImage)

        FileThumb {
          required property var modelData
          Layout.preferredWidth: Math.min(implicitWidth, body.width)
          Layout.preferredHeight: implicitHeight
          service: root.service
          team: root.team
          file: modelData
          onClicked: root.fileClicked(modelData)
        }
      }

      Flow {
        visible: root.chips.length > 0
        Layout.fillWidth: true
        spacing: Style.spacing.sm

        Repeater {
          model: root.chips

          BorderSurface {
            id: chip
            required property var modelData
            implicitWidth: chipText.implicitWidth + Style.spacing.md * 2
            implicitHeight: chipText.implicitHeight + Style.spacing.xxs * 2
            radius: Style.cornerRadius
            color: modelData.mine ? Style.selectedAccentFill : Style.normalFill
            borderSpec: Border.controlSpec(modelData.mine ? "selected" : "normal", root.foreground, Commons.Color.accent)

            Text {
              id: chipText
              anchors.centerIn: parent
              textFormat: Text.PlainText
              text: chip.modelData.label
              color: root.foreground
              font.family: root.fontFamily
              font.pixelSize: Style.font.caption
            }

            MouseArea {
              anchors.fill: parent
              onClicked: root.reactionClicked(chip.modelData.emoji)
            }
          }
        }
      }

      Text {
        objectName: "threadLabel"
        visible: root.threadText !== ""
        textFormat: Text.PlainText
        text: root.threadText
        color: root.accentText
        font.family: root.fontFamily
        font.pixelSize: Style.font.bodySmall
        font.bold: true

        MouseArea {
          anchors.fill: parent
          cursorShape: Qt.PointingHandCursor
          onClicked: root.threadRequested()
        }
      }
    }
  }
}
