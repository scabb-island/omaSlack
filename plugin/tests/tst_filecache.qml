import QtQuick
import QtTest
import "../lib/filecache.js" as Cache

// lib/filecache.js: files.fetch once per file for every thumbnail that
// shows it, retried after a failure.
TestCase {
  name: "FileCache"

  // A service whose fetchFile answers when the test says so.
  function fakeService() {
    var s = { asked: [], pending: [] }
    s.fetchFile = function(team, id, cb) { s.asked.push(team + "/" + id); s.pending.push(cb) }
    s.answer = function(err, res) { var cb = s.pending.shift(); cb(err, res) }
    return s
  }

  function init() { Cache.clear() }

  function test_one_fetch_serves_every_caller_and_later_ones() {
    var s = fakeService()
    var got = []
    Cache.path(s, "T1", "F1", function(p) { got.push("a:" + p) })
    Cache.path(s, "T1", "F1", function(p) { got.push("b:" + p) })
    compare(s.asked, ["T1/F1"])
    s.answer(null, { localPath: "/cache/f1.png" })
    compare(got, ["a:/cache/f1.png", "b:/cache/f1.png"])
    Cache.path(s, "T1", "F1", function(p) { got.push("c:" + p) })
    compare(s.asked.length, 1)
    compare(got[2], "c:/cache/f1.png")
  }

  function test_a_failure_answers_empty_and_is_asked_again() {
    var s = fakeService()
    var got = []
    Cache.path(s, "T1", "F2", function(p) { got.push(p) })
    s.answer({ code: -32602, message: "too large" }, null)
    compare(got, [""])
    Cache.path(s, "T1", "F2", function(p) { got.push(p) })
    compare(s.asked, ["T1/F2", "T1/F2"])
  }

  function test_files_are_kept_per_workspace() {
    var s = fakeService()
    Cache.path(s, "T1", "F1", function() {})
    Cache.path(s, "T2", "F1", function() {})
    compare(s.asked, ["T1/F1", "T2/F1"])
  }
}
