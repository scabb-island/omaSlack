package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// renewTimeout bounds the auth refresh subprocess.
const renewTimeout = 30 * time.Second

// renew renews the active workspace's session when it is an expired
// desktop session, by running the Refresh command (omaslack auth refresh).
// For any other state there is nothing to renew.
func (m *Model) renew() tea.Cmd {
	w := m.st.Workspace(m.team)
	if w.State != "auth_expired" || w.Provider != "desktop-session" {
		m.setInfo("Nothing to renew: this workspace is not an expired desktop session.")
		return nil
	}
	m.setInfo("Renewing the session…")
	return m.refresh(w.Team)
}

// execRefresh runs `omaslack auth refresh <team>` with this same binary. It
// is a seam so tests can run the refresh without a subprocess.
var execRefresh = func(ctx context.Context, team string) ([]byte, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	//nolint:gosec // exe is our own binary (os.Executable); team is a parsed team ID
	return exec.CommandContext(ctx, exe, "auth", "refresh", team).CombinedOutput()
}

// defaultRefresh renews a session by running execRefresh. It reuses the
// resultMsg path: on success the daemon reconnects and a connection.state
// event clears the error; a failure shows why.
func defaultRefresh(team string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), renewTimeout)
		defer cancel()
		if out, err := execRefresh(ctx, team); err != nil {
			return resultMsg{method: "renew", err: fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))}
		}
		return resultMsg{method: "renew", apply: func(m *Model) tea.Cmd {
			m.setInfo("Session renewed; reconnecting…")
			return nil
		}}
	}
}
