.pragma library

// What the plugin offers while the daemon does not answer (spec
// aur-packaging, "The plugin checks for the package", "The plugin offers to
// install the package", "The plugin explains how to start the daemon").
// The plugin ships without the omaslack binary: the manifest cannot declare
// a package and `omarchy plugin add` runs no hooks. So after each failed
// connection attempt Service.qml runs PROBE and turns its output into a
// need with parseProbe() and need(); the views show action(need). Pure, so
// tests/tst_install.qml covers it.
//
// Nothing here installs anything on its own: every package action runs in a
// visible terminal the user authorizes.

var PACKAGE = "omaslack-bin"
var LAUNCH = "omarchy-launch-floating-terminal-with-presentation"

// PROBE is the sh script the Service runs: whether omaslack-bin (or any
// omaslack on PATH, such as a source build) is installed, whether the user
// service is enabled, and the installed binary's version line (which ends
// in "protocol N", internal/version.String).
var PROBE = "p=0; u=0; v=\n"
  + "{ omarchy-pkg-present " + PACKAGE + " || command -v omaslack; } >/dev/null 2>&1 && p=1\n"
  + "command -v omaslack >/dev/null 2>&1 && v=$(omaslack version 2>/dev/null | head -n 1)\n"
  + "systemctl --user is-enabled --quiet omaslack 2>/dev/null && u=1\n"
  + "printf 'package=%s\\nunit=%s\\nversion=%s\\n' \"$p\" \"$u\" \"$v\"\n"

// shouldProbe: the probe runs after an attempt failed, in either way.
function shouldProbe(connectionState) {
  return connectionState === "unavailable" || connectionState === "incompatible"
}

// parseProbe reads PROBE's output, taken after an attempt that ended in
// state ("unavailable" or "incompatible"). protocol is 0 when unknown.
// returns: { state, packagePresent, unitEnabled, protocol }
function parseProbe(text, state) {
  var fields = {}
  String(text || "").split("\n").forEach(function(line) {
    var eq = line.indexOf("=")
    if (eq > 0) fields[line.slice(0, eq).trim()] = line.slice(eq + 1).trim()
  })
  var m = /protocol (\d+)/.exec(fields.version || "")
  return {
    state: state,
    packagePresent: fields.package === "1",
    unitEnabled: fields.unit === "1",
    protocol: m ? Number(m[1]) : 0
  }
}

// need names what the user has to do, from a probe and the protocol this
// plugin speaks: "" (nothing known yet), "install", "update",
// "update-plugin", "restart", "setup" or "start".
function need(probe, protocol) {
  if (!probe) return ""
  var known = probe.protocol > 0
  if (known && probe.protocol > protocol) return "update-plugin"
  if (probe.state === "incompatible") return known && probe.protocol === protocol ? "restart" : "update"
  if (!probe.packagePresent) return "install"
  if (known && probe.protocol < protocol) return "update"
  return probe.unitEnabled ? "start" : "setup"
}

// What each need shows and runs. title: the panel's one-line state; hint:
// what is wrong and the command, for the panel and the bar tooltip; label:
// the panel button ("" for none); argv: what the button runs
// (Quickshell.execDetached), null for none; reconnect: try the daemon
// again right after running it.
var ACTIONS = {
  "install": {
    title: "omaslack-bin not installed",
    hint: "The omaslack plugin needs the " + PACKAGE + " package from the AUR.\n"
      + "Install it with: omarchy-pkg-aur-add " + PACKAGE,
    label: "Install " + PACKAGE,
    argv: [LAUNCH, "omarchy-pkg-aur-add " + PACKAGE],
    reconnect: false
  },
  "update": {
    title: "omaslack-bin out of date",
    hint: "The installed omaslack is older than this plugin.\n"
      + "Update it with: yay -S --needed " + PACKAGE,
    label: "Update " + PACKAGE,
    argv: [LAUNCH, "yay -S --needed " + PACKAGE + " && systemctl --user try-restart omaslack"],
    reconnect: false
  },
  "update-plugin": {
    title: "Plugin out of date",
    hint: "The installed omaslack is newer than this plugin.\n"
      + "Update it with: omarchy plugin update ar.scabb.omaslack, then omarchy restart shell",
    label: "",
    argv: null,
    reconnect: false
  },
  "restart": {
    title: "Daemon out of date",
    hint: "omaslack was updated, but the running daemon is the old version.\n"
      + "Restart it with: systemctl --user restart omaslack",
    label: "Restart the daemon",
    argv: ["systemctl", "--user", "restart", "omaslack"],
    reconnect: true
  },
  "setup": {
    title: "omaslack not set up",
    hint: "omaslack is installed but not set up yet.\n"
      + "Set it up with: omaslack setup",
    label: "Set up omaslack",
    argv: [LAUNCH, "omaslack setup"],
    reconnect: false
  },
  "start": {
    title: "Daemon not running",
    hint: "The omaslack daemon is not running.\n"
      + "Start it with: systemctl --user start omaslack\n"
      + "If it keeps stopping, run: omaslack doctor",
    label: "Start the daemon",
    argv: ["systemctl", "--user", "start", "omaslack"],
    reconnect: true
  }
}

// action returns what need shows and runs, or null for no need.
// returns: { need, title, hint, label, argv, reconnect }
function action(need) {
  var a = Object.prototype.hasOwnProperty.call(ACTIONS, need) ? ACTIONS[need] : null
  if (!a) return null
  return { need: need, title: a.title, hint: a.hint, label: a.label, argv: a.argv ? a.argv.slice() : null, reconnect: a.reconnect }
}
