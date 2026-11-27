pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Layouts
import qs.Commons
import qs.Commons as Commons
import "lib/nav.js" as N

// The conversation finder (Ctrl+K): every conversation of every workspace,
// filtered and ranked as the TUI's finder ranks them (lib/nav.js
// rankConversations: the better match first, then what needs attention,
// then the most recent). Enter reports the pick; the window opens it.
ListOverlay {
  id: root

  property var service: null

  signal conversationPicked(string team, string conv)

  readonly property var entries: service && visible
    ? N.finderEntries(service.workspaces, service.conversations, nameOf)
    : []
  readonly property bool manyWorkspaces: service ? service.workspaces.length > 1 : false

  function nameOf(team, conv) {
    void service.messagesRevision
    return service.conversationName(team, conv)
  }

  title: "Find a conversation"
  placeholder: "Name; start with # ~ @ & for one kind"
  rows: N.rankConversations(entries, query)
  hint: visible && rows.length === 0 ? "No conversation matches." : ""

  onEntered: {
    var e = rows[currentIndex]
    if (!e) return
    hide()
    conversationPicked(e.team, e.conv)
  }

  rowContent: RowLayout {
    id: line
    property var row: ({})
    property bool selected: false
    spacing: Style.spacing.md

    Text {
      Layout.fillWidth: true
      textFormat: Text.PlainText
      text: line.row.title || ""
      color: line.row.unread ? root.foreground : root.dim
      font.family: root.fontFamily
      font.pixelSize: Style.font.body
      font.bold: !!line.row.unread
      elide: Text.ElideRight
    }
    Text {
      visible: line.row.mentions > 0
      textFormat: Text.PlainText
      text: "(" + line.row.mentions + ")"
      color: root.accentText
      font.family: root.fontFamily
      font.pixelSize: Style.font.bodySmall
      font.bold: true
    }
    Text {
      visible: root.manyWorkspaces
      textFormat: Text.PlainText
      text: line.row.workspace || ""
      color: root.dim
      font.family: root.fontFamily
      font.pixelSize: Style.font.caption
    }
  }
}
