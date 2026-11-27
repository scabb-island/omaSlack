package daemon

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/events"
	"gitlab.com/scabb-island/omaslack/internal/notify"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// The client names the plugin and the TUI give in hello.
const (
	pluginClient = "omaslack-plugin"
	tuiClient    = "omaslack-tui"
)

// heldFocusFor is how long a click waits for a TUI that is starting.
const heldFocusFor = 30 * time.Second

// clients is every greeted connection, by the client name it gave in
// hello, and what each one views (ui.viewing). A connection is forgotten
// when it closes. It is safe for concurrent use.
type clients struct {
	views *notify.Views
	now   func() time.Time

	mu    sync.Mutex
	names map[*rpc.Conn]string
	held  *wire.FocusEvent // a click waiting for a TUI to subscribe
	until time.Time        // when held expires
}

func newClients(views *notify.Views, now func() time.Time) *clients {
	return &clients{views: views, now: now, names: map[*rpc.Conn]string{}}
}

// hello wraps next to record each greeted connection.
func (c *clients) hello(next rpc.HelloFunc) rpc.HelloFunc {
	return func(ctx context.Context, h rpc.Hello) (map[string]any, error) {
		out, err := next(ctx, h)
		if conn, ok := rpc.ConnFromContext(ctx); ok && err == nil {
			c.add(conn, h.Client)
		}
		return out, err
	}
}

func (c *clients) add(conn *rpc.Conn, name string) {
	c.mu.Lock()
	c.names[conn] = name
	c.mu.Unlock()
	go func() {
		<-conn.Done()
		c.mu.Lock()
		delete(c.names, conn)
		c.mu.Unlock()
		c.views.Drop(conn)
	}()
}

// focus sends ui.focus to every connection of client name and reports
// how many it reached.
func (c *clients) focus(name string, f wire.FocusEvent) int {
	c.mu.Lock()
	var to []*rpc.Conn
	for conn, n := range c.names {
		if n == name {
			to = append(to, conn)
		}
	}
	c.mu.Unlock()
	sent := 0
	for _, conn := range to {
		if conn.Notify(events.TopicFocus, f) == nil {
			sent++
		}
	}
	return sent
}

// hold keeps f for the next TUI that subscribes, for heldFocusFor.
func (c *clients) hold(f wire.FocusEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.held, c.until = &f, c.now().Add(heldFocusFor)
}

// takeHeld returns the held focus, once, unless it expired.
func (c *clients) takeHeld() (wire.FocusEvent, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f := c.held
	c.held = nil
	if f == nil || c.now().After(c.until) {
		return wire.FocusEvent{}, false
	}
	return *f, true
}

// subscribe wraps events.subscribe: a TUI that subscribes receives the
// focus a click held for it.
func (c *clients) subscribe(next rpc.Handler) rpc.Handler {
	return rpc.HandlerFunc(func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		out, err := next.Handle(ctx, method, params)
		conn, ok := rpc.ConnFromContext(ctx)
		if err != nil || !ok || c.name(conn) != tuiClient {
			return out, err
		}
		if f, held := c.takeHeld(); held {
			_ = conn.Notify(events.TopicFocus, f)
		}
		return out, nil
	})
}

func (c *clients) name(conn *rpc.Conn) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.names[conn]
}

// viewingParams are ui.viewing's params; no team clears the entry.
type viewingParams struct {
	Team     string `json:"team"`
	Conv     string `json:"conv"`
	ThreadTS string `json:"threadTS"`
	Focused  bool   `json:"focused"`
}

// viewing is the ui.viewing method: {team, conv, threadTS?, focused}
// records what the calling connection shows, {} forgets it; the result
// is null. The entry goes when the connection closes.
func (c *clients) viewing() rpc.Handler {
	return rpc.HandlerFunc(func(ctx context.Context, _ string, params json.RawMessage) (any, error) {
		conn, ok := rpc.ConnFromContext(ctx)
		if !ok {
			return nil, &rpc.Error{Code: rpc.CodeInternalError, Message: "ui.viewing needs a connection"}
		}
		v, err := parseView(params)
		if err != nil {
			return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: "ui.viewing: " + err.Error()}
		}
		c.views.Set(conn, v)
		return nil, nil
	})
}

// parseView reads ui.viewing's params into a View; no team is the zero
// View.
func parseView(params json.RawMessage) (notify.View, error) {
	var p viewingParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return notify.View{}, err
		}
	}
	if p.Team == "" {
		return notify.View{}, nil
	}
	var v notify.View
	var errs [3]error
	v.Team, errs[0] = domain.ParseTeamID(p.Team)
	v.Conv, errs[1] = domain.ParseConversationID(p.Conv)
	if p.ThreadTS != "" {
		v.Thread, errs[2] = domain.ParseTS(p.ThreadTS)
	}
	v.Focused = p.Focused
	for _, err := range errs {
		if err != nil {
			return notify.View{}, err
		}
	}
	return v, nil
}
