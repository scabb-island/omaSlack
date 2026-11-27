package daemon

import (
	"context"
	"errors"
	"slices"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

func listTeams(t *testing.T, c *rpcclient.Client) []string {
	t.Helper()
	return teamsOf(call[[]wire.Workspace](t, c, "workspaces.list", nil))
}

func TestReloadStartsAddedAndStopsRemovedWorkspaces(t *testing.T) {
	h := newHarness(t)
	acme := h.addTeam("T0ACME")
	h.start()
	c := h.dial()

	h.addTeam("T0NEW")
	h.reloadNow()
	eventually(t, "T0NEW listed", func() bool { return slices.Equal(listTeams(t, c), []string{"T0ACME", "T0NEW"}) })

	_ = h.keys.Delete(context.Background(), acme)
	h.reloadNow()
	eventually(t, "T0ACME gone", func() bool { return slices.Equal(listTeams(t, c), []string{"T0NEW"}) })
	if h.provs.builds(acme) != 1 {
		t.Fatalf("T0ACME built %d times", h.provs.builds(acme))
	}
}

func TestReloadRestartsAWorkspaceWhoseCredentialsChanged(t *testing.T) {
	h := newHarness(t)
	acme := h.addTeam("T0ACME")
	other := h.addTeam("T0OTHER")
	h.start()
	h.dial()

	_ = h.keys.Set(context.Background(), acme, keyring.InternalApp{UserToken: "xoxp-new", AppToken: "xapp-new"})
	h.reloadNow()
	eventually(t, "T0ACME rebuilt", func() bool { return h.provs.builds(acme) == 2 })
	h.reloadNow() // the reload above is done once this one is taken
	if h.provs.builds(other) != 1 {
		t.Fatalf("the unchanged T0OTHER was rebuilt: %d builds", h.provs.builds(other))
	}
}

func TestReloadRetriesAWorkspaceWhoseEngineStopped(t *testing.T) {
	h := newHarness(t)
	acme := h.addTeam("T0ACME")
	h.provs.fake(acme).Script(providertest.Session{Err: provider.ErrAuthExpired})
	h.start()
	c := h.dial()
	eventually(t, "auth_expired", func() bool {
		return call[[]wire.Workspace](t, c, "workspaces.list", nil)[0].State == "auth_expired"
	})

	h.reloadNow()
	eventually(t, "T0ACME restarted", func() bool { return h.provs.builds(acme) == 2 })
	eventually(t, "a fresh state", func() bool {
		return call[[]wire.Workspace](t, c, "workspaces.list", nil)[0].State != "auth_expired"
	})
}

func TestReloadKeepsTheWorkspacesWhenTheKeyringFails(t *testing.T) {
	h := newHarness(t)
	h.addTeam("T0ACME")
	h.start()
	c := h.dial()

	h.keys.mu.Lock()
	h.keys.err = errors.New("secret service refused the session")
	h.keys.mu.Unlock()
	h.reloadNow()
	h.reloadNow()
	if got := listTeams(t, c); !slices.Equal(got, []string{"T0ACME"}) {
		t.Fatalf("workspaces = %v after a failed reload", got)
	}
}

func TestAWorkspaceThatCannotRestartIsUnlisted(t *testing.T) {
	h := newHarness(t)
	acme := h.addTeam("T0ACME")
	build := h.cfg.NewProvider
	h.cfg.NewProvider = func(team domain.TeamID, creds keyring.Credentials) (provider.Provider, error) {
		if _, ok := creds.(keyring.InternalApp); ok {
			return nil, errors.New("no provider for these credentials")
		}
		return build(team, creds)
	}
	h.start()
	c := h.dial()

	_ = h.keys.Set(context.Background(), acme, keyring.InternalApp{UserToken: "xoxp-new"})
	h.reloadNow()
	eventually(t, "T0ACME unlisted", func() bool { return len(listTeams(t, c)) == 0 })
}

// TestWorkspacesReloadMethodTriggersAReload checks that the RPC method does
// the same as a SIGHUP, so auth add/refresh take effect without systemd.
func TestWorkspacesReloadMethodTriggersAReload(t *testing.T) {
	h := newHarness(t)
	h.addTeam("T0ACME")
	h.start()
	c := h.dial()

	h.addTeam("T0NEW")
	if ack := call[wire.ReloadAck](t, c, "workspaces.reload", nil); !ack.Reloading {
		t.Fatalf("workspaces.reload ack = %+v", ack)
	}
	eventually(t, "T0NEW listed after workspaces.reload", func() bool {
		return slices.Equal(listTeams(t, c), []string{"T0ACME", "T0NEW"})
	})
}
