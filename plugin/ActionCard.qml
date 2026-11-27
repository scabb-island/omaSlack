import QtQuick
import qs.Commons
import qs.Commons as Commons
import qs.Ui
import "lib/contrast.js" as C

// A card over the window for the message actions' choices (the reaction
// picker, the file chooser): a title, the caller's content and a hint
// line, centered, on a click-to-cancel backdrop. It takes the keyboard
// while open and hands every key to keyHandler(event) -> bool.
FocusScope {
  id: root

  property bool opened: false
  property string title: ""
  property string hint: ""
  property color foreground: Commons.Color.foreground
  property string fontFamily: Style.font.family
  property int cardWidth: Style.space(360)
  // function(event) -> bool: true when the key was taken.
  property var keyHandler: null
  default property alias content: body.data
  // Secondary text readable on the card (lib/contrast.js).
  readonly property color dim: C.muted(root.foreground, Commons.Color.popups.background)

  signal dismissed()

  visible: opened
  anchors.fill: parent

  Keys.onPressed: function(event) { if (root.keyHandler && root.keyHandler(event)) event.accepted = true }

  MouseArea {
    anchors.fill: parent
    onClicked: root.dismissed()
  }

  BorderSurface {
    anchors.centerIn: parent
    width: Math.min(root.cardWidth, root.width - Style.spacing.panelPadding * 2)
    height: column.implicitHeight + Style.spacing.popupPadding * 2
    color: Commons.Color.popups.background
    borderSpec: Border.controlSpec("focus", root.foreground, Commons.Color.accent)
    radius: Style.cornerRadius

    // Clicks on the card stay on it.
    MouseArea { anchors.fill: parent }

    Column {
      id: column
      anchors.fill: parent
      anchors.margins: Style.spacing.popupPadding
      spacing: Style.spacing.md

      Text {
        width: parent.width
        textFormat: Text.PlainText
        text: root.title
        color: root.foreground
        font.family: root.fontFamily
        font.pixelSize: Style.font.title
        font.bold: true
        elide: Text.ElideRight
      }

      Column {
        id: body
        width: parent.width
        spacing: Style.spacing.sm
      }

      Text {
        width: parent.width
        textFormat: Text.PlainText
        text: root.hint
        color: root.dim
        font.family: root.fontFamily
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
      }
    }
  }
}
