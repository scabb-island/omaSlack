import QtQuick
import Quickshell
import Quickshell.Io

// The one connection between omarchy-shell and the omaslack daemon
// (spec gui-client, "Service único como cliente del daemon"). The shell mounts
// this once and hands it to every screen's bar widget, so the bar, the panel
// and the window always read the same state.
//
// M0 skeleton: it only probes whether the daemon socket exists. The real
// JSON-RPC client lands in M4, after the M4.1 socket spike.
Item {
  id: root

  readonly property string socketPath: (Quickshell.env("XDG_RUNTIME_DIR") || "/tmp") + "/omaslack.sock"

  // "unknown" until the first probe, then "available" or "unavailable".
  property string daemonState: "unknown"
  readonly property bool daemonAvailable: daemonState === "available"

  function probe() {
    if (!probeProc.running) probeProc.running = true
  }

  Process {
    id: probeProc
    command: ["test", "-S", root.socketPath]
    onExited: function(exitCode) {
      root.daemonState = exitCode === 0 ? "available" : "unavailable"
    }
  }

  Timer {
    interval: 30000
    running: true
    repeat: true
    triggeredOnStart: true
    onTriggered: root.probe()
  }
}
