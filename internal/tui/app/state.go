package app

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// TypingTimeout is how long "is typing" shows after the last typing event.
const TypingTimeout = 5 * time.Second

// State is what the TUI shows, and only that: a copy of what the daemon
// answered and published, fed by method results (Put*, Set*) and events
// (Apply). It is never a source of truth; every change the user makes is
// a daemon call, and its effect comes back here as a result or an event.
// It is owned by the bubbletea Update loop, so it is not locked.
//
// Timestamps are compared as strings: the daemon always sends Slack's
// canonical "%010d.%06d" form, which sorts lexically.
type State struct {
	teams   []string
	ws      map[string]*workspaceState
	msgs    map[convKey]map[string]wire.Message // by ts: a message published twice is kept once
	drafts  map[target]string
	typing  map[convKey]map[string]time.Time // user → last typing event
	summary wire.Summary
}

// convKey names a conversation across workspaces.
type convKey struct{ team, conv string }

// target is where compose writes: a conversation, or a thread in it.
type target struct{ team, conv, thread string }

type workspaceState struct {
	info   wire.Workspace
	loaded bool // conversations.list answered
	convs  map[string]wire.Conversation
	users  map[string]wire.User
	emoji  map[string]string
	own    ownState // own.go
}

// NewState starts the state from the daemon's hello result.
func NewState(hello json.RawMessage) (*State, error) {
	var h struct {
		Workspaces []wire.Workspace `json:"workspaces"`
	}
	if err := json.Unmarshal(hello, &h); err != nil {
		return nil, fmt.Errorf("hello: %w", err)
	}
	s := &State{
		ws:     map[string]*workspaceState{},
		msgs:   map[convKey]map[string]wire.Message{},
		drafts: map[target]string{},
		typing: map[convKey]map[string]time.Time{},
	}
	for _, w := range h.Workspaces {
		s.teams = append(s.teams, w.Team)
		s.ws[w.Team] = &workspaceState{info: w, convs: map[string]wire.Conversation{}, users: map[string]wire.User{}, emoji: map[string]string{}}
	}
	return s, nil
}

// Teams lists the workspaces in rail order.
func (s *State) Teams() []string { return s.teams }

// Workspace is a workspace as the daemon last reported it.
func (s *State) Workspace(team string) wire.Workspace {
	if w := s.ws[team]; w != nil {
		return w.info
	}
	return wire.Workspace{Team: team}
}

// Loaded says whether the workspace's conversation list has arrived.
func (s *State) Loaded(team string) bool { return s.ws[team] != nil && s.ws[team].loaded }

// SetConversations replaces a workspace's list (conversations.list).
func (s *State) SetConversations(team string, list []wire.Conversation) {
	w := s.ws[team]
	if w == nil {
		return
	}
	w.loaded = true
	w.convs = make(map[string]wire.Conversation, len(list))
	for _, c := range list {
		w.convs[c.ID] = c
	}
}

// Conversations is a workspace's list, in no particular order.
func (s *State) Conversations(team string) []wire.Conversation {
	w := s.ws[team]
	if w == nil {
		return nil
	}
	out := make([]wire.Conversation, 0, len(w.convs))
	for _, c := range w.convs {
		out = append(out, c)
	}
	return out
}

// Conversation is one conversation of the list.
func (s *State) Conversation(team, conv string) (wire.Conversation, bool) {
	if w := s.ws[team]; w != nil {
		c, ok := w.convs[conv]
		return c, ok
	}
	return wire.Conversation{}, false
}

// PutMessages merges messages, in any order, into a conversation. A
// message without reactions keeps the ones already known: message events
// never carry them (reaction.changed does).
func (s *State) PutMessages(team, conv string, msgs []wire.Message) {
	k := convKey{team, conv}
	byTS := s.msgs[k]
	if byTS == nil {
		byTS = map[string]wire.Message{}
		s.msgs[k] = byTS
	}
	for _, m := range msgs {
		if old, ok := byTS[m.TS]; ok && m.Reactions == nil {
			m.Reactions = old.Reactions
		}
		byTS[m.TS] = m
	}
}

// Messages are a conversation's top-level messages, oldest first: thread
// replies show in the thread pane only.
func (s *State) Messages(team, conv string) []wire.Message {
	return s.sorted(team, conv, func(m wire.Message) bool { return m.ThreadTS == "" || m.ThreadTS == m.TS })
}

// Replies are a thread's root and replies, oldest first.
func (s *State) Replies(team, conv, thread string) []wire.Message {
	return s.sorted(team, conv, func(m wire.Message) bool { return m.TS == thread || m.ThreadTS == thread })
}

func (s *State) sorted(team, conv string, keep func(wire.Message) bool) []wire.Message {
	var out []wire.Message
	for _, m := range s.msgs[convKey{team, conv}] {
		if keep(m) {
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(a, b wire.Message) int { return compareTS(a.TS, b.TS) })
	return out
}

func compareTS(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// SetDraft records a draft as the daemon has it (drafts.get, open).
func (s *State) SetDraft(t target, text string) { s.drafts[t] = text }

// Draft is the last known draft of a conversation or thread.
func (s *State) Draft(t target) string { return s.drafts[t] }

// PutUsers records users.info answers.
func (s *State) PutUsers(team string, users []wire.User) {
	w := s.ws[team]
	if w == nil {
		return
	}
	for _, u := range users {
		w.users[u.ID] = u
	}
}

// UserName is how a user is called: display name, real name or handle,
// or "" when users.info has not told.
func (s *State) UserName(team, id string) string {
	w := s.ws[team]
	if w == nil {
		return ""
	}
	u := w.users[id]
	for _, n := range []string{u.DisplayName, u.RealName, u.Name} {
		if n != "" {
			return n
		}
	}
	return ""
}

// SetEmoji records a workspace's custom emoji (emoji.list).
func (s *State) SetEmoji(team string, emoji map[string]string) {
	if w := s.ws[team]; w != nil {
		w.emoji = emoji
	}
}

// CustomEmoji looks a custom emoji up by name.
func (s *State) CustomEmoji(team, name string) (string, bool) {
	if w := s.ws[team]; w != nil {
		v, ok := w.emoji[name]
		return v, ok
	}
	return "", false
}

// Summary is the latest unread.summary.
func (s *State) Summary() wire.Summary { return s.summary }

// SetSummary records an unread.summary result.
func (s *State) SetSummary(sum wire.Summary) { s.summary = sum }

// Typing lists who is typing in a conversation at now, sorted.
func (s *State) Typing(team, conv string, now time.Time) []string {
	var out []string
	for user, at := range s.typing[convKey{team, conv}] {
		if now.Sub(at) < TypingTimeout {
			out = append(out, user)
		}
	}
	slices.Sort(out)
	return out
}

// Apply folds one event into the state. Topics the TUI does not show are
// ignored; params that do not parse are an error, and change nothing.
func (s *State) Apply(topic string, params json.RawMessage, now time.Time) error {
	h, ok := eventHandlers[topic]
	if !ok {
		return nil
	}
	if err := h(s, params, now); err != nil {
		return fmt.Errorf("%s: %w", topic, err)
	}
	return nil
}

type eventHandler func(s *State, params json.RawMessage, now time.Time) error

// on adapts a typed handler: it parses params once, at the boundary.
func on[T any](h func(s *State, ev T, now time.Time)) eventHandler {
	return func(s *State, params json.RawMessage, now time.Time) error {
		var ev T
		if err := json.Unmarshal(params, &ev); err != nil {
			return err
		}
		h(s, ev, now)
		return nil
	}
}

var eventHandlers = map[string]eventHandler{
	"message.created":      on((*State).messageCreated),
	"message.updated":      on((*State).messageUpdated),
	"message.deleted":      on((*State).messageDeleted),
	"reaction.changed":     on((*State).reactionChanged),
	"read.changed":         on((*State).readChanged),
	"conversation.updated": on((*State).conversationUpdated),
	"draft.changed":        on((*State).draftChanged),
	"connection.state":     on((*State).connectionChanged),
	"unread.summary":       on((*State).summaryChanged),
	"typing":               on((*State).typingStarted),
	"dnd.changed":          on((*State).dndChanged),
	"presence.changed":     on((*State).presenceChanged),
}

func (s *State) messageCreated(ev wire.MessageEvent, now time.Time) {
	delete(s.typing[convKey{ev.Team, ev.Conv}], ev.Message.Author)
	s.messageUpdated(ev, now)
}

func (s *State) messageUpdated(ev wire.MessageEvent, _ time.Time) {
	s.PutMessages(ev.Team, ev.Conv, []wire.Message{ev.Message})
}

func (s *State) messageDeleted(ev wire.MessageDeletedEvent, _ time.Time) {
	delete(s.msgs[convKey{ev.Team, ev.Conv}], ev.TS)
}

func (s *State) reactionChanged(ev wire.ReactionEvent, _ time.Time) {
	byTS := s.msgs[convKey{ev.Team, ev.Conv}]
	m, ok := byTS[ev.TS]
	if !ok {
		return
	}
	m.Reactions = react(m.Reactions, ev.Emoji, ev.User, ev.Added)
	byTS[ev.TS] = m
}

// react returns rs with user's reaction emoji added or removed. Reactions
// keep their order; one left without users goes away.
func react(rs []wire.Reaction, emoji, user string, added bool) []wire.Reaction {
	out := make([]wire.Reaction, 0, len(rs)+1)
	found := false
	for _, r := range rs {
		if r.Emoji == emoji {
			found = true
			r.Users = withUser(r.Users, user, added)
		}
		if len(r.Users) > 0 {
			out = append(out, r)
		}
	}
	if !found && added {
		out = append(out, wire.Reaction{Emoji: emoji, Users: []string{user}})
	}
	return out
}

func withUser(users []string, user string, add bool) []string {
	out := slices.DeleteFunc(slices.Clone(users), func(u string) bool { return u == user })
	if add {
		out = append(out, user)
	}
	return out
}

func (s *State) readChanged(ev wire.ReadEvent, _ time.Time) {
	c, ok := s.Conversation(ev.Team, ev.Conv)
	if !ok {
		return
	}
	c.LastRead = ev.LastRead
	c.Unread = compareTS(c.Latest, c.LastRead) > 0
	s.ws[ev.Team].convs[c.ID] = c
}

func (s *State) conversationUpdated(ev wire.ConversationEvent, _ time.Time) {
	if w := s.ws[ev.Team]; w != nil {
		w.convs[ev.Conversation.ID] = ev.Conversation
	}
}

func (s *State) draftChanged(ev wire.DraftEvent, _ time.Time) {
	s.SetDraft(target{ev.Team, ev.Conv, ev.ThreadTS}, ev.Text)
}

func (s *State) connectionChanged(ev wire.ConnectionEvent, _ time.Time) {
	if w := s.ws[ev.Team]; w != nil {
		w.info.State = ev.State
	}
}

func (s *State) summaryChanged(ev wire.Summary, _ time.Time) { s.SetSummary(ev) }

func (s *State) typingStarted(ev wire.TypingEvent, now time.Time) {
	k := convKey{ev.Team, ev.Conv}
	if s.typing[k] == nil {
		s.typing[k] = map[string]time.Time{}
	}
	s.typing[k][ev.User] = now
}
