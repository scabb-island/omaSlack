package events

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// ── Per-subscriber queues coalesce state (spec daemon-api, "Slow
// subscribers are disconnected") ──

func publishAll(h *Hub, evs ...domain.Event) {
	for _, ev := range evs {
		h.Publish(ev)
	}
}

// notes is what a subscriber gets for evs, in order.
func notes(evs ...domain.Event) []delivered {
	out := make([]delivered, 0, len(evs))
	for _, ev := range evs {
		out = append(out, delivered{ev.Topic(), wire.FromEvent(ev)})
	}
	return out
}

func checkDelivered(t *testing.T, got, want []delivered) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d notifications, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Fatalf("notification %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func convUpdated(t *testing.T, tm string, i int, latest string) domain.ConversationUpdated {
	return domain.ConversationUpdated{Team: team(t, tm), Conversation: domain.Conversation{
		ID: conv(t, fmt.Sprintf("C%08d", i)), Name: fmt.Sprint("chan-", i), Latest: ts(t, latest),
	}}
}

func readChanged(t *testing.T, i int, lastRead string) domain.ReadChanged {
	return domain.ReadChanged{Team: team(t, "T0001"), Conversation: conv(t, fmt.Sprintf("C%08d", i)), LastRead: ts(t, lastRead)}
}

// Spec daemon-api, "Startup burst of state events": a reconcile publishes
// the whole conversation list and the read markers at once, which used to
// disconnect healthy clients of large workspaces.
func TestAStartupBurstOfStateEventsReachesAReadingClient(t *testing.T) {
	const convs, reads = 1471, 142
	var logs logBuffer
	h := New(Options{Summary: noSummary, Log: logs.logger()}) // the default bound, 1024
	p := stalled(t, h, "conversation.updated", "read.changed")

	var first, last []domain.Event
	for i := range convs {
		first = append(first, convUpdated(t, "T0001", i, "1700000000.000001"))
		last = append(last, convUpdated(t, "T0001", i, "1700000000.000002"))
	}
	var marks []domain.Event
	for i := range reads {
		marks = append(marks, readChanged(t, i, "1700000000.000001"))
	}
	publishAll(h, first...)
	publishAll(h, marks...)
	publishAll(h, last...) // the reconcile ran again before the client read
	p.open()

	checkDelivered(t, p.got(t, reads+convs), notes(append(marks, last...)...))
	if p.isClosed() {
		t.Errorf("a client that reads was disconnected; log = %q", logs.String())
	}
}

// A newer state event replaces the queued one with the same key and goes
// to the end of the queue; events with other keys stay where they were.
func TestANewerStateReplacesTheQueuedOne(t *testing.T) {
	draft := func(thread domain.TS, text string) domain.DraftChanged {
		return domain.DraftChanged{Team: team(t, "T0001"), Conversation: conv(t, "C0001"), Thread: thread, Text: text}
	}
	presence := func(u string, p domain.Presence) domain.PresenceChanged {
		return domain.PresenceChanged{Team: team(t, "T0001"), User: user(t, u), Presence: p}
	}
	state := func(tm string, s domain.ConnectionState) domain.ConnectionChanged {
		return domain.ConnectionChanged{Team: team(t, tm), State: s}
	}
	typer := func(u string) domain.UserTyping {
		return domain.UserTyping{Team: team(t, "T0001"), Conversation: conv(t, "C0001"), User: user(t, u)}
	}
	// Each case publishes old, other, newer: old and newer share a key,
	// other differs from them in one part of it.
	cases := []struct {
		name              string
		old, other, newer domain.Event
	}{
		{"conversation in another team",
			convUpdated(t, "T0001", 1, "1700000000.000001"), convUpdated(t, "T0002", 1, "1700000000.000001"), convUpdated(t, "T0001", 1, "1700000000.000002")},
		{"read marker of another conversation",
			readChanged(t, 1, "1700000000.000001"), readChanged(t, 2, "1700000000.000001"), readChanged(t, 1, "1700000000.000002")},
		{"presence of another user",
			presence("U0001", domain.Away), presence("U0002", domain.Away), presence("U0001", domain.Active)},
		{"connection of another team",
			state("T0001", domain.Connecting), state("T0002", domain.Connecting), state("T0001", domain.Connected)},
		{"draft of a thread in the same conversation",
			draft(domain.TS{}, "a"), draft(ts(t, "1700000000.000001"), "x"), draft(domain.TS{}, "ab")},
		{"typing by another user",
			typer("U0001"), typer("U0002"), typer("U0001")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := New(Options{Summary: noSummary})
			p := stalled(t, h, c.old.Topic())
			publishAll(h, c.old, c.other, c.newer)
			p.open()
			checkDelivered(t, p.got(t, 2), notes(c.other, c.newer))
		})
	}
}

func TestMessageAndReactionEventsAreNeverCoalesced(t *testing.T) {
	h := New(Options{Summary: noSummary})
	p := stalled(t, h, "message.created", "message.updated", "message.deleted", "reaction.changed")
	c := conv(t, "C0001")
	m := created(t, 1)
	reaction := domain.ReactionChanged{Team: m.Team, Conversation: c, TS: m.Message.TS, User: user(t, "U0001"), Emoji: "+1", Added: true}
	updated := domain.MessageUpdated{Team: m.Team, Conversation: c, Message: m.Message}
	deleted := domain.MessageDeleted{Team: m.Team, Conversation: c, TS: m.Message.TS}
	evs := []domain.Event{m, m, updated, updated, reaction, reaction, deleted, deleted}
	publishAll(h, evs...)
	focus := wire.FocusEvent{Team: "T0001", Conv: "C0001"}
	h.Focus(focus)
	h.Focus(focus)
	p.open()

	want := append(notes(evs...), delivered{TopicFocus, focus}, delivered{TopicFocus, focus})
	checkDelivered(t, p.got(t, len(want)), want)
}

// A state event published after a message event of the same conversation
// is delivered after it, even when it replaces a state queued before it.
func TestAStateEventNeverOvertakesAnEarlierMessage(t *testing.T) {
	h := New(Options{Summary: noSummary})
	p := stalled(t, h, "conversation.updated", "read.changed", "message.created")
	m := created(t, 7)
	i := 1 // C00000001
	m.Conversation = conv(t, fmt.Sprintf("C%08d", i))
	evs := []domain.Event{
		convUpdated(t, "T0001", i, "1700000000.000001"), readChanged(t, i, "1700000000.000001"),
		m,
		convUpdated(t, "T0001", i, m.Message.TS.String()), readChanged(t, i, m.Message.TS.String()),
	}
	publishAll(h, evs...)
	p.open()
	checkDelivered(t, p.got(t, 3), notes(evs[2:]...))
}

func TestQueuedSummariesAreCoalesced(t *testing.T) {
	var unread atomic.Int64
	summary := func(context.Context) (wire.Summary, error) {
		return wire.Summary{Total: wire.Counts{DMs: int(unread.Add(1))}}, nil
	}
	h := New(Options{Summary: summary})
	p := stalled(t, h, TopicUnreadSummary)
	h.sendSummary(context.Background())
	h.sendSummary(context.Background())
	p.open()
	checkDelivered(t, p.got(t, 1), []delivered{{TopicUnreadSummary, wire.Summary{Total: wire.Counts{DMs: 2}}}})
}

// The bound counts only events that cannot be coalesced: any amount of
// state fits, but a client that stops reading is still disconnected once
// that many message events are behind.
func TestTheBoundCountsOnlyEventsThatCannotBeCoalesced(t *testing.T) {
	const bound = 4
	var logs logBuffer
	h := New(Options{Summary: noSummary, QueueSize: bound, Log: logs.logger()})
	p := stalled(t, h, "conversation.updated", "presence.changed", "message.created")
	for i := range 5000 {
		h.Publish(convUpdated(t, "T0001", i%10, fmt.Sprintf("1700000000.%06d", i+1)))
		h.Publish(domain.PresenceChanged{Team: team(t, "T0001"), User: user(t, fmt.Sprintf("U%04d", i%10))})
	}
	for i := range bound - 1 { // the primer may still be queued too
		h.Publish(created(t, i))
	}
	if p.isClosed() {
		t.Fatalf("disconnected with %d message events queued; log = %q", bound-1, logs.String())
	}
	h.Publish(created(t, bound))
	h.Publish(created(t, bound+1))
	if !p.isClosed() {
		t.Fatalf("still connected with more than %d message events queued", bound)
	}
}
