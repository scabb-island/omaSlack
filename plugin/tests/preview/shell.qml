import QtQuick
import Quickshell
import qs.Commons
import qs.Commons as Commons
import "omaslack/tests" as T
import "omaslack" as Plugin

// The marketplace preview (tests/preview/run.sh): the bar icon, the quick
// panel and the window, drawn offscreen against FakeService with made-up
// sample data. Every name and message here is invented; nothing comes from
// a real Slack. Each part is saved to $OMASLACK_PREVIEW_DIR as a PNG, the
// theme's colors are logged ("PREVIEW COLORS"), and run.sh lays them out.
ShellRoot {
  id: root

  readonly property string outDir: Quickshell.env("OMASLACK_PREVIEW_DIR") || "/tmp"
  readonly property string rootTS: "1764237480.000100"

  // ---- sample data --------------------------------------------------------------

  // A FakeService whose previews differ per conversation (the stock one
  // answers every messages.history the same).
  component PreviewService: T.FakeService {
    id: svc
    function call(method, params, callback) {
      svc.record(method, params)
      var result = ({})
      if (method === "messages.history") result = (svc.messageLists[params.team + "/" + params.conv] || []).slice().reverse()
      else if (method === "conversations.open") result = { messages: [], draft: "" }
      else if (method === "drafts.get") result = { text: "" }
      if (callback) callback(null, result)
    }
  }

  function msg(minute, author, text, extra) {
    var m = { ts: String(1764237000 + minute * 60) + ".000100", author: author, text: text }
    for (var k in extra) m[k] = extra[k]
    return m
  }

  PreviewService {
    id: fake
    workspaces: [{ team: "T1", name: "Northwind Labs", self: "U1", state: "connected" }]
    presence: ({ T1: { U1: "active" } })
    userNames: ({ "T1/U1": "Alex", "T1/U2": "Sam", "T1/U3": "Jordan", "T1/U4": "Riley" })
    conversations: ({
      T1: [
        { id: "C1", kind: "channel", name: "launch", unread: true, mentions: 1, latest: root.rootTS },
        { id: "C2", kind: "channel", name: "design", unread: true, latest: "1764237300.000100" },
        { id: "C3", kind: "channel", name: "general" },
        { id: "C4", kind: "channel", name: "random" },
        { id: "D1", kind: "im", name: "Sam", user: "U2", unread: true, latest: "1764237660.000100" },
        { id: "D2", kind: "im", name: "Jordan", user: "U3" }
      ]
    })
    summary: ({ workspaces: { T1: { dms: 1, mentions: 1, channels: 1, threads: 0 } },
                total: { dms: 1, mentions: 1, channels: 1, threads: 0 } })
  }

  function load() {
    var lists = {}
    lists["T1/C1"] = [
      msg(0, "U3", "Morning! Release notes for 2.0 are in the doc, comments welcome."),
      msg(2, "U4", "Read them, looks great. The migration section could use an example.",
          { html: "Read them, looks great. The <b>migration</b> section could use an example." }),
      msg(3, "U3", "Good call, adding one now."),
      msg(6, "B1", "Build 2.0.0-rc.3 passed all checks.", { authorName: "ci-bot" }),
      msg(8, "U2", "Who can smoke test the installer on a clean machine before we ship?",
          { replyCount: 3, reactions: [{ emoji: "eyes", users: ["U1", "U3"] }] , ts: root.rootTS }),
      msg(9, "U4", "Screenshots for the blog post are ready.", { reactions: [{ emoji: "tada", users: ["U1", "U2", "U3"] }] })
    ]
    lists["T1/C1/" + root.rootTS] = [
      lists["T1/C1"][4],
      msg(10, "U1", "I can take it after lunch.", { threadTS: root.rootTS }),
      msg(11, "U2", "Thanks! The checklist is pinned in the channel.", { threadTS: root.rootTS }),
      msg(14, "U1", "Done: fresh install, upgrade and uninstall are all clean.", { threadTS: root.rootTS })
    ]
    lists["T1/C2"] = [
      msg(3, "U4", "New icon set is up for review."),
      msg(5, "U3", "The dark variant reads much better now.")
    ]
    lists["T1/D1"] = [
      msg(10, "U2", "Are we still on for the demo at 3?"),
      msg(11, "U2", "I will bring the release checklist.")
    ]
    fake.messageLists = lists
    fake.unreadList = [
      { team: "T1", conversation: fake.conversations.T1[4] },
      { team: "T1", conversation: fake.conversations.T1[0] },
      { team: "T1", conversation: fake.conversations.T1[1] }
    ]
  }

  // ---- the parts --------------------------------------------------------------------

  QtObject {
    id: fakeBar
    property string fontFamily: Style.font.family
    property color barForeground: Commons.Color.bar.text
    property color urgent: Commons.Color.bar.active
    property bool vertical: false
    property int barSize: Style.bar.sizeHorizontal
    property bool foregroundAnimationEnabled: false
    function showTooltip(item, text) {}
    function hideTooltip(item) {}
  }

  Plugin.Window { id: win; service: fake }

  FloatingWindow {
    id: parts
    visible: true
    color: Commons.Color.background
    implicitWidth: Style.space(1800)
    implicitHeight: Style.space(1000)

    Rectangle {
      id: windowBox
      x: Style.space(600)
      border.width: Math.max(1, Style.space(2))
      border.color: Commons.Color.popups.border
      color: Commons.Color.background
    }

    Rectangle {
      id: iconBox
      width: icon.implicitWidth + Style.spacing.md * 2
      height: Style.bar.sizeHorizontal
      color: Commons.Color.bar.background
      Plugin.BarIcon {
        id: icon
        x: Style.spacing.md
        width: icon.implicitWidth
        height: iconBox.height
        bar: fakeBar
        service: fake
      }
    }

    Rectangle {
      id: card
      y: iconBox.height + Style.spacing.md
      readonly property int pad: Style.spacing.popupPadding
      width: Style.space(420) + pad * 2
      height: Math.min(panel.implicitHeight, Style.space(640)) + pad * 2
      color: Commons.Color.popups.background
      radius: Style.cornerRadius
      border.width: Math.max(1, Style.space(2))
      border.color: Commons.Color.popups.border
      Plugin.QuickPanel {
        id: panel
        x: card.pad
        y: card.pad
        width: Style.space(420)
        height: card.height - card.pad * 2
        service: fake
      }
    }
  }

  // ---- rendering --------------------------------------------------------------------

  property int pending: 0

  function save(item, name) {
    pending++
    var ok = item.grabToImage(function(result) {
      if (!result.saveToFile(root.outDir + "/" + name + ".png")) console.log("PREVIEW FAIL could not save " + name)
      if (--root.pending === 0) console.log("PREVIEW DONE")
    })
    if (!ok) { console.log("PREVIEW FAIL could not grab " + name); pending-- }
  }

  // The window's top item. Its content item itself cannot be grabbed (it
  // belongs to no QML engine) and paints no background, so the top item
  // moves into windowBox, which draws the window's color and a frame like
  // the compositor's.
  function windowContent() {
    var list = win.data
    for (var i = 0; i < list.length; i++) {
      var w = list[i]
      if (!w || w.title !== "omaslack" || !w.contentItem) continue
      var best = null
      var kids = w.contentItem.children
      for (var j = 0; j < kids.length; j++)
        if (!best || kids[j].width * kids[j].height > best.width * best.height) best = kids[j]
      return best
    }
    return null
  }

  Timer {
    id: openThread
    interval: 600
    onTriggered: win.openThread(root.rootTS)
  }

  Timer {
    id: shoot
    interval: 1800
    onTriggered: {
      console.log("PREVIEW COLORS bg=" + Commons.Color.background + " bar=" + Commons.Color.bar.background)
      var content = root.windowContent()
      if (!content) { console.log("PREVIEW FAIL the window has no content item"); return }
      var b = windowBox.border.width
      windowBox.width = content.width + b * 2
      windowBox.height = content.height + b * 2
      content.parent = windowBox
      content.x = b
      content.y = b
      Qt.callLater(function() {
        root.save(iconBox, "icon")
        root.save(card, "panel")
        root.save(windowBox, "window")
      })
    }
  }

  Component.onCompleted: {
    load()
    win.open(JSON.stringify({ team: "T1", conv: "C1" }))
    panel.refreshPreviews()
    openThread.start()
    shoot.start()
  }
}
