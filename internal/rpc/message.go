package rpc

import (
	"encoding/json"
	"strings"
)

const jsonrpcVersion = "2.0"

// request is a call parsed and checked at the edge; the rest of the server
// trusts it.
type request struct {
	ID     json.RawMessage // nil for a notification
	Method string
	Params json.RawMessage // nil when absent; otherwise an object or array
}

func (r request) notification() bool { return r.ID == nil }

type wireRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// parseRequest turns one NDJSON line into a request. Text that is not JSON
// is a parse error (-32700); JSON that is not a JSON-RPC 2.0 request is an
// invalid request (-32600). On error, the request still carries the id
// when one could be read, so the reply can name it.
func parseRequest(line []byte) (request, *Error) {
	if !json.Valid(line) {
		return request{}, &Error{Code: CodeParseError, Message: "parse error"}
	}
	var w wireRequest
	err := json.Unmarshal(line, &w)
	req := request{Method: w.Method, Params: w.Params}
	if validID(w.ID) {
		req.ID = w.ID
	}
	if err != nil || !validRequest(w) {
		return request{ID: req.ID}, &Error{Code: CodeInvalidRequest, Message: "invalid request"}
	}
	return req, nil
}

func validRequest(w wireRequest) bool {
	return w.JSONRPC == jsonrpcVersion && w.Method != "" && validID(w.ID) && validParams(w.Params)
}

// validID accepts an absent id, a string, a number or null.
func validID(id json.RawMessage) bool {
	return len(id) == 0 || !strings.ContainsAny(string(id[:1]), "{[tf")
}

// validParams accepts absent params, an object or an array.
func validParams(p json.RawMessage) bool {
	return len(p) == 0 || strings.ContainsAny(string(p[:1]), "{[")
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"` // null when the request's id is unknown
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}
