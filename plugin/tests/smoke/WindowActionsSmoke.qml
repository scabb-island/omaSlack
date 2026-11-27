import QtQuick
import "../.." as Plugin

// The window's message actions against FakeService: e edits your own
// message (messages.edit), D and x delete it only after y, r reacts
// through the picker and reaction chips toggle (reactions.add/remove),
// o opens a message's file (files.fetch, then the opener; a chooser with
// several), image files get thumbnails, and ":attach" in a composer
// uploads (files.upload).
Item {
  id: root
  property var service: null

  Plugin.Window { id: win; service: root.service }

  readonly property string theirsTS: "1700000060.000100"
  readonly property string mineTS: "1700000120.000100"
  readonly property string filesTS: "1700000180.000100"
  readonly property string imageTS: "1700000240.000100"

  property var opened: []

  function callsOf(method) { return root.service.calls.filter(function(c) { return c.method === method }) }
  function last(method) { var c = callsOf(method); return c.length ? c[c.length - 1].params : null }

  function load() {
    var s = root.service
    s.workspaces = [{ team: "T1", name: "Acme", self: "U1", state: "connected", capabilities: ["files", "customEmoji"] }]
    s.conversations = { T1: [{ id: "C1", kind: "channel", name: "general" }] }
    s.userNames = { "T1/U1": "Ana", "T1/U2": "Bo" }
    var lists = {}
    lists["T1/C1"] = [
      { ts: theirsTS, author: "U2", text: "theirs", reactions: [{ emoji: "tada", users: ["U1"] }, { emoji: "eyes", users: ["U2"] }] },
      { ts: mineTS, author: "U1", text: "mine &lt;3", replyCount: 1 },
      { ts: filesTS, author: "U2", text: "two files", files: [{ id: "F1", name: "a.pdf", size: 10 }, { id: "F2", name: "b.pdf", size: 20 }] },
      { ts: imageTS, author: "U2", text: "", files: [{ id: "F3", name: "cat.png", mimetype: "image/png", size: 30 }] }
    ]
    lists["T1/C1/" + mineTS] = [lists["T1/C1"][1]]
    s.messageLists = lists
    s.answers = {
      "conversations.open": { error: null, result: { messages: [], draft: "my draft" } },
      "drafts.get": { error: null, result: { text: "" } },
      "files.fetch": { error: null, result: { localPath: "/cache/omaslack/file 1.png" } },
      "emoji.list": { error: null, result: { emoji: { partyparrot: "https://x/p.gif" } } }
    }
  }

  // select puts the conversation's cursor on ts.
  function select(ts) {
    win.pane = "messages"
    var list = win.conversationView.list
    list.currentIndex = list.keys().indexOf(ts)
  }

  function run() {
    var failures = []
    function check(ok, message) { if (!ok) failures.push(message) }
    var s = root.service
    var saved = { workspaces: s.workspaces, conversations: s.conversations, userNames: s.userNames,
                  messageLists: s.messageLists, answers: s.answers }
    load()
    win.open("")
    win.selectConversation("T1", "C1", "")
    var actions = win.messageActions
    actions.opener = function(url) { root.opened = root.opened.concat([url]) }
    var composer = win.conversationView.composer
    check(composer.text === "my draft", "the draft did not load: " + composer.text)

    // ---- edit --------------------------------------------------------------------

    // Someone else's message: refused, the composer untouched.
    s.reset()
    select(theirsTS)
    win.textKey("e", 0)
    check(composer.editTS === "" && win.status.indexOf("your own") >= 0, "e on another's message: " + composer.editTS + " / " + win.status)

    // Your own: the composer edits its text; Enter saves it with messages.edit.
    select(mineTS)
    win.textKey("e", 0)
    check(composer.editTS === mineTS, "e did not start an edit")
    check(composer.text === "mine <3", "the edit did not start from the text: " + composer.text)
    composer.input.text = "mine, edited"
    composer.flush()
    check(callsOf("drafts.set").length === 0, "the edit was saved as the draft")
    composer.handleKey(Qt.Key_Return, 0)
    var edited = last("messages.edit")
    check(edited && edited.ts === mineTS && edited.conv === "C1" && edited.text === "mine, edited", "Enter edited " + JSON.stringify(edited))
    check(callsOf("messages.send").length === 0, "the edit was also sent as a message")
    check(composer.editTS === "" && composer.text === "my draft", "after the edit the draft did not come back: " + composer.text)

    // Esc cancels an edit and gives the draft back.
    s.reset()
    win.focusKeys()
    win.textKey("e", 0)
    composer.handleKey(Qt.Key_Escape, 0)
    check(composer.editTS === "" && composer.text === "my draft", "Esc did not cancel the edit")
    check(callsOf("messages.edit").length === 0, "Esc saved the edit")
    win.focusKeys()

    // ---- delete ------------------------------------------------------------------

    s.reset()
    select(theirsTS)
    win.textKey("D", 0)
    check(!actions.confirm.opened && win.status.indexOf("your own") >= 0, "D on another's message asked to delete it")
    select(mineTS)
    win.textKey("D", 0)
    check(actions.confirm.opened && actions.active, "D did not ask to confirm")
    check(callsOf("messages.delete").length === 0, "D deleted before the answer")
    actions.confirm.handleKey(Qt.Key_N, "n")
    check(!actions.confirm.opened && callsOf("messages.delete").length === 0, "n deleted the message")
    actions.askDelete() // x and Delete
    check(actions.confirm.opened, "x did not ask to confirm")
    actions.confirm.handleKey(Qt.Key_Y, "y")
    var deleted = last("messages.delete")
    check(deleted && deleted.ts === mineTS && deleted.conv === "C1", "y deleted " + JSON.stringify(deleted))
    check(!actions.confirm.opened, "the confirmation stayed open")

    // ---- reactions ---------------------------------------------------------------

    s.reset()
    select(mineTS)
    win.textKey("r", 0)
    var picker = actions.picker
    check(picker.opened && actions.active, "r did not open the picker")
    check(callsOf("emoji.list").length === 1 && last("emoji.list").team === "T1", "the custom emoji were not asked for")
    check(picker.names.indexOf("partyparrot") >= 0, "the custom emoji are not listed")
    picker.query = "fire"
    check(picker.results[0] === "fire", "the filter shows " + JSON.stringify(picker.results.slice(0, 3)))
    picker.handleKey(Qt.Key_Return, "")
    var added = last("reactions.add")
    check(added && added.ts === mineTS && added.emoji === "fire", "Enter reacted " + JSON.stringify(added))
    check(!picker.opened, "the picker stayed open")
    win.textKey("r", 0)
    check(picker.query === "" && picker.results[0] === "fire", "the recent emoji is not first: " + JSON.stringify(picker.results.slice(0, 3)))
    picker.handleKey(Qt.Key_Escape, "")
    check(!picker.opened && callsOf("reactions.add").length === 1, "Esc reacted or kept the picker")
    win.textKey("r", 0)
    picker.query = ":customthing:"
    picker.handleKey(Qt.Key_Return, "")
    check(last("reactions.add").emoji === "customthing", "a typed name was not sent")
    check(callsOf("emoji.list").length === 1, "the custom emoji were asked for twice")

    // Chips toggle: yours is removed, someone else's gets yours added.
    s.reset()
    var theirs = s.messageLists["T1/C1"][0]
    win.conversationView.reactionClicked(theirs, "tada")
    check(last("reactions.remove") && last("reactions.remove").emoji === "tada", "your reaction was not removed")
    win.conversationView.reactionClicked(theirs, "eyes")
    check(last("reactions.add") && last("reactions.add").emoji === "eyes" && last("reactions.add").ts === theirsTS, "a reaction was not added")

    // ---- files -------------------------------------------------------------------

    s.reset()
    root.opened = []
    select(mineTS)
    win.textKey("o", 0)
    check(win.status.indexOf("no files") >= 0 && callsOf("files.fetch").length === 0, "o on a message without files: " + win.status)
    select(imageTS)
    win.textKey("o", 0)
    // The thumbnail already fetched F3: o opens it from the cache.
    check(root.opened[0] === "file:///cache/omaslack/file%201.png", "o opened " + JSON.stringify(root.opened))
    s.reset()
    select(filesTS)
    win.textKey("o", 0)
    check(actions.chooser.opened && actions.chooser.files.length === 2, "o with two files did not offer a choice")
    actions.chooser.handleKey(Qt.Key_2, "2")
    check(last("files.fetch") && last("files.fetch").fileId === "F2", "2 fetched " + JSON.stringify(last("files.fetch")))
    check(win.info === "Opening b.pdf…", "opening a file says " + JSON.stringify(win.info))
    check(!actions.chooser.opened, "the chooser stayed open")
    win.conversationView.fileClicked(s.messageLists["T1/C1"][2], { id: "F1", name: "a.pdf" })
    check(last("files.fetch").fileId === "F1", "clicking a file did not open it")

    // A thumbnail for the image file.
    var cat = findThumb(win.conversationView.list, "F3")
    check(cat !== null, "the image has no thumbnail")
    check(cat && cat.path === "/cache/omaslack/file 1.png" && String(cat.url).indexOf("file:///cache/") === 0,
          "the thumbnail shows " + (cat ? cat.path : "nothing"))
    check(findThumb(win.conversationView.list, "F1") === null, "a PDF got a thumbnail")

    // ---- upload ------------------------------------------------------------------

    s.reset()
    composer.focusInput()
    composer.input.text = ":attach \"/tmp/my cat.png\" look"
    composer.handleKey(Qt.Key_Return, 0)
    var up = last("files.upload")
    check(up && up.team === "T1" && up.conv === "C1" && JSON.stringify(up.paths) === JSON.stringify(["/tmp/my cat.png"]) && up.caption === "look" && up.threadTS === undefined,
          ":attach uploaded " + JSON.stringify(up))
    check(callsOf("messages.send").length === 0, ":attach was sent as a message")
    check(composer.text === "", "the composer kept the command")
    check(win.info === "Uploaded my cat.png.", "the upload says " + JSON.stringify(win.info))
    composer.input.text = ":attach notes.txt"
    composer.handleKey(Qt.Key_Return, 0)
    check(callsOf("files.upload").length === 1 && win.status.indexOf("absolute") >= 0, "a relative path: " + win.status)

    // In a thread, the upload goes to the thread.
    s.reset()
    win.focusKeys()
    win.openThread(mineTS)
    var tc = win.threadView.composer
    tc.input.text = ":attach /tmp/a.txt"
    tc.handleKey(Qt.Key_Return, 0)
    check(last("files.upload") && last("files.upload").threadTS === mineTS, "the thread's upload went to " + JSON.stringify(last("files.upload")))

    win.close()
    for (var k in saved) s[k] = saved[k]
    return failures
  }

  // findThumb: the FileThumb of file id under item, or null.
  function findThumb(item, id) {
    if (item.objectName === "thumb:" + id) return item
    var kids = item.children || []
    for (var i = 0; i < kids.length; i++) {
      var f = findThumb(kids[i], id)
      if (f) return f
    }
    return null
  }
}
