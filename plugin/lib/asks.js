.pragma library
.import "backoff.js" as Backoff

// Which user IDs the Service has asked users.info for (task 5.8), so each
// is asked once and a failed batch is asked again later instead of never.
// A ledger is { "<team>/<user>": { wait, retryAt } }: retryAt is 0 while
// a batch is in flight or once it was answered, and the epoch ms after
// which a failed one may be asked again. wait is the last backoff, so a
// failure that persists waits longer each time (lib/backoff.js).
//
// Unlike lib/store.js, these functions change the ledger in place: the
// Service asks it inside bindings, where copying it every call would
// cost, and no binding reads it.

function empty() { return ({}) }

// shouldAsk: key was never asked, or its last batch failed and the wait
// is over.
function shouldAsk(ledger, key, now) {
  var e = ledger[key]
  return !e || (e.retryAt > 0 && now >= e.retryAt)
}

// asked records that key is being asked.
function asked(ledger, key) {
  var e = ledger[key]
  ledger[key] = { wait: e ? e.wait : 0, retryAt: 0 }
}

// failed records that the batch asking keys failed at now, and returns
// the shortest wait before one of them may be asked again (0 for none).
function failed(ledger, keys, now) {
  var shortest = 0
  keys.forEach(function(key) {
    var e = ledger[key]
    var wait = Backoff.next(e ? e.wait : 0)
    ledger[key] = { wait: wait, retryAt: now + wait }
    shortest = shortest ? Math.min(shortest, wait) : wait
  })
  return shortest
}
