import QtQuick
import "../.." as Plugin

// The quick-view panel against FakeService: rows, previews that never mark
// anything read, quick reply (send, then mark read), mark as read, open,
// DND and the empty state.
Item {
  id: root
  property var service: null

  property int closes: 0

  Plugin.QuickPanel {
    id: panel
    service: root.service
    openWith: "tui"
    onCloseRequested: root.closes++
  }

  function child(item, name) {
    if (item.objectName === name) return item
    for (var i = 0; i < item.children.length; i++) {
      var found = child(item.children[i], name)
      if (found) return found
    }
    return null
  }

  function methods() { return root.service.calls.map(function(c) { return c.method }) }
  function callsOf(method) { return root.service.calls.filter(function(c) { return c.method === method }) }

  function load() {
    var s = root.service
    s.workspaces = [{ team: "T1", name: "Acme" }, { team: "T2", name: "Side" }]
    s.conversations = {
      T1: [{ id: "D1", kind: "im", name: "Ana", latest: "20.0", mentions: 0, unread: true }],
      T2: [{ id: "C1", kind: "channel", name: "#general", latest: "10.0", mentions: 2, unread: true }]
    }
    s.userNames = { "T1/U1": "Ana", "T2/U2": "Bo" }
    s.answers = {
      "messages.history": { error: null, result: [
        { ts: "20.0", author: "U1", text: "see you", html: "<b>see</b> you" },
        { ts: "19.0", author: "U1", text: "hello" },
        { ts: "18.0", author: "B1", authorName: "deploybot", text: "deployed" }
      ] }
    }
    s.unreadList = [
      { team: "T1", conversation: s.conversations.T1[0] },
      { team: "T2", conversation: s.conversations.T2[0] }
    ]
    panel.refreshPreviews()
  }

  function run() {
    var failures = []
    function check(ok, message) { if (!ok) failures.push(message) }

    load()

    // Rows.
    check(panel.rows.length === 2, "expected 2 rows, got " + panel.rows.length)
    check(panel.rows[0].name === "Ana" && panel.rows[0].workspace === "Acme", "first row is not Ana in Acme")
    check(panel.rows[1].mentions === 2, "the channel row lost its mentions")
    check(!panel.empty, "the panel says it is empty with rows")

    // Previews: messages.history with a small limit, nothing that marks read.
    var history = callsOf("messages.history")
    check(history.length === 2, "expected 2 preview fetches, got " + history.length)
    check(history.length > 0 && history[0].params.limit === 3, "previews do not ask for 3 messages")
    check(callsOf("conversations.open").length === 0, "previews called conversations.open")
    check(callsOf("conversations.markRead").length === 0, "previews marked something read")
    var lines = panel.previewLines("T1/D1")
    check(lines.length === 3 && lines[2].text === "see you" && lines[2].author === "Ana",
          "the preview is not the latest messages: " + JSON.stringify(lines))
    check(lines.length === 3 && lines[0].author === "deploybot", "a bot's line lost its name: " + JSON.stringify(lines))

    // Unchanged latest: no refetch. New latest: one refetch.
    panel.refreshPreviews()
    check(callsOf("messages.history").length === 2, "previews were refetched with the same latest")
    var conv = { id: "D1", kind: "im", name: "Ana", latest: "21.0", mentions: 0, unread: true }
    root.service.unreadList = [{ team: "T1", conversation: conv }, root.service.unreadList[1]]
    panel.refreshPreviews()
    check(callsOf("messages.history").length === 3, "a new latest did not refetch the preview")

    // Quick reply: send, then mark read, in that order.
    root.service.reset()
    panel.sendReply(panel.rows[0], "on my way")
    var m = methods()
    check(m.length === 2 && m[0] === "messages.send" && m[1] === "conversations.markRead",
          "quick reply made " + JSON.stringify(m))
    check(m.length > 0 && root.service.calls[0].params.text === "on my way" && root.service.calls[0].params.conv === "D1",
          "quick reply sent the wrong message")
    root.service.reset()
    panel.sendReply(panel.rows[0], "   ")
    check(root.service.calls.length === 0, "an empty reply was sent")

    // A failed send does not mark read.
    root.service.answers = { "messages.send": { error: { code: 1, message: "nope" }, result: null } }
    panel.sendReply(panel.rows[0], "hi")
    check(callsOf("conversations.markRead").length === 0, "a failed send marked read")
    check(panel.status !== "", "a failed send shows no error")
    root.service.answers = {}

    // Mark as read through the keyboard (x on the cursor row).
    root.service.reset()
    panel.moveCursor(0, 1)
    panel.markCursorRead()
    var reads = callsOf("conversations.markRead")
    check(reads.length === 1 && reads[0].params.conv === panel.rows[1].conv, "x did not mark the second row read")

    // Open through the keyboard: Enter on the cursor row.
    root.service.reset()
    panel.activateCursor()
    var opened = callsOf("openTui")
    check(opened.length === 1 && opened[0].params.team === "T2" && opened[0].params.conv === "C1",
          "Enter did not open the row in the TUI: " + JSON.stringify(root.service.calls))
    check(root.closes === 1, "opening did not ask to close the popup")

    // DND: up from the first row reaches the header; a snooze covers every workspace.
    root.service.reset()
    panel.moveCursor(0, -1)
    panel.moveCursor(0, -1)
    check(panel.cursor.section === "dnd", "up from the first row did not reach the DND header")
    panel.moveCursor(1, 0) // past the scope button, onto "30 min"
    panel.activateCursor()
    var snoozes = callsOf("dnd.snooze")
    check(snoozes.length === 2 && snoozes[0].params.minutes === 30, "DND did not snooze both workspaces for 30 min")

    // Empty state.
    root.service.unreadList = []
    check(panel.rows.length === 0 && panel.empty, "no unread conversations is not the empty state")
    check(panel.implicitHeight > 0, "the empty panel has no height")

    // The daemon does not answer and the probe says what to do: the panel
    // says it and offers the action (lib/install.js); Enter runs it.
    var s = root.service
    s.connectionState = "unavailable"
    s.setupNeed = "install"
    var setupButton = child(panel, "setupButton")
    var setupHint = child(panel, "setupHint")
    check(setupButton && setupButton.visible && setupButton.text === "Install omaslack-bin",
          "no install button: " + (setupButton ? setupButton.visible + " " + setupButton.text : "missing"))
    check(setupHint && setupHint.visible && setupHint.text.indexOf("omaslack-bin") >= 0, "no install hint")
    check(!child(panel, "dndFlow").visible, "the DND buttons show without a daemon")
    s.reset()
    setupButton.clicked()
    check(callsOf("runSetupAction").length === 1, "the install button did not run the action")
    s.reset()
    panel.activateCursor()
    var ran = callsOf("runSetupAction")
    check(ran.length === 1 && ran[0].params.need === "install", "Enter did not run the action: " + JSON.stringify(s.calls))

    // A need without a command only explains.
    s.setupNeed = "update-plugin"
    check(!setupButton.visible, "update-plugin shows a button")
    check(setupHint.text.indexOf("omarchy plugin update") >= 0, "update-plugin hint: " + setupHint.text)
    s.reset()
    panel.activateCursor()
    check(s.calls.length === 0, "Enter ran something without an action")

    s.setupNeed = ""
    s.connectionState = "connected"
    check(!setupHint.visible && child(panel, "dndFlow").visible, "the setup block outlives the connection")
    return failures
  }
}
