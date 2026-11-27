package slackwire

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// The writes behind provider.MessageEditor, provider.Reactor and
// provider.DNDController (dnd.go). Both providers call the same Web API methods
// with their own credentials.
//
// Writes that find their work already done succeed, so a client that
// toggles a reaction or ends a snooze twice sees no error: reactions.add
// answering already_reacted, reactions.remove answering no_reaction and
// dnd.endSnooze answering snooze_not_active.

type updateResponse struct {
	Envelope
	TS      string  `json:"ts"`
	Message Message `json:"message"`
}

// Edit calls chat.update and returns the message as Slack stored it.
// Slack's answer may leave the message's ts out; it is the ts edited. It
// may leave edited out too: Edited is then the local clock, so the message
// still reads as edited until the message_changed event that follows
// brings Slack's own. The answer carries no thread_ts for some replies, so
// ThreadTS may be zero for a message that is in a thread.
func (a *WebAPI) Edit(ctx context.Context, conv domain.ConversationID, ts domain.TS, text string) (domain.Message, error) {
	form := url.Values{"channel": {conv.String()}, "ts": {ts.String()}, "text": {text}}
	var resp updateResponse
	if err := a.Call(ctx, "chat.update", form, &resp); err != nil {
		return domain.Message{}, err
	}
	m, err := resp.stored(a.now()).Parse()
	if err != nil {
		return domain.Message{}, fmt.Errorf("chat.update: %w", err)
	}
	return m, nil
}

// stored is the edited message, with what Slack left out filled in.
func (r updateResponse) stored(now time.Time) Message {
	w := r.Message
	if w.TS == "" {
		w.TS = r.TS
	}
	if w.Edited.TS == "" {
		w.Edited.TS = fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/int(time.Microsecond))
	}
	return w
}

// Delete calls chat.delete.
func (a *WebAPI) Delete(ctx context.Context, conv domain.ConversationID, ts domain.TS) error {
	return a.Call(ctx, "chat.delete", url.Values{"channel": {conv.String()}, "ts": {ts.String()}}, &Envelope{})
}

// AddReaction calls reactions.add. A reaction the user already left is
// not an error.
func (a *WebAPI) AddReaction(ctx context.Context, conv domain.ConversationID, ts domain.TS, emoji string) error {
	return a.callUnlessDone(ctx, "reactions.add", reactionForm(conv, ts, emoji), "already_reacted")
}

// RemoveReaction calls reactions.remove. A reaction the user did not
// leave is not an error.
func (a *WebAPI) RemoveReaction(ctx context.Context, conv domain.ConversationID, ts domain.TS, emoji string) error {
	return a.callUnlessDone(ctx, "reactions.remove", reactionForm(conv, ts, emoji), "no_reaction")
}

func reactionForm(conv domain.ConversationID, ts domain.TS, emoji string) url.Values {
	return url.Values{"channel": {conv.String()}, "timestamp": {ts.String()}, "name": {emoji}}
}

// callUnlessDone calls method, and treats done, the error code that says
// its work is already done, as success.
func (a *WebAPI) callUnlessDone(ctx context.Context, method string, form url.Values, done string) error {
	if err := a.Call(ctx, method, form, &Envelope{}); !HasCode(err, done) {
		return err
	}
	return nil
}
