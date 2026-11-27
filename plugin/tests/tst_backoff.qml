import QtQuick
import QtTest
import "../lib/backoff.js" as Backoff

// lib/backoff.js: reconnection delays from 1 s doubling to 30 s.
TestCase {
  name: "Backoff"

  function test_first_attempt_waits_the_minimum() {
    compare(Backoff.next(0), 1000)
  }

  function test_doubles_after_each_failure() {
    compare(Backoff.next(1000), 2000)
    compare(Backoff.next(2000), 4000)
    compare(Backoff.next(8000), 16000)
  }

  function test_caps_at_the_maximum() {
    compare(Backoff.next(16000), 30000)
    compare(Backoff.next(30000), 30000)
  }

  function test_never_below_the_minimum() {
    compare(Backoff.next(-5), 1000)
    compare(Backoff.next(200), 1000)
  }

  function test_sequence_spreads_attempts() {
    var d = 0, seen = []
    for (var i = 0; i < 7; i++) { d = Backoff.next(d); seen.push(d) }
    compare(seen, [1000, 2000, 4000, 8000, 16000, 30000, 30000])
  }
}
