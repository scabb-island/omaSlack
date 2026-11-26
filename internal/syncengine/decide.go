package syncengine

import (
	"cmp"
	"maps"
	"slices"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// The reconnect delay (spec sync-engine, "Reconnection with backoff").
const (
	minBackoff = time.Second
	maxBackoff = 30 * time.Second
	// doublings is how many times minBackoff doubles before maxBackoff caps
	// it (1, 2, 4, 8, 16, then 30 s).
	doublings = 5
)

// backoff is the delay before reconnect attempt number attempt (0 for the
// first retry after a connection worked). The ceiling doubles from one
// second up to thirty; r in [0, 1) places the delay in the upper half of
// it, so delays grow and spread clients apart ("equal jitter").
func backoff(attempt int, r float64) time.Duration {
	ceiling := maxBackoff
	if attempt < doublings {
		ceiling = minBackoff << attempt
	}
	half := ceiling / 2
	return max(minBackoff, half+time.Duration(r*float64(half)))
}

// status is what decides the connection state clients see: the state of
// the link to Slack, and how many calls are waiting out a 429.
type status struct {
	link    domain.ConnectionState
	limited int
}

// shown is the state published as connection.state. Expired credentials
// outrank a rate limit: nothing gets better by waiting.
func (s status) shown() domain.ConnectionState {
	if s.limited > 0 && s.link != domain.AuthExpired {
		return domain.RateLimited
	}
	return s.link
}

// reconcileSet is every conversation worth catching up after a reconnect:
// the ones open in a client and the ones with activity since startup, in
// a stable order.
func reconcileSet(open map[domain.ConversationID]int, active map[domain.ConversationID]struct{}) []domain.ConversationID {
	all := slices.AppendSeq(slices.Collect(maps.Keys(open)), maps.Keys(active))
	slices.SortFunc(all, func(a, b domain.ConversationID) int {
		return cmp.Compare(a.String(), b.String())
	})
	return slices.Compact(all)
}

// activityIn names the conversation whose messages ev changed, if any. A
// moved read marker or someone typing is not activity: nothing new to
// fetch comes from it.
func activityIn(ev domain.Event) (domain.ConversationID, bool) {
	switch e := ev.(type) {
	case domain.MessageCreated:
		return e.Conversation, true
	case domain.MessageUpdated:
		return e.Conversation, true
	case domain.MessageDeleted:
		return e.Conversation, true
	case domain.ReactionChanged:
		return e.Conversation, true
	default:
		return domain.ConversationID{}, false
	}
}

// missed is the part of fetched (newest first, as History returns it) the
// store does not know, oldest first, which is the order it happened in.
// When the store knew nothing, this is a first backfill and nothing was
// missed: clients learn of it when they read the conversation.
func missed(fetched []domain.Message, known map[domain.TS]bool) []domain.Message {
	if len(known) == 0 {
		return nil
	}
	var out []domain.Message
	for _, msg := range slices.Backward(fetched) {
		if !known[msg.TS] {
			out = append(out, msg)
		}
	}
	return out
}

// bridged reports whether paging back through History can stop after page
// (newest first, never empty): Slack has nothing older, the page reached
// the newest stored message, or the store had nothing to reach.
func bridged(page []domain.Message, newestStored domain.TS, limit int) bool {
	oldest := page[len(page)-1].TS
	return len(page) < limit || newestStored.IsZero() || !newestStored.Before(oldest)
}
