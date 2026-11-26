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

// methodReadMarker is the budget key of provider.ReadMarkerReader.LastRead.
const methodReadMarker = "conversations.info"

// reconcile catches up, at Background priority, every conversation open in
// a client or with activity since startup (spec sync-engine,
// "Reconciliation after reconnecting"), with its read marker.
func (e *Engine) reconcile(ctx context.Context) {
	e.eachInteresting(ctx, "catch up", func(conv domain.ConversationID) error {
		if err := e.catchUp(ctx, conv, ratebudget.Background); err != nil {
			return err
		}
		return e.syncRead(ctx, conv)
	})
}

// pollReads keeps read markers current for a provider that does not
// stream them, until ctx ends or the credentials are rejected.
func (e *Engine) pollReads(ctx context.Context) {
	for e.sleep(ctx, e.cfg.ReadPollInterval) == nil {
		e.eachInteresting(ctx, "read the marker of", func(conv domain.ConversationID) error {
			return e.syncRead(ctx, conv)
		})
	}
}

// eachInteresting runs fn for every conversation open in a client or with
// activity since startup. A conversation that fails is logged and
// skipped; rejected credentials or an ended ctx stop the round.
func (e *Engine) eachInteresting(ctx context.Context, what string, fn func(domain.ConversationID) error) {
	var convs []domain.ConversationID
	if err := e.do(ctx, interestOp(&convs)); err != nil {
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
func interestOp(out *[]domain.ConversationID) op {
	return func(_ context.Context, w *writer) error {
		*out = reconcileSet(w.open, w.active)
		return nil
	}
}

func readOp(conv domain.ConversationID, at domain.TS) op {
	return func(ctx context.Context, w *writer) error { return w.moveRead(ctx, conv, at) }
}
