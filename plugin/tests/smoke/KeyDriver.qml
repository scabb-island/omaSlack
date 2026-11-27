import QtQuick
import QtTest

// Real key events for the smokes: QtTest's event sender, outside a
// TestCase. Keys go to the focus window's focused item, as the user's do,
// so a smoke drives a view with nothing but keys and checks what took
// them. activate(item) makes item's window the focus
// window first.
Item {
  id: root

  TestEvent { id: events }
  TestResult { id: clock }

  // wait runs the event loop for ms (timers, Qt.callLater, focus changes).
  function wait(ms) { clock.wait(ms) }

  // quickshell builds a window an event loop turn after its object, and
  // activates it another turn after asking.
  function activate(item) {
    for (var i = 0; i < 50 && !item.Window.window; i++) wait(10)
    if (!item.Window.window) return false
    item.Window.window.requestActivate()
    for (var j = 0; j < 50 && !item.Window.active; j++) wait(10)
    return item.Window.active
  }

  // A frame and then some: a person's keys are never closer than that.
  readonly property int frame: 20

  // press clicks key (a Qt.Key_*) with modifiers; type clicks each
  // character of text. Both let a frame pass before and after, so queued
  // work (a focus handed back with Qt.callLater) and layouts (a pane that
  // just appeared) are done, as they would be for a person typing.
  function press(key, modifiers) {
    wait(frame)
    var ok = events.keyClick(key, modifiers || Qt.NoModifier, -1)
    wait(frame)
    return ok
  }

  function type(text) {
    wait(frame)
    for (var i = 0; i < text.length; i++) events.keyClickChar(text[i], Qt.NoModifier, -1)
    wait(frame)
  }
}
