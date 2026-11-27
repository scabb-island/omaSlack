.pragma library

// Reconnection delays: 1 s doubling up to 30 s, so a stopped daemon does
// not flood the shell log with socket warnings (spike M4.1).

var MIN_MS = 1000
var MAX_MS = 30000

// next returns the delay before the attempt after one that waited previous
// milliseconds (0 for the first attempt).
function next(previous) {
  return Math.min(MAX_MS, Math.max(MIN_MS, previous * 2))
}
