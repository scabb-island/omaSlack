package notify

import (
	"sync"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// View is what one client reports it shows (ui.viewing): a conversation,
// or a thread beside it, and whether its window has focus. The zero View
// shows nothing.
type View struct {
	Team    domain.TeamID
	Conv    domain.ConversationID
	Thread  domain.TS // zero: no thread open
	Focused bool
}

// Views is what every connection views, one View per connection. It is
// safe for concurrent use; the zero value is empty.
type Views struct {
	mu sync.Mutex
	m  map[any]View
}

// Set records what the connection key views; the zero View forgets it.
func (v *Views) Set(key any, view View) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if view == (View{}) {
		delete(v.m, key)
		return
	}
	if v.m == nil {
		v.m = map[any]View{}
	}
	v.m[key] = view
}

// Drop forgets the connection key, once it has closed.
func (v *Views) Drop(key any) { v.Set(key, View{}) }

// Shows reports whether a connection views m of conv in team focused: a
// top-level message of the conversation it shows, or a reply in the
// thread it shows.
func (v *Views) Shows(team domain.TeamID, conv domain.ConversationID, m domain.Message) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, view := range v.m {
		if view.shows(team, conv, m) {
			return true
		}
	}
	return false
}

func (view View) shows(team domain.TeamID, conv domain.ConversationID, m domain.Message) bool {
	if !view.Focused || view.Team != team || view.Conv != conv {
		return false
	}
	return !m.IsReply() || m.ThreadTS == view.Thread
}
