package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/api"
	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/mrkdwn"
	"gitlab.com/scabb-island/omaslack/internal/notify"
	"gitlab.com/scabb-island/omaslack/internal/syncengine"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// lookupTimeout bounds asking Slack for the names a notification shows.
const lookupTimeout = 5 * time.Second

// publishers is the engines' Publisher: every event goes to the hub and,
// when notifications are on, to the notifier.
type publishers []syncengine.Publisher

func (ps publishers) Publish(ev domain.Event) {
	for _, p := range ps {
		p.Publish(ev)
	}
}

// startNotifier builds the notifier when Config.Notifications is set,
// reads its config and runs it until ctx ends. It returns what the engines
// publish to and a func that closes the sender.
func (d *daemon) startNotifier(ctx context.Context) (syncengine.Publisher, func()) {
	if d.cfg.Notifications == nil {
		return d.hub, func() {}
	}
	var sender notify.Sender
	d.notifier = notify.New(notify.Options{
		Sender: senderFunc(func(ctx context.Context, n notify.Notification) (uint32, error) { return sender.Notify(ctx, n) }),
		Lookup: d.lookup, Open: d.openFocus, Views: d.clients.views,
		Started: time.Now(), Log: d.log,
	})
	sender = d.cfg.Notifications(d.notifier)
	d.loadNotifyConfig()
	go func() { _ = d.notifier.Run(ctx) }()
	return publishers{d.hub, d.notifier}, func() {
		if c, ok := sender.(io.Closer); ok {
			_ = c.Close()
		}
	}
}

type senderFunc func(ctx context.Context, n notify.Notification) (uint32, error)

func (f senderFunc) Notify(ctx context.Context, n notify.Notification) (uint32, error) {
	return f(ctx, n)
}

// loadNotifyConfig reads Paths.Config into the notifier. A file that
// cannot be read or parsed is logged and the previous config stays.
func (d *daemon) loadNotifyConfig() {
	if d.notifier == nil || d.cfg.Paths.Config == "" {
		return
	}
	cfg, err := notify.LoadConfig(d.cfg.Paths.Config)
	if err != nil {
		d.log.Warn("daemon: keeping the previous notification settings", "err", err)
		return
	}
	d.notifier.SetConfig(cfg)
}

// lookup completes a new message for the notifier from its workspace.
func (d *daemon) lookup(ctx context.Context, ev domain.MessageCreated) (notify.Details, error) {
	ws, ok := d.dir.ws.Load(ev.Team)
	if !ok {
		return notify.Details{}, fmt.Errorf("daemon: workspace %s is gone", ev.Team)
	}
	w := ws.(*workspace).Workspace
	conv, err := conversation(ctx, w.Store, ev.Conversation)
	if err != nil {
		return notify.Details{}, err
	}
	following, err := follows(ctx, w, ev.Conversation, ev.Message)
	if err != nil {
		return notify.Details{}, err
	}
	names := askNames(ctx, w, ev.Message)
	d0 := notify.Details{
		Candidate: notify.Candidate{
			Team: ev.Team, Self: w.Self, Conversation: conv, Message: ev.Message, Following: following,
		},
		Names:  names,
		Author: authorName(ev.Message, names),
	}
	if len(d.dir.running()) > 1 {
		d0.Workspace = w.Name
	}
	return d0, nil
}

// authorName is who wrote m: the name Slack put on it (bots), else the
// person's name; "" when unknown.
func authorName(m domain.Message, n names) string {
	if m.AuthorName != "" {
		return m.AuthorName
	}
	if u, ok := m.Author.User(); ok {
		return n.UserName(u.String())
	}
	return ""
}

// conversation is id's entry in the store's list or, for a conversation
// not listed yet, what its ID tells: a DM for D…, else a channel.
func conversation(ctx context.Context, st api.Reader, id domain.ConversationID) (domain.Conversation, error) {
	list, err := st.Conversations(ctx)
	if err != nil {
		return domain.Conversation{}, err
	}
	if i := slices.IndexFunc(list, func(c domain.Conversation) bool { return c.ID == id }); i >= 0 {
		return list[i], nil
	}
	kind := domain.Channel
	if strings.HasPrefix(id.String(), "D") {
		kind = domain.DirectMessage
	}
	return domain.Conversation{ID: id, Kind: kind}, nil
}

// follows reports whether m replies in a thread the user follows, as far
// as the cache knows: one they started or replied in. Slack's own
// following list (threads followed by hand, or unfollowed) is not
// available to the daemon.
func follows(ctx context.Context, w api.Workspace, conv domain.ConversationID, m domain.Message) (bool, error) {
	if !m.IsReply() || w.Self.IsZero() {
		return false, nil
	}
	root, err := w.Store.Messages(ctx, conv, m.ThreadTS.Next(), 1)
	if err != nil {
		return false, err
	}
	replies, err := w.Store.Replies(ctx, conv, m.ThreadTS)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(slices.Concat(replies, root), func(r domain.Message) bool {
		u, ok := r.Author.User()
		return ok && u == w.Self && (r.TS == m.ThreadTS || r.IsReply())
	}), nil
}

// names names the people a notification shows: the ones asked for it,
// then the ones the workspace learned.
type names struct {
	users map[string]string
	known mrkdwn.Names
}

func (n names) UserName(id string) string {
	return cmp.Or(n.users[id], n.known.UserName(id))
}

func (n names) ConversationName(id string) string { return n.known.ConversationName(id) }

// askNames asks the engine for m's author and the people it mentions. A
// failure leaves them unnamed: the notification still goes out.
func askNames(ctx context.Context, w api.Workspace, m domain.Message) names {
	out := names{users: map[string]string{}, known: w.Names}
	var ids []domain.UserID
	if u, ok := m.Author.User(); ok {
		ids = append(ids, u)
	}
	for _, s := range mrkdwn.References(m.Text).Users {
		if id, err := domain.ParseUserID(s); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	users, _ := w.Engine.Users(ctx, ids)
	for _, u := range users {
		out.users[u.ID.String()] = cmp.Or(u.DisplayName, u.RealName, u.Name)
	}
	return out
}

// openFocus opens a clicked notification: in the plugin's window when it
// is preferred and the plugin is connected, else in the TUI, which is
// started (or its window focused) first. A TUI that is only starting
// receives the focus when it subscribes.
func (d *daemon) openFocus(f notify.Focus, open notify.Open) {
	ev := wire.FocusEvent{Team: f.Team.String(), Conv: f.Conv.String(), TS: f.TS.OrEmpty()}
	if open == notify.OpenWindow && d.clients.focus(pluginClient, ev) > 0 {
		return
	}
	if d.cfg.LaunchTUI != nil {
		if err := d.cfg.LaunchTUI(); err != nil {
			d.log.Warn("daemon: cannot start the TUI for a notification", "err", err)
		}
	}
	if d.clients.focus(tuiClient, ev) == 0 {
		d.clients.hold(ev)
	}
}

// LaunchTUI starts `omarchy-launch-or-focus-tui omaslack tui`, detached,
// which focuses the TUI's window when one is open. It is
// Config.LaunchTUI in `omaslack daemon`.
func LaunchTUI() error {
	self, err := os.Executable()
	if err != nil {
		self = "omaslack"
	}
	//nolint:gosec,noctx // self is this executable; detached on purpose, it outlives the click
	cmd := exec.Command("omarchy-launch-or-focus-tui", self, "tui")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return errors.Join(errors.New("daemon: start omarchy-launch-or-focus-tui"), err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
