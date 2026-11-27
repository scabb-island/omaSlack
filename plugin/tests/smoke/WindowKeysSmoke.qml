import QtQuick
import "../.." as Plugin

// The window with nothing but keys (task 4.6), against FakeService and
// with real key events (KeyDriver): pick a conversation in the sidebar,
// select a message, open its thread, write a reply with an @ completion
// and send it; then every overlay (finder, search, help, reaction picker,
// file chooser, delete confirmation, autocomplete) takes the keyboard when
// it opens and hands it back when it closes, Esc steps back one level at
// a time, and the list with the keyboard always shows where its cursor is.
Item {
  id: root
  property var service: null

  Plugin.Window { id: win; service: root.service }
  KeyDriver { id: kd }

  readonly property string theirsTS: "1700000060.000100"
  readonly property string mineTS: "1700000120.000100"
  readonly property string replyTS: "1700000090.000100"

  function callsOf(method) { return root.service.calls.filter(function(c) { return c.method === method }) }
  function last(method) { var c = callsOf(method); return c.length ? c[c.length - 1].params : null }
  function focusItem() { return win.sidebarView.Window.activeFocusItem }
  function focusName() { var f = focusItem(); return f ? (f.objectName || String(f)) : "nothing" }

  function child(item, name) {
    if (!item) return null
    if (item.objectName === name) return item
    for (var i = 0; i < item.children.length; i++) {
      var found = child(item.children[i], name)
      if (found) return found
    }
    return null
  }

  // The bool property prop of item's descendant named name (false when
  // there is none).
  function flag(item, name, prop) { var c = child(item, name); return !!c && !!c[prop] }

  // Whether list's delegate at its currentIndex draws the cursor.
  function cursorShown(list) { list.forceLayout(); var it = list.currentIndex >= 0 ? list.itemAtIndex(list.currentIndex) : null; return !!it && !!it.hasCursor }

  function load() {
    var s = root.service
    s.workspaces = [{ team: "T1", name: "Acme", self: "U1", state: "connected", capabilities: ["files"] }]
    s.conversations = { T1: [{ id: "C1", kind: "channel", name: "general" }, { id: "C2", kind: "channel", name: "random" }] }
    s.userNames = { "T1/U1": "Ana", "T1/U2": "Bo" }
    var lists = {}
    lists["T1/C1"] = [
      { ts: theirsTS, author: "U2", text: "hi <@U1>", replyCount: 1 },
      { ts: mineTS, author: "U1", text: "see <#C2|random> &amp; <@U2>",
        files: [{ id: "F1", name: "a.pdf", size: 10 }, { id: "F2", name: "b.pdf", size: 20 }] }
    ]
    lists["T1/C1/" + theirsTS] = [lists["T1/C1"][0], { ts: replyTS, threadTS: theirsTS, author: "U2", text: "a reply" }]
    lists["T1/C2"] = [{ ts: "1700000000.000100", author: "U2", text: "random" }]
    s.messageLists = lists
    s.answers = {
      "conversations.open": { error: null, result: { messages: [] } },
      "drafts.get": { error: null, result: { text: "" } },
      "users.search": { error: null, result: [{ id: "U7", name: "ana", realName: "Ana Diaz", displayName: "Ana" }] }
    }
  }

  function run() {
    var failures = []
    function check(ok, message) { if (!ok) failures.push(message) }
    function focusOn(item, what) { check(focusItem() === item, what + " does not have the keyboard: " + focusName()) }
    var s = root.service
    var saved = { workspaces: s.workspaces, conversations: s.conversations, userNames: s.userNames,
                  messageLists: s.messageLists, answers: s.answers }
    load()
    win.open("")
    if (!kd.activate(win.sidebarView)) return ["the window did not become active"]
    kd.wait(1)
    var keys = child(win.sidebarView.parent.parent, "windowKeys")
    focusOn(keys, "the window's keys")

    // ---- the sidebar: its cursor shows as soon as it has the keyboard ----------

    var sidebar = win.sidebarView
    var row = sidebar.itemAtIndex(win.sidebarIndex)
    check(win.pane === "sidebar" && row && flag(row, "convRow", "hasCursor"), "the sidebar shows no cursor on open")
    kd.press(Qt.Key_J)
    kd.press(Qt.Key_K)
    check(win.sidebarRows[win.sidebarIndex].conv === "C1", "j k left the cursor on " + JSON.stringify(win.sidebarRows[win.sidebarIndex]))
    kd.press(Qt.Key_Return)
    check(win.conv === "C1" && win.pane === "messages", "Enter opened " + win.conv + " in " + win.pane)
    focusOn(keys, "after opening, the window's keys")

    // ---- a reply in a thread, by keys only --------------------------------------

    var list = win.conversationView.list
    check(flag(win.conversationView, "paneRule", "focusShown"), "the conversation pane does not show it has the keyboard")
    check(!flag(win.threadView, "paneRule", "focusShown"), "the hidden thread pane shows the keyboard")
    kd.press(Qt.Key_K)
    kd.press(Qt.Key_Up)
    check(list.currentMessage() && list.currentMessage().ts === theirsTS, "k ↑ selected " + JSON.stringify(list.currentMessage()))
    check(cursorShown(list), "the selected message shows no cursor")
    kd.press(Qt.Key_Return)
    check(win.threadOpen && win.threadTS === theirsTS && win.pane === "thread", "Enter did not open the message's thread")
    check(flag(win.threadView, "paneRule", "focusShown") && !flag(win.conversationView, "paneRule", "focusShown"),
          "the thread pane does not show it has the keyboard")
    check(!cursorShown(list), "the conversation's cursor still shows while the thread has the keyboard")
    kd.press(Qt.Key_J)
    var thread = win.threadView.list
    check(thread.currentMessage() && cursorShown(thread), "j in the thread shows no cursor")
    kd.press(Qt.Key_Return)
    var composer = win.threadView.composer
    focusOn(composer.input, "Enter in the thread: its composer")

    s.reset()
    kd.type("thanks @an")
    kd.wait(250)
    var complete = child(win.overlayLayer, "threadAutocomplete")
    check(complete && complete.open, "@an offers nobody")
    kd.press(Qt.Key_Escape)
    check(!complete.open, "Esc did not close the suggestions")
    focusOn(composer.input, "after Esc on the suggestions, the composer")
    // Leaving the word forgets the dismissal: a new @ word suggests again.
    for (var b = 0; b < 3; b++) kd.press(Qt.Key_Backspace)
    kd.type("@an")
    kd.wait(250)
    check(complete.open, "the suggestions did not come back on a new word")
    kd.press(Qt.Key_Tab)
    kd.type("ok")
    check(composer.text === "thanks @Ana ok", "the composer has " + JSON.stringify(composer.text))
    kd.press(Qt.Key_Return)
    var sent = last("messages.send")
    check(sent && sent.conv === "C1" && sent.threadTS === theirsTS && sent.text === "thanks <@U7> ok",
          "the reply sent " + JSON.stringify(sent))
    check(composer.text === "", "the composer kept " + JSON.stringify(composer.text))

    // Esc: out of the composer, then the thread closes, nothing more.
    kd.press(Qt.Key_Escape)
    focusOn(keys, "Esc in the composer: the window's keys")
    check(win.threadOpen && win.pane === "thread", "Esc in the composer closed the thread")
    kd.press(Qt.Key_Escape)
    check(!win.threadOpen && win.pane === "messages" && win.opened, "the second Esc did not just close the thread")
    focusOn(keys, "after closing the thread, the window's keys")

    // ---- overlays take the keyboard and give it back ----------------------------

    // The finder: Ctrl+K as the letter with Control held, and Ctrl+T.
    var finder = win.finderView
    kd.press(Qt.Key_K, Qt.ControlModifier)
    check(finder.visible, "Ctrl+K did not open the finder")
    focusOn(finder.field, "the finder's field")
    kd.wait(50) // a frame: the card lays its list out
    check(cursorShown(finder.list), "the finder shows no cursor")
    kd.press(Qt.Key_Escape)
    check(!finder.visible && win.opened, "Esc did not close only the finder")
    kd.wait(1)
    focusOn(keys, "after the finder, the window's keys")
    kd.press(Qt.Key_T, Qt.ControlModifier)
    check(finder.visible, "Ctrl+T did not open the finder")
    kd.type("random")
    kd.press(Qt.Key_Return)
    check(win.conv === "C2" && !finder.visible, "the finder opened " + win.conv)
    kd.wait(1)
    focusOn(keys, "after a pick in the finder, the window's keys")
    kd.press(Qt.Key_K, Qt.ControlModifier)
    kd.type("general")
    kd.press(Qt.Key_Return)
    kd.wait(1)
    check(win.conv === "C1", "the finder did not go back to C1")

    // Search and help.
    kd.type("/")
    check(win.searchView.visible, "/ did not open the search")
    focusOn(win.searchView.field, "the search field")
    kd.press(Qt.Key_Escape)
    kd.wait(1)
    check(!win.searchView.visible && win.opened, "Esc did not close only the search")
    focusOn(keys, "after the search, the window's keys")
    kd.type("?")
    check(win.helpView.visible, "? did not open the help")
    focusOn(win.helpView, "the help")
    kd.press(Qt.Key_Escape)
    kd.wait(1)
    check(!win.helpView.visible && win.opened, "Esc did not close only the help")
    focusOn(keys, "after the help, the window's keys")

    // The reaction picker, on your own message.
    var actions = win.messageActions
    kd.press(Qt.Key_K)
    check(list.currentMessage() && list.currentMessage().ts === mineTS, "k did not select the newest message")
    s.reset()
    kd.type("r")
    check(actions.picker.opened, "r did not open the picker")
    focusOn(child(actions.picker, "reactionFilter"), "the picker's filter")
    kd.press(Qt.Key_Escape)
    check(!actions.picker.opened && win.opened, "Esc did not close only the picker")
    focusOn(keys, "after the picker, the window's keys")
    kd.type("r")
    kd.type("tada")
    kd.press(Qt.Key_Return)
    var reacted = last("reactions.add")
    check(reacted && reacted.ts === mineTS && reacted.emoji === "tada", "the picker reacted " + JSON.stringify(reacted))
    focusOn(keys, "after a reaction, the window's keys")

    // The file chooser.
    kd.type("o")
    check(actions.chooser.opened, "o on two files did not open the chooser")
    focusOn(actions.chooser, "the file chooser")
    check(flag(actions.chooser, "fileRow0", "hasCursor"), "the chooser shows no cursor")
    kd.press(Qt.Key_Escape)
    check(!actions.chooser.opened && win.opened, "Esc did not close only the chooser")
    focusOn(keys, "after the chooser, the window's keys")

    // The delete confirmation: n keeps, Delete then y deletes.
    kd.type("D")
    check(actions.confirm.opened, "D did not ask")
    focusOn(actions.confirm, "the delete confirmation")
    kd.type("n")
    check(!actions.confirm.opened && callsOf("messages.delete").length === 0, "n deleted the message")
    focusOn(keys, "after keeping, the window's keys")

    // ---- editing and deleting your message ------------------------------------

    kd.type("e")
    var convComposer = win.conversationView.composer
    check(convComposer.editTS === mineTS, "e did not start the edit")
    kd.type("!")
    kd.press(Qt.Key_Return)
    var edited = last("messages.edit")
    check(edited && edited.ts === mineTS, "Enter did not save the edit")
    kd.wait(1)
    focusOn(keys, "after the edit, the window's keys")

    // Delete, then y.
    kd.press(Qt.Key_Delete)
    kd.type("y")
    check(last("messages.delete") && last("messages.delete").ts === mineTS, "y did not delete")
    focusOn(keys, "after deleting, the window's keys")

    // Esc from the message list with nothing else open closes the window.
    kd.press(Qt.Key_Escape)
    check(!win.opened, "Esc from the list did not close the window")

    win.close()
    for (var k in saved) s[k] = saved[k]
    return failures
  }
}
