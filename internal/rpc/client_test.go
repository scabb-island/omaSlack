package rpc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"
)

// reply is any line the server writes: a response or a notification.
type reply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *Error          `json:"error"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// testClient speaks the protocol by hand, the way the QML Service does.
type testClient struct {
	t *testing.T
	c *net.UnixConn
	r *bufio.Reader
}

func connect(t *testing.T, path string) *testClient {
	t.Helper()
	c := dial(t, path)
	return &testClient{t: t, c: c, r: bufio.NewReaderSize(c, 1<<16)}
}

func (tc *testClient) send(line string) {
	tc.t.Helper()
	if _, err := io.WriteString(tc.c, line+"\n"); err != nil {
		tc.t.Fatalf("send: %v", err)
	}
}

func (tc *testClient) recv() reply {
	tc.t.Helper()
	line, err := tc.r.ReadBytes('\n')
	if err != nil {
		tc.t.Fatalf("recv: %v", err)
	}
	var r reply
	if err := json.Unmarshal(line, &r); err != nil {
		tc.t.Fatalf("recv %q: %v", line, err)
	}
	if r.JSONRPC != "2.0" {
		tc.t.Fatalf("recv %q: not JSON-RPC 2.0", line)
	}
	return r
}

func (tc *testClient) call(id int, method, params string) reply {
	tc.t.Helper()
	if params == "" {
		tc.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q}`, id, method))
	} else {
		tc.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`, id, method, params))
	}
	r := tc.recv()
	if string(r.ID) != fmt.Sprint(id) {
		tc.t.Fatalf("reply id = %s, want %d", r.ID, id)
	}
	return r
}

func (tc *testClient) hello() reply {
	tc.t.Helper()
	r := tc.call(0, "hello", `{"client":"test","protocol":1}`)
	if r.Error != nil {
		tc.t.Fatalf("hello: %v", r.Error)
	}
	return r
}

// expectClosed passes once the server has closed the connection. Closing a
// unix socket with input still unread resets it, so ECONNRESET counts too.
func (tc *testClient) expectClosed() {
	tc.t.Helper()
	line, err := tc.r.ReadBytes('\n')
	if !errors.Is(err, io.EOF) && !errors.Is(err, syscall.ECONNRESET) {
		tc.t.Fatalf("read %q, %v; want the connection closed", line, err)
	}
}

func wantError(t *testing.T, r reply, code Code) {
	t.Helper()
	if r.Error == nil || r.Error.Code != code {
		t.Fatalf("reply = %+v (error %v), want error %d", r, r.Error, code)
	}
}

func wantResult(t *testing.T, r reply, want string) {
	t.Helper()
	if r.Error != nil || string(r.Result) != want {
		t.Fatalf("result = %s (error %v), want %s", r.Result, r.Error, want)
	}
}
