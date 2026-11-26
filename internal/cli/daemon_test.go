package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// syncBuffer is a bytes.Buffer the daemon's log and the test may share.
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

// daemonEnv is an Env with one internal-app workspace in a file keyring
// and a fake Slack that only answers auth.test, and the XDG directories
// in a temporary directory. It returns the socket path.
func daemonEnv(t *testing.T, stderr *syncBuffer) (Env, string) {
	t.Helper()
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth.test" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"team":"Acme","team_id":"T0ACME","user_id":"U0ME"}`))
	}))
	t.Cleanup(slack.Close)

	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	ks := keyring.NewFileStore(filepath.Join(dir, "credentials"))
	if err := ks.Set(context.Background(), team(t, "T0ACME"), keyring.InternalApp{UserToken: "xoxp-1", AppToken: "xapp-1"}); err != nil {
		t.Fatal(err)
	}
	return Env{
		Stdout: &bytes.Buffer{}, Stderr: stderr,
		Keyring:     func(context.Context) (keyring.ClosableStore, error) { return ks, nil },
		SlackAPIURL: slack.URL + "/",
	}, filepath.Join(dir, rpcclient.SocketName)
}

func waitForLog(t *testing.T, log *syncBuffer, line string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(log.String(), line) {
		if time.Now().After(deadline) {
			t.Fatalf("no %q in the log:\n%s", line, log.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The daemon serves the keyring's workspaces, refuses a second instance,
// reloads on SIGHUP and stops cleanly on SIGTERM.
func TestDaemonRunsUntilSIGTERM(t *testing.T) {
	var log syncBuffer
	env, socket := daemonEnv(t, &log)
	code := make(chan int, 1)
	go func() { code <- Run(env, []string{"daemon"}) }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := rpcclient.Dial(ctx, rpcclient.Options{Client: "cli-test", Path: socket,
		Run: func(context.Context, string, ...string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var hello struct {
		Workspaces []wire.Workspace `json:"workspaces"`
	}
	if err := json.Unmarshal(c.Hello(), &hello); err != nil || len(hello.Workspaces) != 1 || hello.Workspaces[0].Name != "Acme" {
		t.Fatalf("hello = %s (%v)", c.Hello(), err)
	}

	var second syncBuffer
	if got := Run(Env{Stdout: &bytes.Buffer{}, Stderr: &second, Keyring: env.Keyring}, []string{"daemon"}); got != ExitError ||
		!strings.Contains(second.String(), "another omaslack daemon is already running") {
		t.Errorf("second daemon: code %d, stderr %q", got, second.String())
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	waitForLog(t, &log, "daemon: reloaded")

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-code:
		if got != ExitOK {
			t.Fatalf("exit code %d, log:\n%s", got, log.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the daemon did not stop on SIGTERM")
	}
	if _, err := os.Stat(socket); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("socket left behind: %v", err)
	}
}

func TestDaemonNeedsTheRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	code, _, errOut := run("daemon")
	if code != ExitError || !strings.Contains(errOut, "XDG_RUNTIME_DIR is not set") {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
}

func TestDaemonTakesNoArguments(t *testing.T) {
	code, _, errOut := run("daemon", "--now")
	if code != ExitUsage || !strings.Contains(errOut, "Usage: omaslack daemon") {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
}
