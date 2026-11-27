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

  // press clicks key (a Qt.Key_*) with modifiers; type clicks each
  // character of text. Both then let queued work run.
  function press(key, modifiers) {
    var ok = events.keyClick(key, modifiers || Qt.NoModifier, -1)
    wait(1)
    return ok
  }

  function type(text) {
    for (var i = 0; i < text.length; i++) events.keyClickChar(text[i], Qt.NoModifier, -1)
    wait(1)
  }
}
