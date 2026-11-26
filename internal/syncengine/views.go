package syncengine

import (
	"context"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// The conversation list as clients see it: the Store derives each entry
// (store.Store.Conversations) and the writer publishes conversation.updated
// whenever an entry differs from what clients were last told. Only the
// writer's goroutine runs this code.

// listOp stores the provider's conversation list. Its read markers go
// first, in order, each published as read.changed when it moves (one the
// Store rejects is logged and skipped); then the list itself, and every
// entry that changed is published.
func listOp(list []domain.Conversation) op {
	return func(ctx context.Context, w *writer) error {
		w.loadViews(ctx)
		for _, c := range list {
			if err := w.moveRead(ctx, c.ID, c.LastRead); err != nil {
				w.log.Warn("sync: could not store a read marker", "team", w.team, "conversation", c.ID, "err", err)
			}
		}
		if err := w.store.PutConversations(ctx, list); err != nil {
			w.log.Warn("sync: could not store the conversation list", "team", w.team, "err", err)
			return nil
		}
		w.publishViews(ctx)
		return nil
	}
}

// refresh publishes conv's entry if it changed. A conversation that is
// not in the list has no entry.
func (w *writer) refresh(ctx context.Context, conv domain.ConversationID) {
	w.loadViews(ctx)
	if _, ok := w.views[conv]; ok {
		w.publishViews(ctx)
	}
}

// loadViews takes the stored list, as a previous session left it, for
// what clients know, so a restart publishes only what changed since.
func (w *writer) loadViews(ctx context.Context) {
	if w.views != nil {
		return
	}
	if list, ok := w.readList(ctx); ok {
		w.views = byID(list)
	}
}

// publishViews reads the list back from the Store and publishes, in its
// order, every entry clients were not yet told of as it now is.
func (w *writer) publishViews(ctx context.Context) {
	list, ok := w.readList(ctx)
	if !ok {
		return
	}
	for _, c := range list {
		if w.views[c.ID] != c {
			w.pub.Publish(domain.ConversationUpdated{Team: w.team, Conversation: c})
		}
	}
	w.views = byID(list)
}

// readList reads the stored list. A failure is only logged: whatever
// triggered the read is already stored and published.
func (w *writer) readList(ctx context.Context) ([]domain.Conversation, bool) {
	list, err := w.store.Conversations(ctx)
	if err != nil {
		w.log.Warn("sync: could not read the conversation list", "team", w.team, "err", err)
		return nil, false
	}
	return list, true
}

func byID(list []domain.Conversation) map[domain.ConversationID]domain.Conversation {
	m := make(map[domain.ConversationID]domain.Conversation, len(list))
	for _, c := range list {
		m[c.ID] = c
	}
	return m
}

// entryChangedBy names the conversation whose list entry ev can change:
// a new or deleted message moves its latest, a read marker its unread
// state. An edit or a reaction changes neither.
func entryChangedBy(ev domain.Event) (domain.ConversationID, bool) {
	switch e := ev.(type) {
	case domain.MessageCreated, domain.MessageDeleted:
		return activityIn(e)
	case domain.ReadChanged:
		return e.Conversation, true
	default:
		return domain.ConversationID{}, false
	}
}
