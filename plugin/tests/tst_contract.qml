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
    "pluginId", "socketPath", "shell", "connectionState", "daemonAvailable", "connectionError", "setupNeed",
    "workspaces", "summary", "conversations", "dnd", "presence", "messagesRevision"
  ]
  readonly property var functions: [
    "messages", "hasMoreHistory", "unreadConversations", "userName", "conversationName",
    "call", "openConversation", "loadOlder", "loadReplies", "send", "edit", "remove", "react",
    "markRead", "setDraft", "snoozeDND", "endDND", "setPresence", "fetchFile",
    "openWindow", "openTui", "openPreferred", "reconnect", "runSetupAction"
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

  // runSetupAction runs the action of setupNeed (lib/install.js).
  function test_run_setup_action_is_recorded() {
    fake.reset()
    fake.setupNeed = "install"
    fake.runSetupAction()
    compare(fake.calls.length, 1)
    compare(fake.calls[0].method, "runSetupAction")
    compare(fake.calls[0].params.need, "install")
    fake.setupNeed = ""
  }

  // presence.set takes "away" or "auto" (internal/api/methods.go).
  function test_set_presence_sends_away_or_auto() {
    fake.reset()
    fake.setPresence("T1", true, null)
    fake.setPresence("T1", false, null)
    compare(fake.calls[0].method, "presence.set")
    compare(fake.calls[0].params.presence, "away")
    compare(fake.calls[1].params.presence, "auto")
  }

  // files.fetch names the file "fileId" (internal/api/files.go).
  function test_fetch_file_names_the_file_id() {
    fake.reset()
    fake.fetchFile("T1", "F1", null)
    compare(fake.calls[0].method, "files.fetch")
    compare(fake.calls[0].params.fileId, "F1")
  }
}
