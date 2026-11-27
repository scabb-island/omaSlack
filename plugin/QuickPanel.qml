import QtQuick
import qs.Commons
import qs.Commons as Commons
import qs.Ui

// The quick-view popup under the bar icon (task 4.4): conversations with
// unread messages, DMs and mentions first, each with a preview; a quick
// reply that sends and marks the conversation read; mark as read; DND; and
// opening a conversation in the window or the TUI.
//
// CONTRACT (M4 step 0): inputs and signals fixed; content lands with 4.4.
Item {
  id: root

  property var service: null
  property QtObject bar: null
  // "window" or "tui": where picking a conversation opens it.
  property string openWith: "window"

  // The popup should close (after opening a conversation, or on Esc).
  signal closeRequested()

  implicitWidth: Style.space(420)
  implicitHeight: placeholder.implicitHeight + Style.space(32)

  Text {
    id: placeholder
    anchors.centerIn: parent
    text: "omaslack"
    color: Commons.Color.foreground
    font.family: Style.font.family
  }
}
