package api

import (
	"cmp"
	"context"
	"sync"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/mrkdwn"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// Rich text (task 4.9): every wire.Message carries Text rendered to HTML
// by mrkdwn.HTML, with the names of the people and conversations it
// refers to. A read names them before it renders (named); message events
// must not wait on Slack, so they render with the names earlier reads
// left in the workspace's Names.

// Names is what a workspace knows to call the people and conversations
// its messages refer to. It is safe for concurrent use, and a nil *Names
// knows nobody.
type Names struct {
	mu    sync.Mutex
	names map[string]string // "u:" or "c:" and the ID → name
}

var _ mrkdwn.Names = (*Names)(nil)

// NewNames returns a Names that knows nobody yet.
func NewNames() *Names { return &Names{names: map[string]string{}} }

// UserName is the person's display name, real name or username, or ""
// when unknown.
func (n *Names) UserName(id string) string { return n.get("u:" + id) }

// ConversationName is the conversation's name, or "" when unknown.
func (n *Names) ConversationName(id string) string { return n.get("c:" + id) }

func (n *Names) get(key string) string {
	if n == nil {
		return ""
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.names[key]
}

func (n *Names) set(key, name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if name != "" {
		n.names[key] = name
	}
}

func (n *Names) setUsers(users []domain.User) {
	for _, u := range users {
		n.set("u:"+u.ID.String(), cmp.Or(u.DisplayName, u.RealName, u.Name))
	}
}

func (n *Names) setConversations(convs []domain.Conversation) {
	for _, c := range convs {
		n.set("c:"+c.ID.String(), c.Name)
	}
}

// HTML renders text with the names w knows now, without asking anyone:
// what message events use.
func (w Workspace) HTML(text string) string { return mrkdwn.HTML(text, w.Team.String(), w.Names) }

// named learns the names msgs refer to, then returns w.HTML. People are
// asked of the engine, in one call that its provider answers from its
// cached directory; conversations are read from the cache. A failure
// leaves them as Names had them: rich text never fails a read.
func (w Workspace) named(ctx context.Context, msgs ...domain.Message) wire.HTMLFunc {
	if w.Names == nil {
		return w.HTML
	}
	var refs mrkdwn.Refs
	for _, m := range msgs {
		r := mrkdwn.References(m.Text)
		refs.Users = append(refs.Users, r.Users...)
		refs.Channels = append(refs.Channels, r.Channels...)
	}
	if ids := userIDs(refs.Users); len(ids) > 0 {
		if users, err := w.Engine.Users(ctx, ids); err == nil {
			w.Names.setUsers(users)
		}
	}
	if len(refs.Channels) > 0 {
		if convs, err := w.Store.Conversations(ctx); err == nil {
			w.Names.setConversations(convs)
		}
	}
	return w.HTML
}

// userIDs parses ids, once each, leaving out what is not a user ID.
func userIDs(ids []string) []domain.UserID {
	var out []domain.UserID
	seen := map[string]bool{}
	for _, s := range ids {
		id, err := domain.ParseUserID(s)
		if err == nil && !seen[s] {
			seen[s] = true
			out = append(out, id)
		}
	}
	return out
}
