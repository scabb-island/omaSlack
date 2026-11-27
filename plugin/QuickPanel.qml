pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls
import qs.Commons
import qs.Commons as Commons
import qs.Ui
import "lib/quickpanel.js" as Lib

// The quick-view popup under the bar icon (task 4.4): conversations with
// unread messages, DMs and mentions first, each with a preview; a quick
// reply that sends and marks the conversation read; mark as read; DND; and
// opening a conversation in the window or the TUI.
//
// Previews come from messages.history (limit 3), which only reads the
// daemon's cache (and at most pages older history in): it never moves the
// read marker, unlike opening the conversation. They live here, in the
// panel, and load only for rows on screen, again when a row's latest
// message changes.
//
// Keys (PanelKeyCatcher): Up/Down or j/k move between rows, and up from the
// first row into the DND buttons (Left/Right or h/l across them); Enter or
// Space opens the row or presses the button; r or Tab replies to the row;
// x or Delete marks it read; d jumps to DND; Esc leaves the reply field,
// then closes the popup.
//
// The logic lives in lib/quickpanel.js (tests/tst_quickpanel.qml).
FocusScope {
  id: root

  property var service: null
  property QtObject bar: null
  // "window" or "tui": where picking a conversation opens it.
  property string openWith: "window"

  // The popup should close (after opening a conversation, or on Esc).
  signal closeRequested()

  // ---- state --------------------------------------------------------------

  readonly property var rows: service ? buildRows(service.summary, service.conversations) : []
  readonly property bool empty: rows.length === 0
  readonly property bool multiWorkspace: service ? service.workspaces.length > 1 : false

  // The cursor the user moved to, { section: "dnd" | "rows", index }, and
  // the key of its row, so it follows that row when the list reorders. The
  // cursor shown is that one clamped to what exists now, so an empty list
  // parks it on the header without forgetting that it belongs in the rows.
  property var wantedCursor: ({ section: "rows", index: 0 })
  property string cursorKey: ""
  readonly property var cursor: Lib.clamp(wantedCursor, dndButtons.length, rows.length)
  // The row whose quick reply is open ("" for none) and whether its field
  // has the keyboard.
  property string replyKey: ""
  property bool replyFocused: false
  // The last error of an action, shown under the header.
  property string status: ""

  // Previews: { <key>: { latest, lines } }, lines with author IDs, and the
  // fetches in flight { <key>: latest }.
  property var previews: ({})
  property var pending: ({})

  property int scopeIndex: 0
  readonly property var scopes: Lib.dndScopes(service ? service.workspaces : [])
  readonly property string scopeTeam: scopes[Math.min(scopeIndex, scopes.length - 1)].team
  readonly property var scopeTeams: Lib.dndTeams(scopeTeam, service ? service.workspaces : [])
  readonly property var dndState: Lib.dndStatus(service ? service.dnd : null, scopeTeams)
  // Rebuilt when the panel opens, so "Tomorrow" counts from now.
  property var choices: Lib.dndChoices(new Date())
  readonly property var dndButtons: Lib.dndButtons(scopes, Math.min(scopeIndex, scopes.length - 1), choices, dndState.snoozed)

  readonly property color foreground: Commons.Color.foreground
  readonly property color dim: Qt.darker(foreground, 1.4)
  readonly property string fontFamily: Style.font.family
  readonly property bool shown: !Window.window || Window.window.visible

  implicitWidth: Style.space(420)
  implicitHeight: header.implicitHeight + Style.spacing.panelGap + (empty ? emptyText.implicitHeight : rowColumn.implicitHeight)

  // summary and conversations are read so the binding re-runs when either
  // changes; unreadConversations() is derived from them.
  function buildRows(summary, conversations) {
    var s = root.service
    return Lib.rows(s.unreadConversations(), s.workspaces, function(team, conv) { return s.conversationName(team, conv) })
  }

  onRowsChanged: {
    var i = Lib.indexOfKey(rows, cursorKey)
    if (i >= 0) wantedCursor = { section: "rows", index: i }
    if (replyKey !== "" && Lib.indexOfKey(rows, replyKey) < 0) cancelReply()
    Qt.callLater(refreshPreviews)
  }
  onShownChanged: if (shown) { choices = Lib.dndChoices(new Date()); Qt.callLater(refreshPreviews) }
  onHeightChanged: Qt.callLater(refreshPreviews)

  // ---- cursor -----------------------------------------------------------------

  function setCursor(c) {
    wantedCursor = Lib.clamp(c, dndButtons.length, rows.length)
    cursorKey = cursor.section === "rows" && rows.length > 0 ? rows[cursor.index].key : ""
    scrollToCursor()
  }
  function moveCursor(dx, dy) { setCursor(Lib.move(cursor, dx, dy, dndButtons.length, rows.length)) }
  function cursorRow() { return cursor.section === "rows" && rows.length > 0 ? rows[cursor.index] : null }

  function activateCursor() {
    if (cursor.section === "dnd") pressDnd(dndButtons[cursor.index])
    else if (cursorRow()) openRow(cursorRow())
  }
  function markCursorRead() { if (cursorRow()) markRead(cursorRow()) }
  function replyToCursor() { if (cursorRow()) startReply(cursorRow()) }

  function scrollToCursor() {
    var item = cursor.section === "rows" ? repeater.itemAt(cursor.index) : null
    if (!item) return
    if (item.y < flick.contentY) flick.contentY = item.y
    else if (item.y + item.height > flick.contentY + flick.height)
      flick.contentY = item.y + item.height - flick.height
  }

  // ---- actions ------------------------------------------------------------------

  function openRow(row) {
    service.openPreferred(openWith, row.team, row.conv)
    closeRequested()
  }

  function markRead(row) {
    service.markRead(row.team, row.conv, "", function(err) { root.status = err ? "Could not mark as read: " + err.message : "" })
  }

  function startReply(row) {
    setCursor({ section: "rows", index: Lib.indexOfKey(rows, row.key) })
    replyKey = row.key
    var item = repeater.itemAt(cursor.index)
    if (item) Qt.callLater(item.focusReply)
  }

  function cancelReply() {
    replyKey = ""
    replyFocused = false
    catcher.forceActiveFocus()
  }

  // Send, then mark the conversation read; the row leaves the list when the
  // daemon's unread state follows.
  function sendReply(row, text) {
    if (!Lib.canSend(text)) return
    service.send(row.team, row.conv, text.trim(), "", function(err) {
      if (err) { root.status = "Could not send: " + err.message; return }
      root.status = ""
      bus.sent(row.key)
      if (root.replyKey === row.key) root.cancelReply()
      root.markRead(row)
    })
  }

  function pressDnd(button) {
    if (!button) return
    if (button.id === "scope") { scopeIndex = Lib.nextScope(scopeIndex, scopes.length); return }
    Lib.dndCalls(button, scopeTeams).forEach(function(c) {
      var done = function(err) { if (err) root.status = "Could not change DND: " + err.message }
      if (c.method === "snooze") root.service.snoozeDND(c.team, c.minutes, done)
      else root.service.endDND(c.team, done)
    })
  }

  // ---- previews -----------------------------------------------------------------

  function refreshPreviews() {
    if (!service || !shown) return
    rowColumn.forceLayout()
    for (var i = 0; i < rows.length; i++) {
      var item = repeater.itemAt(i)
      if (!item || !Lib.inView(item.y, item.height, flick.contentY, flick.height)) continue
      if (Lib.needsPreview(previews, pending, rows[i])) fetchPreview(rows[i])
    }
  }

  function fetchPreview(row) {
    var p = Object.assign({}, pending)
    p[row.key] = row.latest
    pending = p
    service.call("messages.history", { team: row.team, conv: row.conv, limit: 3 }, function(err, result) {
      var left = Object.assign({}, root.pending)
      delete left[row.key]
      root.pending = left
      // A failed fetch is cached empty too, so it is retried on the next
      // message rather than on every refresh.
      var next = Object.assign({}, root.previews)
      next[row.key] = { latest: row.latest, lines: err ? [] : Lib.preview(result, function(u) { return u }, 3, 140) }
      root.previews = next
    })
  }

  // The preview of a row with its authors named (names arrive later than
  // messages, so they are resolved on every read).
  function previewLines(key) {
    var hit = previews[key]
    var s = service
    if (!hit || !s) return []
    var team = key.split("/")[0]
    // Read so the binding re-runs when users.info answers.
    var revision = s.messagesRevision
    return revision < 0 ? [] : hit.lines.map(function(l) { return { author: l.author ? s.userName(team, l.author) : "", text: l.text } })
  }

  QtObject {
    id: bus
    signal sent(string key)
  }

  // ---- view ---------------------------------------------------------------------

  PanelKeyCatcher {
    id: catcher
    anchors.fill: parent
    focus: true
    blocked: root.replyFocused
    onMoveRequested: function(dx, dy) { root.moveCursor(dx, dy) }
    onActivateRequested: root.activateCursor()
    onDeleteRequested: root.markCursorRead()
    onTabRequested: root.replyToCursor()
    onCloseRequested: root.closeRequested()
    onTextKey: function(t) {
      if (t === "r" || t === "R") root.replyToCursor()
      else if (t === "d" || t === "D") root.setCursor({ section: "dnd", index: 0 })
    }

    Column {
      id: header
      width: parent.width
      spacing: Style.spacing.rowGap

      PanelHero {
        width: parent.width
        title: "Slack"
        meta: !root.service || !root.service.daemonAvailable
          ? "Daemon not running"
          : (root.empty ? "Nothing unread" : root.rows.length + " unread")
        detail: root.dndState.snoozed ? "DND until " + Lib.clockLabel(root.dndState.until) : ""
        foreground: root.foreground
        fontFamily: root.fontFamily
        iconComponent: Component {
          Text {
            textFormat: Text.PlainText
            text: "󰒱"
            color: root.foreground
            font.family: root.fontFamily
            font.pixelSize: Style.font.display
          }
        }
      }

      Text {
        visible: root.status !== ""
        width: parent.width
        textFormat: Text.PlainText
        text: root.status
        color: Commons.Color.urgent
        font.family: root.fontFamily
        font.pixelSize: Style.font.bodySmall
        wrapMode: Text.WordWrap
      }

      PanelSeparator { foreground: root.foreground }

      PanelSectionHeader {
        text: "Do not disturb"
        foreground: root.foreground
        fontFamily: root.fontFamily
      }

      Flow {
        width: parent.width
        spacing: Style.spacing.md

        Repeater {
          model: root.dndButtons
          Button {
            required property var modelData
            required property int index
            text: modelData.label
            bordered: true
            fontFamily: root.fontFamily
            foreground: root.foreground
            hasCursor: root.cursor.section === "dnd" && root.cursor.index === index
            onHovered: function(on) { if (on) root.setCursor({ section: "dnd", index: index }) }
            onClicked: root.pressDnd(modelData)
          }
        }
      }

      PanelSeparator { foreground: root.foreground }
    }

    Text {
      id: emptyText
      visible: root.empty
      anchors.top: header.bottom
      anchors.topMargin: Style.spacing.panelGap
      width: parent.width
      textFormat: Text.PlainText
      text: root.service && root.service.daemonAvailable
        ? "Nothing unread. Enjoy the quiet."
        : "omaslack cannot reach its daemon. Start it with: omaslack daemon"
      color: root.dim
      font.family: root.fontFamily
      font.pixelSize: Style.font.body
      horizontalAlignment: Text.AlignHCenter
      wrapMode: Text.WordWrap
    }

    Flickable {
      id: flick
      visible: !root.empty
      anchors.top: header.bottom
      anchors.topMargin: Style.spacing.panelGap
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.bottom: parent.bottom
      contentWidth: width
      contentHeight: rowColumn.implicitHeight
      clip: true
      boundsBehavior: Flickable.StopAtBounds
      flickableDirection: Flickable.VerticalFlick
      interactive: contentHeight > height
      ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }
      onContentYChanged: Qt.callLater(root.refreshPreviews)

      Column {
        id: rowColumn
        width: flick.width
        spacing: Style.spacing.xs

        Repeater {
          id: repeater
          model: root.rows

          QuickPanelRow {
            id: rowItem
            required property var modelData
            required property int index
            width: rowColumn.width
            row: modelData
            lines: root.previewLines(modelData.key)
            showWorkspace: root.multiWorkspace
            replying: root.replyKey === modelData.key
            hasCursor: root.cursor.section === "rows" && root.cursor.index === index
            foreground: root.foreground
            fontFamily: root.fontFamily
            onPointed: root.setCursor({ section: "rows", index: index })
            onOpenRequested: root.openRow(modelData)
            onMarkReadRequested: root.markRead(modelData)
            onReplyRequested: root.startReply(modelData)
            onReplyAccepted: function(text) { root.sendReply(modelData, text) }
            onReplyCancelled: root.cancelReply()
            onReplyFocusChanged: function(focused) { root.replyFocused = focused }

            Connections {
              target: bus
              function onSent(key) { if (key === rowItem.modelData.key) rowItem.clearReply() }
            }
          }
        }
      }
    }
  }
}
