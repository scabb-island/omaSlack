import QtQuick
import QtTest
import "../lib/asks.js" as Asks

// lib/asks.js: which user IDs the Service has asked users.info for. Each
// is asked once; after a failed batch, again once a backoff has passed.
TestCase {
  name: "Asks"

  function test_a_new_id_is_asked_once() {
    var l = Asks.empty()
    verify(Asks.shouldAsk(l, "T1/U1", 0))
    Asks.asked(l, "T1/U1")
    verify(!Asks.shouldAsk(l, "T1/U1", 0))
    verify(!Asks.shouldAsk(l, "T1/U1", 1e12)) // answered or in flight: never again
  }

  // A failed batch used to leave its IDs marked forever, so those names
  // never came. Now they are asked again, after a delay.
  function test_a_failed_batch_is_asked_again_after_a_backoff() {
    var l = Asks.empty()
    Asks.asked(l, "T1/U1")
    Asks.asked(l, "T1/U2")
    compare(Asks.failed(l, ["T1/U1", "T1/U2"], 5000), 1000)
    verify(!Asks.shouldAsk(l, "T1/U1", 5999))
    verify(Asks.shouldAsk(l, "T1/U1", 6000))
    verify(Asks.shouldAsk(l, "T1/U2", 6000))
  }

  // A failure that persists does not loop: each one doubles the wait,
  // up to the reconnection backoff's maximum.
  function test_repeated_failures_back_off() {
    var l = Asks.empty(), now = 0, waits = []
    for (var i = 0; i < 7; i++) {
      Asks.asked(l, "T1/U1")
      var wait = Asks.failed(l, ["T1/U1"], now)
      waits.push(wait)
      verify(!Asks.shouldAsk(l, "T1/U1", now + wait - 1))
      now += wait
      verify(Asks.shouldAsk(l, "T1/U1", now))
    }
    compare(waits, [1000, 2000, 4000, 8000, 16000, 30000, 30000])
  }

  function test_failed_returns_the_shortest_wait() {
    var l = Asks.empty()
    Asks.asked(l, "T1/U1")
    Asks.failed(l, ["T1/U1"], 0) // U1 now waits 2 s on its next failure
    Asks.asked(l, "T1/U1")
    Asks.asked(l, "T1/U2")
    compare(Asks.failed(l, ["T1/U1", "T1/U2"], 0), 1000)
    compare(Asks.failed(l, [], 0), 0)
  }
}
