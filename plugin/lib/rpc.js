.pragma library

// JSON-RPC 2.0 over NDJSON, as the daemon speaks it (internal/rpc). Pure
// functions: Service.qml owns the socket and feeds lines in. A client
// state is never mutated; each function returns the next one.

// PROTOCOL is the major protocol version this client speaks in hello; keep
// it equal to internal/version.Protocol.
var PROTOCOL = 1
// INCOMPATIBLE is the error code hello answers when the daemon does not
// speak PROTOCOL (rpc.CodeIncompatibleProtocol).
var INCOMPATIBLE = -32001

// newClient returns an empty client state: the next request id and the
// callbacks waiting for a response, keyed by id.
function newClient() {
  return { nextId: 1, pending: ({}) }
}

function withPending(client, edit) {
  var pending = ({})
  for (var k in client.pending) pending[k] = client.pending[k]
  edit(pending)
  return pending
}

// request builds one request line (with the trailing "\n") and records its
// callback. Returns { client, line }.
function request(client, method, params, callback) {
  var id = client.nextId
  var msg = { jsonrpc: "2.0", id: id, method: method }
  if (params !== undefined && params !== null) msg.params = params
  var pending = withPending(client, function(p) { p[id] = callback || null })
  return { client: { nextId: id + 1, pending: pending }, line: JSON.stringify(msg) + "\n" }
}

// receive parses one line from the daemon. Returns
// { client, kind, ... } where kind is:
//   "response"     with callback, error, result (callback may be null)
//   "notification" with method, params
//   "invalid"      with reason (the line is dropped and logged)
function receive(client, line) {
  var msg
  try { msg = JSON.parse(line) } catch (e) {
    return { client: client, kind: "invalid", reason: "not JSON" }
  }
  if (msg === null || typeof msg !== "object" || Array.isArray(msg))
    return { client: client, kind: "invalid", reason: "not a JSON object" }
  if (typeof msg.method === "string" && msg.id === undefined)
    return { client: client, kind: "notification", method: msg.method, params: msg.params || ({}) }
  if (msg.id !== undefined && msg.method === undefined && ("result" in msg || "error" in msg))
    return response(client, msg)
  return { client: client, kind: "invalid", reason: "neither a response nor a notification" }
}

function response(client, msg) {
  var callback = client.pending[msg.id] || null
  var pending = withPending(client, function(p) { delete p[msg.id] })
  return {
    client: { nextId: client.nextId, pending: pending },
    kind: "response",
    callback: callback,
    error: msg.error || null,
    result: msg.error || msg.result === undefined ? null : msg.result
  }
}

// failAll answers every pending callback with error (the socket closed).
// Returns { client, callbacks: [function] }: call each with no arguments
// and it calls its callback with (error, null).
function failAll(client, error) {
  var callbacks = []
  for (var id in client.pending) {
    var cb = client.pending[id]
    if (cb) callbacks.push(answer(cb, error))
  }
  return { client: { nextId: client.nextId, pending: ({}) }, callbacks: callbacks }
}

function answer(cb, error) {
  return function() { cb(error, null) }
}
