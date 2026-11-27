import QtQuick
import Quickshell
import "../.." as Plugin

// The quick panel with nothing but keys (task 4.6), with real key events
// (KeyDriver) in a window standing in for the bar's popup: j/k move a
// visible cursor over the rows, r opens the quick reply with the keyboard
// in it, Enter sends and gives the keyboard back, Esc in the field only
// leaves it, x marks read, d and h/l reach the DND buttons, Enter opens a
// row, and Esc with nothing open asks to close the popup.
Item {
  id: root
  property var service: null

  property int closes: 0

  FloatingWindow {
    visible: true
    implicitWidth: 460
    implicitHeight: 520

    Plugin.QuickPanel {
      id: panel
      anchors.fill: parent
      service: root.service
      openWith: "window"
      onCloseRequested: root.closes++
    }
  }
  KeyDriver { id: kd }

  function callsOf(method) { return root.service.calls.filter(function(c) { return c.method === method }) }
  function last(method) { var c = callsOf(method); return c.length ? c[c.length - 1].params : null }
  function focusItem() { return panel.Window.activeFocusItem }
  // The QuickPanelRow drawing rows[i].
  function rowItem(i) {
    var queue = [panel]
    while (queue.length) {
      var it = queue.shift()
      if (it.hasOwnProperty("replying") && it.row && it.row.key === panel.rows[i].key) return it
      for (var k = 0; k < it.children.length; k++) queue.push(it.children[k])
    }
    return null
  }

  function load() {
    var s = root.service
    s.workspaces = [{ team: "T1", name: "Acme", self: "U1" }]
    s.conversations = { T1: [
      { id: "D1", kind: "im", name: "Ana", latest: "20.0", mentions: 0, unread: true },
      { id: "C1", kind: "channel", name: "#general", latest: "10.0", mentions: 2, unread: true }
    ] }
    s.unreadList = [
      { team: "T1", conversation: s.conversations.T1[0] },
      { team: "T1", conversation: s.conversations.T1[1] }
    ]
    s.answers = { "messages.history": { error: null, result: [] } }
  }

  function run() {
    var failures = []
    function check(ok, message) { if (!ok) failures.push(message) }
    var s = root.service
    var saved = { workspaces: s.workspaces, conversations: s.conversations, unreadList: s.unreadList, answers: s.answers }
    load()
    if (!kd.activate(panel)) return ["the window did not become active"]
    panel.forceActiveFocus()
    kd.wait(1)
    var catcher = focusItem()
    check(catcher && catcher.hasOwnProperty("blocked"), "the panel's keys do not have the keyboard: " + catcher)

    // The cursor: first row, j moves it, and the row draws it.
    check(panel.cursor.section === "rows" && panel.cursor.index === 0 && rowItem(0).hasCursor, "no cursor on the first row")
    kd.press(Qt.Key_J)
    check(panel.cursor.index === 1 && rowItem(1).hasCursor && !rowItem(0).hasCursor, "j did not move the drawn cursor")

    // r: the reply field takes the keyboard; Esc only leaves it.
    s.reset()
    kd.type("r")
    check(panel.replyKey === "T1/C1" && panel.replyFocused, "r did not open the reply with the keyboard in it")
    kd.press(Qt.Key_Escape)
    check(panel.replyKey === "" && root.closes === 0, "Esc in the reply closed more than the reply")
    check(focusItem() === catcher, "after the reply, the panel's keys do not have the keyboard")

    // r again, type, Enter: sent, marked read, keyboard back.
    kd.type("r")
    kd.type("on it")
    kd.press(Qt.Key_Return)
    var sent = last("messages.send")
    check(sent && sent.conv === "C1" && sent.text === "on it", "the reply sent " + JSON.stringify(sent))
    check(last("conversations.markRead") && last("conversations.markRead").conv === "C1", "the reply did not mark the row read")
    check(panel.replyKey === "" && focusItem() === catcher, "after sending, the panel's keys do not have the keyboard")

    // x marks read; k back up; d to DND, l across, Enter presses.
    s.reset()
    kd.press(Qt.Key_K)
    kd.type("x")
    check(last("conversations.markRead") && last("conversations.markRead").conv === "D1", "x marked " + JSON.stringify(last("conversations.markRead")))
    kd.type("d")
    check(panel.cursor.section === "dnd" && panel.cursor.index === 0, "d did not reach the DND buttons")
    kd.press(Qt.Key_L)
    check(panel.cursor.index === 1, "l did not move across the DND buttons")
    kd.press(Qt.Key_Return)
    check(callsOf("dnd.snooze").length + callsOf("dnd.end").length > 0, "Enter on a DND button did nothing")

    // Back down to a row, Enter opens it; Esc asks to close.
    kd.press(Qt.Key_J)
    kd.press(Qt.Key_Return)
    var opened = last("openWindow")
    check(opened && opened.team === "T1", "Enter opened " + JSON.stringify(opened))
    var before = root.closes
    kd.press(Qt.Key_Escape)
    check(root.closes === before + 1, "Esc did not ask to close the popup")

    for (var k in saved) s[k] = saved[k]
    return failures
  }
}
