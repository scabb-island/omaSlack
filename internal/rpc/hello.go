package rpc

import (
	"context"
	"encoding/json"
	"maps"

	"gitlab.com/scabb-island/omaslack/internal/version"
)

// Hello is the first call every client makes (spec daemon-api, "Handshake
// and protocol version"). Protocol is the client's major protocol version.
type Hello struct {
	Client   string `json:"client"`
	Protocol int    `json:"protocol"`
}

// HelloFunc adds fields to the hello result, such as workspaces and
// capabilities. "protocol" and "daemonVersion" are always the server's own.
// An error goes through FromError and leaves the client not greeted.
type HelloFunc func(ctx context.Context, h Hello) (map[string]any, error)

func noExtras(context.Context, Hello) (map[string]any, error) { return nil, nil }

func parseHello(params json.RawMessage) (Hello, *Error) {
	var h Hello
	if err := json.Unmarshal(params, &h); err != nil || h.Client == "" || h.Protocol == 0 {
		return Hello{}, &Error{Code: CodeInvalidParams, Message: "hello needs {client, protocol}"}
	}
	return h, nil
}

func helloResult(ctx context.Context, extras HelloFunc, h Hello) (map[string]any, error) {
	out, err := extras(ctx, h)
	if err != nil {
		return nil, err
	}
	out = maps.Clone(out)
	if out == nil {
		out = map[string]any{}
	}
	out["protocol"] = version.Protocol
	out["daemonVersion"] = version.Version
	return out, nil
}
