package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/rpc"
)

// The events hub calls Summary for its unread.summary event, so it must
// match what the method answers.
func TestSummaryIsWhatUnreadSummaryAnswers(t *testing.T) {
	f := newFixture()
	got, err := Summary(context.Background(), f.dir)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := Methods(f.dir, f.focus).Handle(context.Background(), "unread.summary", nil)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(answer)
	g, _ := json.Marshal(got)
	if string(a) != string(g) {
		t.Fatalf("unread.summary = %s, Summary = %s", a, g)
	}
}

func TestAnUnreadableListFailsTheSummaryAndTheList(t *testing.T) {
	f := newFixture()
	f.dir[1].Store.(*fakeStore).listErr = errDisk
	if _, err := Summary(context.Background(), f.dir); !errors.Is(err, errDisk) {
		t.Fatalf("Summary err = %v, want the store's", err)
	}
	m := Methods(f.dir, f.focus)
	_, err := m.Handle(context.Background(), "conversations.list", json.RawMessage(`{"team":"T0BETA"}`))
	if !errors.Is(err, errDisk) {
		t.Fatalf("conversations.list err = %v, want the store's", err)
	}
}

func TestUsersInfoTakesUpToItsMaximumOfIDs(t *testing.T) {
	f := newFixture()
	ids := make([]string, UsersInfoMaxIDs)
	for i := range ids {
		ids[i] = fmt.Sprintf("U%03d", i)
	}
	params, _ := json.Marshal(map[string]any{"team": "T0ACME", "ids": ids})
	if _, err := Methods(f.dir, f.focus).Handle(context.Background(), "users.info", params); err != nil {
		t.Fatalf("users.info with %d IDs: %v", len(ids), err)
	}
}

// ── End to end, over a real socket ──

// session is a client connected to a daemon serving Methods.
type session struct {
	t  *testing.T
	nc net.Conn
	r  *bufio.Reader
	id int
}

func serveMethods(t *testing.T, f *fixture) *session {
	t.Helper()
	path := filepath.Join(t.TempDir(), "omaslack.sock")
	l, err := rpc.Listen(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	s := rpc.NewServer(Methods(f.dir, f.focus), rpc.Options{})
	done := make(chan error, 1)
	go func() { done <- s.Serve(l) }()
	t.Cleanup(func() {
		_ = s.Close()
		if err := <-done; err != nil {
			t.Errorf("Serve: %v", err)
		}
	})
	var d net.Dialer
	nc, err := d.DialContext(context.Background(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nc.Close() })
	c := &session{t: t, nc: nc, r: bufio.NewReader(nc)}
	c.call("hello", `{"client":"test","protocol":1}`)
	return c
}

type answer struct {
	Result json.RawMessage `json:"result"`
	Error  *rpc.Error      `json:"error"`
}

func (c *session) call(method, params string) answer {
	c.t.Helper()
	c.id++
	if _, err := fmt.Fprintf(c.nc, `{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`+"\n", c.id, method, params); err != nil {
		c.t.Fatal(err)
	}
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		c.t.Fatal(err)
	}
	var a answer
	if err := json.Unmarshal(line, &a); err != nil {
		c.t.Fatalf("%q: %v", line, err)
	}
	return a
}

func (c *session) wantError(a answer, code rpc.Code) {
	c.t.Helper()
	if a.Error == nil || a.Error.Code != code {
		c.t.Fatalf("answer = %s %+v, want error %d", a.Result, a.Error, code)
	}
}

// Spec daemon-api, "Search not supported".
func TestSearchWithoutTheCapabilityOverTheSocket(t *testing.T) {
	f := newFixture()
	c := serveMethods(t, f)
	c.wantError(c.call("search.messages", `{"team":"T0BETA","query":"deploy"}`), rpc.CodeUnsupportedCapability)
	if calls := f.engines["T0BETA"].recorded(); len(calls) != 0 {
		t.Fatalf("the engine was called: %v", calls)
	}
}

func TestFilesWithoutTheCapabilityOverTheSocket(t *testing.T) {
	f := newFixture()
	c := serveMethods(t, f)
	c.wantError(c.call("files.fetch", `{"team":"T0BETA","fileId":"F0REPORT"}`), rpc.CodeUnsupportedCapability)
	c.wantError(c.call("files.upload", `{"team":"T0BETA","conv":"C0ALERTS","paths":["/tmp/a"]}`), rpc.CodeUnsupportedCapability)
	if calls := f.engines["T0BETA"].recorded(); len(calls) != 0 {
		t.Fatalf("the engine was called: %v", calls)
	}
}

// Download URLs need Slack credentials: no answer may carry one. Every
// fake file's URL contains "must-not-leak".
func TestNoAnswerCarriesADownloadURL(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte("must-not-leak")) {
			t.Errorf("%s carries a download URL", name)
		}
	}
}

func TestErrorCodesOverTheSocket(t *testing.T) {
	c := serveMethods(t, newFixture())
	c.wantError(c.call("messages.send", `{"team":"T0EXPIRED","conv":"C0GENERAL","text":"x"}`), rpc.CodeAuthExpired)
	c.wantError(c.call("messages.send", `{"team":"T0NOPE","conv":"C0GENERAL","text":"x"}`), rpc.CodeInvalidParams)
	c.wantError(c.call("messages.history", `{"team":"T0ACME","conv":"C0BROKEN"}`), rpc.CodeInternalError)
}

func TestUnreadSummaryOverTheSocket(t *testing.T) {
	c := serveMethods(t, newFixture())
	a := c.call("unread.summary", `{}`)
	want := `{"workspaces":{"T0ACME":{"dms":1,"mentions":1,"channels":1,"threads":0},` +
		`"T0BETA":{"dms":0,"mentions":3,"channels":1,"threads":0},` +
		`"T0EXPIRED":{"dms":0,"mentions":0,"channels":0,"threads":0}},` +
		`"total":{"dms":1,"mentions":4,"channels":2,"threads":0}}`
	if a.Error != nil || string(a.Result) != want {
		t.Fatalf("unread.summary = %s %+v\nwant %s", a.Result, a.Error, want)
	}
}
