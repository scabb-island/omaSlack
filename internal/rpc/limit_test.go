package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

// gate is a slow method: every call reports that it started, then waits
// for release. It records the most calls that ever ran at once.
type gate struct {
	started chan struct{}
	release chan struct{}

	mu      sync.Mutex
	running int
	most    int
}

func newGate(calls int) *gate {
	return &gate{started: make(chan struct{}, calls), release: make(chan struct{})}
}

// Handle also returns when the server closes, so a failed test never hangs.
func (g *gate) Handle(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
	g.mu.Lock()
	g.running++
	g.most = max(g.most, g.running)
	g.mu.Unlock()
	g.started <- struct{}{}
	select {
	case <-g.release:
	case <-ctx.Done():
	}
	g.mu.Lock()
	g.running--
	g.mu.Unlock()
	return "done", nil
}

func (g *gate) peak() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.most
}

// waitStarts waits for n calls to start, then checks that no other one
// starts while they all still run.
func (g *gate) waitStarts(t *testing.T, n int) {
	t.Helper()
	for i := range n {
		select {
		case <-g.started:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d calls started, want %d", i, n)
		}
	}
	select {
	case <-g.started:
		t.Fatalf("call %d started while %d were running", n+1, n)
	case <-time.After(100 * time.Millisecond):
	}
}

func flood(c *testClient, calls int) {
	for i := 1; i <= calls; i++ {
		c.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"slow"}`, i))
	}
}

// Spec daemon-api, "A client floods calls".
func TestAClientFloodingCallsRunsAtMost16AndOthersAreServed(t *testing.T) {
	const calls = 100
	g := newGate(calls)
	path := serve(t, NewServer(Router{"slow": g, "echo": HandlerFunc(echo)}, Options{}))
	a, b := connect(t, path), connect(t, path)
	a.hello()
	flood(a, calls)

	g.waitStarts(t, 16)
	b.hello()
	wantResult(t, b.call(1, "echo", `[1]`), `[1]`)

	close(g.release)
	seen := map[string]bool{}
	for range calls {
		r := a.recv()
		wantResult(t, r, `"done"`)
		seen[string(r.ID)] = true
	}
	if len(seen) != calls {
		t.Fatalf("got %d distinct replies, want %d", len(seen), calls)
	}
	if g.peak() != 16 {
		t.Fatalf("at most %d calls ran at once, want 16", g.peak())
	}
}

func TestTheCallLimitIsAServerOption(t *testing.T) {
	g := newGate(5)
	c := connect(t, serve(t, NewServer(Router{"slow": g}, Options{MaxCalls: 2})))
	c.hello()
	flood(c, 5)
	g.waitStarts(t, 2)
	close(g.release)
	for range 5 {
		wantResult(t, c.recv(), `"done"`)
	}
}

// The reader is parked at the limit when the server closes; Close (run by
// serve's cleanup) must still return instead of waiting for a free slot.
func TestClosingTheServerWhileAConnectionIsAtItsLimit(t *testing.T) {
	g := newGate(3)
	c := connect(t, serve(t, NewServer(Router{"slow": g}, Options{MaxCalls: 1})))
	c.hello()
	flood(c, 3)
	g.waitStarts(t, 1)
}
