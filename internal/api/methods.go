package api

import (
	"context"
	"encoding/json"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// Limits on how many items one call returns. A larger limit is lowered to
// the maximum; a limit below 1 is -32602.
const (
	HistoryLimit    = 50  // messages.history default, and the page conversations.open returns
	HistoryMaxLimit = 200 // messages.history maximum
	SearchLimit     = 20  // search.messages and users.search default
	SearchMaxLimit  = 100 // search.messages and users.search maximum
)

// method answers one call from its parsed params.
type method func(ctx context.Context, p *params) (any, error)

func handler(dir Directory, m method) rpc.Handler {
	return rpc.HandlerFunc(func(ctx context.Context, _ string, raw json.RawMessage) (any, error) {
		return m(ctx, parseParams(dir, raw))
	})
}

// target is what most methods act on: a workspace and one of its
// conversations.
type target struct {
	w    Workspace
	conv domain.ConversationID
}

func (p *params) target() target { return target{w: p.workspace(), conv: p.conv()} }

// ── Reads ──

func workspacesList(dir Directory) method {
	return func(context.Context, *params) (any, error) {
		out := []wire.Workspace{}
		for _, w := range dir.Workspaces() {
			out = append(out, w.Info())
		}
		return out, nil
	}
}

func conversationsList(ctx context.Context, p *params) (any, error) {
	w := p.workspace()
	unreadOnly := p.choice("filter", "all", "all", "unread") == "unread"
	if err := p.err(); err != nil {
		return nil, err
	}
	list, err := w.Store.Conversations(ctx)
	if err != nil {
		return nil, err
	}
	out := []wire.Conversation{}
	for _, c := range list {
		if c.Unread() || !unreadOnly {
			out = append(out, wire.FromConversation(c))
		}
	}
	return out, nil
}

type openResult struct {
	Messages []wire.Message `json:"messages"`
	Draft    string         `json:"draft"`
}

func conversationsOpen(ctx context.Context, p *params) (any, error) {
	t := p.target()
	if err := p.err(); err != nil {
		return nil, err
	}
	if err := t.w.Engine.OpenConversation(ctx, t.conv); err != nil {
		return nil, err
	}
	msgs, err := t.w.Store.Messages(ctx, t.conv, domain.TS{}, HistoryLimit)
	if err != nil {
		return nil, err
	}
	out := openResult{}
	if out.Messages, err = t.withReactions(ctx, msgs); err != nil {
		return nil, err
	}
	out.Draft, err = t.w.Store.Draft(ctx, t.conv, domain.TS{})
	return out, err
}

// messagesHistory reads from the cache. When the cache holds fewer than
// limit messages before the cursor, it asks the engine for one older page
// and reads again: one Slack call at most, so a client pages through long
// history one call at a time.
func messagesHistory(ctx context.Context, p *params) (any, error) {
	t := p.target()
	before := p.optTS("before")
	limit := p.limit(HistoryLimit, HistoryMaxLimit)
	if err := p.err(); err != nil {
		return nil, err
	}
	msgs, err := t.w.Store.Messages(ctx, t.conv, before, limit)
	if err == nil && len(msgs) < limit {
		msgs, err = t.loadOlder(ctx, before, limit)
	}
	if err != nil {
		return nil, err
	}
	return t.withReactions(ctx, msgs)
}

func (t target) loadOlder(ctx context.Context, before domain.TS, limit int) ([]domain.Message, error) {
	if _, err := t.w.Engine.LoadOlder(ctx, t.conv); err != nil {
		return nil, err
	}
	return t.w.Store.Messages(ctx, t.conv, before, limit)
}

func messagesReplies(ctx context.Context, p *params) (any, error) {
	t := p.target()
	thread := p.ts("threadTS")
	if err := p.err(); err != nil {
		return nil, err
	}
	if err := t.w.Engine.OpenThread(ctx, t.conv, thread); err != nil {
		return nil, err
	}
	msgs, err := t.w.Store.Replies(ctx, t.conv, thread)
	if err != nil {
		return nil, err
	}
	return t.withReactions(ctx, msgs)
}

// withReactions converts msgs, each with its reactions from the cache.
// The result is never nil, so an empty list is [] on the wire.
func (t target) withReactions(ctx context.Context, msgs []domain.Message) ([]wire.Message, error) {
	out := make([]wire.Message, 0, len(msgs))
	for _, m := range msgs {
		wm, err := t.message(ctx, m)
		if err != nil {
			return nil, err
		}
		out = append(out, wm)
	}
	return out, nil
}

func (t target) message(ctx context.Context, m domain.Message) (wire.Message, error) {
	reactions, err := t.w.Store.Reactions(ctx, t.conv, m.TS)
	return wire.FromMessage(m, reactions), err
}

type draftResult struct {
	Text string `json:"text"`
}

func draftsGet(ctx context.Context, p *params) (any, error) {
	t := p.target()
	thread := p.optTS("threadTS")
	if err := p.err(); err != nil {
		return nil, err
	}
	text, err := t.w.Store.Draft(ctx, t.conv, thread)
	return draftResult{Text: text}, err
}

// Summary is the unread summary across every signed-in workspace: the
// result of unread.summary and what the events hub sends as its
// unread.summary event (events.Options.Summary). It fails if any
// workspace's list cannot be read.
func Summary(ctx context.Context, dir Directory) (wire.Summary, error) {
	lists := map[domain.TeamID][]domain.Conversation{}
	for _, w := range dir.Workspaces() {
		list, err := w.Store.Conversations(ctx)
		if err != nil {
			return wire.Summary{}, err
		}
		lists[w.Team] = list
	}
	return wire.Summarize(lists), nil
}

func unreadSummary(dir Directory) method {
	return func(ctx context.Context, _ *params) (any, error) { return Summary(ctx, dir) }
}

// ── Actions: each goes through the workspace's engine ──

func conversationsClose(ctx context.Context, p *params) (any, error) {
	t := p.target()
	return nothing(p, func() error { return t.w.Engine.CloseConversation(ctx, t.conv) })
}

func conversationsMarkRead(ctx context.Context, p *params) (any, error) {
	t := p.target()
	upTo := p.ts("ts")
	return nothing(p, func() error { return t.w.Engine.MarkRead(ctx, t.conv, upTo) })
}

func messagesSend(ctx context.Context, p *params) (any, error) {
	t := p.target()
	thread := p.optTS("threadTS")
	text := p.text("text", false)
	if err := p.err(); err != nil {
		return nil, err
	}
	m, err := t.w.Engine.Send(ctx, t.conv, thread, text)
	if err != nil {
		return nil, err
	}
	return wire.FromMessage(m, nil), nil
}

func messagesEdit(ctx context.Context, p *params) (any, error) {
	t := p.target()
	at := p.ts("ts")
	text := p.text("text", false)
	if err := p.err(); err != nil {
		return nil, err
	}
	m, err := t.w.Engine.Edit(ctx, t.conv, at, text)
	if err != nil {
		return nil, err
	}
	return t.message(ctx, m)
}

func messagesDelete(ctx context.Context, p *params) (any, error) {
	t := p.target()
	at := p.ts("ts")
	return nothing(p, func() error { return t.w.Engine.Delete(ctx, t.conv, at) })
}

func react(add bool) method {
	return func(ctx context.Context, p *params) (any, error) {
		t := p.target()
		at := p.ts("ts")
		emoji := p.emoji()
		return nothing(p, func() error { return t.w.Engine.React(ctx, t.conv, at, emoji, add) })
	}
}

func draftsSet(ctx context.Context, p *params) (any, error) {
	t := p.target()
	thread := p.optTS("threadTS")
	text := p.text("text", true)
	return nothing(p, func() error { return t.w.Engine.SetDraft(ctx, t.conv, thread, text) })
}

func searchMessages(ctx context.Context, p *params) (any, error) {
	w := p.workspace()
	query := p.text("query", false)
	limit := p.limit(SearchLimit, SearchMaxLimit)
	if err := needs(p, w, domain.Search); err != nil {
		return nil, err
	}
	hits, err := w.Engine.SearchMessages(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]wire.SearchHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, wire.FromSearchHit(h))
	}
	return out, nil
}

func usersSearch(ctx context.Context, p *params) (any, error) {
	w := p.workspace()
	query := p.text("q", false)
	limit := p.limit(SearchLimit, SearchMaxLimit)
	if err := p.err(); err != nil {
		return nil, err
	}
	users, err := w.Engine.SearchUsers(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]wire.User, 0, len(users))
	for _, u := range users {
		out = append(out, wire.FromUser(u))
	}
	return out, nil
}

func presenceSet(ctx context.Context, p *params) (any, error) {
	w := p.workspace()
	away := p.required("presence", "auto", "away") == "away"
	if err := needs(p, w, domain.PresenceWrite); err != nil {
		return nil, err
	}
	return nil, w.Engine.SetPresence(ctx, away)
}

func dndSnooze(ctx context.Context, p *params) (any, error) {
	w := p.workspace()
	minutes := p.positive("minutes")
	return nothing(p, func() error { return w.Engine.SnoozeDND(ctx, minutes) })
}

func dndEnd(ctx context.Context, p *params) (any, error) {
	w := p.workspace()
	return nothing(p, func() error { return w.Engine.EndDND(ctx) })
}

type focusResult struct {
	Clients int `json:"clients"`
}

func uiFocus(focus Focuser) method {
	return func(_ context.Context, p *params) (any, error) {
		t := p.target()
		at := p.optTS("ts")
		if err := p.err(); err != nil {
			return nil, err
		}
		ev := wire.FocusEvent{Team: t.w.Team.String(), Conv: t.conv.String()}
		if !at.IsZero() {
			ev.TS = at.String()
		}
		return focusResult{Clients: focus.Focus(ev)}, nil
	}
}

// nothing runs act once the params are valid; the result is null.
func nothing(p *params, act func() error) (any, error) {
	if err := p.err(); err != nil {
		return nil, err
	}
	return nil, act()
}

// needs checks the params, then that w's provider has c, before the
// engine is asked for anything.
func needs(p *params, w Workspace, c domain.Capability) error {
	if err := p.err(); err != nil {
		return err
	}
	return w.Capabilities.Require(c)
}
