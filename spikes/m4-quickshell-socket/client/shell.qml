// Client side of spike M4.1, run as a standalone Quickshell config:
//
//   quickshell -p spikes/m4-quickshell-socket/client/shell.qml
//
// It drives the spike server through every phase, measures, prints one line
// "RESULT {json}" and quits. No windows: it only exercises Quickshell.Io.
import QtQuick
import Quickshell
import Quickshell.Io

ShellRoot {
  id: root

  readonly property string socketPath: Quickshell.env("OMASLACK_SPIKE_SOCKET")
    || ((Quickshell.env("XDG_RUNTIME_DIR") || "/tmp") + "/omaslack-spike.sock")
  readonly property int burstCount: parseInt(Quickshell.env("OMASLACK_SPIKE_BURST") || "20000")

  property string phase: "connecting"
  property int nextId: 1
  property var pending: ({})          // id -> {method, sentAt}
  property var results: ({ connectAttempts: 0, parseErrors: 0, unexpected: [] })

  // burst
  property int burstReceived: 0
  property int burstExpectedSeq: 0
  property int burstOutOfOrder: 0
  property real burstTextBytes: 0
  property real burstMaxLine: 0
  property real burstStart: 0
  // event-loop responsiveness
  property real lastTick: 0
  property real maxGap: 0
  property bool measuringGap: false
  // ping
  property var rtts: []
  property int pingsLeft: 0
  // trickle
  property int ticksReceived: 0
  property int ticksOutOfOrder: 0
  property var trickleRtts: []
  property bool trickleDone: false
  // reconnect
  property real dropAt: 0

  function utf8Len(s) {
    var n = 0
    for (var i = 0; i < s.length; i++) {
      var c = s.charCodeAt(i)
      if (c < 0x80) n += 1
      else if (c < 0x800) n += 2
      else if (c >= 0xD800 && c <= 0xDBFF) { n += 4; i++ }
      else n += 3
    }
    return n
  }

  function send(method, params) {
    var id = nextId++
    pending[id] = { method: method, sentAt: Date.now() }
    var msg = { jsonrpc: "2.0", id: id, method: method }
    if (params !== undefined) msg.params = params
    sock.write(JSON.stringify(msg) + "\n")
    sock.flush()
    return id
  }

  function stats(list) {
    if (list.length === 0) return null
    var s = list.slice().sort(function(a, b) { return a - b })
    function pct(p) { return s[Math.min(s.length - 1, Math.floor(p * s.length))] }
    return { n: s.length, p50: pct(0.5), p95: pct(0.95), p99: pct(0.99), max: s[s.length - 1] }
  }

  function finish(ok, reason) {
    results.ok = ok
    if (reason) results.reason = reason
    results.phase = phase
    console.log("RESULT " + JSON.stringify(results))
    Qt.quit()
  }

  function onLine(line) {
    var msg
    try { msg = JSON.parse(line) } catch (e) {
      results.parseErrors++
      return
    }
    if (msg.method === "event") {
      var p = msg.params
      if (p.seq !== burstExpectedSeq) burstOutOfOrder++
      burstExpectedSeq = p.seq + 1
      burstReceived++
      burstTextBytes += utf8Len(p.text)
      burstMaxLine = Math.max(burstMaxLine, line.length)
      return
    }
    if (msg.method === "tick") {
      if (msg.params.seq !== ticksReceived) ticksOutOfOrder++
      ticksReceived++
      return
    }
    var req = pending[msg.id]
    if (!req) { results.unexpected.push(line.slice(0, 120)); return }
    delete pending[msg.id]
    var rtt = Date.now() - req.sentAt
    handleResult(req.method, msg, rtt)
  }

  function handleResult(method, msg, rtt) {
    if (msg.error) { finish(false, method + " error: " + JSON.stringify(msg.error)); return }
    if (method === "hello" && phase === "connecting") {
      results.helloMs = rtt
      phase = "burst"
      burstStart = Date.now()
      lastTick = Date.now(); maxGap = 0; measuringGap = true
      send("burst", { count: burstCount })
    } else if (method === "burst") {
      measuringGap = false
      var ms = Date.now() - burstStart
      results.burst = {
        requested: burstCount, received: burstReceived, outOfOrder: burstOutOfOrder,
        textBytesClient: burstTextBytes, textBytesServer: msg.result.textBytes,
        maxLineChars: burstMaxLine, ms: ms,
        eventsPerSec: Math.round(burstReceived / (ms / 1000)),
        maxEventLoopGapMs: maxGap
      }
      phase = "ping"
      pingsLeft = 200
      send("ping")
    } else if (method === "ping" && phase === "ping") {
      rtts.push(rtt)
      if (--pingsLeft > 0) { send("ping"); return }
      results.pingRttMs = stats(rtts)
      phase = "trickle"
      send("trickle", { everyMs: 5, n: 400 })
      trickleTimer.start()
    } else if (method === "ping" && phase === "trickle") {
      trickleRtts.push(rtt)
    } else if (method === "trickle") {
      trickleTimer.stop()
      results.trickle = { expected: 400, received: ticksReceived, outOfOrder: ticksOutOfOrder,
        pingRttDuringTrickleMs: stats(trickleRtts) }
      phase = "drop"
      dropAt = Date.now()
      send("drop")
    } else if (method === "hello" && phase === "reconnect") {
      results.reconnect = { ok: true, msFromDropToHello: Date.now() - dropAt }
      var b = results.burst
      var ok = b.received === b.requested && b.outOfOrder === 0
        && b.textBytesClient === b.textBytesServer && results.parseErrors === 0
        && results.trickle.received === 400 && results.trickle.outOfOrder === 0
        && results.unexpected.length === 0
      finish(ok, ok ? "" : "integrity check failed")
    }
  }

  Socket {
    id: sock
    path: root.socketPath
    connected: true

    parser: SplitParser {
      splitMarker: "\n"
      onRead: function(data) { root.onLine(data) }
    }

    onConnectionStateChanged: {
      if (connected) {
        root.results.connectAttempts++
        if (root.phase === "connecting" || root.phase === "reconnect") root.send("hello")
      } else if (root.phase === "drop") {
        root.results.disconnectDetectedMs = Date.now() - root.dropAt
        root.phase = "reconnect"
        reconnectTimer.start()
      }
    }
    onError: function(e) {
      // Socket not there yet (server starts late) or gone: retry.
      if (root.phase === "connecting" || root.phase === "reconnect") reconnectTimer.start()
    }
  }

  Timer {
    id: reconnectTimer
    interval: 250
    onTriggered: {
      root.results.retries = (root.results.retries || 0) + 1
      sock.connected = false
      sock.connected = true
    }
  }

  Timer {
    id: trickleTimer
    interval: 20
    repeat: true
    onTriggered: root.send("ping")
  }

  // Event-loop stall probe: a 16 ms tick; the largest gap seen while the burst
  // is arriving is how long the shell's UI thread would have been frozen.
  Timer {
    interval: 16
    repeat: true
    running: true
    onTriggered: {
      var now = Date.now()
      if (root.measuringGap) root.maxGap = Math.max(root.maxGap, now - root.lastTick)
      root.lastTick = now
    }
  }

  Timer {
    interval: 120000
    running: true
    onTriggered: root.finish(false, "timeout")
  }
}
