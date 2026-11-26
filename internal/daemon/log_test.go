package daemon

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
)

// syncBuffer is a log destination the daemon's goroutines can share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The sync engine's log lines name their workspace once, not twice.
func TestEngineLogLinesNameTheWorkspaceOnce(t *testing.T) {
	var logs syncBuffer
	h := newHarness(t)
	h.cfg.Log = slog.New(slog.NewTextHandler(&logs, nil))
	team := h.addTeam("T0ACME")
	h.provs.fake(team).Script(providertest.Session{Err: errors.New("slack is down")})
	h.start()
	eventually(t, "the engine's warning", func() bool {
		return strings.Contains(logs.String(), "sync: connection failed")
	})

	for line := range strings.Lines(logs.String()) {
		if strings.Contains(line, `msg="sync:`) && strings.Count(line, "team=") != 1 {
			t.Errorf("log line names the team %d times: %s", strings.Count(line, "team="), line)
		}
	}
}
