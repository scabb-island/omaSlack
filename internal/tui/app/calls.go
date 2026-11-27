package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
	"gitlab.com/scabb-island/omaslack/internal/tui/client"
	"gitlab.com/scabb-island/omaslack/internal/tui/theme"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// CallTimeout bounds every daemon call the TUI makes.
const CallTimeout = 15 * time.Second

// NoteMsg is one daemon event; Run forwards Client.Notifications as these.
type NoteMsg rpcclient.Notification

// DisconnectedMsg says the daemon connection ended.
type DisconnectedMsg struct{}

// ThemeMsg carries a reloaded theme (Options.Themes).
type ThemeMsg theme.Theme

// resultMsg is a daemon call's answer, applied in Update: apply on
// success; the error, and undo when set, on failure.
type resultMsg struct {
	method string
	err    error
	apply  func(*Model) tea.Cmd
	undo   func(*Model)
}

// call makes one daemon call off the Update loop and decodes its result
// into T, which then receives inside Update. then may be nil.
func call[T any](c client.Client, method string, params any, then func(*Model, T) tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), CallTimeout)
		defer cancel()
		raw, err := c.Call(ctx, method, params)
		var v T
		if err == nil && then != nil {
			err = json.Unmarshal(raw, &v)
		}
		return resultMsg{method: method, err: err, apply: func(m *Model) tea.Cmd {
			if then == nil {
				return nil
			}
			return then(m, v)
		}}
	}
}

// withUndo makes a failed call run undo.
func withUndo(cmd tea.Cmd, undo func(*Model)) tea.Cmd {
	return func() tea.Msg {
		msg := cmd().(resultMsg)
		msg.undo = undo
		return msg
	}
}

// then runs first for its effect, then next, and answers next's message:
// for work that must finish before the program quits.
func then(first, next tea.Cmd) tea.Cmd {
	if first == nil {
		return next
	}
	return func() tea.Msg {
		first()
		return next()
	}
}

// optional are the methods newer daemons answer (task 3.8): an older one
// says -32601, and a provider without the capability -32002. Either way
// the TUI does without, silently.
var optional = map[string]bool{"users.info": true, "emoji.list": true, "typing.send": true}

// unsupported reports whether err says the daemon cannot do the method.
func unsupported(err error) bool {
	var e *rpc.Error
	return errors.As(err, &e) && (e.Code == rpc.CodeMethodNotFound || e.Code == rpc.CodeUnsupportedCapability)
}

// failed shows what went wrong, except for optional methods the daemon
// does not have, which are switched off for the workspace.
func (m *Model) failed(r resultMsg) {
	if r.undo != nil {
		r.undo(m)
	}
	if optional[r.method] && unsupported(r.err) {
		m.without[r.method] = true
		return
	}
	m.setError(fmt.Sprintf("%s: %v", r.method, r.err))
}

// params builds a call's params for t: team, conv and threadTS when t is
// a thread, plus extra key/value pairs.
func (t target) params(extra ...any) map[string]any {
	p := map[string]any{"team": t.team, "conv": t.conv}
	if t.thread != "" {
		p["threadTS"] = t.thread
	}
	for i := 0; i+1 < len(extra); i += 2 {
		p[extra[i].(string)] = extra[i+1]
	}
	return p
}

// subscribedTopics is what the TUI listens to, once per connection: the
// events reach every conversation, so opening one needs no new subscribe.
var subscribedTopics = []string{
	"message.created", "message.updated", "message.deleted",
	"reaction.changed", "conversation.updated", "read.changed",
	"draft.changed", "typing", "connection.state", "unread.summary", "ui.focus",
}

func (m *Model) subscribe() tea.Cmd {
	return call[json.RawMessage](m.client, "events.subscribe", map[string]any{"topics": subscribedTopics}, nil)
}

// loadWorkspace asks for a workspace's conversations and custom emoji.
func (m *Model) loadWorkspace(team string) tea.Cmd {
	return tea.Batch(
		call(m.client, "conversations.list", map[string]any{"team": team}, func(m *Model, list []wire.Conversation) tea.Cmd {
			m.st.SetConversations(team, list)
			m.keepSelection()
			return m.askNames()
		}),
		optionalCall(m, "emoji.list", map[string]any{"team": team}, func(m *Model, e wire.EmojiList) tea.Cmd {
			m.st.SetEmoji(team, e.Emoji)
			return nil
		}),
	)
}

// optionalCall is call for a method in optional: once the daemon said it
// cannot, it is not asked again.
func optionalCall[T any](m *Model, method string, params any, then func(*Model, T) tea.Cmd) tea.Cmd {
	if m.without[method] {
		return nil
	}
	return call(m.client, method, params, then)
}

// askNames asks users.info for the people the view shows by ID: the other
// person of each DM and the authors of the open conversation and thread.
// Each ID is asked once.
func (m *Model) askNames() tea.Cmd {
	if m.without["users.info"] || m.team == "" {
		return nil
	}
	team := m.team
	var ids []string
	for _, id := range m.shownUserIDs() {
		if m.unnamed(team, id) {
			m.asked[userKey{team, id}] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return call(m.client, "users.info", map[string]any{"team": team, "ids": ids}, func(m *Model, users []wire.User) tea.Cmd {
		m.st.PutUsers(team, users)
		return nil
	})
}

// shownUserIDs are the users the view may show: the other person of each
// DM and the authors of the open conversation and thread.
func (m *Model) shownUserIDs() []string {
	var ids []string
	for _, c := range m.st.Conversations(m.team) {
		ids = append(ids, c.User)
	}
	for _, msg := range append(m.messages(), m.replies()...) {
		ids = append(ids, msg.Author)
	}
	return ids
}

// unnamed says id is a user (not a bot) with no name yet and not asked.
func (m *Model) unnamed(team, id string) bool {
	isUser := id != "" && (id[0] == 'U' || id[0] == 'W')
	return isUser && !m.asked[userKey{team, id}] && m.st.UserName(team, id) == ""
}
