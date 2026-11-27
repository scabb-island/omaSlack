import QtQuick
import "../.." as Plugin

// The full window against FakeService: the shell lifecycle, the sidebar
// sections, opening a conversation (conversations.open, draft restored),
// sending from the composer and from a thread (messages.send with the
// right threadTS), drafts, older history at the top with the reading
// position kept, links, ui.focus, ui.viewing and Esc stepping back.
Item {
  id: root
  property var service: null

  Plugin.Window { id: win; service: root.service }

  readonly property string rootTS: "1700009000.000100"

  function callsOf(method) { return root.service.calls.filter(function(c) { return c.method === method }) }
  function last(method) { var c = callsOf(method); return c.length ? c[c.length - 1].params : null }

  // n messages, one a minute, from start (seconds), alternating authors.
  function page(start, n) {
    var out = []
    for (var i = 0; i < n; i++)
      out.push({ ts: (start + i * 60) + ".000100", author: i % 2 ? "U2" : "U1", text: "message " + (start + i * 60) })
    return out
  }

  function load() {
    var s = root.service
    s.workspaces = [{ team: "T1", name: "Acme", self: "U1", state: "connected" }]
    s.conversations = {
      T1: [
        { id: "C1", kind: "channel", name: "general", unread: true, mentions: 1, latest: rootTS },
        { id: "C2", kind: "channel", name: "random" },
        { id: "D1", kind: "im", name: "Bo", user: "U2" }
      ]
    }
    s.userNames = { "T1/U1": "Ana", "T1/U2": "Bo" }
    var history = page(1700000000, 60)
    history.push({ ts: rootTS, author: "U2", text: "a *root*", html: "a <b>root</b>", replyCount: 2,
                   edited: "1700000200.000000", reactions: [{ emoji: "tada", users: ["U1"] }],
                   files: [{ id: "F1", name: "plan.pdf", size: 2048 }] })
    var lists = {}
    lists["T1/C1"] = history
    lists["T1/C1/" + rootTS] = [history[history.length - 1], { ts: "1700009500.000100", threadTS: rootTS, author: "U1", text: "a reply" }]
    s.messageLists = lists
    s.answers = {
      "conversations.open": { error: null, result: { messages: [], draft: "half written" } },
      "drafts.get": { error: null, result: { text: "" } }
    }
  }

  function run() {
    var failures = []
    function check(ok, message) { if (!ok) failures.push(message) }
    var s = root.service
    var saved = { workspaces: s.workspaces, conversations: s.conversations, userNames: s.userNames,
                  messageLists: s.messageLists, answers: s.answers, moreHistory: s.moreHistory }
    load()

    // Lifecycle.
    win.open("")
    check(win.opened, "open() did not show the window")

    // Sidebar: the workspace, then its sections.
    var keys = win.sidebarRows.map(function(r) { return r.key })
    check(JSON.stringify(keys) === JSON.stringify(["T1", "T1#Unread", "T1/C1", "T1#Channels", "T1/C2", "T1#Direct messages", "T1/D1"]),
          "sidebar rows: " + JSON.stringify(keys))
    check(win.sidebarView.count === keys.length, "the sidebar lists " + win.sidebarView.count + " rows")

    // Keyboard: the cursor starts on the first conversation, Enter opens it.
    s.reset()
    win.pane = "sidebar"
    check(win.sidebarIndex === 2, "the cursor did not start on the first conversation: " + win.sidebarIndex)
    win.activate()
    var opened = last("conversations.open")
    check(opened && opened.team === "T1" && opened.conv === "C1", "Enter did not call conversations.open on C1")
    check(win.pane === "messages", "opening did not move to the messages")
    var conv = win.conversationView
    check(conv.composer.text === "half written", "the draft was not restored: " + conv.composer.text)
    // ui.viewing (sent a moment later; flushed here): the open conversation.
    win.reportViewing()
    var viewing = last("ui.viewing")
    check(viewing && viewing.team === "T1" && viewing.conv === "C1" && typeof viewing.focused === "boolean" && !viewing.threadTS,
          "ui.viewing for C1: " + JSON.stringify(viewing))
    var views = callsOf("ui.viewing").length
    win.reportViewing()
    check(callsOf("ui.viewing").length === views, "the same view was reported again")
    check(conv.list.count === 61, "the list shows " + conv.list.count + " messages")
    check(conv.title === "general", "the header says " + conv.title)

    // Composer: Shift+Enter is a new line; Enter sends to the conversation.
    s.reset()
    conv.composer.focusInput()
    conv.composer.handleKey(Qt.Key_Return, Qt.ShiftModifier)
    check(callsOf("messages.send").length === 0, "Shift+Enter sent")
    check(conv.composer.text === "half written\n", "Shift+Enter did not add a line: " + JSON.stringify(conv.composer.text))
    conv.composer.input.text = "hello"
    conv.composer.handleKey(Qt.Key_Return, 0)
    var sent = last("messages.send")
    check(sent && sent.conv === "C1" && sent.text === "hello" && sent.threadTS === undefined,
          "Enter sent " + JSON.stringify(sent))
    check(conv.composer.text === "", "the composer kept the sent text")
    check(callsOf("drafts.set").length === 1 && last("drafts.set").text === "", "the stored draft was not cleared")

    // Drafts: typing saves the draft (debounced; flushed here).
    s.reset()
    conv.composer.input.text = "later"
    conv.composer.flush()
    var draft = last("drafts.set")
    check(draft && draft.conv === "C1" && draft.text === "later", "the draft was not saved: " + JSON.stringify(draft))
    conv.composer.handleKey(Qt.Key_Escape, 0)
    win.focusKeys()

    // A failed send keeps the text and says why.
    s.answers = Object.assign({}, s.answers, { "messages.send": { error: { code: 1, message: "nope" }, result: null } })
    conv.composer.handleKey(Qt.Key_Return, 0)
    check(conv.composer.text === "later" && win.status.indexOf("nope") >= 0, "a failed send lost the text or the error")
    var a = Object.assign({}, s.answers)
    delete a["messages.send"]
    s.answers = a
    conv.composer.input.text = ""
    conv.composer.flush()

    // The thread: k from the newest selects the root, Enter opens its thread.
    s.reset()
    win.moveVertical(-1)
    check(conv.list.currentMessage() && conv.list.currentMessage().ts === rootTS, "k did not select the newest message")
    win.activate()
    var replies = last("messages.replies")
    check(replies && replies.threadTS === rootTS, "Enter did not load the thread: " + JSON.stringify(replies))
    check(callsOf("drafts.get").length === 1, "the thread's draft was not asked for")
    check(win.threadOpen && win.pane === "thread", "the thread pane did not open")
    var thread = win.threadView
    check(thread.list.count === 2, "the thread shows " + thread.list.count + " messages")
    win.reportViewing()
    check(last("ui.viewing") && last("ui.viewing").threadTS === rootTS, "ui.viewing for the thread: " + JSON.stringify(last("ui.viewing")))

    // Enter in the thread focuses its composer; Enter there replies in the thread.
    win.activate()
    check(thread.composer.editing || thread.composer.input.activeFocus || win.pane === "thread", "Enter did not go to the thread composer")
    thread.composer.input.text = "on it"
    thread.composer.handleKey(Qt.Key_Return, 0)
    var reply = last("messages.send")
    check(reply && reply.threadTS === rootTS && reply.text === "on it", "the reply went to " + JSON.stringify(reply))

    // Esc: out of the composer, then the thread closes.
    thread.composer.handleKey(Qt.Key_Escape, 0)
    check(win.threadOpen, "Esc in the composer closed the thread")
    win.stepBack()
    check(!win.threadOpen && win.pane === "messages", "Esc did not close the thread")

    // Older history: up past the oldest message asks for it, and the
    // message that was on screen stays where it was.
    s.reset()
    s.moreHistory = { "T1/C1": true }
    conv.list.toTop()
    check(callsOf("messages.history").length === 1 && last("messages.history").conv === "C1",
          "the top did not load older history: " + JSON.stringify(s.calls))
    var list = conv.list
    list.loadOlder()
    check(callsOf("messages.history").length === 1, "the same page was asked for twice")
    var before = list.indexAt(list.width / 2, list.contentY + 1)
    var anchorKey = before >= 0 ? list.keys()[before] : ""
    var anchorY = before >= 0 ? list.itemAtIndex(before).y - list.contentY : 0
    var lists = Object.assign({}, s.messageLists)
    lists["T1/C1"] = page(1690000000, 50).concat(lists["T1/C1"])
    s.messageLists = lists
    list.forceLayout()
    check(list.count === 111, "the older page did not arrive: " + list.count)
    var now = list.keys().indexOf(anchorKey)
    var item = now >= 0 ? list.itemAtIndex(now) : null
    check(anchorKey !== "" && item && Math.abs(item.y - list.contentY - anchorY) < 1,
          "the reading position moved: " + anchorKey + " " + (item ? item.y - list.contentY : "gone") + " vs " + anchorY)
    s.reset()
    list.toTop()
    check(callsOf("messages.history").length === 1 && last("messages.history").conv === "C1",
          "the new top did not ask for the next page")

    // Links: a channel link opens it; another conversation closes this one.
    s.reset()
    win.openLink("omaslack://conv/T1/C2")
    check(last("conversations.open") && last("conversations.open").conv === "C2", "a channel link did not open it")
    check(last("conversations.close") && last("conversations.close").conv === "C1", "C1 was not closed in the daemon")

    // Summon with a payload, and ui.focus, open the conversation asked for.
    s.reset()
    win.open(JSON.stringify({ team: "T1", conv: "C1", ts: rootTS }))
    check(win.conv === "C1" && conv.list.currentMessage() && conv.list.currentMessage().ts === rootTS,
          "the payload did not open C1 at its ts")
    s.focusRequested("T1", "D1", "")
    check(win.conv === "D1", "ui.focus did not open D1")

    // Esc with nothing open dismisses the window; it closes the conversation.
    s.reset()
    win.focusKeys()
    win.stepBack()
    check(!win.opened, "Esc did not dismiss the window")
    check(last("conversations.close") && last("conversations.close").conv === "D1", "closing the window left D1 open")
    win.reportViewing()
    check(last("ui.viewing") && JSON.stringify(last("ui.viewing")) === "{}", "closing did not clear ui.viewing: " + JSON.stringify(last("ui.viewing")))

    for (var k in saved) s[k] = saved[k]
    return failures
  }
}
