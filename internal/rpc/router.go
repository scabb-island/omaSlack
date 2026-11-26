package rpc

import (
	"context"
	"encoding/json"
)

// Router is a Handler that dispatches on the method name. A name it does
// not know gets -32601 Method not found.
type Router map[string]Handler

func (r Router) Handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	h, ok := r[method]
	if !ok {
		return nil, &Error{Code: CodeMethodNotFound, Message: "Method not found"}
	}
	return h.Handle(ctx, method, params)
}
