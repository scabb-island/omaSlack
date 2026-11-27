import QtQuick
import Quickshell
import qs.Commons
import qs.Commons as Commons
import "lib/actions.js" as A
import "lib/filecache.js" as Files
import "lib/nav.js" as N

// The window's message actions (task 4.5), with the TUI's keys: e edits
// your own selected message in its pane's composer (people and channels
// by name, saved back as mentions, as a composed message is), D (and x, Delete)
// deletes it after an inline y/n, r opens the reaction picker, o opens
// the message's file (a chooser when it has several), reaction chips and
// files react to clicks, and ":attach <path> [caption]" sent from a
// composer uploads to its conversation or thread. The rules live in
// lib/actions.js (tests/tst_actions.qml); this turns them into Service
// calls and draws the overlays. Window.qml routes keys here (textKey,
// deleteRequested), and composer submits through submit().
//
// The reaction picker lists the common standard emoji of lib/actions.js
// and the workspace's custom ones (emoji.list, asked once per workspace
// when the workspace has the customEmoji capability); any other name can
// be typed.
Item {
  id: root

  // Window.qml: its state (team, conv, pane), currentMessage(),
  // paneComposer(), selfUser(), workspace(), status and focusKeys().
  property var window: null
  property var service: null
  property color foreground: Commons.Color.foreground
  property string fontFamily: Style.font.family
  // Opens a URL in the desktop's default app; tests replace it.
  property var opener: function(url) { Qt.openUrlExternally(url) }

  // Whether an overlay has the keyboard (the window's keys wait).
  readonly property bool active: pickerView.opened || chooserView.opened || confirmView.opened
  readonly property alias picker: pickerView
  readonly property alias chooser: chooserView
  readonly property alias confirm: confirmView

  // This session's reactions, most recent first.
  property var recentEmoji: []
  // { <team>: { <name>: url | "alias:<name>" } } from emoji.list.
  property var customEmoji: ({})
  // The message the picker or the confirmation is for: { team, conv, ts, pane }.
  property var pending: null

  function self() { return window.selfUser(window.team) }
  function say(text) { window.status = text }
  function failed(what) { return function(err) { root.say(err ? what + ": " + (err.message || err.code) : "") } }

  function selected() {
    var m = window.currentMessage()
    return m ? { team: window.team, conv: window.conv, ts: m.ts, pane: window.pane, message: m } : null
  }

  // textKey takes the action keys; false for any other key.
  function textKey(t) {
    if (t === "e") startEdit()
    else if (t === "D") askDelete()
    else if (t === "r") openPicker()
    else if (t === "o") openFile()
    else return false
    return true
  }

  function done() {
    pending = null
    window.focusKeys()
  }

  // ---- edit ------------------------------------------------------------------------

  function startEdit() {
    var m = window.currentMessage()
    var why = A.ownMessage(m, self(), "edit")
    say(why)
    if (why === "") window.paneComposer(window.pane).startEdit(m.ts, displayText(m.text))
  }

  // displayText: a message's text as the composer shows it (lib/nav.js
  // toDisplay), its mentions added to the window's tokens so the save
  // sends them back as mentions.
  function displayText(text) {
    var team = window.team
    var shown = N.toDisplay(String(text || ""),
      function(id) { var n = service.userName(team, id); return n !== id ? n : "" },
      function(id) { var c = window.conversation(team, id); return c && c.name ? c.name : "" })
    for (var k in shown.tokens) window.addMentionToken(k, shown.tokens[k])
    return shown.text
  }

  // submit handles what a composer sends when it is not a plain message:
  // an edit, or :attach. true when it took the text.
  function submit(composer, text) {
    if (composer.editTS !== "") { saveEdit(composer, text); return true }
    var attach = A.parseAttach(text, String(Quickshell.env("HOME") || ""))
    if (!attach) return false
    upload(composer, attach)
    return true
  }

  function saveEdit(composer, text) {
    var t = composer.target
    var ts = composer.editTS
    if (text.trim() === "") { say("An edit cannot be empty; to delete the message, press D."); return }
    service.edit(t.team, t.conv, ts, N.toWire(text.trim(), window.mentionTokens), function(err) {
      if (err) { root.say("Could not edit: " + (err.message || err.code)); return }
      root.say("")
      if (composer.editTS === ts) composer.cancelEdit()
      root.window.focusKeys()
    })
  }

  // ---- delete ----------------------------------------------------------------------

  function askDelete() {
    var s = selected()
    var why = A.ownMessage(s ? s.message : null, self(), "delete")
    say(why)
    if (why !== "") return
    pending = s
    confirmView.open()
  }

  function deleteConfirmed() {
    var p = pending
    done()
    if (p) service.remove(p.team, p.conv, p.ts, failed("Could not delete"))
  }

  // ---- reactions -------------------------------------------------------------------

  function openPicker() {
    var s = selected()
    if (!s) { say("Select a message to react to first."); return }
    say("")
    pending = s
    loadCustomEmoji(s.team)
    pickerView.names = A.emojiNames(customEmoji[s.team])
    pickerView.recent = recentEmoji
    pickerView.open()
  }

  function loadCustomEmoji(team) {
    if (customEmoji.hasOwnProperty(team) || !A.can(window.workspace(team), "customEmoji")) return
    var next = Object.assign({}, customEmoji)
    next[team] = ({})
    customEmoji = next
    service.call("emoji.list", { team: team }, function(err, res) {
      if (err || !res || !res.emoji) return
      var got = Object.assign({}, root.customEmoji)
      got[team] = res.emoji
      root.customEmoji = got
      if (pickerView.opened && root.pending && root.pending.team === team) pickerView.names = A.emojiNames(res.emoji)
    })
  }

  // The message as the list has it now (reactions may have changed since
  // the picker opened).
  function current(p) {
    var r = window.paneList(p.pane).rowByKey[p.ts]
    return r ? r.message : p.message
  }

  function picked(name) {
    var p = pending
    done()
    if (p) react(p.team, p.conv, current(p), name)
  }

  // toggleReaction: a click on a reaction chip of message.
  function toggleReaction(message, emoji) { react(window.team, window.conv, message, emoji) }

  function react(team, conv, message, emoji) {
    recentEmoji = A.rememberEmoji(recentEmoji, emoji)
    service.react(team, conv, message.ts, emoji, A.reactionAdds(message, emoji, window.selfUser(team)), failed("Could not react"))
  }

  // ---- files -----------------------------------------------------------------------

  function openFile() {
    var m = window.currentMessage()
    if (!m) { say("Select a message first."); return }
    var c = A.fileChoice(m.files)
    if (c.kind === "none") say("This message has no files.")
    else if (c.kind === "open") openOne(c.file)
    else { say(""); chooserView.open(c.files) }
  }

  // openOne downloads file through the daemon and opens the local copy.
  function openOne(file) {
    if (!A.can(window.workspace(window.team), "files")) { say("This workspace cannot download files (no files capability)."); return }
    var name = A.fileName(file)
    say("")
    Files.path(service, window.team, file.id, function(path) {
      if (path === "") root.say("Could not download " + name + ".")
      else root.opener(A.fileUrl(path))
    })
  }

  function upload(composer, attach) {
    var t = composer.target
    if (attach.error !== "") { say(attach.error); return }
    if (!t) { say("Open a conversation first."); return }
    if (!A.can(window.workspace(t.team), "files")) { say("This workspace cannot send files (no files capability)."); return }
    var params = { team: t.team, conv: t.conv, paths: [attach.path] }
    if (t.threadTS) params.threadTS = t.threadTS
    if (attach.caption !== "") params.caption = attach.caption
    say("")
    service.call("files.upload", params, function(err) {
      if (err) { root.say("Could not attach " + attach.path + ": " + (err.message || err.code)); return }
      composer.clear()
    })
  }

  // ---- overlays --------------------------------------------------------------------

  ReactionPicker {
    id: pickerView
    foreground: root.foreground
    fontFamily: root.fontFamily
    onPicked: function(name) { root.picked(name) }
    onOpenedChanged: if (!opened && root.pending) root.done()
  }

  FileChooser {
    id: chooserView
    foreground: root.foreground
    fontFamily: root.fontFamily
    onChosen: function(file) { root.openOne(file) }
    onOpenedChanged: if (!opened) root.window.focusKeys()
  }

  DeleteConfirm {
    id: confirmView
    anchors.left: parent.left
    anchors.right: parent.right
    anchors.bottom: parent.bottom
    foreground: root.foreground
    fontFamily: root.fontFamily
    onConfirmed: root.deleteConfirmed()
    onCanceled: root.done()
  }
}
