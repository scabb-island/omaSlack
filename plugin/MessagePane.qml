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
  // A reaction chip or a file of message was clicked.
  signal reactionClicked(var message, string emoji)
  signal fileClicked(var message, var file)

  function messageAt(ts) { var r = root.list.rowByKey[ts]; return r ? r.message : null }

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

    // The rule under the header turns accent while the pane has the
    // keyboard, so it shows before any message is selected.
    Rectangle {
      objectName: "paneRule"
      readonly property bool focusShown: root.focused
      Layout.fillWidth: true
      Layout.preferredHeight: Style.space(2)
      color: focusShown ? Commons.Color.accent : Style.normalBorderColor
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
        onReactionClicked: function(ts, emoji) { if (root.messageAt(ts)) root.reactionClicked(root.messageAt(ts), emoji) }
        onFileClicked: function(ts, file) { if (root.messageAt(ts)) root.fileClicked(root.messageAt(ts), file) }
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

    Text {
      visible: composer.editTS !== ""
      Layout.fillWidth: true
      textFormat: Text.PlainText
      text: "Editing a message: Enter saves, Esc cancels"
      color: Commons.Color.accent
      font.family: root.fontFamily
      font.pixelSize: Style.font.caption
      elide: Text.ElideRight
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
