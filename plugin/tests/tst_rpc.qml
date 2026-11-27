import QtQuick
import QtTest
import "../lib/rpc.js" as Rpc

// lib/rpc.js: JSON-RPC 2.0 framing and request/response correlation.
TestCase {
  name: "Rpc"

  function noop() {}

  function test_request_builds_one_ndjson_line() {
    var r = Rpc.request(Rpc.newClient(), "hello", { client: "omaslack-plugin", protocol: 1 }, noop)
    compare(r.line.charAt(r.line.length - 1), "\n")
    compare(r.line.indexOf("\n"), r.line.length - 1)
    var msg = JSON.parse(r.line)
    compare(msg.jsonrpc, "2.0")
    compare(msg.id, 1)
    compare(msg.method, "hello")
    compare(msg.params.protocol, 1)
  }

  function test_request_omits_absent_params() {
    var r = Rpc.request(Rpc.newClient(), "workspaces.list", undefined, null)
    verify(!("params" in JSON.parse(r.line)))
  }

  function test_ids_increase_and_the_input_client_is_not_mutated() {
    var c0 = Rpc.newClient()
    var r1 = Rpc.request(c0, "a", {}, noop)
    var r2 = Rpc.request(r1.client, "b", {}, noop)
    compare(JSON.parse(r2.line).id, 2)
    compare(c0.nextId, 1)
    compare(Object.keys(c0.pending).length, 0)
    compare(Object.keys(r2.client.pending).length, 2)
  }

  function test_response_finds_its_callback_once() {
    var got = null
    var cb = function(err, res) { got = res }
    var r = Rpc.request(Rpc.newClient(), "unread.summary", {}, cb)
    var out = Rpc.receive(r.client, '{"jsonrpc":"2.0","id":1,"result":{"total":{"dms":2}}}')
    compare(out.kind, "response")
    compare(out.callback, cb)
    compare(out.error, null)
    compare(out.result.total.dms, 2)
    compare(Object.keys(out.client.pending).length, 0)
    var again = Rpc.receive(out.client, '{"jsonrpc":"2.0","id":1,"result":{}}')
    compare(again.kind, "response")
    compare(again.callback, null)
  }

  function test_error_response() {
    var r = Rpc.request(Rpc.newClient(), "hello", {}, noop)
    var out = Rpc.receive(r.client, '{"jsonrpc":"2.0","id":1,"error":{"code":-32001,"message":"incompatible protocol"}}')
    compare(out.kind, "response")
    compare(out.error.code, -32001)
    compare(out.result, null)
  }

  function test_null_result_is_null() {
    var r = Rpc.request(Rpc.newClient(), "drafts.set", {}, noop)
    var out = Rpc.receive(r.client, '{"jsonrpc":"2.0","id":1,"result":null}')
    compare(out.error, null)
    compare(out.result, null)
  }

  function test_notification() {
    var out = Rpc.receive(Rpc.newClient(), '{"jsonrpc":"2.0","method":"read.changed","params":{"team":"T1","conv":"C1","lastRead":"1.000002"}}')
    compare(out.kind, "notification")
    compare(out.method, "read.changed")
    compare(out.params.conv, "C1")
  }

  function test_notification_without_params_gets_an_object() {
    var out = Rpc.receive(Rpc.newClient(), '{"jsonrpc":"2.0","method":"ping"}')
    compare(out.kind, "notification")
    compare(typeof out.params, "object")
  }

  function test_invalid_lines() {
    var c = Rpc.newClient()
    compare(Rpc.receive(c, "not json").kind, "invalid")
    compare(Rpc.receive(c, "[1,2]").kind, "invalid")
    compare(Rpc.receive(c, '{"jsonrpc":"2.0"}').kind, "invalid")
    compare(Rpc.receive(c, "").kind, "invalid")
    verify(Rpc.receive(c, "nope").reason.length > 0)
  }

  function test_fail_all_answers_every_pending_callback_with_the_error() {
    var errs = []
    var cb = function(err) { errs.push(err) }
    var r = Rpc.request(Rpc.newClient(), "a", {}, cb)
    r = Rpc.request(r.client, "b", {}, null)
    r = Rpc.request(r.client, "c", {}, cb)
    var out = Rpc.failAll(r.client, { code: -32000, message: "closed" })
    compare(out.callbacks.length, 2)
    compare(Object.keys(out.client.pending).length, 0)
    for (var i = 0; i < out.callbacks.length; i++) out.callbacks[i]()
    compare(errs.length, 2)
    compare(errs[0].code, -32000)
  }
}
