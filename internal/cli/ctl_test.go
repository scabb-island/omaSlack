package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/events"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

var barSummary = wire.Summary{
	Workspaces: map[string]wire.Counts{"T0001": {DMs: 1, Mentions: 2, Channels: 3}},
	Total:      wire.Counts{DMs: 1, Mentions: 2, Channels: 3},
}

// ctlDaemon is a daemon on a temp socket with a few methods and a real
// event hub.
type ctlDaemon struct {
	hub  *events.Hub
	path string
	stop func()
}

func startCtlDaemon(t *testing.T) *ctlDaemon {
	t.Helper()
	hub := events.New(events.Options{Summary: func(context.Context) (wire.Summary, error) { return barSummary, nil }})
	router := rpc.Router{
		"events.subscribe": hub.Subscribe(),
		"unread.summary": rpc.HandlerFunc(func(context.Context, string, json.RawMessage) (any, error) {
			return barSummary, nil
		}),
		"echo": rpc.HandlerFunc(func(_ context.Context, _ string, p json.RawMessage) (any, error) { return p, nil }),
		"fail": rpc.HandlerFunc(func(context.Context, string, json.RawMessage) (any, error) {
			return nil, &rpc.Error{Code: rpc.CodeUnsupportedCapability, Message: "unsupported_capability"}
		}),
	}
	path := filepath.Join(t.TempDir(), "omaslack.sock")
	l, err := rpc.Listen(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	srv := rpc.NewServer(router, rpc.Options{})
	done := make(chan error, 1)
	go func() { done <- srv.Serve(l) }()
	var once sync.Once
	d := &ctlDaemon{hub: hub, path: path, stop: func() {
		once.Do(func() { _ = srv.Close(); <-done })
	}}
	t.Cleanup(d.stop)
	return d
}

func (d *ctlDaemon) env(stdin io.Reader, stdout, stderr io.Writer) Env {
	return Env{Stdin: stdin, Stdout: stdout, Stderr: stderr, DialDaemon: func(ctx context.Context) (*rpcclient.Client, error) {
		return rpcclient.Dial(ctx, rpcclient.Options{
			Path: d.path, Client: "omaslack-ctl",
			Run: func(context.Context, string, ...string) error { return errors.New("no systemd in tests") },
		})
	}}
}

func (d *ctlDaemon) ctl(args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = Run(d.env(strings.NewReader(""), &out, &errb), append([]string{"ctl"}, args...))
	return code, out.String(), errb.String()
}

// Spec daemon-api, "Bar script".
func TestCtlPrintsTheResultAsOneJSONObject(t *testing.T) {
	code, out, errOut := startCtlDaemon(t).ctl("unread.summary")
	if code != ExitOK || errOut != "" {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	var got wire.Summary
	if err := json.Unmarshal([]byte(out), &got); err != nil || !strings.HasSuffix(out, "}\n") || strings.Count(out, "\n") != 1 {
		t.Fatalf("stdout = %q (%v), want one JSON object on one line", out, err)
	}
	for _, key := range []string{`"dms":1`, `"mentions":2`, `"channels":3`, `"threads":0`, `"total"`} {
		if !strings.Contains(out, key) {
			t.Errorf("stdout %s has no %s", out, key)
		}
	}
}

func TestCtlSendsTheParams(t *testing.T) {
	code, out, _ := startCtlDaemon(t).ctl("echo", `{"team":"T0001"}`)
	if code != ExitOK || out != "{\"team\":\"T0001\"}\n" {
		t.Fatalf("code=%d stdout=%q", code, out)
	}
}

func TestCtlReportsTheDaemonsError(t *testing.T) {
	code, out, errOut := startCtlDaemon(t).ctl("fail")
	if code != ExitRPCError || out != "" {
		t.Fatalf("code=%d stdout=%q, want %d and nothing on stdout", code, out, ExitRPCError)
	}
	if !strings.Contains(errOut, "-32002") || !strings.Contains(errOut, "unsupported_capability") {
		t.Errorf("stderr = %q, want the code and message", errOut)
	}
}

func TestCtlWithoutADaemonIsUnavailable(t *testing.T) {
	var out, errb bytes.Buffer
	env := Env{Stdout: &out, Stderr: &errb, DialDaemon: func(context.Context) (*rpcclient.Client, error) {
		return nil, fmt.Errorf("%w: Unit omaslack.service not found", rpcclient.ErrUnavailable)
	}}
	code := Run(env, []string{"ctl", "unread.summary"})
	if code != ExitUnavailable || !strings.Contains(errb.String(), "not found") {
		t.Fatalf("code=%d stderr=%q, want %d with the reason", code, errb.String(), ExitUnavailable)
	}
}

func TestCtlUsageErrors(t *testing.T) {
	d := startCtlDaemon(t)
	for _, args := range [][]string{
		{},
		{"echo", "not json"},
		{"echo", `"a string"`},
		{"echo", "{}", "extra"},
		{"--stream"},
		{"--stream", ""},
		{"--stream", "a", "b"},
		{"--bogus"},
	} {
		code, out, errOut := d.ctl(args...)
		if code != ExitUsage || out != "" || !strings.Contains(errOut, "Usage: omaslack ctl") {
			t.Errorf("ctl %q: code=%d stdout=%q stderr=%q", args, code, out, errOut)
		}
	}
}

// ── --stream (spec daemon-api, "Control CLI for scripts") ──

// stream runs ctl --stream with pipes for stdin and stdout. stderr is only
// read after it exits.
type stream struct {
	t    *testing.T
	in   *io.PipeWriter
	out  *bufio.Reader
	errb *bytes.Buffer
	code chan int
}

func (d *ctlDaemon) stream(t *testing.T, topics string) *stream {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := &stream{t: t, in: inW, out: bufio.NewReader(outR), errb: &bytes.Buffer{}, code: make(chan int, 1)}
	go func() {
		s.code <- Run(d.env(inR, outW, s.errb), []string{"ctl", "--stream", topics})
		_ = outW.Close()
	}()
	t.Cleanup(func() { _ = inW.Close(); _ = outR.Close() })
	return s
}

func (s *stream) send(line string) {
	s.t.Helper()
	if _, err := io.WriteString(s.in, line+"\n"); err != nil {
		s.t.Fatalf("stdin: %v", err)
	}
}

func (s *stream) line() string {
	s.t.Helper()
	got := make(chan string, 1)
	go func() {
		l, err := s.out.ReadString('\n')
		if err != nil {
			l = "ERROR " + err.Error()
		}
		got <- l
	}()
	select {
	case l := <-got:
		return strings.TrimSuffix(l, "\n")
	case <-time.After(5 * time.Second):
		s.t.Fatal("no line on stdout")
		return ""
	}
}

func (s *stream) exit() int {
	s.t.Helper()
	select {
	case c := <-s.code:
		return c
	case <-time.After(5 * time.Second):
		s.t.Fatal("ctl --stream did not exit")
		return 0
	}
}

func TestStreamPrintsEventsAndForwardsRequests(t *testing.T) {
	d := startCtlDaemon(t)
	s := d.stream(t, "message.created,unread.summary")

	if l := s.line(); l != `{"jsonrpc":"2.0","method":"unread.summary","params":`+jsonText(t, barSummary)+`}` {
		t.Fatalf("first line = %s, want the current summary", l)
	}
	// Requests are read once the subscription is in place.
	s.send(`{"jsonrpc":"2.0","id":"a","method":"echo","params":[1]}`)
	if l := s.line(); l != `{"jsonrpc":"2.0","id":"a","result":[1]}` {
		t.Fatalf("echo response = %s", l)
	}

	ev := domain.MessageCreated{
		Team:         mustParse(t, domain.ParseTeamID, "T0001"),
		Conversation: mustParse(t, domain.ParseConversationID, "C0001"),
		Message:      domain.Message{TS: mustParse(t, domain.ParseTS, "1700000000.000001"), Text: "hi"},
	}
	d.hub.Publish(ev)
	if l := s.line(); l != `{"jsonrpc":"2.0","method":"message.created","params":`+jsonText(t, wire.FromEvent(ev))+`}` {
		t.Fatalf("event line = %s", l)
	}

	s.send(`{"jsonrpc":"2.0","id":7,"method":"fail"}`)
	if l := s.line(); l != `{"jsonrpc":"2.0","id":7,"error":{"code":-32002,"message":"unsupported_capability"}}` {
		t.Fatalf("fail response = %s", l)
	}
	s.send(`not json`)
	if l := s.line(); l != `{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"parse error"}}` {
		t.Fatalf("parse error response = %s", l)
	}
	s.send(`{"jsonrpc":"2.0","method":"echo","params":[2]}`) // a notification: no response
	s.send(`{"jsonrpc":"2.0","id":8,"method":"echo","params":[3]}`)
	if l := s.line(); l != `{"jsonrpc":"2.0","id":8,"result":[3]}` {
		t.Fatalf("got %s, want only the answer to id 8", l)
	}

	_ = s.in.Close() // stdin ending does not stop the stream
	d.hub.Publish(ev)
	if l := s.line(); !strings.Contains(l, "message.created") {
		t.Fatalf("after stdin closed got %s", l)
	}

	d.stop()
	if code := s.exit(); code != ExitUnavailable {
		t.Errorf("exit code = %d after the daemon left, want %d", code, ExitUnavailable)
	}
	if !strings.Contains(s.errb.String(), "connection") {
		t.Errorf("stderr = %q, want the lost connection", s.errb.String())
	}
}

func TestStreamRejectsUnknownTopics(t *testing.T) {
	d := startCtlDaemon(t)
	s := d.stream(t, "message.created,nope")
	if code := s.exit(); code != ExitRPCError {
		t.Fatalf("exit code = %d, want %d", code, ExitRPCError)
	}
	if !strings.Contains(s.errb.String(), "unknown topic nope") {
		t.Errorf("stderr = %q", s.errb.String())
	}
}

func TestCtlFindsTheDaemonInTheRuntimeDir(t *testing.T) {
	d := startCtlDaemon(t)
	t.Setenv("XDG_RUNTIME_DIR", filepath.Dir(d.path))
	var out, errb bytes.Buffer
	code := Run(Env{Stdout: &out, Stderr: &errb}, []string{"ctl", "echo", "[1]"})
	if code != ExitOK || out.String() != "[1]\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errb.String())
	}
}

func TestStreamWithoutADaemonIsUnavailable(t *testing.T) {
	var errb bytes.Buffer
	env := Env{Stdout: io.Discard, Stderr: &errb, DialDaemon: func(context.Context) (*rpcclient.Client, error) {
		return nil, rpcclient.ErrUnavailable
	}}
	if code := Run(env, []string{"ctl", "--stream", "typing"}); code != ExitUnavailable {
		t.Fatalf("code=%d stderr=%q", code, errb.String())
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// A reader that went away (ctl --stream | head -1) ends the stream.
func TestStreamEndsWhenStdoutFails(t *testing.T) {
	d := startCtlDaemon(t)
	var errb bytes.Buffer
	code := Run(d.env(nil, brokenWriter{}, &errb), []string{"ctl", "--stream", "unread.summary"})
	if code != ExitError || !strings.Contains(errb.String(), "closed pipe") {
		t.Fatalf("code=%d stderr=%q, want %d and the write error", code, errb.String(), ExitError)
	}
}

func TestStreamSkipsBlankLinesAndStopsForwardingWhenStdoutFails(t *testing.T) {
	d := startCtlDaemon(t)
	out := &failingStdout{failAfter: 1, writes: make(chan struct{}, 16)}
	in := strings.NewReader("\n  \n" + `{"jsonrpc":"2.0","id":1,"method":"echo","params":[1]}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"echo","params":[2]}` + "\n")
	done := make(chan int, 1)
	go func() { done <- Run(d.env(in, out, io.Discard), []string{"ctl", "--stream", "typing"}) }()

	out.wait(t, 2) // the first response, then the failed write
	if got := out.String(); got != `{"jsonrpc":"2.0","id":1,"result":[1]}`+"\n" {
		t.Fatalf("stdout = %q", got)
	}
	d.stop()
	<-done
}

// failingStdout keeps the first failAfter writes, fails every later one
// and signals each attempt on writes.
type failingStdout struct {
	mu        sync.Mutex
	b         bytes.Buffer
	failAfter int
	writes    chan struct{}
}

func (f *failingStdout) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes <- struct{}{}
	if f.failAfter == 0 {
		return 0, io.ErrClosedPipe
	}
	f.failAfter--
	return f.b.Write(p)
}

func (f *failingStdout) String() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.b.String()
}

func (f *failingStdout) wait(t *testing.T, writes int) {
	t.Helper()
	for range writes {
		select {
		case <-f.writes:
		case <-time.After(5 * time.Second):
			t.Fatal("ctl stopped writing")
		}
	}
}

func jsonText(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustParse[T any](t *testing.T, parse func(string) (T, error), s string) T {
	t.Helper()
	v, err := parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
