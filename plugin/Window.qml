import QtQuick
import Quickshell
import qs.Commons
import qs.Commons as Commons
import qs.Ui

// The full window (task 4.5): a FloatingWindow with the fixed title
// "omaslack" (for Hyprland rules), with the workspace and conversation
// sidebar, messages, threads, compose, reactions, attachments, search and
// the finder. It is the plugin's "panel" entry point, so
// `omarchy-shell shell toggle ar.scabb.omaslack` shows and hides it, and
// `summon` with a payload { team, conv, ts } opens that conversation.
//
// CONTRACT (M4 step 0): the shell lifecycle (open/close/dismiss) is fixed;
// content lands with 4.5.
Item {
  id: root

  // Set by the shell for panel plugins.
  property var shell: null
  property var manifest: null

  // Overridable for tests (tests/smoke).
  property var service: shell && typeof shell.serviceFor === "function"
    ? shell.serviceFor("ar.scabb.omaslack")
    : null

  // Whether the shell considers the window open.
  property bool opened: false

  // Shell-initiated show; payloadJson may carry { team, conv, ts }.
  function open(payloadJson) {
    opened = true
  }

  // Shell-initiated hide (`shell hide`, `toggle`).
  function close() {
    opened = false
  }

  // User-initiated close (Esc, the close button): tell the shell, so its
  // open-panel state stays right and the next toggle shows the window.
  function dismiss() {
    if (shell && typeof shell.hide === "function") shell.hide("ar.scabb.omaslack")
    else close()
  }

  FloatingWindow {
    id: window
    visible: root.opened
    title: "omaslack"
    color: Commons.Color.background
    implicitWidth: 1100
    implicitHeight: 720

    Text {
      anchors.centerIn: parent
      text: "omaslack"
      color: Commons.Color.foreground
      font.family: Style.font.family
    }
  }
}
