package syncengine

import (
	"context"
	"errors"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/ratebudget"
)

// reconcile catches up, at Background priority, every conversation open in
// a client or with activity since startup (spec sync-engine,
// "Reconciliation after reconnecting"). A conversation that fails is
// logged and skipped; rejected credentials or an ended ctx stop it all.
func (e *Engine) reconcile(ctx context.Context) {
	var convs []domain.ConversationID
	if err := e.do(ctx, interestOp(&convs)); err != nil {
		return
	}
	for _, conv := range convs {
		if err := e.catchUp(ctx, conv, ratebudget.Background); err != nil {
			if stopping(ctx, err) {
				return
			}
			e.cfg.Log.Warn("sync: could not catch up a conversation", "team", e.w.team, "conversation", conv, "err", err)
		}
	}
}

// stopping reports whether err means the engine is shutting down, so
// there is no point trying the next conversation.
func stopping(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, provider.ErrAuthExpired) || errors.Is(err, ErrStopped)
}

// interestOp copies the conversations worth catching up into out.
func interestOp(out *[]domain.ConversationID) op {
	return func(_ context.Context, w *writer) error {
		*out = reconcileSet(w.open, w.active)
		return nil
	}
}
