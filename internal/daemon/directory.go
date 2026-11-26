package daemon

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"

	"gitlab.com/scabb-island/omaslack/internal/api"
	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/store"
	"gitlab.com/scabb-island/omaslack/internal/syncengine"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// workspace is one signed-in workspace and its running engine. Once
// built, nothing in it changes: a reload replaces the whole workspace.
type workspace struct {
	api.Workspace
	creds  keyring.Credentials // what it was started with
	store  store.Store
	cancel context.CancelFunc // stops the engine
	done   chan struct{}      // closed once the engine's Run returned
}

// run runs the engine until the daemon stops it or it stops by itself.
// Either way the workspace stays listed with the engine's last state, so a
// workspace whose credentials expired reads auth_expired.
func (w *workspace) run(ctx context.Context, eng *syncengine.Engine, log *slog.Logger) {
	defer close(w.done)
	err := eng.Run(ctx)
	switch {
	case ctx.Err() != nil:
	case errors.Is(err, provider.ErrAuthExpired):
		log.Warn("daemon: Slack rejected the credentials; sign in again with omaslack auth add, then systemctl --user reload omaslack",
			"team", w.Team, "provider", w.Provider)
	default:
		log.Error("daemon: the sync engine stopped; the workspace serves its cache until systemctl --user reload omaslack", "team", w.Team, "err", err)
	}
}

// stopped reports whether the engine stopped by itself.
func (w *workspace) stopped() bool {
	select {
	case <-w.done:
		return true
	default:
		return false
	}
}

// stop stops the engine, waits for it and closes the store.
func (w *workspace) stop() error {
	w.cancel()
	<-w.done
	return w.store.Close()
}

// directory is the api.Directory of the running workspaces. It is safe
// for concurrent use.
type directory struct {
	mu sync.RWMutex
	ws map[domain.TeamID]*workspace
}

var _ api.Directory = (*directory)(nil)

func newDirectory() *directory {
	return &directory{ws: map[domain.TeamID]*workspace{}}
}

// Workspaces returns every workspace, ordered by team ID.
func (d *directory) Workspaces() []api.Workspace {
	running := d.running()
	out := make([]api.Workspace, len(running))
	for i, w := range running {
		out[i] = w.Workspace
	}
	return out
}

// running returns the running workspaces, ordered by team ID.
func (d *directory) running() []*workspace {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return slices.SortedFunc(maps.Values(d.ws), func(a, b *workspace) int {
		return strings.Compare(a.Team.String(), b.Team.String())
	})
}

func (d *directory) put(w *workspace) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ws[w.Team] = w
}

func (d *directory) remove(team domain.TeamID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.ws, team)
}

// hello adds the workspaces and each one's capabilities to the hello
// result: {workspaces: []wire.Workspace, capabilities: {team: []string}}.
func (d *directory) hello(context.Context, rpc.Hello) (map[string]any, error) {
	ws := d.Workspaces()
	infos := make([]wire.Workspace, len(ws))
	caps := make(map[string][]string, len(ws))
	for i, w := range ws {
		infos[i] = w.Info()
		caps[infos[i].Team] = infos[i].Capabilities
	}
	return map[string]any{"workspaces": infos, "capabilities": caps}, nil
}
