import QtQuick
import QtQuick.Layouts
import qs.Commons
import qs.Commons as Commons
import qs.Ui

// A column with a header, a MessageList and a Composer: the window shows
// one for the open conversation and one for the open thread (threadTS
// set, with a close button). It draws and forwards; the window owns the
// state and the Service calls.
FocusScope {
  id: root

  property var service: null
  property string team: ""
  property string conv: ""
  property string threadTS: ""
  property string title: ""
  property string subtitle: ""
  property bool closable: false
  // Whether this pane has the keyboard.
  property bool focused: false
  property string selfUser: ""
  property string placeholder: ""
  // Shown in the middle when there is nothing to list.
  property string emptyText: ""
  property color foreground: Commons.Color.foreground
  property string fontFamily: Style.font.family

  readonly property alias list: list
  readonly property alias composer: composer

  signal closeRequested()
  signal submitted(string text)
  signal composerEscaped()
  signal threadRequested(string ts)
  signal linkActivated(string url)
  signal messageClicked(int index)

  ColumnLayout {
    anchors.fill: parent
    anchors.margins: Style.spacing.lg
    spacing: Style.spacing.md

    RowLayout {
      Layout.fillWidth: true
      spacing: Style.spacing.md

      Text {
        objectName: "paneTitle"
        Layout.fillWidth: !subtitleText.visible
        textFormat: Text.PlainText
        text: root.title
        color: root.foreground
        font.family: root.fontFamily
        font.pixelSize: Style.font.title
        font.bold: true
        elide: Text.ElideRight
      }

      Text {
        id: subtitleText
        visible: text !== ""
        Layout.fillWidth: true
        textFormat: Text.PlainText
        text: root.subtitle
        color: Qt.darker(root.foreground, 1.4)
        font.family: root.fontFamily
        font.pixelSize: Style.font.caption
        elide: Text.ElideRight
      }

      PanelActionButton {
        visible: root.closable
        iconText: "󰅖"
        tooltipText: "Close the thread (Esc)"
        foreground: root.foreground
        fontFamily: root.fontFamily
        onClicked: root.closeRequested()
      }
    }

    PanelSeparator {
      Layout.fillWidth: true
      foreground: root.foreground
    }

    Item {
      Layout.fillWidth: true
      Layout.fillHeight: true

      MessageList {
        id: list
        anchors.fill: parent
        service: root.service
        team: root.team
        conv: root.conv
        threadTS: root.threadTS
        focused: root.focused && !composer.editing
        selfUser: root.selfUser
        foreground: root.foreground
        fontFamily: root.fontFamily
        onThreadRequested: function(ts) { root.threadRequested(ts) }
        onLinkActivated: function(url) { root.linkActivated(url) }
        onMessageClicked: function(index) { root.messageClicked(index) }
      }

      Text {
        visible: list.count === 0 && root.emptyText !== ""
        anchors.centerIn: parent
        width: parent.width
        horizontalAlignment: Text.AlignHCenter
        wrapMode: Text.WordWrap
        textFormat: Text.PlainText
        text: root.emptyText
        color: Qt.darker(root.foreground, 1.4)
        font.family: root.fontFamily
        font.pixelSize: Style.font.body
      }
    }

    Composer {
      id: composer
      Layout.fillWidth: true
      Layout.preferredHeight: implicitHeight
      service: root.service
      placeholder: root.placeholder
      foreground: root.foreground
      fontFamily: root.fontFamily
      onSubmitted: function(text) { root.submitted(text) }
      onEscaped: root.composerEscaped()
    }
  }
}
