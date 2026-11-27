pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Layouts
import qs.Commons
import qs.Commons as Commons
import qs.Ui
import "lib/nav.js" as N

// The compose autocomplete of one Composer, as the TUI's: typing @ offers
// people (users.search, debounced), # the workspace's channels and : the
// standard and custom emoji (emoji.list, once per workspace). The popup
// sits over the window (put this in its overlay layer) right above the
// text cursor. While it shows, it takes the composer's ↑/↓ (Ctrl+P/
// Ctrl+N), Tab and Enter (pick) and Esc (close; it stays closed on that
// word) through Composer.keyFilter.
//
// Picking replaces the word with the suggestion's display text and a
// space ("@Ana ") and emits tokenPicked(display, wire) for a person or a
// channel; the window keeps those pairs and sends the wire form
// (lib/nav.js toWire).
Item {
  id: root

  property var service: null
  property var composer: null
  property color foreground: Commons.Color.popups.text
  property string fontFamily: Style.font.family

  // How long typing pauses before users.search is asked (the TUI's
  // SearchDebounce).
  property int searchDelay: 150

  // { trig, dismissed } (lib/nav.js completionStep).
  property var completion: ({ trig: null, dismissed: -1 })
  property var items: []
  property int selected: 0
  // Bumped on every new trigger: older users.search answers are stale.
  property int seq: 0
  // { <team>: [names] } of custom emoji, once loaded.
  property var customEmoji: ({})

  readonly property bool open: items.length > 0 && !!completion.trig
  readonly property string team: composer && composer.target ? composer.target.team : ""

  signal tokenPicked(string display, string wire)

  visible: open
  x: anchorPoint.x
  y: Math.max(0, anchorPoint.y - height - Style.spacing.sm)
  width: popup.width
  height: popup.height

  property point anchorPoint: Qt.point(0, 0)

  function place() {
    if (!composer || !parent) return
    var input = composer.input
    var r = input.cursorRectangle
    var p = input.mapToItem(parent, r.x, r.y)
    anchorPoint = Qt.point(Math.max(0, Math.min(p.x, parent.width - popup.width)), p.y)
  }

  Component.onCompleted: if (composer) composer.keyFilter = root.keyFilter

  // Edits and cursor moves refresh the popup; text the composer loads
  // itself (a draft, a cleared field) does not open it, and leaving the
  // field closes it.
  Connections {
    target: root.composer ? root.composer.input : null
    function onTextChanged() { if (!root.composer._loading) Qt.callLater(root.refresh) }
    function onCursorPositionChanged() { if (!root.composer._loading) Qt.callLater(root.refresh) }
    function onActiveFocusChanged() { if (!root.composer.input.activeFocus) root.reset() }
  }

  function reset() {
    completion = { trig: null, dismissed: -1 }
    close()
  }

  // refresh follows the composer: opens, updates or closes the popup for
  // the word at the cursor.
  function refresh() {
    var input = composer ? composer.input : null
    var found = input && composer.target ? N.findTrigger(input.text, input.cursorPosition) : null
    var step = N.completionStep(completion, found)
    completion = { trig: step.trig, dismissed: step.dismissed }
    if (step.close) { close(); return }
    if (step.clearItems) items = []
    if (!step.changed) return
    seq++
    place()
    suggest(step.trig)
  }

  function close() {
    debounce.stop()
    items = []
    selected = 0
    seq++
  }

  function show(list) {
    items = list
    selected = Math.min(selected, Math.max(list.length - 1, 0))
  }

  function suggest(trig) {
    if (!service || team === "") return
    if (trig.sign === "#") show(N.channelSuggestions(service.conversations[team] || [], trig.query))
    else if (trig.sign === ":") suggestEmoji(trig)
    else debounce.restart()
  }

  function suggestEmoji(trig) {
    var forTeam = team
    if (customEmoji[forTeam] === undefined) loadEmoji(forTeam)
    show(N.emojiSuggestions(customEmoji[forTeam], trig.query))
  }

  // loadEmoji asks emoji.list once for forTeam; the popup updates when it
  // answers. A provider without custom emoji leaves the standard ones.
  function loadEmoji(forTeam) {
    var loaded = Object.assign({}, customEmoji)
    loaded[forTeam] = []
    customEmoji = loaded
    service.call("emoji.list", { team: forTeam }, function(err, res) {
      if (err || !res || !res.emoji) return
      var next = Object.assign({}, root.customEmoji)
      next[forTeam] = Object.keys(res.emoji).sort()
      root.customEmoji = next
      var t = root.completion.trig
      if (t && t.sign === ":" && root.team === forTeam) root.show(N.emojiSuggestions(next[forTeam], t.query))
    })
  }

  // searchPeople asks users.search for the current @ trigger; answers for
  // an older trigger are dropped.
  function searchPeople() {
    debounce.stop()
    var trig = completion.trig
    if (!service || !trig || trig.sign !== "@") return
    var mine = seq
    var q = trig.query
    service.call("users.search", { team: team, q: q, limit: N.MAX_SUGGESTIONS }, function(err, res) {
      if (err || mine !== root.seq) return
      root.show(N.peopleSuggestions(res || [], q))
    })
  }

  Timer {
    id: debounce
    interval: root.searchDelay
    onTriggered: root.searchPeople()
  }

  // accept puts the selected suggestion in place of the word.
  function accept() {
    var s = items[selected]
    var trig = completion.trig
    if (!s || !trig) return
    var input = composer.input
    var cursor = input.cursorPosition
    close()
    completion = { trig: null, dismissed: -1 }
    input.remove(trig.start, cursor)
    input.insert(trig.start, s.text + " ")
    input.cursorPosition = trig.start + s.text.length + 1
    if (s.wire) tokenPicked(s.text, s.wire)
  }

  // keyFilter for Composer: the popup's keys while it shows.
  function keyFilter(event) {
    if (!open) return false
    var ctrl = (event.modifiers & Qt.ControlModifier) !== 0
    var shift = (event.modifiers & Qt.ShiftModifier) !== 0
    if (event.key === Qt.Key_Tab || ((event.key === Qt.Key_Return || event.key === Qt.Key_Enter) && !shift)) accept()
    else if (event.key === Qt.Key_Down || (ctrl && event.key === Qt.Key_N)) selected = N.cycle(selected, items.length, 1)
    else if (event.key === Qt.Key_Up || (ctrl && event.key === Qt.Key_P)) selected = N.cycle(selected, items.length, -1)
    else if (event.key === Qt.Key_Escape) { completion = N.dismiss(completion); close() }
    else return false
    return true
  }

  BorderSurface {
    id: popup
    objectName: "autocompletePopup"
    width: Math.min(Math.max(Style.space(240), rowsColumn.implicitWidth + padding * 2), Style.space(420))
    height: rowsColumn.implicitHeight + padding * 2
    padding: Style.spacing.sm
    color: Commons.Color.popups.background
    radius: Style.cornerRadius
    borderSpec: Border.localOrSurfaceSpec("popups", "border", Commons.Color.popups.border, Commons.Color.popups.border, Math.max(1, Style.space(2)))

    ColumnLayout {
      id: rowsColumn
      x: popup.padding
      y: popup.padding
      width: popup.width - popup.padding * 2
      spacing: 0

      Text {
        Layout.fillWidth: true
        Layout.bottomMargin: Style.spacing.xs
        textFormat: Text.PlainText
        text: (root.completion.trig ? N.POPUP_TITLES[root.completion.trig.sign] : "") + " · Tab picks"
        color: Qt.darker(root.foreground, 1.4)
        font.family: root.fontFamily
        font.pixelSize: Style.font.caption
      }

      Repeater {
        model: root.items

        CursorSurface {
          id: itemRow
          required property var modelData
          required property int index
          Layout.fillWidth: true
          implicitWidth: label.implicitWidth + Style.spacing.rowPaddingX * 2
          implicitHeight: label.implicitHeight + Style.spacing.xs * 2
          hasCursor: root.selected === index
          foreground: root.foreground

          MouseArea {
            anchors.fill: parent
            cursorShape: Qt.PointingHandCursor
            onClicked: {
              root.selected = itemRow.index
              root.accept()
              root.composer.input.forceActiveFocus()
            }
          }

          Text {
            id: label
            anchors.left: parent.left
            anchors.right: parent.right
            anchors.verticalCenter: parent.verticalCenter
            anchors.leftMargin: Style.spacing.rowPaddingX
            anchors.rightMargin: Style.spacing.rowPaddingX
            textFormat: Text.PlainText
            text: itemRow.modelData.label
            color: root.foreground
            font.family: root.fontFamily
            font.pixelSize: Style.font.body
            elide: Text.ElideRight
          }
        }
      }
    }
  }
}
