package desktopsession

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Slack's web client socket stays silent (no hello, no pings) when the
// start arguments carry an empty agent_version, and answers an error frame
// to an implausible one. It must look like a client build timestamp.
func TestTheSocketDeclaresAClientBuildVersion(t *testing.T) {
	p, _ := newFakeSlack(t, nil).provider(t)

	q, err := url.Parse(p.socketAddress())
	if err != nil {
		t.Fatal(err)
	}
	args, err := url.ParseQuery(strings.TrimPrefix(q.Query().Get("start_args"), "?"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := strconv.ParseInt(args.Get("agent_version"), 10, 64)
	if err != nil {
		t.Fatalf("agent_version = %q, want a unix timestamp", args.Get("agent_version"))
	}
	if v < 1_600_000_000 {
		t.Errorf("agent_version = %d, want a plausible build time (after 2020)", v)
	}
}

func TestAnErrorFrameIsLoggedWithSlacksCodeAndMessage(t *testing.T) {
	frames := []string{`{"type":"error","error":{"code":2,"msg":"bad start args"}}`}
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/auth.test": jsonBody(http.StatusOK, authOK),
		"/ws/":           socketServer(t, frames, waitForClose),
	})
	p, logs := f.provider(t)
	p.readTimeout = 50 * time.Millisecond

	rest(t, connect(t, p, context.Background()))

	for _, want := range []string{"error frame", "2", "bad start args"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs = %q, want them to contain %q", logs, want)
		}
	}
}

func TestASocketThatNeverSaysHelloIsReportedAsSuch(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/auth.test": jsonBody(http.StatusOK, authOK),
		"/ws/":           socketServer(t, nil, waitForClose),
	})
	p, logs := f.provider(t)
	p.readTimeout = 50 * time.Millisecond

	rest(t, connect(t, p, context.Background()))

	if !strings.Contains(logs.String(), "never sent hello") {
		t.Errorf("logs = %q, want the missing hello named", logs)
	}
}
