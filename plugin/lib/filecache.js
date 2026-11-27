.pragma library

// Local paths of files fetched through the daemon (files.fetch), shared
// by every view that shows or opens one: a thumbnail scrolled out and
// back, or shown in both the conversation and its thread, asks once.
// The daemon keeps the bytes in its cache; this keeps only the paths. A
// failed fetch is not remembered, so the next view asks again.

var paths = ({})
var waiting = ({})

function clear() { paths = ({}); waiting = ({}) }

// path calls done(localPath) with the file id of team's local path, ""
// when the daemon could not fetch it. service is the Service (fetchFile).
function path(service, team, id, done) {
  var key = team + "/" + id
  if (paths.hasOwnProperty(key)) { done(paths[key]); return }
  if (waiting[key]) { waiting[key].push(done); return }
  waiting[key] = [done]
  service.fetchFile(team, id, function(err, res) {
    var callers = waiting[key] || []
    delete waiting[key]
    var p = !err && res && res.localPath ? String(res.localPath) : ""
    if (p !== "") paths[key] = p
    callers.forEach(function(cb) { cb(p) })
  })
}
