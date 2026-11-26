package syncengine

import (
	"context"
	"errors"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/ratebudget"
)

// The engine polls provider.ReadMarkerReader, when the provider has it, for
// the markers of the conversations that matter every
// Config.ReadPollInterval and after every reconnect.

// The budget keys of provider.ReadMarkerReader.LastRead and
// Provider.Conversations.
const (
	methodReadMarker    = "conversations.info"
	methodConversations = "users.conversations"
)

// reconcile runs, at Background priority, after every connection. It
// stores the provider's conversation list and the read markers it
// reports, so unread state is known before any client opens a
// conversation (a list that could not be fetched leaves the stored one
// alone); after an
// outage it also catches up every conversation open in a client, with
// activity since startup, or that the list says is unread or mentions the
// user (spec sync-engine, "Reconciliation after reconnecting"), with its
// read marker. Without a list it catches up the rest.
func (e *Engine) reconcile(ctx context.Context, outage bool) {
	list, err := e.conversations(ctx)
	if stopping(err) {
		return
	}
	if err == nil {
		if err := e.do(ctx, listOp(list)); err != nil {
			return
		}
	}
	if !outage {
		return
	}
	e.eachInteresting(ctx, list, "catch up", func(conv domain.ConversationID) error {
		if err := e.catchUp(ctx, conv, ratebudget.Background); err != nil {
			return err
		}
		return e.syncRead(ctx, conv)
	})
}

// conversations asks the provider for its conversation list. A provider
// that cannot list them (or not yet) is logged the first time only, since
// it fails the same way after every reconnect.
func (e *Engine) conversations(ctx context.Context) ([]domain.Conversation, error) {
	var list []domain.Conversation
	err := e.call(ctx, methodConversations, ratebudget.Background, func(ctx context.Context) error {
		var err error
		list, err = e.cfg.Provider.Conversations(ctx)
		return err
	})
	e.logListHealth(err)
	return list, err
}

// logListHealth logs the conversation list's health when it changes: a
// warning when listing starts failing, a note when it works again, and
// nothing while it stays the same.
func (e *Engine) logListHealth(err error) {
	switch {
	case err == nil:
		if e.listFailed.CompareAndSwap(true, false) {
			e.cfg.Log.Info("sync: listing conversations works again", "team", e.w.team)
		}
	case !stopping(err) && e.listFailed.CompareAndSwap(false, true):
		e.cfg.Log.Warn("sync: could not list conversations; catching up only open and active ones", "team", e.w.team, "err", err)
	}
}

// pollReads keeps read markers current for a provider that does not
// stream them, until ctx ends or the credentials are rejected.
func (e *Engine) pollReads(ctx context.Context) {
	for e.sleep(ctx, e.cfg.ReadPollInterval) == nil {
		e.eachInteresting(ctx, nil, "read the marker of", func(conv domain.ConversationID) error {
			return e.syncRead(ctx, conv)
		})
	}
}

// eachInteresting runs fn for every conversation reconcileSet picks from
// list and what the engine knows. A conversation that fails is logged and
// skipped; rejected credentials or an ended ctx stop the round.
func (e *Engine) eachInteresting(ctx context.Context, list []domain.Conversation, what string, fn func(domain.ConversationID) error) {
	var convs []domain.ConversationID
	if err := e.do(ctx, interestOp(list, &convs)); err != nil {
		return
	}
	for _, conv := range convs {
		if err := fn(conv); err != nil {
			if stopping(err) {
				return
			}
			e.cfg.Log.Warn("sync: could not "+what+" a conversation", "team", e.w.team, "conversation", conv, "err", err)
		}
	}
}

// syncRead moves conv's stored marker to the provider's, when the
// provider can read it.
func (e *Engine) syncRead(ctx context.Context, conv domain.ConversationID) error {
	r, ok := e.cfg.Provider.(provider.ReadMarkerReader)
	if !ok {
		return nil
	}
	var at domain.TS
	err := e.call(ctx, methodReadMarker, ratebudget.Background, func(ctx context.Context) error {
		var err error
		at, err = r.LastRead(ctx, conv)
		return err
	})
	if err != nil {
		return err
	}
	return e.do(ctx, readOp(conv, at))
}

// stopping reports whether err means the engine is shutting down, so
// there is no point trying the next conversation. Once Run's ctx has
// ended, every call fails with its error before reaching Slack.
func stopping(err error) bool {
	return errors.Is(err, provider.ErrAuthExpired) || errors.Is(err, context.Canceled)
}

// interestOp copies the conversations worth catching up into out.
func interestOp(list []domain.Conversation, out *[]domain.ConversationID) op {
	return func(_ context.Context, w *writer) error {
		*out = reconcileSet(w.open, w.active, list)
		return nil
	}
}

func readOp(conv domain.ConversationID, at domain.TS) op {
	return refreshing(conv, func(ctx context.Context, w *writer) error { return w.moveRead(ctx, conv, at) })
}
