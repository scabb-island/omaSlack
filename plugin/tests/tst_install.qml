import QtQuick
import QtTest
import "../lib/install.js" as Install

// lib/install.js: what the plugin offers while the daemon does not answer
// (spec aur-packaging, "The plugin checks for the package", "The plugin
// offers to install the package", "The plugin explains how to start the
// daemon").
TestCase {
  name: "Install"

  function probe(state, text) { return Install.parseProbe(text, state) }

  // ---- when to probe ----------------------------------------------------------

  function test_probes_only_after_a_failed_attempt() {
    verify(Install.shouldProbe("unavailable"))
    verify(Install.shouldProbe("incompatible"))
    verify(!Install.shouldProbe("connected"))
    verify(!Install.shouldProbe("connecting"))
    verify(!Install.shouldProbe("unknown"))
  }

  // ---- parsing the probe ------------------------------------------------------

  function test_parses_the_probe_output() {
    var p = probe("unavailable", "package=1\nunit=0\nversion=omaslack 0.1.0 (commit abc, built now, protocol 1)\n")
    compare(p.state, "unavailable")
    compare(p.packagePresent, true)
    compare(p.unitEnabled, false)
    compare(p.protocol, 1)
  }

  function test_missing_lines_read_as_absent() {
    var p = probe("unavailable", "")
    compare(p.packagePresent, false)
    compare(p.unitEnabled, false)
    compare(p.protocol, 0)
  }

  function test_a_version_without_protocol_reads_as_unknown() {
    compare(probe("unavailable", "package=1\nversion=omaslack dev\n").protocol, 0)
  }

  // ---- the need ---------------------------------------------------------------

  function test_nothing_to_offer_without_a_probe() {
    compare(Install.need(null, 1), "")
  }

  function test_missing_package_asks_to_install() {
    compare(Install.need(probe("unavailable", "package=0\nunit=0\n"), 1), "install")
  }

  function test_an_older_binary_asks_to_update() {
    compare(Install.need(probe("unavailable", "package=1\nunit=1\nversion=omaslack 0.1.0 (protocol 1)\n"), 2), "update")
  }

  function test_a_newer_binary_asks_to_update_the_plugin() {
    compare(Install.need(probe("unavailable", "package=1\nunit=1\nversion=omaslack 9 (protocol 2)\n"), 1), "update-plugin")
    compare(Install.need(probe("incompatible", "package=1\nunit=1\nversion=omaslack 9 (protocol 2)\n"), 1), "update-plugin")
  }

  function test_present_but_not_set_up_asks_for_setup() {
    compare(Install.need(probe("unavailable", "package=1\nunit=0\nversion=omaslack 0.1.0 (protocol 1)\n"), 1), "setup")
  }

  function test_present_and_enabled_asks_to_start() {
    compare(Install.need(probe("unavailable", "package=1\nunit=1\nversion=omaslack 0.1.0 (protocol 1)\n"), 1), "start")
  }

  // A daemon refused hello but the binary on disk now matches: it was
  // updated and the running daemon is the old one.
  function test_incompatible_with_a_matching_binary_asks_to_restart() {
    compare(Install.need(probe("incompatible", "package=1\nunit=1\nversion=omaslack 0.2.0 (protocol 1)\n"), 1), "restart")
  }

  function test_incompatible_with_an_older_binary_asks_to_update() {
    compare(Install.need(probe("incompatible", "package=1\nunit=1\nversion=omaslack 0.1.0 (protocol 1)\n"), 2), "update")
  }

  // The daemon answered, so something is installed even when the probe
  // could not tell which version (a custom build on PATH).
  function test_incompatible_without_a_version_asks_to_update() {
    compare(Install.need(probe("incompatible", "package=0\n"), 1), "update")
  }

  // ---- actions ----------------------------------------------------------------

  function test_no_action_without_a_need() {
    compare(Install.action(""), null)
    compare(Install.action("nonsense"), null)
  }

  function test_install_runs_the_aur_helper_in_a_visible_terminal() {
    var a = Install.action("install")
    compare(a.label, "Install omaslack-bin")
    compare(a.argv, ["omarchy-launch-floating-terminal-with-presentation", "omarchy-pkg-aur-add omaslack-bin"])
  }

  // omarchy-pkg-aur-add skips installed packages, so it cannot update.
  function test_update_runs_yay_then_restarts_a_running_daemon() {
    var a = Install.action("update")
    compare(a.label, "Update omaslack-bin")
    compare(a.argv, ["omarchy-launch-floating-terminal-with-presentation",
                     "yay -S --needed omaslack-bin && systemctl --user try-restart omaslack"])
  }

  function test_setup_runs_in_a_visible_terminal() {
    var a = Install.action("setup")
    compare(a.label, "Set up omaslack")
    compare(a.argv, ["omarchy-launch-floating-terminal-with-presentation", "omaslack setup"])
  }

  function test_start_and_restart_run_systemctl_and_reconnect() {
    var start = Install.action("start")
    compare(start.argv, ["systemctl", "--user", "start", "omaslack"])
    verify(start.reconnect)
    var restart = Install.action("restart")
    compare(restart.argv, ["systemctl", "--user", "restart", "omaslack"])
    verify(restart.reconnect)
    verify(!Install.action("install").reconnect)
  }

  function test_updating_the_plugin_is_only_explained() {
    var a = Install.action("update-plugin")
    compare(a.argv, null)
    compare(a.label, "")
    verify(a.hint.indexOf("omarchy plugin update ar.scabb.omaslack") >= 0, a.hint)
  }

  function test_every_action_has_a_title_and_a_hint() {
    var needs = ["install", "update", "update-plugin", "restart", "setup", "start"]
    for (var i = 0; i < needs.length; i++) {
      var a = Install.action(needs[i])
      verify(a.title !== "", needs[i])
      verify(a.hint !== "", needs[i])
      compare(a.need, needs[i])
    }
  }

  // The tooltip names the command, so it helps without opening the panel.
  function test_start_hint_names_the_command() {
    verify(Install.action("start").hint.indexOf("systemctl --user start omaslack") >= 0)
  }
}
