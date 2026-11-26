package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/version"
)

// serve runs s on a fresh socket until the test ends and returns the path.
func serve(t *testing.T, s *Server) string {
	t.Helper()
	path := socketPath(t)
	l := listen(t, path)
	done := make(chan error, 1)
	go func() { done <- s.Serve(l) }()
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		if err := <-done; err != nil {
			t.Errorf("Serve: %v", err)
		}
	})
	return path
}

func echo(_ context.Context, _ string, params json.RawMessage) (any, error) {
	return params, nil
}

func startEcho(t *testing.T) string {
	t.Helper()
	return serve(t, NewServer(Router{"echo": HandlerFunc(echo)}, Options{}))
}

// ── Handshake (spec daemon-api, "Handshake and protocol version") ──

func TestHelloAnswersTheProtocolAndDaemonVersion(t *testing.T) {
	extra := func(_ context.Context, h Hello) (map[string]any, error) {
		return map[string]any{"workspaces": []string{h.Client}, "protocol": 99}, nil
	}
	path := serve(t, NewServer(Router{}, Options{Hello: extra}))

	r := connect(t, path).hello()
	want := fmt.Sprintf(`{"daemonVersion":%q,"protocol":%d,"workspaces":["test"]}`, version.Version, version.Protocol)
	wantResult(t, r, want)
}

func TestHelloWithoutExtraFields(t *testing.T) {
	r := connect(t, startEcho(t)).hello()
	wantResult(t, r, fmt.Sprintf(`{"daemonVersion":%q,"protocol":%d}`, version.Version, version.Protocol))
}

// Spec daemon-api, "QML plugin newer than the daemon".
func TestNewerClientGetsIncompatibleProtocolAndIsDisconnected(t *testing.T) {
	c := connect(t, startEcho(t))
	r := c.call(1, "hello", `{"client":"omarchy-shell","protocol":2}`)
	wantError(t, r, CodeIncompatibleProtocol)
	if r.Error.Message != "incompatible_protocol" {
		t.Errorf("message = %q", r.Error.Message)
	}
	c.expectClosed()
}

func TestCallsBeforeHelloAreInvalidAndTheConnectionStays(t *testing.T) {
	c := connect(t, startEcho(t))
	wantError(t, c.call(1, "echo", `[1]`), CodeInvalidRequest)
	c.send(`{"jsonrpc":"2.0","method":"echo"}`) // a notification: no reply
	c.hello()
	wantResult(t, c.call(2, "echo", `[2]`), `[2]`)
}

func TestHelloWithBadParamsIsInvalidParams(t *testing.T) {
	c := connect(t, startEcho(t))
	for i, params := range []string{"", `[1]`, `{"protocol":1}`, `{"client":"x"}`, `{"client":"x","protocol":"1"}`} {
		wantError(t, c.call(i, "hello", params), CodeInvalidParams)
	}
	c.hello()
}

func TestHelloFailureGoesThroughFromError(t *testing.T) {
	failing := func(context.Context, Hello) (map[string]any, error) {
		return nil, errors.New("/home/someone/secret")
	}
	c := connect(t, serve(t, NewServer(Router{}, Options{Hello: failing})))
	r := c.call(1, "hello", `{"client":"x","protocol":1}`)
	wantError(t, r, CodeInternalError)
	if strings.Contains(r.Error.Message, "secret") {
		t.Fatal("internal error text leaked to the client")
	}
}

func TestSecondHelloIsInvalid(t *testing.T) {
	c := connect(t, startEcho(t))
	c.hello()
	wantError(t, c.call(1, "hello", `{"client":"x","protocol":1}`), CodeInvalidRequest)
}

// ── Dispatch ──

// Spec daemon-api, "Unknown method".
func TestUnknownMethodIsNotFoundAndTheConnectionStays(t *testing.T) {
	c := connect(t, startEcho(t))
	c.hello()
	wantError(t, c.call(1, "foo.bar", ""), CodeMethodNotFound)
	wantResult(t, c.call(2, "echo", `{"a":1}`), `{"a":1}`)
}

func TestHandlerErrorsGoThroughFromError(t *testing.T) {
	h := Router{
		"own": HandlerFunc(func(context.Context, string, json.RawMessage) (any, error) {
			return nil, &Error{Code: CodeAuthExpired, Message: "auth_expired"}
		}),
		"internal": HandlerFunc(func(context.Context, string, json.RawMessage) (any, error) { return nil, errors.New("disk path") }),
		"panics":   HandlerFunc(func(context.Context, string, json.RawMessage) (any, error) { panic("boom") }),
		"chan":     HandlerFunc(func(context.Context, string, json.RawMessage) (any, error) { return make(chan int), nil }),
	}
	c := connect(t, serve(t, NewServer(h, Options{})))
	c.hello()
	wantError(t, c.call(1, "own", ""), CodeAuthExpired)
	wantError(t, c.call(2, "internal", ""), CodeInternalError)
	wantError(t, c.call(3, "panics", ""), CodeInternalError)
	wantError(t, c.call(4, "chan", ""), CodeInternalError)
	wantError(t, c.call(5, "own", ""), CodeAuthExpired) // still served after a panic
}

func TestNilResultIsSentAsNull(t *testing.T) {
	h := Router{"nothing": HandlerFunc(func(context.Context, string, json.RawMessage) (any, error) { return nil, nil })}
	c := connect(t, serve(t, NewServer(h, Options{})))
	c.hello()
	wantResult(t, c.call(1, "nothing", ""), "null")
}

func TestNotificationsAreHandledWithoutAReply(t *testing.T) {
	got := make(chan string, 1)
	h := Router{
		"note": HandlerFunc(func(_ context.Context, _ string, p json.RawMessage) (any, error) {
			got <- string(p)
			return nil, errors.New("not sent")
		}),
		"echo": HandlerFunc(echo),
	}
	c := connect(t, serve(t, NewServer(h, Options{})))
	c.hello()
	c.send(`{"jsonrpc":"2.0","method":"note","params":["x"]}`)
	if p := <-got; p != `["x"]` {
		t.Fatalf("params = %s", p)
	}
	c.send(`{"jsonrpc":"2.0","method":"unknown"}`)
	wantResult(t, c.call(1, "echo", `[1]`), `[1]`) // the next line is this reply
}

// ── Framing ──

func TestMalformedLinesGetErrorsAndTheConnectionStays(t *testing.T) {
	c := connect(t, startEcho(t))
	c.hello()

	c.send(`{"jsonrpc":`)
	r := c.recv()
	wantError(t, r, CodeParseError)
	if string(r.ID) != "null" {
		t.Fatalf("id = %s, want null", r.ID)
	}
	c.send(`{"jsonrpc":"1.0","id":9,"method":"echo"}`)
	r = c.recv()
	wantError(t, r, CodeInvalidRequest)
	if string(r.ID) != "9" {
		t.Fatalf("id = %s, want 9", r.ID)
	}
	c.send(`{"jsonrpc":"2.0","method":7}`) // invalid even without an id
	wantError(t, c.recv(), CodeInvalidRequest)
	c.send("x") // one byte is still a line to answer
	wantError(t, c.recv(), CodeParseError)
	c.send("   ")
	c.send("")
	wantResult(t, c.call(1, "echo", `[1]`), `[1]`)
}

func TestOversizedLineIsAParseErrorAndClosesTheConnection(t *testing.T) {
	s := NewServer(Router{"echo": HandlerFunc(echo)}, Options{})
	s.maxLine = 1024
	c := connect(t, serve(t, s))
	c.hello()
	wantResult(t, c.call(1, "echo", fmt.Sprintf(`[%q]`, strings.Repeat("a", 900))), fmt.Sprintf(`[%q]`, strings.Repeat("a", 900)))

	c.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"echo","params":[%q]}`, strings.Repeat("a", 2048)))
	wantError(t, c.recv(), CodeParseError)
	c.expectClosed()
}

func TestDefaultMaxLineAllowsSeveralMiB(t *testing.T) {
	if MaxLineSize < 4<<20 {
		t.Fatalf("MaxLineSize = %d", MaxLineSize)
	}
	if NewServer(Router{}, Options{}).maxLine != MaxLineSize {
		t.Fatal("a new server does not use MaxLineSize")
	}
}

func TestConcurrentRepliesAreNeverInterleaved(t *testing.T) {
	big := strings.Repeat("x", 256<<10)
	h := Router{"big": HandlerFunc(func(_ context.Context, _ string, p json.RawMessage) (any, error) {
		return string(p) + big, nil
	})}
	c := connect(t, serve(t, NewServer(h, Options{})))
	c.hello()

	const n = 32
	for i := range n {
		c.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"big","params":[%d]}`, i, i))
	}
	seen := map[string]bool{}
	for range n {
		r := c.recv() // fails on any line that is not whole JSON
		var s string
		if err := json.Unmarshal(r.Result, &s); err != nil || !strings.HasSuffix(s, big) {
			t.Fatalf("reply %s is damaged", r.ID)
		}
		seen[string(r.ID)] = true
	}
	if len(seen) != n {
		t.Fatalf("got %d distinct replies, want %d", len(seen), n)
	}
}

func TestRequestsOnOneConnectionRunConcurrently(t *testing.T) {
	release := make(chan struct{})
	h := Router{
		"block": HandlerFunc(func(context.Context, string, json.RawMessage) (any, error) { <-release; return "late", nil }),
		"echo":  HandlerFunc(echo),
	}
	c := connect(t, serve(t, NewServer(h, Options{})))
	c.hello()
	c.send(`{"jsonrpc":"2.0","id":1,"method":"block"}`)
	wantResult(t, c.call(2, "echo", `[2]`), `[2]`)
	close(release)
	wantResult(t, c.recv(), `"late"`)
}

func TestConnectionsAreIndependent(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	h := Router{
		"block": HandlerFunc(func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil, nil
		}),
		"echo": HandlerFunc(echo),
	}
	path := serve(t, NewServer(h, Options{}))
	a, b := connect(t, path), connect(t, path)
	a.hello()
	a.send(`{"jsonrpc":"2.0","id":1,"method":"block"}`)
	b.hello()
	wantResult(t, b.call(1, "echo", `[1]`), `[1]`)
}

// ── Peer check (spec daemon-api, "Another user tries to connect") ──

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestAnotherUserIsDisconnectedUnreadAndLogged(t *testing.T) {
	var logs syncBuffer
	var called atomic.Bool
	h := HandlerFunc(func(context.Context, string, json.RawMessage) (any, error) { called.Store(true); return nil, nil })
	s := NewServer(h, Options{Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	s.owner = uint32(os.Getuid()) + 1 // the test process plays the other user
	c := connect(t, serve(t, s))

	// The server may already have closed it, so the write may fail.
	_, _ = c.c.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"hello","params":{"client":"x","protocol":1}}` + "\n"))
	c.expectClosed()
	if called.Load() {
		t.Fatal("the handler ran for another user")
	}
	if !strings.Contains(logs.String(), "rejected connection") {
		t.Fatalf("not logged: %q", logs.String())
	}
}

func TestTheOwnerIsTheServerProcessUser(t *testing.T) {
	if NewServer(Router{}, Options{}).owner != uint32(os.Getuid()) {
		t.Fatal("owner is not the current user")
	}
}

// ── Shutdown ──

func TestCloseEndsConnectionsStopsAcceptingAndRemovesTheSocket(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	h := Router{"block": HandlerFunc(func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	})}
	s := NewServer(h, Options{})
	path := socketPath(t)
	l := listen(t, path)
	served := make(chan error, 1)
	go func() { served <- s.Serve(l) }()

	c := connect(t, path)
	c.hello()
	c.send(`{"jsonrpc":"2.0","id":1,"method":"block"}`)
	<-started

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	<-cancelled
	if err := <-served; err != nil {
		t.Fatalf("Serve = %v, want nil after Close", err)
	}
	c.expectClosed()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("socket still there: %v", err)
	}
}

func TestServeAfterCloseReturnsAtOnce(t *testing.T) {
	s := NewServer(Router{}, Options{})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	path := socketPath(t)
	if err := s.Serve(listen(t, path)); err != nil {
		t.Fatalf("Serve = %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("Serve left the socket of a closed server")
	}
}

func TestServeReportsAcceptFailures(t *testing.T) {
	s := NewServer(Router{}, Options{})
	l := listen(t, socketPath(t))
	_ = l.Close() // closed under the server's feet, not by Server.Close
	if err := s.Serve(l); err == nil {
		t.Fatal("Serve = nil on a broken listener")
	}
}

// A connection accepted while Close runs is closed, never served.
func TestConnectionAcceptedDuringShutdownIsClosed(t *testing.T) {
	s := NewServer(Router{}, Options{})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	nc := accepted(t)
	s.wg.Add(1)
	s.serveConn(nc)
	if _, err := nc.Write([]byte("x")); err == nil {
		t.Fatal("the connection is still open")
	}
}
