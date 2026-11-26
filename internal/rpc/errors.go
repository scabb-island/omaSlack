// Package rpc is the daemon's local protocol (spec daemon-api): JSON-RPC
// 2.0 over NDJSON on a private unix socket. This file is the contract the
// rest of the daemon programs against: the Handler a server dispatches to
// and the error codes clients see.
package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

// Code is a JSON-RPC error code.
type Code int

// Standard JSON-RPC codes, plus omaslack's own from -32000 down.
const (
	CodeParseError     Code = -32700
	CodeInvalidRequest Code = -32600
	CodeMethodNotFound Code = -32601
	CodeInvalidParams  Code = -32602
	CodeInternalError  Code = -32603

	CodeIncompatibleProtocol  Code = -32001
	CodeUnsupportedCapability Code = -32002
	CodeAuthExpired           Code = -32003
)

// Error is a JSON-RPC error object.
type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// FromError turns any error a handler returns into the error object a
// client receives. An *Error passes through unchanged, a missing capability
// becomes -32002, credentials Slack rejected become -32003 auth_expired
// (without the provider's text, which names Slack methods and codes the
// client has no use for), and anything else becomes a generic internal error whose
// text is not exposed (it may hold paths or Slack responses).
func FromError(err error) *Error {
	var rpcErr *Error
	if errors.As(err, &rpcErr) {
		return rpcErr
	}
	if errors.Is(err, domain.ErrUnsupported) {
		return &Error{Code: CodeUnsupportedCapability, Message: err.Error()}
	}
	if errors.Is(err, provider.ErrAuthExpired) {
		return &Error{Code: CodeAuthExpired, Message: "auth_expired"}
	}
	return &Error{Code: CodeInternalError, Message: "internal error"}
}

// Handler answers one method call. params is the raw "params" member, nil
// when absent. The result is marshaled as the "result" member. Errors go
// through FromError.
type Handler interface {
	Handle(ctx context.Context, method string, params json.RawMessage) (any, error)
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(ctx context.Context, method string, params json.RawMessage) (any, error)

func (f HandlerFunc) Handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	return f(ctx, method, params)
}
