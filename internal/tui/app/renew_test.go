package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"gitlab.com/scabb-island/omaslack/internal/tui/client"
	"gitlab.com/scabb-island/omaslack/internal/tui/image"
	"gitlab.com/scabb-island/omaslack/internal/tui/theme"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// renewModel builds a model whose only workspace has the given provider
// and state, and records the team any renew refresh is asked for.
func renewModel(t *testing.T, provider, state string, called *string) *Model {
	t.Helper()
	ws := wire.Workspace{Team: "T1", Name: "Acme", Provider: provider, State: state}
	f := client.NewFake(map[string]any{"protocol": 1, "workspaces": []wire.Workspace{ws}}, nil)
	return New(Options{
		Client: f, Theme: theme.Default(), Images: image.HalfBlock,
		Now: func() time.Time { return clock }, Loc: time.UTC,
		Refresh: func(team string) tea.Cmd { *called = team; return nil },
	}).(*Model)
}

func TestRenewRefreshesAnExpiredDesktopSession(t *testing.T) {
	var called string
	m := renewModel(t, "desktop-session", "auth_expired", &called)
	m.renew()
	if called != "T1" {
		t.Errorf("refresh asked for %q, want T1", called)
	}
	if !strings.Contains(m.status, "Renewing") || m.statusIsError {
		t.Errorf("status = %q (error=%v)", m.status, m.statusIsError)
	}
}

func TestRenewDoesNothingWhenNotExpired(t *testing.T) {
	for _, tc := range []struct{ provider, state string }{
		{"desktop-session", "connected"},
		{"internal-app", "auth_expired"}, // internal-app is not refreshed this way
	} {
		var called string
		m := renewModel(t, tc.provider, tc.state, &called)
		m.renew()
		if called != "" {
			t.Errorf("%s/%s: refreshed anyway (%q)", tc.provider, tc.state, called)
		}
		if !strings.Contains(m.status, "Nothing to renew") {
			t.Errorf("%s/%s: status = %q", tc.provider, tc.state, m.status)
		}
	}
}

func TestDefaultRefreshMapsTheCommandResult(t *testing.T) {
	orig := execRefresh
	t.Cleanup(func() { execRefresh = orig })

	execRefresh = func(context.Context, string) ([]byte, error) { return nil, nil }
	if msg := defaultRefresh("T1")().(resultMsg); msg.err != nil || msg.apply == nil {
		t.Errorf("success msg = %+v", msg)
	}
	execRefresh = func(context.Context, string) ([]byte, error) { return []byte("boom output"), errors.New("exit 1") }
	msg := defaultRefresh("T1")().(resultMsg)
	if msg.err == nil || !strings.Contains(msg.err.Error(), "boom output") {
		t.Errorf("failure msg = %+v", msg)
	}
}

func TestRenewResultReportsFailure(t *testing.T) {
	var called string
	m := renewModel(t, "desktop-session", "auth_expired", &called)
	// A failed renew arrives as a resultMsg through Update.
	m.Update(resultMsg{method: "renew", err: errors.New("boom output")})
	if !m.statusIsError || !strings.Contains(m.status, "boom output") {
		t.Errorf("status = %q (error=%v)", m.status, m.statusIsError)
	}
}
