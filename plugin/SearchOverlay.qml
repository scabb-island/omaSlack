pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Layouts
import qs.Commons
import qs.Commons as Commons
import "lib/nav.js" as N
import "lib/window.js" as W

// Message search (/) in one workspace, as the TUI's :search: Enter runs
// search.messages for the query; the results list where, who, when and
// the message's rich text (wire.Message.html). Enter on a result (the
// query unchanged since it ran) reports the pick; the window opens the
// conversation on that message.
ListOverlay {
  id: root

  property var service: null
  // The workspace searched.
  property string team: ""

  // The query the shown results answer ("" before the first search).
  property string searched: ""
  property bool searching: false
  property string error: ""
  property var hits: []

  // row: N.searchRows's { conv, ts, root, ... }.
  signal hitPicked(string team, var row)

  title: "Search messages" + (workspaceName !== "" ? " in " + workspaceName : "")
  placeholder: "Query, then Enter (Slack search syntax)"
  rows: hits
  hint: searching ? "Searching…"
    : error !== "" ? error
    : searched !== "" && hits.length === 0 ? "No messages match " + searched + "."
    : ""

  readonly property string workspaceName: {
    var list = service ? service.workspaces : []
    for (var i = 0; i < list.length; i++) if (list[i].team === team) return list.length > 1 ? (list[i].name || team) : ""
    return ""
  }

  function open(forTeam) {
    team = forTeam
    show(searched)
  }

  onEntered: {
    var q = query.trim()
    if (rows.length > 0 && q === searched) {
      var row = rows[currentIndex]
      hide()
      hitPicked(team, row)
    } else {
      run(q)
    }
  }

  // run asks search.messages for q in team; a provider without search
  // answers -32002.
  function run(q) {
    if (!service || q === "" || team === "") return
    var forTeam = team
    searching = true
    error = ""
    service.call("search.messages", { team: forTeam, query: q }, function(err, res) {
      if (forTeam !== root.team) return
      root.searching = false
      root.searched = q
      root.currentIndex = 0
      if (err) {
        root.hits = []
        root.error = err.code === -32002 ? "Search is not available for this workspace." : "Search failed: " + (err.message || err.code)
        return
      }
      root.hits = N.searchRows(res || [], root.service.conversations[forTeam] || [],
        function(conv) { return root.service.conversationName(forTeam, conv) },
        function(user) { return root.service.userName(forTeam, user) })
    })
  }

  onTeamChanged: {
    hits = []
    searched = ""
    error = ""
  }

  rowContent: ColumnLayout {
    id: line
    property var row: ({})
    property bool selected: false
    spacing: Style.spacing.xxs

    RowLayout {
      Layout.fillWidth: true
      spacing: Style.spacing.md

      Text {
        textFormat: Text.PlainText
        text: line.row.where || ""
        color: root.accentText
        font.family: root.fontFamily
        font.pixelSize: Style.font.bodySmall
        font.bold: true
      }
      Text {
        Layout.fillWidth: true
        textFormat: Text.PlainText
        text: (line.row.who || "") + " · " + Qt.formatDateTime(new Date((line.row.seconds || 0) * 1000), "yyyy-MM-dd hh:mm")
        color: root.dim
        font.family: root.fontFamily
        font.pixelSize: Style.font.caption
        elide: Text.ElideRight
      }
    }
    Text {
      Layout.fillWidth: true
      textFormat: Text.RichText
      // Rich text ignores linkColor: W.body styles the links.
      text: W.body({ html: line.row.html || "" }, String(root.accentText)).text
      color: root.foreground
      font.family: root.fontFamily
      font.pixelSize: Style.font.body
      wrapMode: Text.Wrap
      maximumLineCount: 2
      elide: Text.ElideRight
    }
  }
}
