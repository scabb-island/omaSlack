package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
)

// ctlUsage is the usage of `omaslack ctl`.
const ctlUsage = `Usage: omaslack ctl <method> [params-json]
       omaslack ctl --stream <topic>[,<topic>...]

The first form calls one daemon method and prints its result as one line
of JSON. params-json is a JSON object or array.

--stream subscribes to the topics, prints every event as one NDJSON line
(a JSON-RPC notification) and forwards each JSON-RPC request read on
stdin, printing its response as one more line. It runs until the daemon
goes away; stdin ending does not stop it.

The daemon is started with systemctl --user start omaslack if it is not
running.

Exit codes:
  0  success
  1  any other error
  2  usage error
  4  the daemon answered with an error (printed on stderr)
  5  the daemon is not running and could not be started, or went away
`

// ctlClient is the name ctl gives the daemon in hello.
const ctlClient = "omaslack-ctl"

// runCtl calls one daemon method or streams its events.
func runCtl(env Env, args []string) error {
	switch {
	case len(args) == 0:
		return ctlUsageError("missing method")
	case args[0] == "--stream":
		if len(args) != 2 || args[1] == "" {
			return ctlUsageError("--stream needs one comma-separated list of topics")
		}
		return ctlStream(env, strings.Split(args[1], ","))
	case strings.HasPrefix(args[0], "-"):
		return ctlUsageError("unknown flag %q", args[0])
	case len(args) > 2:
		return ctlUsageError("too many arguments")
	}
	params, err := ctlParams(args[1:])
	if err != nil {
		return err
	}
	return ctlCall(env, args[0], params)
}

func ctlUsageError(format string, a ...any) error {
	return &usageError{err: fmt.Errorf(format, a...), usage: ctlUsage}
}

// ctlParams parses the optional params argument.
func ctlParams(args []string) (json.RawMessage, error) {
	if len(args) == 0 {
		return nil, nil
	}
	p := bytes.TrimSpace([]byte(args[0]))
	if !json.Valid(p) || len(p) == 0 || (p[0] != '{' && p[0] != '[') {
		return nil, ctlUsageError("params must be a JSON object or array: %s", args[0])
	}
	return p, nil
}

func dialDaemon(ctx context.Context, env Env, client string) (*rpcclient.Client, error) {
	if env.DialDaemon != nil {
		return env.DialDaemon(ctx)
	}
	return rpcclient.Dial(ctx, rpcclient.Options{Client: client})
}

func ctlCall(env Env, method string, params json.RawMessage) error {
	ctx := context.Background()
	c, err := dialDaemon(ctx, env, ctlClient)
	if err != nil {
		return err
	}
	defer c.Close()
	result, err := c.Call(ctx, method, params)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(env.Stdout, "%s\n", result)
	return err
}

// ctlStream subscribes to topics and prints events until the daemon goes
// away, while forwarding the requests read on stdin.
func ctlStream(env Env, topics []string) error {
	ctx := context.Background()
	c, err := dialDaemon(ctx, env, ctlClient)
	if err != nil {
		return err
	}
	defer c.Close()
	if _, err := c.Call(ctx, "events.subscribe", map[string][]string{"topics": topics}); err != nil {
		return err
	}
	out := newLineWriter(env.Stdout)
	if env.Stdin != nil {
		go forwardRequests(ctx, c, env.Stdin, out)
	}
	for n := range c.Notifications() {
		if err := out.write(bridgeLine{JSONRPC: "2.0", Method: n.Method, Params: n.Params}); err != nil {
			return err
		}
	}
	return fmt.Errorf("lost the connection to the daemon: %w", rpcclient.ErrClosed)
}

// bridgeLine is one JSON-RPC line ctl --stream prints: a notification, or
// the response to a request read on stdin.
type bridgeLine struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpc.Error      `json:"error,omitempty"`
}

// forwardRequests sends each NDJSON request on in to the daemon, one at a
// time, and prints its response with the caller's own id. A request
// without an id is a notification: it is sent, its answer is not printed.
func forwardRequests(ctx context.Context, c *rpcclient.Client, in io.Reader, out *lineWriter) {
	sc := bufio.NewScanner(in)
	sc.Buffer(nil, rpc.MaxLineSize)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if resp, ok := forward(ctx, c, line); ok && out.write(resp) != nil {
			return
		}
	}
}

func forward(ctx context.Context, c *rpcclient.Client, line []byte) (bridgeLine, bool) {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if json.Unmarshal(line, &req) != nil {
		parseErr := &rpc.Error{Code: rpc.CodeParseError, Message: "parse error"}
		return bridgeLine{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: parseErr}, true
	}
	result, err := c.Call(ctx, req.Method, req.Params)
	resp := bridgeLine{JSONRPC: "2.0", ID: req.ID, Result: result}
	if err != nil {
		resp.Error = rpc.FromError(err)
	}
	return resp, req.ID != nil
}

// lineWriter prints one JSON value per line from any goroutine.
type lineWriter struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func newLineWriter(w io.Writer) *lineWriter { return &lineWriter{enc: json.NewEncoder(w)} }

func (l *lineWriter) write(v bridgeLine) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.enc.Encode(v) // one value and a newline in one Write
}
