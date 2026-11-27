import QtQuick
import QtQuick.Controls
import qs.Commons
import qs.Commons as Commons
import qs.Ui

// The window's message field, for a conversation or a thread: multi-line,
// Enter sends, Shift+Enter starts a new line, Esc hands the keyboard back.
// It keeps the draft of its target in the daemon (drafts.set, debounced)
// and never sends by itself: it emits submitted(text) and the window
// sends, then calls clear() on success.
//
// Seams for the autocomplete (@ # :) and attachments: `input` is the
// TextArea (text, cursorPosition, cursorRectangle for placing a popup),
// insertText() edits at the cursor, and keyFilter, when set, sees every
// key first and returns true to take it (a suggestion popup's Up/Down,
// Tab and Enter).
//
// Editing a message (e): startEdit(ts, text) puts the field in edit mode
// (editTS set) with the message's text, saving the draft first and
// leaving it alone while editing; submitted(text) then means "save the
// edit" (the window checks editTS). cancelEdit(), Esc or loading another
// target give the draft back.
Item {
  id: root

  property var service: null
  // { team, conv, threadTS } the text is for; null for none.
  property var target: null
  property string placeholder: ""
  property color foreground: Commons.Color.foreground
  property string fontFamily: Style.font.family
  // function(event) -> bool: return true to take the key (see above).
  property var keyFilter: null
  // The field grows with its text up to this many lines, then scrolls.
  property int maxLines: 8
  // The ts of the message being edited; "" while composing a new one.
  property string editTS: ""

  readonly property alias input: area
  readonly property alias text: area.text
  readonly property bool editing: area.activeFocus

  signal submitted(string text)
  signal escaped()

  // How long typing pauses before the draft is saved.
  readonly property int draftDelay: 800

  property bool _loading: false
  property bool _dirty: false
  // A non-empty draft is stored in the daemon for target.
  property bool _stored: false
  // The field's text when the edit started, given back when it ends.
  property string _draftBeforeEdit: ""

  implicitHeight: Math.min(area.implicitHeight, area.font.pixelSize * 1.5 * maxLines + area.topPadding + area.bottomPadding)

  function focusInput() {
    area.forceActiveFocus()
    area.cursorPosition = area.length
  }

  function insertText(t) { area.insert(area.cursorPosition, t) }

  // startEdit edits the message ts, starting from text.
  function startEdit(ts, text) {
    flush()
    if (root.editTS === "") root._draftBeforeEdit = area.text
    root.editTS = ts
    _setText(text)
    focusInput()
  }

  // cancelEdit ends an edit, if one is going, and gives the draft back.
  function cancelEdit() {
    if (root.editTS === "") return
    root.editTS = ""
    _setText(root._draftBeforeEdit)
    root._draftBeforeEdit = ""
  }

  // load points the field at target with its draft, saving the draft of
  // the target it leaves first.
  function load(next, draft) {
    cancelEdit()
    flush()
    root.target = next
    _setText(draft || "")
    root._stored = !!draft
  }

  // A draft that arrives after load(): taken only while nothing was typed.
  function restoreDraft(forTarget, draft) {
    if (!draft || !_same(forTarget, root.target) || area.text !== "") return
    _setText(draft)
    root._stored = true
  }

  // flush saves a pending draft now.
  function flush() {
    draftTimer.stop()
    if (!root._dirty || !root.target || !root.service) return
    root._dirty = false
    root.service.setDraft(root.target.team, root.target.conv, root.target.threadTS, area.text)
    root._stored = area.text !== ""
  }

  // clear empties the field after a send, and the stored draft with it.
  function clear() {
    draftTimer.stop()
    root._dirty = false
    _setText("")
    if (root._stored && root.target && root.service)
      root.service.setDraft(root.target.team, root.target.conv, root.target.threadTS, "")
    root._stored = false
  }

  function _setText(t) {
    root._loading = true
    area.text = t
    area.cursorPosition = area.length
    root._loading = false
  }

  function _same(a, b) {
    return !!a && !!b && a.team === b.team && a.conv === b.conv && (a.threadTS || "") === (b.threadTS || "")
  }

  // handleKey runs the field's own keys and says whether it took the key:
  // Enter sends, Shift+Enter starts a new line, Esc hands the keyboard back.
  function handleKey(key, modifiers) {
    if (key === Qt.Key_Escape) {
      cancelEdit()
      root.escaped()
      return true
    }
    if (key !== Qt.Key_Return && key !== Qt.Key_Enter) return false
    if (modifiers & Qt.ShiftModifier) area.insert(area.cursorPosition, "\n")
    else root._submit()
    return true
  }

  function _submit() {
    if (area.text.trim() === "") return
    root.submitted(area.text)
  }

  Timer {
    id: draftTimer
    interval: root.draftDelay
    onTriggered: root.flush()
  }

  ScrollView {
    anchors.fill: parent
    ScrollBar.horizontal.policy: ScrollBar.AlwaysOff

    TextArea {
      id: area
      objectName: "composerInput"
      enabled: root.target !== null
      wrapMode: TextEdit.Wrap
      textFormat: TextEdit.PlainText
      placeholderText: root.placeholder
      placeholderTextColor: Qt.darker(root.foreground, 1.6)
      color: root.foreground
      selectionColor: Style.selectionFillFor(root.foreground, Commons.Color.accent)
      selectedTextColor: root.foreground
      font.family: root.fontFamily
      font.pixelSize: Style.font.body
      leftPadding: Style.spacing.controlPaddingX
      rightPadding: Style.spacing.controlPaddingX
      topPadding: Style.spacing.inputPaddingY
      bottomPadding: Style.spacing.inputPaddingY

      background: BorderSurface {
        color: Style.controlFill(area.activeFocus, area.hovered, root.foreground, Commons.Color.accent)
        borderSpec: Border.controlSpec(area.activeFocus ? "focus" : (area.hovered ? "hover-cursor" : "normal"), root.foreground, Commons.Color.accent)
        radius: Style.cornerRadius
      }

      onTextChanged: {
        if (root._loading || root.editTS !== "") return
        root._dirty = true
        draftTimer.restart()
      }

      Keys.onPressed: function(event) {
        if (root.keyFilter && root.keyFilter(event)) event.accepted = true
        else event.accepted = root.handleKey(event.key, event.modifiers)
      }
    }
  }
}
