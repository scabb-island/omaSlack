import QtQuick
import QtQuick.Layouts
import Quickshell
import qs.Commons
import qs.Commons as Commons
import qs.Ui
import "lib/window.js" as W
import "lib/nav.js" as N

// The full window (task 4.5): a FloatingWindow with the fixed title
// "omaslack" (for Hyprland rules), with the workspace and conversation
// sidebar, the open conversation, its thread and their composers. It is
// the plugin's "panel" entry point, so
// `omarchy-shell shell toggle ar.scabb.omaslack` shows and hides it, and
// `summon` with a payload { team, conv, ts } opens that conversation.
//
// Parts: WindowSidebar (the sidebar), MessagePane (header, MessageList of
// MessageDelegates, Composer), used twice: the conversation and the
// thread. This file owns the state (open conversation and thread, which
// pane has the keyboard) and every Service call; the parts draw and
// report. Pure logic lives in lib/window.js (tests/tst_window.qml).
//
// Keys (PanelKeyCatcher, while no composer has the keyboard):
//   Tab / Shift+Tab   next / previous pane (sidebar, messages, thread)
//   h l, ← →          pane to the left / right
//   j k, ↓ ↑          move in the pane's list (up past the oldest message
//                     loads older history)
//   g G               oldest / newest message
//   Enter, Space      sidebar: open the conversation; messages: open the
//                     message's thread; thread: write a reply
//   i                 write in the pane's composer (the conversation's
//                     from the sidebar)
//   R                 reply in the selected message's thread
//   e                 edit your selected message (in its pane's composer)
//   D, x, Delete      delete your selected message (asks y/n first)
//   r                 react to the selected message (the emoji picker)
//   o                 open the selected message's file (a chooser for
//                     several)
//   Esc               leave the composer, then close the thread, then the
//                     window
//   Ctrl+K, Ctrl+T    find a conversation in any workspace (Finder)
//   /                 search messages in this workspace (SearchOverlay)
//   1–9               the n-th workspace's first conversation in the sidebar
//   ?                 every key (HelpOverlay; lib/nav.js HELP)
// In a composer: Enter sends, Shift+Enter is a new line, Esc leaves; @ # :
// suggest people, channels and emoji (Autocomplete);
// ":attach <path> [caption]" uploads a file there. The message actions
// live in MessageActions.qml.
//
// Seams for the rest of 4.5 (see textKey() and the `overlays` layer):
// message actions (e edit, D delete) act on currentMessage(); the
// reactions picker (r) opens in `overlays` and calls service.react; search
// (/) and the finder (Ctrl+K) open in `overlays` and end in
// selectConversation(team, conv, ts); compose autocomplete and attachments
// hook into Composer (keyFilter, insertText, input).
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

  // ---- state ----------------------------------------------------------------

  // The open conversation ("" for none) and thread ("" for closed).
  property string team: ""
  property string conv: ""
  property string threadTS: ""
  // The pane with the keyboard: "sidebar", "messages" or "thread".
  property string pane: "sidebar"
  property int sidebarIndex: -1
  // The last error of an action, shown at the bottom.
  property string status: ""
  // The newest ts this window sent to conversations.markRead.
  property string marked: ""

  readonly property bool threadOpen: threadTS !== ""
  readonly property bool composing: convPane.composer.editing || threadPane.composer.editing
  readonly property var sidebarRows: service
    ? W.sidebarRows(service.workspaces, service.conversations, service.summary, nameOf)
    : []
  readonly property color foreground: Commons.Color.foreground
  readonly property string fontFamily: Style.font.family

  // The parts, for the features that build on them and for tests.
  readonly property alias sidebarView: sidebar
  readonly property alias conversationView: convPane
  readonly property alias threadView: threadPane
  readonly property alias overlayLayer: overlays
  readonly property alias finderView: finder
  readonly property alias searchView: searchOverlay
  readonly property alias helpView: help
  readonly property alias conversationAutocomplete: convComplete
  readonly property alias messageActions: actions

  function nameOf(team, conv) {
    void service.messagesRevision
    return service.conversationName(team, conv)
  }

  function workspace(team) {
    var list = service ? service.workspaces : []
    for (var i = 0; i < list.length; i++) if (list[i].team === team) return list[i]
    return null
  }

  function conversation(team, conv) {
    var list = (service && service.conversations[team]) || []
    for (var i = 0; i < list.length; i++) if (list[i].id === conv) return list[i]
    return null
  }

  function selfUser(team) {
    var w = workspace(team)
    return w && w.self ? w.self : ""
  }

  function paneList(name) { return name === "thread" ? threadPane.list : convPane.list }
  function paneComposer(name) { return name === "thread" ? threadPane.composer : convPane.composer }

  // The selected message of the focused message pane (null for none).
  function currentMessage() {
    if (pane === "sidebar") return null
    return paneList(pane).currentMessage()
  }

  function failed(what) {
    return function(err) { root.status = err ? what + ": " + (err.message || err.code) : "" }
  }

  // ---- shell lifecycle -----------------------------------------------------------

  // Shell-initiated show; payloadJson may carry { team, conv, ts }.
  function open(payloadJson) {
    var wasOpen = opened
    opened = true
    var p = W.parsePayload(payloadJson)
    if (p) selectConversation(p.team, p.conv, p.ts)
    else if (!wasOpen && conv !== "") reopen()
    Qt.callLater(root.focusKeys)
  }

  // Shell-initiated hide (`shell hide`, `toggle`). The conversation is
  // closed in the daemon, so nothing is marked read while nobody looks.
  function close() {
    if (!opened) return
    opened = false
    convPane.composer.flush()
    threadPane.composer.flush()
    if (conv !== "" && service) service.call("conversations.close", { team: team, conv: conv }, null)
  }

  // User-initiated close (Esc, the close button): tell the shell, so its
  // open-panel state stays right and the next toggle shows the window.
  function dismiss() {
    if (shell && typeof shell.hide === "function") shell.hide("ar.scabb.omaslack")
    else close()
  }

  Connections {
    target: root.service
    ignoreUnknownSignals: true

    // ui.focus from another client (a notification click, a deep link):
    // show the window on that conversation, through the shell when there
    // is one so its open-panel state follows.
    function onFocusRequested(team, conv, ts) {
      var payload = JSON.stringify({ team: team, conv: conv, ts: ts })
      if (root.opened) root.selectConversation(team, conv, ts)
      else if (root.shell && typeof root.shell.summon === "function") root.shell.summon("ar.scabb.omaslack", payload)
      else root.open(payload)
    }

    function onMessagesUpdated(team, conv, threadTS) {
      if (team === root.team && conv === root.conv && threadTS === "") Qt.callLater(root.markReadIfFollowing)
    }
  }

  // ---- conversations and threads -------------------------------------------------

  // selectConversation opens conv of team (conversations.open marks it
  // read in the daemon: the user opened it), with the cursor on ts when
  // set. The conversation left is closed in the daemon and its draft saved.
  function selectConversation(team, conv, ts) {
    if (!service || !team || !conv) return
    var at = indexOfRow(team + "/" + conv)
    if (at >= 0) sidebarIndex = at
    pane = "messages"
    if (team === root.team && conv === root.conv) {
      if (ts) convPane.list.scrollTo(ts)
      return
    }
    closeThread()
    convPane.composer.flush()
    if (root.conv !== "") service.call("conversations.close", { team: root.team, conv: root.conv }, null)
    root.team = team
    root.conv = conv
    marked = ""
    status = ""
    convPane.composer.load({ team: team, conv: conv, threadTS: "" }, "")
    convPane.list.scrollWhenLoaded(ts || "end")
    openInDaemon()
  }

  // openInDaemon: conversations.open for the open conversation, restoring
  // its draft.
  function openInDaemon() {
    var t = { team: team, conv: conv, threadTS: "" }
    service.openConversation(t.team, t.conv, function(err, res) {
      if (err) { root.status = "Could not open the conversation: " + (err.message || err.code); return }
      if (res && res.draft) convPane.composer.restoreDraft(t, res.draft)
    })
  }

  // reopen: the window shows again on the conversation it had.
  function reopen() {
    marked = ""
    openInDaemon()
  }

  function indexOfRow(key) {
    for (var i = 0; i < sidebarRows.length; i++) if (sidebarRows[i].key === key) return i
    return -1
  }

  // openThread shows the thread of the message ts in the thread pane:
  // messages.replies, and its draft from drafts.get.
  function openThread(ts) {
    if (!service || conv === "" || !ts) return
    pane = "thread"
    if (ts === threadTS) return
    threadPane.composer.flush()
    threadTS = ts
    var t = { team: team, conv: conv, threadTS: ts }
    threadPane.composer.load(t, "")
    threadPane.list.scrollWhenLoaded("end")
    service.loadReplies(t.team, t.conv, ts, failed("Could not load the thread"))
    service.call("drafts.get", { team: t.team, conv: t.conv, threadTS: ts }, function(err, res) {
      if (!err && res) threadPane.composer.restoreDraft(t, res.text)
    })
  }

  function closeThread() {
    if (!threadOpen) return
    threadPane.composer.flush()
    threadPane.composer.load(null, "")
    threadTS = ""
    if (pane === "thread") pane = "messages"
  }

  // send posts text from a composer to its target (the conversation or the
  // thread), and empties the composer once the daemon took it.
  function send(composer, text) {
    if (actions.submit(composer, text)) return
    var t = composer.target
    if (!service || !t || text.trim() === "") return
    service.send(t.team, t.conv, N.toWire(text.trim(), mentionTokens), t.threadTS, function(err) {
      if (err) { root.status = "Could not send: " + (err.message || err.code); return }
      root.status = ""
      composer.clear()
    })
  }

  // markReadIfFollowing marks the open conversation read up to its newest
  // message while the window shows it at the bottom, as the TUI does.
  function markReadIfFollowing() {
    if (!opened || conv === "" || !convPane.list.following) return
    var c = conversation(team, conv)
    var ts = W.readUpTo(service.messages(team, conv, ""), c ? c.lastRead : "", marked)
    if (ts === "") return
    marked = ts
    service.markRead(team, conv, ts, failed("Could not mark as read"))
  }

  function openLink(url) {
    var a = W.linkAction(url)
    if (a.kind === "conv") selectConversation(a.team, a.conv, "")
    else if (a.kind === "external") Qt.openUrlExternally(a.url)
    // "user": a profile view is not there yet.
  }

  // ---- keyboard --------------------------------------------------------------------

  function focusKeys() { keys.forceActiveFocus() }

  function compose(paneName) {
    var name = paneName === "thread" && threadOpen ? "thread" : "messages"
    if (conv === "") return
    pane = name
    paneComposer(name).focusInput()
  }

  function moveVertical(dy) {
    if (pane === "sidebar") sidebarIndex = W.step(sidebarRows, sidebarIndex, dy)
    else paneList(pane).moveCursor(dy)
  }

  function activate() {
    if (pane === "sidebar") {
      var row = sidebarRows[sidebarIndex]
      if (row && row.type === "conv") selectConversation(row.team, row.conv, "")
      return
    }
    if (pane === "thread") { compose("thread"); return }
    var m = currentMessage()
    if (m) openThread(m.ts)
  }

  // leaveComposer hands the keyboard back to the pane's list.
  function leaveComposer() { focusKeys() }

  function stepBack() {
    var next = W.escape({ pane: pane, composing: composing, threadOpen: threadOpen })
    if (next.dismiss) { dismiss(); return }
    if (!next.threadOpen) closeThread()
    pane = next.pane
    focusKeys()
  }

  function textKey(t, modifiers) {
    if (navKey(t, modifiers)) return
    if (t === "i") compose(pane)
    else if (t === "g" && pane !== "sidebar") paneList(pane).toTop()
    else if (t === "G" && pane !== "sidebar") paneList(pane).toBottom()
    else if (t === "R") {
      var m = currentMessage()
      if (m) { openThread(pane === "thread" ? threadTS : m.ts); compose("thread") }
    }
    else if (actions.textKey(t)) {}
  }

  // ---- navigation: finder, search, autocomplete, workspaces, help (lib/nav.js) ----

  // The composers' display -> wire mention tokens (lib/nav.js toWire),
  // for the session, across conversations, as in the TUI.
  property var mentionTokens: ({})
  // A search hit being brought into view: { team, conv, root, ts, pages }.
  property var seeking: null
  readonly property bool navOpen: finder.visible || searchOverlay.visible || help.visible

  // How many older pages a jump to a search hit loads, at most (the TUI's
  // jumpPages).
  readonly property int seekPages: 10

  function addMentionToken(display, wire) {
    var next = Object.assign({}, mentionTokens)
    next[display] = wire
    mentionTokens = next
  }

  // navKey runs Ctrl+K, /, ? and 1–9; false for any other key.
  function navKey(t, modifiers) {
    var k = N.navKey(t, (modifiers & Qt.ControlModifier) !== 0)
    if (!k) return false
    if (k.action === "finder") finder.show("")
    else if (k.action === "search") searchOverlay.open(searchTeam())
    else if (k.action === "help") help.show()
    else switchWorkspace(k.index)
    return true
  }

  // switchWorkspace puts the sidebar cursor on the first conversation of
  // the index-th workspace.
  function switchWorkspace(index) {
    var j = N.workspaceJump(sidebarRows, index)
    if (j.index < 0) { status = j.error; return }
    status = ""
    sidebarIndex = j.index
    pane = "sidebar"
  }

  // searchTeam: the workspace / searches: the open conversation's, else
  // the sidebar cursor's, else the first.
  function searchTeam() {
    if (team !== "") return team
    var row = sidebarRows[sidebarIndex]
    if (row) return row.team
    return service && service.workspaces.length > 0 ? service.workspaces[0].team : ""
  }

  function closeNav() { Qt.callLater(root.focusKeys) }

  // openHit opens a search result's conversation on its message (the
  // thread's root for a reply, with the thread open on the reply), loading
  // older history until it arrives, as the TUI's jump does.
  function openHit(hitTeam, row) {
    seeking = { team: hitTeam, conv: row.conv, root: row.root, ts: row.ts, pages: seekPages, waiting: false }
    selectConversation(hitTeam, row.conv, row.root)
    seek()
  }

  function seek() {
    var s = seeking
    if (!s || s.waiting) return
    if (s.team !== team || s.conv !== conv) { seeking = null; return }
    var msgs = service.messages(s.team, s.conv, "")
    if (N.containsTS(msgs, s.root)) {
      seeking = null
      convPane.list.scrollWhenLoaded(s.root)
      if (s.ts !== s.root) {
        openThread(s.root)
        threadPane.list.scrollWhenLoaded(s.ts)
      }
      return
    }
    // Nothing yet: conversations.open has not answered; messagesUpdated
    // brings us back.
    if (msgs.length === 0) return
    if (s.pages <= 0 || !service.hasMoreHistory(s.team, s.conv)) {
      seeking = null
      status = "Could not reach that message in the history."
      return
    }
    seeking = Object.assign({}, s, { pages: s.pages - 1, waiting: true })
    service.loadOlder(s.team, s.conv, function(err) {
      if (!root.seeking) return
      root.seeking = Object.assign({}, root.seeking, { waiting: false })
      if (err) { root.seeking = null; root.status = "Could not load the history: " + (err.message || err.code); return }
      root.seek()
    })
  }

  Connections {
    target: root.service
    ignoreUnknownSignals: true
    function onMessagesUpdated(team, conv, threadTS) {
      if (root.seeking && threadTS === "" && team === root.seeking.team && conv === root.seeking.conv) Qt.callLater(root.seek)
    }
  }

  // ---- view -------------------------------------------------------------------------

  FloatingWindow {
    id: window
    visible: root.opened
    title: "omaslack"
    color: Commons.Color.background
    implicitWidth: 1100
    implicitHeight: 720

    // The compositor closed the window: tell the shell.
    onVisibleChanged: if (!visible && root.opened) root.dismiss()

    PanelKeyCatcher {
      id: keys
      anchors.fill: parent
      blocked: root.composing || actions.active || root.navOpen
      onMoveRequested: function(dx, dy) {
        if (dx !== 0) root.pane = W.movePane(root.pane, dx, root.threadOpen, false)
        else root.moveVertical(dy)
      }
      onTabRequested: function(direction) { root.pane = W.movePane(root.pane, direction, root.threadOpen, true) }
      onActivateRequested: root.activate()
      onCloseRequested: root.stepBack()
      onDeleteRequested: actions.askDelete()
      onTextKey: function(t, modifiers) { root.textKey(t, modifiers) }

      RowLayout {
        anchors.fill: parent
        anchors.bottomMargin: statusText.visible ? statusText.height : 0
        spacing: 0

        WindowSidebar {
          id: sidebar
          objectName: "sidebar"
          Layout.preferredWidth: Style.space(240)
          Layout.fillHeight: true
          Layout.margins: Style.spacing.lg
          rows: root.sidebarRows
          cursorIndex: root.sidebarIndex
          currentKey: root.conv !== "" ? root.team + "/" + root.conv : ""
          focused: root.pane === "sidebar" && !root.composing
          foreground: root.foreground
          fontFamily: root.fontFamily
          onPicked: function(index) {
            root.sidebarIndex = index
            root.pane = "sidebar"
            root.activate()
            root.focusKeys()
          }
        }

        Rectangle {
          Layout.fillHeight: true
          Layout.preferredWidth: Style.spacing.hairline
          color: Style.normalBorderColor
        }

        MessagePane {
          id: convPane
          objectName: "conversation"
          Layout.fillWidth: true
          Layout.fillHeight: true
          service: root.service
          team: root.team
          conv: root.conv
          title: root.conv !== "" ? root.nameOf(root.team, root.conv) : "omaslack"
          subtitle: root.conv !== "" && root.service.workspaces.length > 1 ? (root.workspace(root.team) || { name: root.team }).name : ""
          focused: root.pane === "messages"
          selfUser: root.selfUser(root.team)
          placeholder: root.conv !== "" ? "Message " + title + " (Enter sends, Shift+Enter new line)" : ""
          emptyText: !root.service || !root.service.daemonAvailable
            ? "omaslack cannot reach its daemon. Start it with: omaslack daemon"
            : (root.conv === "" ? "Pick a conversation: j/k and Enter in the sidebar." : "")
          foreground: root.foreground
          fontFamily: root.fontFamily
          onSubmitted: function(text) { root.send(convPane.composer, text) }
          onReactionClicked: function(message, emoji) { actions.toggleReaction(message, emoji) }
          onFileClicked: function(message, file) { actions.openOne(file) }
          onComposerEscaped: root.leaveComposer()
          onThreadRequested: function(ts) { root.openThread(ts) }
          onLinkActivated: function(url) { root.openLink(url) }
          onMessageClicked: function(index) {
            root.pane = "messages"
            convPane.list.currentIndex = index
            root.focusKeys()
          }
        }

        Rectangle {
          visible: root.threadOpen
          Layout.fillHeight: true
          Layout.preferredWidth: Style.spacing.hairline
          color: Style.normalBorderColor
        }

        MessagePane {
          id: threadPane
          objectName: "thread"
          visible: root.threadOpen
          Layout.preferredWidth: Style.space(380)
          Layout.fillHeight: true
          service: root.service
          team: root.team
          conv: root.threadOpen ? root.conv : ""
          threadTS: root.threadTS
          title: "Thread"
          subtitle: root.conv !== "" ? root.nameOf(root.team, root.conv) : ""
          closable: true
          focused: root.pane === "thread"
          selfUser: root.selfUser(root.team)
          placeholder: "Reply (Enter sends, Shift+Enter new line)"
          foreground: root.foreground
          fontFamily: root.fontFamily
          onCloseRequested: { root.closeThread(); root.focusKeys() }
          onSubmitted: function(text) { root.send(threadPane.composer, text) }
          onReactionClicked: function(message, emoji) { actions.toggleReaction(message, emoji) }
          onFileClicked: function(message, file) { actions.openOne(file) }
          onComposerEscaped: root.leaveComposer()
          onLinkActivated: function(url) { root.openLink(url) }
          onMessageClicked: function(index) {
            root.pane = "thread"
            threadPane.list.currentIndex = index
            root.focusKeys()
          }
        }
      }

      Text {
        id: statusText
        visible: root.status !== ""
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.bottom: parent.bottom
        padding: Style.spacing.md
        textFormat: Text.PlainText
        text: root.status
        color: Commons.Color.urgent
        font.family: root.fontFamily
        font.pixelSize: Style.font.bodySmall
        wrapMode: Text.WordWrap
      }

      // Popups over the window (finder, search, reactions picker) go here.
      Item {
        id: overlays
        objectName: "overlays"
        anchors.fill: parent

        // Navigation (lib/nav.js): the composers' autocomplete, then the
        // finder, search and help.
        Autocomplete {
          id: convComplete
          objectName: "conversationAutocomplete"
          service: root.service
          composer: convPane.composer
          fontFamily: root.fontFamily
          onTokenPicked: function(display, wire) { root.addMentionToken(display, wire) }
        }
        Autocomplete {
          id: threadComplete
          objectName: "threadAutocomplete"
          service: root.service
          composer: threadPane.composer
          fontFamily: root.fontFamily
          onTokenPicked: function(display, wire) { root.addMentionToken(display, wire) }
        }
        Finder {
          id: finder
          objectName: "finder"
          anchors.fill: parent
          service: root.service
          fontFamily: root.fontFamily
          onConversationPicked: function(team, conv) { root.selectConversation(team, conv, "") }
          onClosed: root.closeNav()
        }
        SearchOverlay {
          id: searchOverlay
          objectName: "search"
          anchors.fill: parent
          service: root.service
          fontFamily: root.fontFamily
          onHitPicked: function(team, row) { root.openHit(team, row) }
          onClosed: root.closeNav()
        }
        HelpOverlay {
          id: help
          objectName: "help"
          anchors.fill: parent
          fontFamily: root.fontFamily
          onClosed: root.closeNav()
        }
        MessageActions {
          id: actions
          anchors.fill: parent
          window: root
          service: root.service
          foreground: root.foreground
          fontFamily: root.fontFamily
        }
      }
    }
  }
}
