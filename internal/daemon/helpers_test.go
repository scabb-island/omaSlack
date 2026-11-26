package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// fakeKeyring is a keyring in memory. A team in broken has credentials
// that cannot be read.
type fakeKeyring struct {
	mu     sync.Mutex
	creds  map[domain.TeamID]keyring.Credentials
	broken map[domain.TeamID]error
	err    error // what opening it fails with
}

func newKeyring() *fakeKeyring {
	return &fakeKeyring{creds: map[domain.TeamID]keyring.Credentials{}, broken: map[domain.TeamID]error{}}
}

func (k *fakeKeyring) open(context.Context) (keyring.ClosableStore, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.err != nil {
		return nil, k.err
	}
	return k, nil
}

func (k *fakeKeyring) Get(_ context.Context, team domain.TeamID) (keyring.Credentials, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.broken[team]; err != nil {
		return nil, err
	}
	c, ok := k.creds[team]
	if !ok {
		return nil, keyring.ErrNotFound
	}
	return c, nil
}

func (k *fakeKeyring) Set(_ context.Context, team domain.TeamID, c keyring.Credentials) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.creds[team] = c
	return nil
}

func (k *fakeKeyring) Delete(_ context.Context, team domain.TeamID) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.creds, team)
	return nil
}

func (k *fakeKeyring) Teams(context.Context) ([]domain.TeamID, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	teams := slices.Collect(maps.Keys(k.creds))
	for t := range k.broken {
		teams = append(teams, t)
	}
	return teams, nil
}

func (k *fakeKeyring) Backend() keyring.Backend { return keyring.File }
func (k *fakeKeyring) Close() error             { return nil }

// providers hands out one scripted Fake per team and counts how many
// times each was built.
type providers struct {
	mu    sync.Mutex
	fakes map[domain.TeamID]*providertest.Fake
	built map[domain.TeamID]int
}

func newProviders() *providers {
	return &providers{fakes: map[domain.TeamID]*providertest.Fake{}, built: map[domain.TeamID]int{}}
}

var testCaps = domain.NewCapabilities(domain.Realtime, domain.Search)

// fake returns team's Fake, creating it.
func (ps *providers) fake(team domain.TeamID) *providertest.Fake {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	f, ok := ps.fakes[team]
	if !ok {
		f = providertest.New(team, testCaps)
		ps.fakes[team] = f
	}
	return f
}

func (ps *providers) new(team domain.TeamID, _ keyring.Credentials) (provider.Provider, error) {
	f := ps.fake(team)
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.built[team]++
	return f, nil
}

func (ps *providers) builds(team domain.TeamID) int {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.built[team]
}

// identities names every workspace "Name of <team>", with U0ME signed in.
func identities(_ context.Context, p provider.Provider) (Identity, error) {
	self, _ := domain.ParseUserID("U0ME")
	return Identity{Name: "Name of " + p.Team().String(), Self: self}, nil
}

// harness is a daemon running on a temporary socket.
type harness struct {
	t      *testing.T
	cfg    Config
	keys   *fakeKeyring
	provs  *providers
	reload chan os.Signal
	cancel context.CancelFunc
	done   chan error
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	h := &harness{t: t, keys: newKeyring(), provs: newProviders(), reload: make(chan os.Signal)}
	h.cfg = Config{
		Paths:       Paths{Socket: filepath.Join(dir, "omaslack.sock"), Data: filepath.Join(dir, "data")},
		Keyring:     h.keys.open,
		NewProvider: h.provs.new,
		Identify:    identities,
		Reload:      h.reload,
	}
	return h
}

func (h *harness) addTeam(team string) domain.TeamID {
	id := mustTeam(h.t, team)
	_ = h.keys.Set(context.Background(), id, keyring.DesktopSession{Token: "xoxc-" + team, Cookie: "xoxd-1"})
	return id
}

// start runs the daemon until the test ends.
func (h *harness) start() {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel, h.done = cancel, make(chan error, 1)
	go func() { h.done <- Run(ctx, h.cfg) }()
	h.t.Cleanup(func() { _ = h.stop() })
}

// stop signals the daemon and returns what Run returned.
func (h *harness) stop() error {
	h.cancel()
	select {
	case err := <-h.done:
		h.done <- err // for a second stop
		return err
	case <-time.After(10 * time.Second):
		h.t.Fatal("the daemon did not stop")
		return nil
	}
}

// reloadNow asks the daemon to reload, as SIGHUP does.
func (h *harness) reloadNow() {
	h.t.Helper()
	select {
	case h.reload <- syscall.SIGHUP:
	case <-time.After(5 * time.Second):
		h.t.Fatal("the daemon did not take the reload")
	}
}

func (h *harness) dial() *rpcclient.Client {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := rpcclient.Dial(ctx, rpcclient.Options{
		Client: "daemon-test", Path: h.cfg.Paths.Socket,
		Run: func(context.Context, string, ...string) error { return nil }, // the test started it
	})
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _ = c.Close() })
	return c
}

func call[T any](t *testing.T, c *rpcclient.Client, method string, params any) T {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := c.Call(ctx, method, params)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: %v in %s", method, err, raw)
	}
	return out
}

// eventually polls cond until it holds or 5 s pass.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func teamsOf(ws []wire.Workspace) []string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Team)
	}
	return out
}

var errBroken = errors.New("keyring: malformed record")
