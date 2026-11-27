.pragma library

// JSON-RPC 2.0 over NDJSON, as the daemon speaks it (internal/rpc). Pure
// functions: Service.qml owns the socket and feeds lines in.
//
// CONTRACT (M4 step 0): signatures fixed; bodies land with task 4.2.

// newClient returns an empty client state: the next request id and the
// callbacks waiting for a response, keyed by id.
function newClient() {
  return { nextId: 1, pending: ({}) }
}

// request builds one request line (with the trailing "\n") and records its
// callback. Returns { client, line }.
function request(client, method, params, callback) {
  return { client: client, line: "" }
}

// receive parses one line from the daemon. Returns
// { client, kind, ... } where kind is:
//   "response"     with callback, error, result (callback may be null)
//   "notification" with method, params
//   "invalid"      with reason (the line is dropped and logged)
function receive(client, line) {
  return { client: client, kind: "invalid", reason: "not implemented" }
}

// failAll answers every pending callback with error (the socket closed).
// Returns { client, callbacks: [function] } with the callbacks to call.
function failAll(client, error) {
  return { client: newClient(), callbacks: [] }
}
