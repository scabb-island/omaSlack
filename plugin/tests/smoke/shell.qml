import QtQuick
import Quickshell
import qs.Commons
import qs.Ui
import "omaslack/tests" as T
// Imported so quickshell's scanner maps every plugin and smoke file into
// its qs:@ tree; the smokes are then created by name at runtime.
import "omaslack" as Plugin
import "omaslack/tests/smoke" as Smokes

// Smoke harness for the views: views import qs.Ui and Quickshell, which
// only load inside quickshell, not in qmltestrunner. run.sh builds a config
// dir where qs.* points at the installed omarchy-shell and omaslack/ at the
// plugin, copies this file in as its shell.qml and runs it offscreen.
//
// Each *Smoke.qml under tests/smoke is an Item with `property var service`
// and `function run()` returning a list of failure messages ([] = pass).
// The harness prints "SMOKE PASS <name>", "SMOKE FAIL <name>: <message>" and
// finally "SMOKE DONE"; run.sh turns that into an exit status.
ShellRoot {
  T.FakeService { id: fake }

  Component.onCompleted: {
    var names = String(Quickshell.env("OMASLACK_SMOKE") || "").split(",").filter(function(n) { return n !== "" })
    for (var i = 0; i < names.length; i++) {
      var name = names[i]
      var comp = Qt.createComponent("omaslack/tests/smoke/" + name)
      if (comp.status !== Component.Ready) {
        console.log("SMOKE FAIL " + name + ": " + comp.errorString().replace(/\n/g, " "))
        continue
      }
      fake.reset()
      var obj = comp.createObject(null, { service: fake })
      if (!obj) {
        console.log("SMOKE FAIL " + name + ": createObject returned null")
        continue
      }
      var failures = []
      try { failures = obj.run() || [] } catch (e) { failures = ["threw: " + e] }
      if (failures.length === 0) console.log("SMOKE PASS " + name)
      for (var f = 0; f < failures.length; f++) console.log("SMOKE FAIL " + name + ": " + failures[f])
      obj.destroy()
    }
    console.log("SMOKE DONE")
  }
}
