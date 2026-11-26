package syncengine

import (
	"context"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/ratebudget"
)

// The budget keys of the Slack methods behind Provider.History and
// Provider.Replies.
const (
	methodHistory = "conversations.history"
	methodReplies = "conversations.replies"
)

// OpenConversation records that a client opened conv, until a matching
// CloseConversation, and catches it up at Interactive priority: it fetches
// the newest page, and keeps paging back until it reaches what the Store
// already had. Messages the Store lacked are stored and published as
// message.created; a conversation the Store knew nothing about is only
// stored, since that is a backfill rather than news. Once it returns, the
// RPC layer reads the conversation from the Store.
func (e *Engine) OpenConversation(ctx context.Context, conv domain.ConversationID) error {
	if err := e.do(ctx, openOp(conv, 1)); err != nil {
		return err
	}
	return e.catchUp(ctx, conv, ratebudget.Interactive)
}

// CloseConversation undoes one OpenConversation.
func (e *Engine) CloseConversation(ctx context.Context, conv domain.ConversationID) error {
	return e.do(ctx, openOp(conv, -1))
}

// LoadOlder fetches, at Interactive priority, the page of conv's messages
// just older than the oldest the Store holds (store.Store.Oldest), and
// stores it. When the Store holds nothing for conv, that is the newest
// page, as on a first open. It returns how many messages Slack had; zero
// means the start of the conversation.
func (e *Engine) LoadOlder(ctx context.Context, conv domain.ConversationID) (int, error) {
	before, err := e.cfg.Store.Oldest(ctx, conv)
	if err != nil {
		return 0, err
	}
	page, err := e.history(ctx, conv, before, e.cfg.PageSize, ratebudget.Interactive)
	if err != nil {
		return 0, err
	}
	return len(page), e.do(ctx, putOp(conv, page))
}

// OpenThread fetches the replies of the thread rooted at thread at
// Interactive priority and stores them.
func (e *Engine) OpenThread(ctx context.Context, conv domain.ConversationID, thread domain.TS) error {
	var replies []domain.Message
	err := e.call(ctx, methodReplies, ratebudget.Interactive, func(ctx context.Context) error {
		var err error
		replies, err = e.cfg.Provider.Replies(ctx, conv, thread)
		return err
	})
	if err != nil {
		return err
	}
	return e.do(ctx, putOp(conv, replies))
}

// catchUp fetches conv from the newest message back to what the Store
// has, then stores it and publishes what was missed, in one op so a live
// event cannot slip between the comparison and the write.
func (e *Engine) catchUp(ctx context.Context, conv domain.ConversationID, p ratebudget.Priority) error {
	newest, err := e.newestStored(ctx, conv)
	if err != nil {
		return err
	}
	fetched, err := pageBack(func(before domain.TS) ([]domain.Message, error) {
		return e.history(ctx, conv, before, e.cfg.PageSize, p)
	}, func(page []domain.Message) bool {
		return bridged(page, newest, e.cfg.PageSize)
	})
	if err != nil || len(fetched) == 0 {
		return err
	}
	return e.do(ctx, catchUpOp(conv, fetched))
}

// newestStored is the newest top-level message the Store holds for conv,
// or zero. Reads are safe from any goroutine; only writes go to the writer.
func (e *Engine) newestStored(ctx context.Context, conv domain.ConversationID) (domain.TS, error) {
	msgs, err := e.cfg.Store.Messages(ctx, conv, domain.TS{}, 1)
	if err != nil || len(msgs) == 0 {
		return domain.TS{}, err
	}
	return msgs[0].TS, nil
}

// history fetches up to limit of conv's messages older than before.
func (e *Engine) history(ctx context.Context, conv domain.ConversationID, before domain.TS, limit int, p ratebudget.Priority) ([]domain.Message, error) {
	var page []domain.Message
	err := e.call(ctx, methodHistory, p, func(ctx context.Context) error {
		var err error
		page, err = e.cfg.Provider.History(ctx, conv, before, limit)
		return err
	})
	return page, err
}

// pageBack reads pages newest first, each before the oldest message of the
// previous one, until a page is empty or enough says it is the last.
func pageBack(fetch func(before domain.TS) ([]domain.Message, error), enough func([]domain.Message) bool) ([]domain.Message, error) {
	var all []domain.Message
	var before domain.TS
	for {
		page, err := fetch(before)
		if err != nil || len(page) == 0 {
			return all, err
		}
		all = append(all, page...)
		if enough(page) {
			return all, nil
		}
		before = page[len(page)-1].TS
	}
}
