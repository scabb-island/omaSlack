import QtQuick
import "../.." as Plugin

// The window's navigation against FakeService: the finder opens a
// conversation of another workspace, search runs search.messages and
// opening a hit (a message, then a reply) selects it with its ts, 1–9
// switch workspaces, ? shows the help, and @ autocomplete asks
// users.search and sends the picked person as <@U…>.
Item {
  id: root
  property var service: null

  Plugin.Window { id: win; service: root.service }

  function callsOf(method) { return root.service.calls.filter(function(c) { return c.method === method }) }
  function last(method) { var c = callsOf(method); return c.length ? c[c.length - 1].params : null }
  function key(k, modifiers) { return { key: k, modifiers: modifiers || 0, accepted: false } }

  readonly property string hitTS: "1700000060.000100"
  readonly property string replyTS: "1700000500.000100"

  function load() {
    var s = root.service
    s.workspaces = [
      { team: "T1", name: "Acme", self: "U1", state: "connected" },
      { team: "T2", name: "Side", self: "U1", state: "connected" }
    ]
    s.conversations = {
      T1: [{ id: "C1", kind: "channel", name: "general" }, { id: "D1", kind: "im", name: "Bo", user: "U2" }],
      T2: [{ id: "C8", kind: "private", name: "ops", unread: true }, { id: "C9", kind: "channel", name: "random" }]
    }
    s.userNames = { "T1/U1": "Ana", "T1/U2": "Bo", "T2/U1": "Ana" }
    var lists = {}
    lists["T2/C9"] = [
      { ts: "1700000000.000100", author: "U1", text: "first" },
      { ts: hitTS, author: "U1", text: "the hit", replyCount: 1 },
      { ts: "1700000120.000100", author: "U1", text: "last" }
    ]
    lists["T2/C9/" + hitTS] = [lists["T2/C9"][1], { ts: replyTS, threadTS: hitTS, author: "U1", text: "a reply" }]
    lists["T2/C8"] = [{ ts: "1700000000.000100", author: "U1", text: "ops" }]
    s.messageLists = lists
    s.answers = {
      "conversations.open": { error: null, result: { messages: [] } },
      "drafts.get": { error: null, result: { text: "" } },
      "search.messages": { error: null, result: [
        { conv: "C9", message: { ts: hitTS, author: "U1", text: "the hit", html: "the <b>hit</b>" } },
        { conv: "C9", message: { ts: replyTS, threadTS: hitTS, author: "U1", text: "a reply" } }
      ] },
      "users.search": { error: null, result: [{ id: "U7", name: "ana", realName: "Ana Diaz", displayName: "Ana" }] }
    }
  }

  function run() {
    var failures = []
    function check(ok, message) { if (!ok) failures.push(message) }
    var s = root.service
    var saved = { workspaces: s.workspaces, conversations: s.conversations, userNames: s.userNames,
                  messageLists: s.messageLists, answers: s.answers, moreHistory: s.moreHistory }
    load()
    win.open("")

    // Finder (Ctrl+K): a conversation of the second workspace.
    s.reset()
    win.textKey("\u000b", Qt.ControlModifier)
    var finder = win.finderView
    check(finder.visible && win.navOpen, "Ctrl+K did not open the finder")
    check(finder.rows.length === 4, "the finder lists " + finder.rows.length + " conversations")
    finder.field.text = "ops"
    check(finder.rows.length === 1 && finder.rows[0].team === "T2" && finder.rows[0].title === "~ops",
          "the finder ranked " + JSON.stringify(finder.rows))
    finder.entered()
    var opened = last("conversations.open")
    check(opened && opened.team === "T2" && opened.conv === "C8", "the finder opened " + JSON.stringify(opened))
    check(!finder.visible && !win.navOpen, "the finder stayed open")
    check(win.team === "T2" && win.conv === "C8", "the window shows " + win.team + "/" + win.conv)

    // Search (/): search.messages in the open conversation's workspace.
    s.reset()
    win.textKey("/", 0)
    var search = win.searchView
    check(search.visible && search.team === "T2", "/ did not open the search on T2")
    search.field.text = "hit"
    search.entered()
    var asked = last("search.messages")
    check(asked && asked.team === "T2" && asked.query === "hit", "search asked " + JSON.stringify(asked))
    check(search.rows.length === 2 && search.rows[0].where === "#random" && search.rows[0].who === "Ana",
          "search shows " + JSON.stringify(search.rows))
    search.entered()
    opened = last("conversations.open")
    check(opened && opened.team === "T2" && opened.conv === "C9", "the hit opened " + JSON.stringify(opened))
    var cur = win.conversationView.list.currentMessage()
    check(cur && cur.ts === hitTS, "the hit is not selected: " + JSON.stringify(cur))
    check(!search.visible, "the search stayed open")

    // A reply: its thread opens on it.
    s.reset()
    win.textKey("/", 0)
    check(search.field.text === "hit" && search.rows.length === 2, "the search forgot its last query")
    search.move(1)
    search.entered()
    check(win.threadOpen && win.threadTS === hitTS, "the reply did not open its thread")
    var reply = win.threadView.list.currentMessage()
    check(reply && reply.ts === replyTS, "the reply is not selected: " + JSON.stringify(reply))
    win.closeThread()

    // A hit older than the cache: older pages are asked for, up to
    // seekPages, then the window says it could not get there.
    s.reset()
    s.moreHistory = { "T2/C9": true }
    win.openHit("T2", { conv: "C9", ts: "1600000000.000100", root: "1600000000.000100" })
    check(callsOf("messages.history").length === win.seekPages, "the seek asked " + callsOf("messages.history").length + " pages")
    check(win.status === "Could not reach that message in the history.", "the seek said " + JSON.stringify(win.status))
    s.moreHistory = {}

    // 1–9: the first conversation of that workspace in the sidebar.
    win.textKey("1", 0)
    var row = win.sidebarRows[win.sidebarIndex]
    check(win.pane === "sidebar" && row && row.team === "T1" && row.type === "conv", "1 went to " + JSON.stringify(row))
    win.textKey("2", 0)
    row = win.sidebarRows[win.sidebarIndex]
    check(row && row.team === "T2", "2 went to " + JSON.stringify(row))
    win.textKey("3", 0)
    check(win.status === "There is no workspace 3.", "3 said " + win.status)

    // ?: the help lists the keys; any key closes it.
    win.textKey("?", 0)
    check(win.helpView.visible && win.navOpen, "? did not show the help")
    win.helpView.hide()
    check(!win.helpView.visible, "the help did not close")

    // @ autocomplete: users.search, Tab picks, the send carries <@U7>.
    s.reset()
    win.selectConversation("T2", "C9", "")
    var composer = win.conversationView.composer
    var complete = win.conversationAutocomplete
    composer.focusInput()
    composer.input.text = "hi @an"
    composer.input.cursorPosition = composer.input.length
    complete.refresh()
    complete.searchPeople()
    var people = last("users.search")
    check(people && people.team === "T2" && people.q === "an", "users.search got " + JSON.stringify(people))
    check(complete.open && complete.items.length > 0 && complete.items[0].text === "@Ana", "the popup shows " + JSON.stringify(complete.items))
    check(complete.keyFilter(key(Qt.Key_Tab)), "Tab was not taken by the popup")
    check(composer.text === "hi @Ana ", "the composer has " + JSON.stringify(composer.text))
    complete.refresh()
    check(!complete.open, "the popup stayed open after the pick")
    composer.handleKey(Qt.Key_Return, 0)
    var sent = last("messages.send")
    check(sent && sent.text === "hi <@U7>", "sent " + JSON.stringify(sent))

    // # and : complete from what the window holds; Esc dismisses.
    composer.input.text = "see #ra"
    composer.input.cursorPosition = composer.input.length
    complete.refresh()
    check(complete.open && complete.items[0].text === "#random", "# offers " + JSON.stringify(complete.items))
    check(complete.keyFilter(key(Qt.Key_Escape)), "Esc was not taken by the popup")
    composer.input.text = "see #ran"
    composer.input.cursorPosition = composer.input.length
    complete.refresh()
    check(!complete.open, "the popup came back on the dismissed word")
    // Leaving the word (here: clearing the field) forgets the dismissal.
    composer.input.text = ""
    complete.refresh()
    composer.input.text = "yay :tad"
    composer.input.cursorPosition = composer.input.length
    complete.refresh()
    check(callsOf("emoji.list").length === 1, "emoji.list was not asked")
    check(complete.open && complete.items[0].text === ":tada:", ": offers " + JSON.stringify(complete.items))
    composer.input.text = ""
    composer.flush()

    win.close()
    for (var k in saved) s[k] = saved[k]
    return failures
  }
}
