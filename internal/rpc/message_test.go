package rpc

import (
	"encoding/json"
	"testing"
)

func TestParseRequestAcceptsValidRequests(t *testing.T) {
	cases := map[string]struct {
		line         string
		id, params   string
		notification bool
	}{
		"numeric id":    {`{"jsonrpc":"2.0","id":1,"method":"m"}`, "1", "", false},
		"string id":     {`{"jsonrpc":"2.0","id":"a","method":"m","params":{"x":1}}`, `"a"`, `{"x":1}`, false},
		"negative id":   {`{"jsonrpc":"2.0","id":-3,"method":"m"}`, "-3", "", false},
		"null id":       {`{"jsonrpc":"2.0","id":null,"method":"m","params":[1]}`, "null", "[1]", false},
		"notification":  {`{"jsonrpc":"2.0","method":"m"}`, "", "", true},
		"unknown field": {`{"jsonrpc":"2.0","id":2,"method":"m","extra":true}`, "2", "", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req, rpcErr := parseRequest([]byte(tc.line))
			if rpcErr != nil {
				t.Fatalf("parseRequest = %v", rpcErr)
			}
			if string(req.ID) != tc.id || req.Method != "m" || string(req.Params) != tc.params {
				t.Fatalf("got id %s method %q params %s", req.ID, req.Method, req.Params)
			}
			if req.notification() != tc.notification {
				t.Fatalf("notification() = %v", req.notification())
			}
		})
	}
}

func TestParseRequestRejectsMalformedJSON(t *testing.T) {
	for _, line := range []string{`{"jsonrpc":"2.0",`, `not json`, `{"a":1} {"b":2}`} {
		req, rpcErr := parseRequest([]byte(line))
		if rpcErr == nil || rpcErr.Code != CodeParseError {
			t.Errorf("%s: error = %v, want -32700", line, rpcErr)
		}
		if req.ID != nil {
			t.Errorf("%s: id = %s, want none", line, req.ID)
		}
	}
}

func TestParseRequestRejectsInvalidRequests(t *testing.T) {
	cases := map[string]struct{ line, id string }{
		"no version":       {`{"id":1,"method":"m"}`, "1"},
		"old version":      {`{"jsonrpc":"1.0","id":1,"method":"m"}`, "1"},
		"no method":        {`{"jsonrpc":"2.0","id":1}`, "1"},
		"numeric method":   {`{"jsonrpc":"2.0","id":1,"method":5}`, "1"},
		"object id":        {`{"jsonrpc":"2.0","id":{},"method":"m"}`, ""},
		"array id":         {`{"jsonrpc":"2.0","id":[1],"method":"m"}`, ""},
		"boolean id":       {`{"jsonrpc":"2.0","id":true,"method":"m"}`, ""},
		"false id":         {`{"jsonrpc":"2.0","id":false,"method":"m"}`, ""},
		"scalar params":    {`{"jsonrpc":"2.0","id":1,"method":"m","params":3}`, "1"},
		"string params":    {`{"jsonrpc":"2.0","id":1,"method":"m","params":"x"}`, "1"},
		"not an object":    {`[1,2]`, ""},
		"a bare string":    {`"hello"`, ""},
		"version a number": {`{"jsonrpc":2.0,"id":1,"method":"m"}`, "1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req, rpcErr := parseRequest([]byte(tc.line))
			if rpcErr == nil || rpcErr.Code != CodeInvalidRequest {
				t.Fatalf("error = %v, want -32600", rpcErr)
			}
			if string(req.ID) != tc.id {
				t.Fatalf("id = %s, want %q", req.ID, tc.id)
			}
		})
	}
}

func TestResponsesAreJSONRPC(t *testing.T) {
	cases := map[string]struct {
		got  any
		want string
	}{
		"success": {
			response{JSONRPC: "2.0", ID: json.RawMessage("7"), Result: json.RawMessage("null")},
			`{"jsonrpc":"2.0","id":7,"result":null}`,
		},
		"failure without id": {
			response{JSONRPC: "2.0", Error: &Error{Code: CodeParseError, Message: "parse error"}},
			`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"parse error"}}`,
		},
		"notification": {
			notification{JSONRPC: "2.0", Method: "unread.summary", Params: map[string]int{"dms": 1}},
			`{"jsonrpc":"2.0","method":"unread.summary","params":{"dms":1}}`,
		},
	}
	for name, tc := range cases {
		b, err := json.Marshal(tc.got)
		if err != nil || string(b) != tc.want {
			t.Errorf("%s: %s, %v; want %s", name, b, err, tc.want)
		}
	}
}
