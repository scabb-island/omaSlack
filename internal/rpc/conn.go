package rpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"

	"gitlab.com/scabb-island/omaslack/internal/version"
)

var errHandlerPanic = errors.New("rpc: handler panicked")

// Conn is one client connection. Handlers reach it through
// ConnFromContext, so a call such as events.subscribe can keep pushing
// notifications on it after it has answered.
type Conn struct {
	srv    *Server
	nc     *net.UnixConn
	ctx    context.Context // carries the Conn; ends when the connection does
	cancel context.CancelFunc

	wmu     sync.Mutex     // one whole line at a time
	greeted bool           // only touched by the reading goroutine
	calls   sync.WaitGroup // calls still running
}

type connKey struct{}

// ConnFromContext returns the connection a handler is answering on.
func ConnFromContext(ctx context.Context) (*Conn, bool) {
	c, ok := ctx.Value(connKey{}).(*Conn)
	return c, ok
}

func newConn(s *Server, nc *net.UnixConn) *Conn {
	c := &Conn{srv: s, nc: nc}
	ctx, cancel := context.WithCancel(s.ctx)
	c.ctx, c.cancel = context.WithValue(ctx, connKey{}, c), cancel
	return c
}

// Done is closed once the connection has ended.
func (c *Conn) Done() <-chan struct{} { return c.ctx.Done() }

// Close ends the connection from the daemon's side, for example when a
// subscriber falls too far behind its events. Calls still running finish
// first, but their answers can no longer be delivered.
func (c *Conn) Close() error { return c.nc.Close() }

// Notify sends a JSON-RPC notification (no id) to the client. It fails if
// params cannot be marshaled or the connection has ended.
func (c *Conn) Notify(method string, params any) error {
	return c.send(notification{JSONRPC: jsonrpcVersion, Method: method, Params: params})
}

func (c *Conn) send(v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.nc.Write(append(line, '\n'))
	return err
}

// run reads one request per line until the client leaves, the line is too
// long or the handshake fails. Calls still running are allowed to answer
// (a client may half-close after sending) before the connection closes.
func (c *Conn) run() {
	defer c.finish()
	sc := bufio.NewScanner(c.nc)
	sc.Buffer(nil, c.srv.maxLine)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) > 0 && !c.handle(bytes.Clone(line)) {
			return
		}
	}
	if errors.Is(sc.Err(), bufio.ErrTooLong) {
		c.write(response{JSONRPC: jsonrpcVersion, Error: &Error{Code: CodeParseError, Message: "line too long"}})
	}
}

func (c *Conn) finish() {
	c.calls.Wait()
	_ = c.nc.Close()
	c.cancel()
}

// handle processes one line and reports whether to keep reading.
func (c *Conn) handle(line []byte) bool {
	req, perr := parseRequest(line)
	switch {
	case perr != nil:
		c.write(response{JSONRPC: jsonrpcVersion, ID: req.ID, Error: perr})
	case !c.greeted:
		return c.greet(req)
	case req.Method == "hello":
		c.reply(req, nil, &Error{Code: CodeInvalidRequest, Message: "hello was already sent"})
	default:
		c.dispatch(req)
	}
	return true
}

// greet runs the handshake. A major protocol mismatch ends the connection.
func (c *Conn) greet(req request) bool {
	if req.Method != "hello" {
		c.reply(req, nil, &Error{Code: CodeInvalidRequest, Message: "the first call must be hello"})
		return true
	}
	h, herr := parseHello(req.Params)
	if herr != nil {
		c.reply(req, nil, herr)
		return true
	}
	if h.Protocol != version.Protocol {
		c.srv.log.Info("rpc: incompatible client", "client", h.Client, "protocol", h.Protocol)
		c.reply(req, nil, &Error{Code: CodeIncompatibleProtocol, Message: "incompatible_protocol"})
		return false
	}
	result, err := helloResult(c.ctx, c.srv.hello, h)
	c.greeted = err == nil
	c.reply(req, result, err)
	return true
}

func (c *Conn) dispatch(req request) {
	c.calls.Add(1)
	go func() {
		defer c.calls.Done()
		result, err := c.call(req)
		c.reply(req, result, err)
	}()
}

func (c *Conn) call(req request) (result any, err error) {
	defer func() {
		if p := recover(); p != nil {
			c.srv.log.Error("rpc: handler panicked", "method", req.Method, "panic", p)
			err = errHandlerPanic
		}
	}()
	return c.srv.handler.Handle(c.ctx, req.Method, req.Params)
}

// reply answers req unless it is a notification. Every error goes through
// FromError, including a result that cannot be marshaled.
func (c *Conn) reply(req request, result any, err error) {
	if req.notification() {
		return
	}
	resp := response{JSONRPC: jsonrpcVersion, ID: req.ID}
	if err == nil {
		resp.Result, err = json.Marshal(result)
	}
	if err != nil {
		resp.Result, resp.Error = nil, FromError(err)
	}
	c.write(resp)
}

// write sends a response. A response always marshals, so it only fails
// when the connection is gone, which the reading goroutine sees and ends.
func (c *Conn) write(resp response) { _ = c.send(resp) }
