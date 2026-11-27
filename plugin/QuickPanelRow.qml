pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Layouts
import qs.Commons
import qs.Commons as Commons
import qs.Ui
import "lib/quickpanel.js" as Lib
import "lib/contrast.js" as C

// One unread conversation in the quick-view panel: name, workspace,
// mention badge, a preview of the latest messages, the row actions and,
// while replying, the quick reply field. It draws and reports; QuickPanel
// owns the cursor and talks to the Service.
CursorSurface {
  id: root

  // A row of Lib.rows().
  property var row: ({})
  // [{ author, text }], oldest first, authors already named.
  property var lines: []
  property bool showWorkspace: true
  property bool replying: false
  property string fontFamily: Style.font.family

  signal pointed()
  signal openRequested()
  signal markReadRequested()
  signal replyRequested()
  signal replyAccepted(string text)
  signal replyCancelled()
  // The reply field took or lost the keyboard.
  signal replyFocusChanged(bool focused)

  // Secondary text and the badge's text readable in the popup
  // (lib/contrast.js).
  readonly property color dim: C.muted(root.foreground, Commons.Color.popups.background)
  readonly property string badgeText: Lib.badge(row)

  function focusReply() { replyField.forceActiveFocus() }
  function clearReply() { replyField.text = "" }

  implicitHeight: body.implicitHeight + Style.spacing.rowGap * 2

  MouseArea {
    anchors.fill: parent
    hoverEnabled: true
    cursorShape: Qt.PointingHandCursor
    onEntered: root.pointed()
    onClicked: root.openRequested()
  }

  ColumnLayout {
    id: body
    anchors.left: parent.left
    anchors.right: parent.right
    anchors.top: parent.top
    anchors.margins: Style.spacing.rowGap
    anchors.leftMargin: Style.spacing.rowPaddingX
    anchors.rightMargin: Style.spacing.md
    spacing: Style.spacing.labelGap

    RowLayout {
      Layout.fillWidth: true
      spacing: Style.spacing.md

      Text {
        Layout.fillWidth: true
        textFormat: Text.PlainText
        text: root.row.name || ""
        color: root.foreground
        font.family: root.fontFamily
        font.pixelSize: Style.font.subtitle
        font.bold: true
        elide: Text.ElideRight
      }

      Text {
        visible: root.showWorkspace
        textFormat: Text.PlainText
        text: root.row.workspace || ""
        color: root.dim
        font.family: root.fontFamily
        font.pixelSize: Style.font.caption
        font.bold: true
      }

      Rectangle {
        visible: root.badgeText !== ""
        implicitWidth: Math.max(implicitHeight, badgeLabel.implicitWidth + Style.spacing.md * 2)
        implicitHeight: badgeLabel.implicitHeight + Style.spacing.xxs * 2
        radius: implicitHeight / 2
        color: Commons.Color.accent

        Text {
          id: badgeLabel
          anchors.centerIn: parent
          textFormat: Text.PlainText
          text: root.badgeText
          color: C.textOn(Commons.Color.accent, Commons.Color.popups.background, root.foreground)
          font.family: root.fontFamily
          font.pixelSize: Style.font.caption
          font.bold: true
        }
      }

      PanelActionButton {
        iconText: "󰑚"
        tooltipText: "Reply (r)"
        foreground: root.foreground
        fontFamily: root.fontFamily
        onClicked: root.replyRequested()
        onHovered: function(on) { if (on) root.pointed() }
      }
      PanelActionButton {
        iconText: "󰄬"
        tooltipText: "Mark as read (x)"
        foreground: root.foreground
        fontFamily: root.fontFamily
        onClicked: root.markReadRequested()
        onHovered: function(on) { if (on) root.pointed() }
      }
      PanelActionButton {
        iconText: "󰏌"
        tooltipText: "Open (Enter)"
        foreground: root.foreground
        fontFamily: root.fontFamily
        onClicked: root.openRequested()
        onHovered: function(on) { if (on) root.pointed() }
      }
    }

    Repeater {
      model: root.lines
      Text {
        required property var modelData
        Layout.fillWidth: true
        textFormat: Text.PlainText
        text: (modelData.author !== "" ? modelData.author + ": " : "") + modelData.text
        color: root.dim
        font.family: root.fontFamily
        font.pixelSize: Style.font.bodySmall
        elide: Text.ElideRight
      }
    }

    TextField {
      id: replyField
      visible: root.replying
      Layout.fillWidth: true
      placeholderText: "Reply to " + (root.row.name || "") + " (Enter sends, Esc leaves)"
      // The shell's darker placeholder reads as typed text on a light theme.
      placeholderTextColor: root.dim
      foreground: root.foreground
      font.family: root.fontFamily
      onAccepted: root.replyAccepted(text)
      onActiveFocusChanged: root.replyFocusChanged(activeFocus)
      Keys.onEscapePressed: function(event) { event.accepted = true; root.replyCancelled() }
    }
  }
}
