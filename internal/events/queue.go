package events

import (
	"container/list"
	"sync"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// key identifies the state a notification carries. Two notifications with
// the same non-zero key carry two versions of one state, so a subscriber
// only needs the newer. The zero key marks a notification that is never
// coalesced: message.*, reaction.changed and ui.focus.
type key struct {
	topic, team, conv, sub string
}

// stateKey returns the key of ev, zero for the events that are never
// coalesced. typing is keyed too: a repeat of the same person typing in
// the same place says nothing new.
func stateKey(ev domain.Event) key {
	k := key{topic: ev.Topic(), team: ev.TeamID().String()}
	switch e := ev.(type) {
	case domain.ConversationUpdated:
		k.conv = e.Conversation.ID.String()
	case domain.ReadChanged:
		k.conv = e.Conversation.String()
	case domain.DraftChanged:
		k.conv, k.sub = e.Conversation.String(), e.Thread.String()
	case domain.UserTyping:
		k.conv, k.sub = e.Conversation.String(), e.User.String()
	case domain.PresenceChanged:
		k.sub = e.User.String()
	case domain.ConnectionChanged, domain.DNDChanged:
	default:
		return key{}
	}
	return k
}

// summaryKey keys unread.summary: each one replaces the whole summary.
var summaryKey = key{topic: TopicUnreadSummary}

// note is one notification waiting in a subscriber's queue.
type note struct {
	method string
	params any
	key    key
}

// queue is one subscriber's pending notifications, in the order they
// leave. Pushing a note whose key is already queued removes the queued one
// and appends the new one, so what leaves is always a subsequence of what
// was pushed, in push order, with only superseded states left out.
//
// The bound counts only the notes that are never coalesced; keyed notes
// are bounded by the number of distinct keys instead.
type queue struct {
	mu     sync.Mutex
	notes  list.List             // of note
	keyed  map[key]*list.Element // the queued note of each key
	strict int                   // queued notes with no key
	bound  int

	wake chan struct{} // something was pushed; holds at most one signal
}

func newQueue(bound int) *queue {
	return &queue{keyed: make(map[key]*list.Element), bound: bound, wake: make(chan struct{}, 1)}
}

// push queues n and reports false, queuing nothing, when n has no key and
// bound such notes are already queued.
func (q *queue) push(n note) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if n.key == (key{}) {
		if q.strict >= q.bound {
			return false
		}
		q.strict++
		q.notes.PushBack(n)
	} else {
		if old, ok := q.keyed[n.key]; ok {
			q.notes.Remove(old)
		}
		q.keyed[n.key] = q.notes.PushBack(n)
	}
	select {
	case q.wake <- struct{}{}:
	default: // the drain is already woken
	}
	return true
}

// pop takes the first note, if any.
func (q *queue) pop() (note, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	e := q.notes.Front()
	if e == nil {
		return note{}, false
	}
	n := q.notes.Remove(e).(note)
	if n.key == (key{}) {
		q.strict--
	} else {
		delete(q.keyed, n.key)
	}
	return n, true
}
