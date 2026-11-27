package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// A client sees the workspace, subscribes, and receives what the provider
// streams, which the cache then serves.
func TestEndToEndAMessageReachesTheClientAndTheCache(t *testing.T) {
	h := newHarness(t)
	team := h.addTeam("T0ACME")
	live := make(chan domain.Event)
	h.provs.fake(team).Script(providertest.Session{Live: live})
	h.start()
	c := h.dial()

	var hello struct {
		Workspaces   []wire.Workspace    `json:"workspaces"`
		Capabilities map[string][]string `json:"capabilities"`
	}
	if err := json.Unmarshal(c.Hello(), &hello); err != nil {
		t.Fatal(err)
	}
	if len(hello.Workspaces) != 1 || hello.Workspaces[0].Name != "Name of T0ACME" || hello.Workspaces[0].Provider != "desktop-session" ||
		hello.Workspaces[0].Self != "U0ME" {
		t.Fatalf("hello workspaces = %+v", hello.Workspaces)
	}
	if got := hello.Capabilities["T0ACME"]; !slices.Equal(got, []string{"realtime", "search"}) {
		t.Fatalf("hello capabilities = %v", hello.Capabilities)
	}
	if ws := call[[]wire.Workspace](t, c, "workspaces.list", nil); !slices.Equal(teamsOf(ws), []string{"T0ACME"}) || ws[0].Self != "U0ME" {
		t.Fatalf("workspaces.list = %+v", ws)
	}
	call[json.RawMessage](t, c, "events.subscribe", map[string]any{"topics": []string{"message.created"}})

	conv, _ := domain.ParseConversationID("C0GENERAL")
	ts, _ := domain.ParseTS("1700000000.000100")
	live <- domain.MessageCreated{Team: team, Conversation: conv, Message: domain.Message{
		TS: ts, Author: domain.UserAuthor(mustUser(t, "U0YOU")), Text: "hello daemon",
	}}

	select {
	case n := <-c.Notifications():
		var ev wire.MessageEvent
		if err := json.Unmarshal(n.Params, &ev); err != nil || n.Method != "message.created" || ev.Message.Text != "hello daemon" {
			t.Fatalf("notification %s %s (%v)", n.Method, n.Params, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no message.created notification")
	}
	history := call[[]wire.Message](t, c, "messages.history", map[string]string{"team": "T0ACME", "conv": "C0GENERAL"})
	if len(history) != 1 || history[0].Text != "hello daemon" || history[0].TS != "1700000000.000100" {
		t.Fatalf("messages.history = %+v", history)
	}
}

func TestWorkspacesAreListedInTeamOrder(t *testing.T) {
	h := newHarness(t)
	h.addTeam("T0ZED")
	h.addTeam("T0ACME")
	h.addTeam("T0MID")
	h.start()
	ws := call[[]wire.Workspace](t, h.dial(), "workspaces.list", nil)
	if got := teamsOf(ws); !slices.Equal(got, []string{"T0ACME", "T0MID", "T0ZED"}) {
		t.Fatalf("workspaces.list order = %v", got)
	}
}

func TestUnreadableCredentialsSkipOnlyTheirWorkspace(t *testing.T) {
	h := newHarness(t)
	h.addTeam("T0ACME")
	h.keys.broken[mustTeam(t, "T0BAD")] = errBroken
	h.start()
	if got := teamsOf(call[[]wire.Workspace](t, h.dial(), "workspaces.list", nil)); !slices.Equal(got, []string{"T0ACME"}) {
		t.Fatalf("workspaces = %v, want only T0ACME", got)
	}
}

func TestAProviderThatCannotBeBuiltSkipsOnlyItsWorkspace(t *testing.T) {
	h := newHarness(t)
	h.addTeam("T0ACME")
	h.addTeam("T0NOPE")
	build := h.cfg.NewProvider
	h.cfg.NewProvider = func(team domain.TeamID, creds keyring.Credentials) (provider.Provider, error) {
		if team.String() == "T0NOPE" {
			return nil, errors.New("unknown provider")
		}
		return build(team, creds)
	}
	h.start()
	if got := teamsOf(call[[]wire.Workspace](t, h.dial(), "workspaces.list", nil)); !slices.Equal(got, []string{"T0ACME"}) {
		t.Fatalf("workspaces = %v, want only T0ACME", got)
	}
}

func TestAWorkspaceWithExpiredCredentialsStaysListed(t *testing.T) {
	h := newHarness(t)
	team := h.addTeam("T0ACME")
	h.provs.fake(team).Script(providertest.Session{Err: provider.ErrAuthExpired})
	h.start()
	c := h.dial()
	eventually(t, "state auth_expired", func() bool {
		ws := call[[]wire.Workspace](t, c, "workspaces.list", nil)
		return len(ws) == 1 && ws[0].State == "auth_expired"
	})
}

func TestShutdownRemovesTheSocket(t *testing.T) {
	h := newHarness(t)
	h.addTeam("T0ACME")
	h.start()
	c := h.dial()
	if err := h.stop(); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if _, err := os.Stat(h.cfg.Paths.Socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket still there: %v", err)
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the client stayed connected")
	}
}

// stuck is a provider whose stream ignores its context until released,
// so its engine cannot stop.
type stuck struct {
	*providertest.Fake
	release chan struct{}
}

func (s stuck) Connect(context.Context) (<-chan domain.Event, error) {
	<-s.release
	return nil, errors.New("released")
}

func TestShutdownIsBounded(t *testing.T) {
	h := newHarness(t)
	h.addTeam("T0ACME")
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	h.cfg.NewProvider = func(team domain.TeamID, _ keyring.Credentials) (provider.Provider, error) {
		return stuck{Fake: providertest.New(team, testCaps), release: release}, nil
	}
	h.cfg.ShutdownTimeout = 50 * time.Millisecond
	h.start()
	h.dial()
	if err := h.stop(); !errors.Is(err, ErrShutdownTimeout) {
		t.Fatalf("Run = %v, want ErrShutdownTimeout", err)
	}
}

func TestASecondDaemonFindsTheSocketInUse(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.dial()
	err := Run(context.Background(), h.cfg)
	if !errors.Is(err, rpc.ErrSocketInUse) {
		t.Fatalf("second Run = %v, want ErrSocketInUse", err)
	}
}

func TestAKeyringThatCannotBeOpenedStopsTheStart(t *testing.T) {
	h := newHarness(t)
	h.keys.err = errors.New("secret service refused the session")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Run(ctx, h.cfg); err == nil || !errors.Is(err, h.keys.err) {
		t.Fatalf("Run = %v, want the keyring's error", err)
	}
	if _, err := os.Stat(h.cfg.Paths.Socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket left behind: %v", err)
	}
}

func TestRunNeedsPaths(t *testing.T) {
	if err := Run(context.Background(), Config{}); err == nil {
		t.Fatal("Run without paths did not fail")
	}
}
