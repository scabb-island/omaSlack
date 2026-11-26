package rpcclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/version"
)

// router is what the test daemon answers.
var router = rpc.Router{
	"echo": rpc.HandlerFunc(func(_ context.Context, _ string, p json.RawMessage) (any, error) {
		if p == nil {
			return "no params", nil
		}
		return p, nil
	}),
	"fail": rpc.HandlerFunc(func(context.Context, string, json.RawMessage) (any, error) {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: "bad"}
	}),
	"notify": rpc.HandlerFunc(func(ctx context.Context, _ string, p json.RawMessage) (any, error) {
		conn, _ := rpc.ConnFromContext(ctx)
		return nil, conn.Notify("ping", p)
	}),
	"hang": rpc.HandlerFunc(func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}),
}

func socketPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "omaslack.sock")
}

// serveAt runs a daemon on path until the test ends and returns a func
// that stops it early.
func serveAt(t *testing.T, path string, opts rpc.Options) (stop func()) {
	t.Helper()
	l, err := rpc.Listen(context.Background(), path)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := rpc.NewServer(router, opts)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(l) }()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			_ = srv.Close()
			if err := <-done; err != nil {
				t.Errorf("Serve: %v", err)
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

// noStart fails the test if the client tries to start the daemon.
func noStart(t *testing.T) func(context.Context, string, ...string) error {
	return func(_ context.Context, name string, args ...string) error {
		t.Errorf("ran %s %v; the daemon was running", name, args)
		return nil
	}
}

func dial(t *testing.T, opts Options) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, opts)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func call(t *testing.T, c *Client, method string, params any) (json.RawMessage, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.Call(ctx, method, params)
}

func TestDialGreetsTheDaemon(t *testing.T) {
	path := socketPath(t)
	var got rpc.Hello
	serveAt(t, path, rpc.Options{Hello: func(_ context.Context, h rpc.Hello) (map[string]any, error) {
		got = h
		return map[string]any{"workspaces": []string{}}, nil
	}})
	c := dial(t, Options{Path: path, Client: "omaslack-test", Run: noStart(t)})

	if got != (rpc.Hello{Client: "omaslack-test", Protocol: version.Protocol}) {
		t.Errorf("hello = %+v", got)
	}
	var hello map[string]any
	if err := json.Unmarshal(c.Hello(), &hello); err != nil || hello["workspaces"] == nil {
		t.Errorf("Hello() = %s (%v)", c.Hello(), err)
	}
}

func TestCallReturnsTheResult(t *testing.T) {
	path := socketPath(t)
	serveAt(t, path, rpc.Options{})
	c := dial(t, Options{Path: path, Client: "test", Run: noStart(t)})

	for _, tc := range []struct {
		params any
		want   string
	}{
		{map[string]int{"a": 1}, `{"a":1}`},
		{json.RawMessage(`[1,2]`), `[1,2]`},
		{nil, `"no params"`},
		{json.RawMessage(nil), `"no params"`},
	} {
		got, err := call(t, c, "echo", tc.params)
		if err != nil || string(got) != tc.want {
			t.Errorf("echo %v = %s, %v; want %s", tc.params, got, err, tc.want)
		}
	}
}

func TestErrorResponsesAreRPCErrors(t *testing.T) {
	path := socketPath(t)
	serveAt(t, path, rpc.Options{})
	c := dial(t, Options{Path: path, Client: "test", Run: noStart(t)})

	_, err := call(t, c, "fail", nil)
	var rerr *rpc.Error
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeInvalidParams || rerr.Message != "bad" {
		t.Fatalf("err = %v, want the daemon's -32602", err)
	}
	if _, err := call(t, c, "echo", map[string]int{"a": 1}); err != nil {
		t.Errorf("the connection did not survive an error: %v", err)
	}
}

func TestParamsThatDoNotMarshalFail(t *testing.T) {
	path := socketPath(t)
	serveAt(t, path, rpc.Options{})
	c := dial(t, Options{Path: path, Client: "test", Run: noStart(t)})
	if _, err := call(t, c, "echo", func() {}); err == nil {
		t.Fatal("a func was sent as params")
	}
}

func TestConcurrentCallsGetTheirOwnAnswers(t *testing.T) {
	path := socketPath(t)
	serveAt(t, path, rpc.Options{})
	c := dial(t, Options{Path: path, Client: "test", Run: noStart(t)})

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			got, err := call(t, c, "echo", []int{i})
			if want := fmt.Sprintf("[%d]", i); err != nil || string(got) != want {
				t.Errorf("echo [%d] = %s, %v", i, got, err)
			}
		})
	}
	wg.Wait()
}

func TestNotificationsArriveInOrder(t *testing.T) {
	path := socketPath(t)
	serveAt(t, path, rpc.Options{})
	c := dial(t, Options{Path: path, Client: "test", Run: noStart(t)})

	for i := range 3 {
		if _, err := call(t, c, "notify", []int{i}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 3 {
		n := <-c.Notifications()
		if want := fmt.Sprintf("[%d]", i); n.Method != "ping" || string(n.Params) != want {
			t.Fatalf("notification %d = %+v, want ping %s", i, n, want)
		}
	}
}

func TestACanceledCallReturnsTheContextError(t *testing.T) {
	path := socketPath(t)
	serveAt(t, path, rpc.Options{})
	c := dial(t, Options{Path: path, Client: "test", Run: noStart(t)})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Call(ctx, "hang", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline", err)
	}
}

func TestWhenTheDaemonGoesAwayEverythingEnds(t *testing.T) {
	path := socketPath(t)
	stop := serveAt(t, path, rpc.Options{})
	c := dial(t, Options{Path: path, Client: "test", Run: noStart(t)})

	stop()
	<-c.Done()
	if _, ok := <-c.Notifications(); ok {
		t.Error("got a notification after the daemon left")
	}
	if _, err := call(t, c, "echo", nil); !errors.Is(err, ErrClosed) {
		t.Errorf("err = %v, want ErrClosed", err)
	}
}

func TestAFailedHelloFailsDial(t *testing.T) {
	path := socketPath(t)
	serveAt(t, path, rpc.Options{Hello: func(context.Context, rpc.Hello) (map[string]any, error) {
		return nil, &rpc.Error{Code: rpc.CodeIncompatibleProtocol, Message: "incompatible_protocol"}
	}})
	_, err := Dial(context.Background(), Options{Path: path, Client: "test", Run: noStart(t)})
	var rerr *rpc.Error
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeIncompatibleProtocol {
		t.Fatalf("err = %v, want -32001", err)
	}
}

// ── On-demand start (spec daemon-api, "User service and on-demand start") ──

func TestAMissingDaemonIsStartedWithSystemd(t *testing.T) {
	path := socketPath(t)
	var ran []string
	start := func(_ context.Context, name string, args ...string) error {
		ran = append([]string{name}, args...)
		go func() {
			time.Sleep(30 * time.Millisecond) // the daemon takes a moment
			serveAt(t, path, rpc.Options{})
		}()
		return nil
	}
	c := dial(t, Options{Path: path, Client: "test", Run: start, PollInterval: 5 * time.Millisecond})

	if want := []string{"systemctl", "--user", "start", "omaslack"}; !slices.Equal(ran, want) {
		t.Errorf("ran %v, want %v", ran, want)
	}
	if _, err := call(t, c, "echo", nil); err != nil {
		t.Errorf("call after start: %v", err)
	}
}

func TestAStaleSocketCountsAsMissing(t *testing.T) {
	path := socketPath(t)
	l, err := rpc.Listen(context.Background(), path) // leaves a socket nobody answers
	if err != nil {
		t.Fatal(err)
	}
	_ = l.UnixListener.Close()

	started := false
	start := func(context.Context, string, ...string) error { started = true; return errors.New("no unit") }
	_, err = Dial(context.Background(), Options{Path: path, Client: "test", Run: start})
	if !started || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("started=%v err=%v, want a start attempt and ErrUnavailable", started, err)
	}
}

func TestAFailedStartIsUnavailable(t *testing.T) {
	start := func(context.Context, string, ...string) error { return errors.New("Unit omaslack.service not found") }
	_, err := Dial(context.Background(), Options{Path: socketPath(t), Client: "test", Run: start})
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want ErrUnavailable with the reason", err)
	}
}

func TestASocketThatNeverAppearsIsUnavailable(t *testing.T) {
	start := func(context.Context, string, ...string) error { return nil }
	begin := time.Now()
	_, err := Dial(context.Background(), Options{
		Path: socketPath(t), Client: "test", Run: start,
		StartTimeout: 50 * time.Millisecond, PollInterval: 5 * time.Millisecond,
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if waited := time.Since(begin); waited < 50*time.Millisecond {
		t.Errorf("gave up after %v, before the timeout", waited)
	}
}

func TestOtherDialErrorsDoNotStartTheDaemon(t *testing.T) {
	// A path through a regular file fails with ENOTDIR, even for root (CI
	// runs as root, which ignores directory permissions).
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Dial(context.Background(), Options{Path: filepath.Join(file, "omaslack.sock"), Client: "test", Run: noStart(t)})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestCloseEndsAClientThatStoppedReadingNotifications(t *testing.T) {
	path := socketPath(t)
	serveAt(t, path, rpc.Options{})
	c := dial(t, Options{Path: path, Client: "test", Run: noStart(t)})
	for i := range cap(c.notes) + 10 { // more than the channel holds
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_, err := c.Call(ctx, "notify", []int{i})
		cancel()
		if errors.Is(err, context.DeadlineExceeded) {
			break // the reader is stuck on the full channel: the point
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = c.Close()
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the client never ended")
	}
}

func TestTheDefaultRunnerReportsWhatTheCommandSaid(t *testing.T) {
	if err := run(context.Background(), "true"); err != nil {
		t.Errorf("true: %v", err)
	}
	err := run(context.Background(), "sh", "-c", "echo Unit omaslack.service not found >&2; exit 5")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v, want the command's output", err)
	}
	if err := run(context.Background(), "/nonexistent/omaslack-test"); err == nil {
		t.Error("a missing command succeeded")
	}
}

func TestTheSocketIsInTheRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if p, err := SocketPath(); err != nil || p != "/run/user/1000/omaslack.sock" {
		t.Errorf("SocketPath() = %q, %v", p, err)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	if _, err := SocketPath(); err == nil {
		t.Error("SocketPath() without XDG_RUNTIME_DIR succeeded")
	}
}

func TestDialFindsTheDefaultSocket(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	serveAt(t, filepath.Join(dir, "omaslack.sock"), rpc.Options{})
	dial(t, Options{Client: "test", Run: noStart(t)})

	t.Setenv("XDG_RUNTIME_DIR", "")
	if _, err := Dial(context.Background(), Options{Client: "test", Run: noStart(t)}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}
