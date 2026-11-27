package client

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
)

// Handler answers one fake call. Its result is marshaled to JSON.
type Handler func(params json.RawMessage) (any, error)

// Call is one call a Fake received.
type Call struct {
	Method string
	Params json.RawMessage
}

// Fake is a Client for tests: methods answer from Handlers, events are
// pushed with Push, and every call is recorded. Unknown methods answer
// -32601 like the daemon. Safe for concurrent use.
type Fake struct {
	HelloResult json.RawMessage
	Handlers    map[string]Handler

	mu     sync.Mutex
	calls  []Call
	notes  chan rpcclient.Notification
	done   chan struct{}
	closed bool
}

var _ Client = (*Fake)(nil)

// NewFake returns a Fake with room for 256 pushed events.
func NewFake(hello any, handlers map[string]Handler) *Fake {
	h, err := json.Marshal(hello)
	if err != nil {
		panic(err)
	}
	if handlers == nil {
		handlers = map[string]Handler{}
	}
	return &Fake{HelloResult: h, Handlers: handlers, notes: make(chan rpcclient.Notification, 256), done: make(chan struct{})}
}

// Result is a Handler that always answers v.
func Result(v any) Handler { return func(json.RawMessage) (any, error) { return v, nil } }

func (f *Fake) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.calls = append(f.calls, Call{Method: method, Params: raw})
	h, ok := f.Handlers[method]
	f.mu.Unlock()
	if !ok {
		return nil, &rpc.Error{Code: rpc.CodeMethodNotFound, Message: "Method not found"}
	}
	v, err := h(raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// Push delivers an event as if the daemon sent it.
func (f *Fake) Push(method string, params any) {
	raw, err := json.Marshal(params)
	if err != nil {
		panic(fmt.Sprintf("client.Fake.Push: %v", err))
	}
	f.notes <- rpcclient.Notification{Method: method, Params: raw}
}

// Calls returns the calls received so far.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}

func (f *Fake) Notifications() <-chan rpcclient.Notification { return f.notes }
func (f *Fake) Hello() json.RawMessage                       { return f.HelloResult }
func (f *Fake) Done() <-chan struct{}                        { return f.done }

// Close ends the fake connection: Done and Notifications close.
func (f *Fake) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.done)
		close(f.notes)
	}
	return nil
}
