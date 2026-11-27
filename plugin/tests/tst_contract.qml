import QtQuick
import QtTest

// FakeService must keep Service.qml's public API, so view tests built on it
// hold for the real Service. Service.qml needs Quickshell, which
// qmltestrunner cannot load, so its members are listed here; update both
// files and this list together.
TestCase {
  name: "ServiceContract"

  FakeService { id: fake }

  readonly property var properties: [
    "pluginId", "socketPath", "shell", "connectionState", "daemonAvailable", "connectionError",
    "workspaces", "summary", "conversations", "dnd", "messagesRevision"
  ]
  readonly property var functions: [
    "messages", "hasMoreHistory", "unreadConversations", "userName", "conversationName",
    "call", "openConversation", "loadOlder", "loadReplies", "send", "edit", "remove", "react",
    "markRead", "setDraft", "snoozeDND", "endDND", "fetchFile",
    "openWindow", "openTui", "openPreferred", "reconnect"
  ]
  readonly property var signalNames: ["daemonEvent", "messagesUpdated", "focusRequested"]

  function test_properties() {
    for (var i = 0; i < properties.length; i++)
      verify(properties[i] in fake, properties[i])
  }

  function test_functions() {
    for (var i = 0; i < functions.length; i++)
      compare(typeof fake[functions[i]], "function", functions[i])
  }

  function test_signals() {
    for (var i = 0; i < signalNames.length; i++)
      verify(fake[signalNames[i]] !== undefined, signalNames[i])
  }

  function test_calls_are_recorded() {
    fake.reset()
    fake.send("T1", "C1", "hi", "", null)
    compare(fake.calls.length, 1)
    compare(fake.calls[0].method, "messages.send")
    compare(fake.calls[0].params.text, "hi")
  }
}
