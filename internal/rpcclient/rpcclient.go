// Package rpcclient talks to the omaslack daemon over its local socket
// (spec daemon-api): JSON-RPC 2.0 over NDJSON. omaslack ctl and the TUI
// use it; the QML plugin speaks the same protocol by hand.
//
// Dial connects and sends hello. If the socket is missing or stale it
// first runs `systemctl --user start omaslack` and waits for the socket
// (spec daemon-api, "User service and on-demand start").
package rpcclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/version"
)

// SocketName is the daemon socket's name inside $XDG_RUNTIME_DIR.
const SocketName = "omaslack.sock"

// Defaults for the zero Options fields.
const (
	DefaultStartTimeout = 5 * time.Second
	DefaultPollInterval = 50 * time.Millisecond
)

var (
	// ErrUnavailable means the daemon could not be reached, even after
	// trying to start it.
	ErrUnavailable = errors.New("the omaslack daemon is not available")
	// ErrClosed means the connection has ended.
	ErrClosed = errors.New("rpcclient: connection closed")
)

// SocketPath returns $XDG_RUNTIME_DIR/omaslack.sock.
func SocketPath() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return "", errors.New("XDG_RUNTIME_DIR is not set")
	}
	return filepath.Join(dir, SocketName), nil
}

// Runner runs a command to completion, like exec.Cmd.Run.
type Runner func(ctx context.Context, name string, args ...string) error

// Options configures Dial. Client is required: it names the client in
// hello and in the daemon's log.
type Options struct {
	Client string
	Path   string // default SocketPath()
	Run    Runner // starts the daemon; default runs the real command

	StartTimeout time.Duration // how long to wait for the socket; default 5 s
	PollInterval time.Duration // how often to look for it; default 50 ms
}

// Notification is an event the daemon pushed.
type Notification struct {
	Method string
	Params json.RawMessage
}

// Client is one connection to the daemon. Its methods are safe for
// concurrent use.
type Client struct {
	nc    net.Conn
	hello json.RawMessage
	notes chan Notification
	done  chan struct{}
	quit  chan struct{} // closed by Close
	once  sync.Once
	ids   atomic.Int64

	wmu sync.Mutex // one whole line at a time

	mu      sync.Mutex
	pending map[int64]chan response
}

// Dial connects to the daemon, starting it if needed, and sends hello. An
// error the daemon answers hello with is returned as an *rpc.Error.
func Dial(ctx context.Context, opts Options) (*Client, error) {
	opts = withDefaults(opts)
	nc, err := connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	c := &Client{
		nc:      nc,
		notes:   make(chan Notification, 256),
		done:    make(chan struct{}),
		quit:    make(chan struct{}),
		pending: make(map[int64]chan response),
	}
	go c.read()
	c.hello, err = c.Call(ctx, "hello", rpc.Hello{Client: opts.Client, Protocol: version.Protocol})
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func withDefaults(o Options) Options {
	if o.Run == nil {
		o.Run = run
	}
	if o.StartTimeout <= 0 {
		o.StartTimeout = DefaultStartTimeout
	}
	if o.PollInterval <= 0 {
		o.PollInterval = DefaultPollInterval
	}
	return o
}

func run(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // Dial only runs systemctl --user start omaslack
	if err != nil && len(out) > 0 {
		return fmt.Errorf("%w: %s", err, out)
	}
	return err
}

// connect dials the socket. A socket that is missing, or that nobody
// answers on, means the daemon is not running: start it and wait.
func connect(ctx context.Context, opts Options) (net.Conn, error) {
	path := opts.Path
	if path == "" {
		p, err := SocketPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	nc, err := dialUnix(ctx, path)
	if !errors.Is(err, syscall.ENOENT) && !errors.Is(err, syscall.ECONNREFUSED) {
		return nc, err
	}
	if err := opts.Run(ctx, "systemctl", "--user", "start", "omaslack"); err != nil {
		return nil, fmt.Errorf("start it with systemctl --user start omaslack: %w", err)
	}
	return waitFor(ctx, path, opts)
}

// waitFor dials path until it answers or StartTimeout passes.
func waitFor(ctx context.Context, path string, opts Options) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, opts.StartTimeout)
	defer cancel()
	tick := time.NewTicker(opts.PollInterval)
	defer tick.Stop()
	for {
		if nc, err := dialUnix(ctx, path); err == nil {
			return nc, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("started it, but %s did not answer within %v", path, opts.StartTimeout)
		case <-tick.C:
		}
	}
}

func dialUnix(ctx context.Context, path string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", path)
}

// Hello returns the daemon's answer to hello.
func (c *Client) Hello() json.RawMessage { return c.hello }

// Notifications delivers the daemon's notifications in order. It is closed
// when the connection ends. A client that subscribes must keep reading it:
// while it is full no responses are read either, and the daemon
// disconnects a client that falls too far behind.
func (c *Client) Notifications() <-chan Notification { return c.notes }

// Done is closed when the connection has ended.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close ends the connection, even while nobody reads Notifications.
func (c *Client) Close() error {
	c.once.Do(func() { close(c.quit) })
	return c.nc.Close()
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// response is any line the daemon sends: a response or a notification.
type response struct {
	ID     *int64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpc.Error      `json:"error"`
}

// Call sends one request and waits for its response. params is marshaled
// (a json.RawMessage is sent as is; nil sends none). An error response is
// returned as an *rpc.Error; a lost connection as ErrClosed.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.ids.Add(1)
	line, err := encode(id, method, params)
	if err != nil {
		return nil, err
	}
	answer := make(chan response, 1)
	c.track(id, answer)
	defer c.track(id, nil)
	if err := c.send(line); err != nil {
		return nil, ErrClosed
	}
	select {
	case r := <-answer:
		return r.result()
	case <-c.done:
		return nil, ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func encode(id int64, method string, params any) ([]byte, error) {
	if raw, ok := params.(json.RawMessage); ok && len(raw) == 0 {
		params = nil // a JSON-RPC request has no "params": null
	}
	line, err := json.Marshal(request{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("rpcclient: %s params: %w", method, err)
	}
	return line, nil
}

func (r response) result() (json.RawMessage, error) {
	if r.Error != nil {
		return nil, r.Error
	}
	return r.Result, nil
}

// track makes ch the place where id's response goes; nil forgets id.
func (c *Client) track(id int64, ch chan response) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ch == nil {
		delete(c.pending, id)
		return
	}
	c.pending[id] = ch
}

func (c *Client) send(line []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err := c.nc.Write(append(line, '\n'))
	return err
}

// read routes every line the daemon sends until the connection ends.
func (c *Client) read() {
	defer close(c.notes)
	defer close(c.done)
	defer c.nc.Close()
	r := bufio.NewReader(c.nc)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var resp response
		if json.Unmarshal(line, &resp) != nil {
			continue // not ours to fix: the daemon only sends JSON
		}
		c.route(resp)
	}
}

func (c *Client) route(r response) {
	if r.ID == nil {
		if r.Method != "" {
			select {
			case c.notes <- Notification{Method: r.Method, Params: r.Params}:
			case <-c.quit:
			}
		}
		return
	}
	c.mu.Lock()
	ch := c.pending[*r.ID]
	c.mu.Unlock()
	if ch != nil {
		ch <- r
	}
}
