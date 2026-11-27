package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/api"
	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/events"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/notify"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/store/sqlite"
	"gitlab.com/scabb-island/omaslack/internal/syncengine"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// ErrShutdownTimeout means some engine did not stop within
// Config.ShutdownTimeout; Run returned without waiting for it.
var ErrShutdownTimeout = errors.New("daemon: shutdown timed out")

// DefaultShutdownTimeout bounds the orderly shutdown, well inside
// systemd's default 90 s stop timeout.
const DefaultShutdownTimeout = 10 * time.Second

// Config is what Run needs. Paths and Keyring are required.
type Config struct {
	Paths Paths
	// Keyring opens the credential store (keyring.Open).
	Keyring func(context.Context) (keyring.ClosableStore, error)
	// NewProvider builds each workspace's provider; default
	// SlackProviders(desktopsession.DefaultAPIURL, Log).
	NewProvider ProviderFactory
	// Identify asks Slack who is signed in; default AuthTest.
	Identify Identifier
	// Log is where the daemon logs; default discard.
	Log *slog.Logger
	// ShutdownTimeout bounds the shutdown; default DefaultShutdownTimeout.
	ShutdownTimeout time.Duration
	// Reload asks to read the keyring again: every signal received on it
	// (SIGHUP, from signal.Notify) is one reload. Nil means never.
	Reload <-chan os.Signal
	// Notifications builds the desktop notification sender, which tells
	// events about clicks and closes (notify.NewBus on the session bus).
	// Nil means no notifications.
	Notifications func(events notify.Events) notify.Sender
	// LaunchTUI starts the TUI or focuses its window, for a click that
	// opens it (LaunchTUI). Nil means never.
	LaunchTUI func() error
}

func (c Config) withDefaults() Config {
	c.Log = cmp.Or(c.Log, slog.New(slog.DiscardHandler))
	if c.NewProvider == nil {
		c.NewProvider = SlackProviders(desktopsession.DefaultAPIURL, c.Log)
	}
	if c.Identify == nil {
		c.Identify = AuthTest
	}
	c.ShutdownTimeout = cmp.Or(c.ShutdownTimeout, DefaultShutdownTimeout)
	return c
}

// daemon is one Run: the running workspaces and the events hub they
// publish to.
type daemon struct {
	cfg   Config
	log   *slog.Logger
	dir   *directory
	hub   *events.Hub
	cache identityCache
	// clients is every greeted connection and what it views.
	clients *clients
	// notifier is nil when notifications are off; pub is what the engines
	// publish to.
	notifier *notify.Notifier
	pub      syncengine.Publisher
	// engines is the context every engine derives from. It is not the
	// signal's context: engines stop only once the server has.
	engines context.Context
}

// Run is `omaslack daemon`. It claims the socket, starts one sync engine
// per workspace in the keyring and serves the v1 methods and
// events.subscribe until ctx ends, then shuts down in order: stop
// accepting and close every connection, stop every engine, close every
// store. It returns nil after a clean shutdown, ErrShutdownTimeout when
// an engine did not stop in time, and an error wrapping
// rpc.ErrSocketInUse when another daemon already answers on the socket.
func Run(ctx context.Context, cfg Config) error {
	cfg = cfg.withDefaults()
	if cfg.Paths.Socket == "" || cfg.Paths.Data == "" {
		return errors.New("daemon: the socket and data paths are required")
	}
	if err := os.MkdirAll(cfg.Paths.Data, 0o700); err != nil {
		return fmt.Errorf("daemon: create the data directory: %w", err)
	}
	l, err := rpc.Listen(ctx, cfg.Paths.Socket)
	if err != nil {
		return err
	}
	d := newDaemon(ctx, cfg)
	stored, err := d.stored(ctx)
	if err != nil {
		return errors.Join(err, l.Close())
	}
	hubCtx, stopHub := context.WithCancel(d.engines)
	defer stopHub()
	go func() { _ = d.hub.Run(hubCtx) }()
	var closeSender func()
	d.pub, closeSender = d.startNotifier(hubCtx)
	defer closeSender()
	d.apply(ctx, stored)

	srv := rpc.NewServer(d.router(), rpc.Options{Hello: d.clients.hello(d.dir.hello), Logger: d.log})
	served := make(chan error, 1)
	go func() { served <- srv.Serve(l) }()
	d.log.Info("daemon: serving", "socket", cfg.Paths.Socket, "workspaces", len(d.dir.running()))

	err = d.wait(ctx, served)
	return errors.Join(err, d.shutdown(srv))
}

func newDaemon(ctx context.Context, cfg Config) *daemon {
	d := &daemon{
		cfg: cfg, log: cfg.Log, dir: newDirectory(),
		cache:   identityCache{dir: cfg.Paths.Data},
		engines: context.WithoutCancel(ctx),
		clients: newClients(&notify.Views{}, time.Now),
	}
	d.hub = events.New(events.Options{
		Summary: func(ctx context.Context) (wire.Summary, error) { return api.Summary(ctx, d.dir) },
		HTML:    d.dir.html,
		Log:     cfg.Log,
	})
	return d
}

// router is every method: the v1 methods, events.subscribe and
// ui.viewing.
func (d *daemon) router() rpc.Router {
	r := api.Methods(d.dir, d.hub)
	r["events.subscribe"] = d.clients.subscribe(d.hub.Subscribe())
	r["ui.viewing"] = d.clients.viewing()
	return r
}

// wait serves, reloading on request, until ctx ends or the server fails.
func (d *daemon) wait(ctx context.Context, served <-chan error) error {
	for {
		select {
		case <-ctx.Done():
			d.log.Info("daemon: shutting down")
			return nil
		case err := <-served:
			return fmt.Errorf("daemon: serve: %w", err)
		case <-d.cfg.Reload:
			d.reload(ctx)
			d.loadNotifyConfig()
		}
	}
}

// reload brings the workspaces in line with the keyring: it stops the
// ones removed, restarts the ones whose credentials changed or whose
// engine stopped (so signing in again with auth add takes effect), and
// starts the ones added. A restarted workspace stays listed meanwhile.
// When the keyring cannot be read, nothing changes.
func (d *daemon) reload(ctx context.Context) {
	stored, err := d.stored(ctx)
	if err != nil {
		d.log.Error("daemon: reload failed; the workspaces stay as they were", "err", err)
		return
	}
	for _, w := range d.dir.running() {
		creds, ok := stored[w.Team]
		if ok && creds == w.creds && !w.stopped() {
			delete(stored, w.Team) // keep it running
			continue
		}
		if !ok {
			d.dir.remove(w.Team)
		}
		if err := w.stop(); err != nil {
			d.log.Warn("daemon: could not close a workspace's store", "team", w.Team, "err", err)
		}
	}
	d.apply(ctx, stored)
	d.log.Info("daemon: reloaded", "workspaces", len(d.dir.running()))
}

// shutdown closes the server, then stops every workspace, within
// ShutdownTimeout.
func (d *daemon) shutdown(srv *rpc.Server) error {
	done := make(chan error, 1)
	go func() {
		err := srv.Close()
		done <- errors.Join(err, d.stopAll())
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(d.cfg.ShutdownTimeout):
		return ErrShutdownTimeout
	}
}

// stopAll stops every workspace at once and waits for them all.
func (d *daemon) stopAll() error {
	running := d.dir.running()
	errs := make([]error, len(running))
	var wg sync.WaitGroup
	for i, w := range running {
		wg.Go(func() { errs[i] = w.stop() })
	}
	wg.Wait()
	return errors.Join(errs...)
}

// stored reads every workspace's credentials. Credentials that cannot be
// read are logged and skipped; only a keyring that cannot be read at all
// is an error.
func (d *daemon) stored(ctx context.Context) (map[domain.TeamID]keyring.Credentials, error) {
	ks, err := d.cfg.Keyring(ctx)
	if err != nil {
		return nil, fmt.Errorf("daemon: open the keyring: %w", err)
	}
	defer ks.Close()
	teams, err := ks.Teams(ctx)
	if err != nil {
		return nil, fmt.Errorf("daemon: list the workspaces in the keyring: %w", err)
	}
	out := make(map[domain.TeamID]keyring.Credentials, len(teams))
	for _, team := range teams {
		creds, err := ks.Get(ctx, team)
		if err != nil {
			d.log.Error("daemon: skipping a workspace whose credentials cannot be read; add it again with omaslack auth add",
				"team", team, "err", err)
			continue
		}
		out[team] = creds
	}
	return out, nil
}

// apply starts the workspaces in stored, all at once, since each asks
// Slack who is signed in.
func (d *daemon) apply(ctx context.Context, stored map[domain.TeamID]keyring.Credentials) {
	var wg sync.WaitGroup
	for team, creds := range stored {
		wg.Go(func() { d.start(ctx, team, creds) })
	}
	wg.Wait()
}

// start runs team's workspace and lists it, in place of any stopped one
// listed before; one that cannot start is logged and unlisted.
func (d *daemon) start(ctx context.Context, team domain.TeamID, creds keyring.Credentials) {
	w, err := d.open(ctx, team, creds)
	if err != nil {
		d.dir.remove(team)
		d.log.Error("daemon: skipping a workspace that cannot start", "team", team, "err", err)
		return
	}
	d.dir.put(w)
	d.log.Info("daemon: workspace started", "team", team, "name", w.Name, "provider", w.Provider)
}

// open builds team's provider, store, budget and engine and starts the
// engine.
func (d *daemon) open(ctx context.Context, team domain.TeamID, creds keyring.Credentials) (*workspace, error) {
	p, err := d.cfg.NewProvider(team, creds)
	if err != nil {
		return nil, err
	}
	budget, err := newBudget()
	if err != nil {
		return nil, err
	}
	st, err := sqlite.Open(ctx, filepath.Join(d.cfg.Paths.Data, team.String()+".db"))
	if err != nil {
		return nil, err
	}
	id := identify(ctx, d.cfg.Identify, d.cache, team, p, d.log)
	eng := syncengine.New(syncengine.Config{
		Provider: p, Store: st, Budget: budget, Publisher: d.pub, Self: id.Self,
		Log: d.log, // the engine names the workspace in each line
	})
	ectx, cancel := context.WithCancel(d.engines)
	w := &workspace{
		Workspace: api.Workspace{
			Team: team, Name: id.Name, Self: id.Self, Provider: creds.Provider(), Capabilities: p.Capabilities(),
			Store: st, Engine: eng, Names: api.NewNames(),
		},
		creds: creds, store: st, cancel: cancel, done: make(chan struct{}),
	}
	go w.run(ectx, eng, d.log)
	return w, nil
}
