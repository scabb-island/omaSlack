pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls
import qs.Commons
import qs.Commons as Commons
import "lib/window.js" as W

// The messages of a conversation (or, with threadTS, of a thread): a
// virtualized ListView over a KeyedModel, so a long history only builds
// the delegates on screen, and an older page arriving above them does not
// move what the user is reading. Scrolling to the top of a conversation
// loads older history (service.loadOlder) while there is more.
//
// The keyboard cursor is currentIndex (-1: none, the view follows the
// newest message). MessageList owns the cursor, paging and scrolling; it
// reports clicks and links and never talks to the Service otherwise.
ListView {
  id: root

  property var service: null
  property string team: ""
  property string conv: ""
  property string threadTS: ""
  // Whether this list's pane has the keyboard (the cursor shows).
  property bool focused: false
  property string selfUser: ""
  property color foreground: Commons.Color.foreground
  property string fontFamily: Style.font.family

  signal threadRequested(string ts)
  signal linkActivated(string url)
  signal messageClicked(int index)
  signal reactionClicked(string ts, string emoji)
  signal fileClicked(string ts, var file)

  // The service's messages, as rows, keyed by ts.
  readonly property var messages: service && conv !== "" ? service.messages(team, conv, threadTS) : []
  property var rows: []
  property var rowByKey: ({})
  // The oldest ts older history was asked before: each page is asked for
  // once, however many times the top is reached ("" for none).
  property string olderAskedBefore: ""
  // Where to scroll once rows arrive: "end", a ts, or "" for nothing.
  property string pendingScroll: ""
  // The view shows the newest message (new ones keep it there).
  property bool following: true

  model: KeyedModel { id: keyed }
  clip: true
  boundsBehavior: Flickable.StopAtBounds
  currentIndex: -1
  highlightFollowsCurrentItem: false
  keyNavigationEnabled: false
  reuseItems: true
  cacheBuffer: height
  ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }

  delegate: MessageDelegate {
    id: item
    required property string key
    required property int index
    width: ListView.view.width
    row: root.rowByKey[key] || ({ message: ({}) })
    team: root.team
    service: root.service
    selfUser: root.selfUser
    showThread: root.threadTS === ""
    hasCursor: root.focused && root.currentIndex === index
    foreground: root.foreground
    fontFamily: root.fontFamily
    onClicked: root.messageClicked(index)
    onThreadRequested: root.threadRequested(item.key)
    onLinkActivated: function(url) { root.linkActivated(url) }
    onReactionClicked: function(emoji) { root.reactionClicked(item.key, emoji) }
    onFileClicked: function(file) { root.fileClicked(item.key, file) }
  }

  // ---- reading ----------------------------------------------------------------

  function currentMessage() {
    return currentIndex >= 0 && currentIndex < rows.length ? rows[currentIndex].message : null
  }
  function keys() { return keyed.keys }

  // ---- syncing ----------------------------------------------------------------

  onMessagesChanged: sync()
  onConvChanged: reset()
  onThreadTSChanged: reset()
  onTeamChanged: reset()

  function reset() {
    currentIndex = -1
    olderAskedBefore = ""
    following = true
    keyed.sync([])
    rows = []
    rowByKey = ({})
    sync()
  }

  function sync() {
    var next = W.messageRows(messages, new Date())
    var byKey = ({})
    for (var i = 0; i < next.length; i++) byKey[next[i].key] = next[i]
    var wasFollowing = following
    var anchor = wasFollowing ? null : readingAnchor()
    rowByKey = byKey
    rows = next
    keyed.sync(next.map(function(r) { return r.key }))
    if (pendingScroll !== "" && next.length > 0) {
      var target = pendingScroll
      pendingScroll = ""
      scrollTo(target)
    } else if (wasFollowing && currentIndex < 0) {
      Qt.callLater(root.toEnd)
    } else if (anchor) {
      keepAnchor(anchor)
    }
  }

  // readingAnchor: the message at the top of the view and how far below
  // the view's top it starts, so keepAnchor can put it back there after
  // rows are inserted above it (an older page: a ListView at its top would
  // otherwise show the new rows and lose the reader's place).
  function readingAnchor() {
    var at = indexAt(width / 2, contentY + 1)
    var item = at >= 0 ? itemAtIndex(at) : null
    return item ? { key: keyed.keys[at], offset: item.y - contentY } : null
  }

  function keepAnchor(anchor) {
    var at = keyed.keys.indexOf(anchor.key)
    if (at < 0) return
    forceLayout()
    var item = itemAtIndex(at)
    if (item && item.y - contentY === anchor.offset) return
    positionViewAtIndex(at, ListView.Beginning)
    item = itemAtIndex(at)
    if (item) contentY = item.y - anchor.offset
  }

  // ---- scrolling and the cursor -------------------------------------------------

  // scrollWhenLoaded scrolls to target ("end" or a ts) now if there are
  // rows, else once they arrive.
  function scrollWhenLoaded(target) {
    if (rows.length > 0) scrollTo(target)
    else pendingScroll = target
  }

  function scrollTo(target) {
    if (target === "end" || target === "") { toEnd(); return }
    var at = W.indexForTS(keyed.keys, target)
    if (at < 0) return
    forceLayout()
    currentIndex = at
    positionViewAtIndex(at, ListView.Center)
    following = false
  }

  function toEnd() {
    if (count === 0) return
    forceLayout()
    positionViewAtEnd()
    following = true
  }

  function toTop() {
    if (count === 0) return
    currentIndex = 0
    positionViewAtBeginning()
    following = false
    loadOlder()
  }

  function toBottom() {
    if (count === 0) return
    currentIndex = count - 1
    toEnd()
  }

  // moveCursor by dy rows; from no cursor (the view follows the newest
  // message), either way selects the newest. Up past the oldest loads
  // older history.
  function moveCursor(dy) {
    if (count === 0) return
    if (currentIndex < 0) {
      currentIndex = count - 1
    } else if (dy < 0 && currentIndex === 0) {
      loadOlder()
      return
    } else {
      currentIndex = Math.max(0, Math.min(currentIndex + dy, count - 1))
    }
    positionViewAtIndex(currentIndex, ListView.Contain)
    following = currentIndex === count - 1 && atYEnd
  }

  // ---- paging -------------------------------------------------------------------

  function loadOlder() {
    if (!service || conv === "" || threadTS !== "") return
    var oldest = keyed.keys.length > 0 ? keyed.keys[0] : ""
    if (oldest === olderAskedBefore || !service.hasMoreHistory(team, conv)) return
    olderAskedBefore = oldest
    var want = team + "/" + conv
    service.loadOlder(team, conv, function(err) {
      // A failed page may be asked for again.
      if (err && root.team + "/" + root.conv === want && root.olderAskedBefore === oldest) root.olderAskedBefore = ""
    })
  }

  // Reaching the top, by wheel, drag or keys, asks for the page before it.
  // A list shorter than the view never reaches it: k on its first message
  // does (moveCursor).
  onAtYBeginningChanged: if (atYBeginning && count > 0 && contentHeight > height) loadOlder()
  onContentYChanged: following = atYEnd
}
