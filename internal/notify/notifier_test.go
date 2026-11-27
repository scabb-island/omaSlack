package notify

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// fakeSender records what it is asked to show, numbering each from 1.
type fakeSender struct {
	mu   sync.Mutex
	sent []Notification
	got  chan Notification
}

// errNoBus is what the fake answers for a body of "unsendable".
var errNoBus = errors.New("no bus")

func (s *fakeSender) Notify(_ context.Context, n Notification) (uint32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n.Body == "unsendable" {
		return 0, errNoBus
	}
	s.sent = append(s.sent, n)
	s.got <- n
	return uint32(len(s.sent)), nil
}

type opened struct {
	target Focus
	open   Open
}

// harness runs a Notifier with a fake sender, a lookup that knows every
// conversation as a DM unless told otherwise, and records clicks.
type harness struct {
	t      *testing.T
	n      *Notifier
	sender *fakeSender
	views  *Views
	opens  chan opened
	kinds  map[string]domain.ConversationKind
	now    time.Time
}

var started = time.Unix(1700000000, 0)

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		t: t, sender: &fakeSender{got: make(chan Notification, 16)}, views: &Views{},
		opens: make(chan opened, 4), kinds: map[string]domain.ConversationKind{}, now: started.Add(time.Minute),
	}
	self, _ := domain.ParseUserID("U0SELF")
	h.n = New(Options{
		Sender: h.sender, Views: h.views, Started: started,
		Now: func() time.Time { return h.now },
		Lookup: func(_ context.Context, ev domain.MessageCreated) (Details, error) {
			if ev.Message.Text == "lookup fails" {
				return Details{}, errors.New("boom")
			}
			kind := domain.DirectMessage
			if k, ok := h.kinds[ev.Conversation.String()]; ok {
				kind = k
			}
			return Details{
				Candidate: Candidate{
					Team: ev.Team, Self: self, Message: ev.Message,
					Conversation: domain.Conversation{ID: ev.Conversation, Kind: kind, Name: "general"},
				},
				Author: "Bob",
			}, nil
		},
		Open: func(t Focus, o Open) { h.opens <- opened{t, o} },
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = h.n.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	return h
}

func (h *harness) message(convID, at, text string) domain.MessageCreated {
	return domain.MessageCreated{
		Team: team(h.t, "T01"), Conversation: conv(h.t, convID),
		Message: domain.Message{TS: ts(h.t, at), Author: domain.UserAuthor(user(h.t, "U0BOB")), Text: text},
	}
}

// expect waits for the next notification.
func (h *harness) expect(summary string) {
	h.t.Helper()
	select {
	case n := <-h.sender.got:
		if n.Summary != summary {
			h.t.Errorf("summary %q, want %q", n.Summary, summary)
		}
	case <-time.After(5 * time.Second):
		h.t.Fatalf("no notification, want %q", summary)
	}
}

// settle publishes a marker DM and waits for it, so every event before
// it has been decided.
func (h *harness) settle() {
	h.t.Helper()
	h.n.Publish(h.message("D0MARK", "1700000099.000000", "marker"))
	h.expect("Bob")
	h.none()
}

func (h *harness) none() {
	h.t.Helper()
	select {
	case n := <-h.sender.got:
		h.t.Errorf("unexpected notification %+v", n)
	default:
	}
}

func TestNotifierNotifiesOncePerMessage(t *testing.T) {
	h := newHarness(t)
	m := h.message("D01", "1700000010.000000", "hi")
	h.n.Publish(m)
	h.n.Publish(m) // a reconcile can publish the same message again
	h.expect("Bob")
	h.settle()
}

func TestNotifierSkips(t *testing.T) {
	h := newHarness(t)
	h.kinds["C0GEN"] = domain.Channel
	h.n.Publish(h.message("C0GEN", "1700000010.000000", "chatter"))
	h.n.Publish(h.message("D01", "1699999999.000000", "pending at startup"))
	h.n.Publish(h.message("D01", "1700000011.000000", "lookup fails"))
	h.n.Publish(domain.MessageUpdated{Team: team(t, "T01")})
	h.settle()
}

func TestNotifierDND(t *testing.T) {
	h := newHarness(t)
	t1 := team(t, "T01")
	h.n.Publish(domain.DNDChanged{Team: t1, Until: h.now.Add(time.Hour)})
	h.n.Publish(domain.DNDChanged{Team: team(t, "T02"), Until: time.Time{}}) // another workspace's DND
	h.n.Publish(h.message("D01", "1700000010.000000", "snoozed"))
	h.n.Publish(domain.DNDChanged{Team: t1})
	h.n.Publish(h.message("D01", "1700000011.000000", "awake"))
	h.expect("Bob")
	h.none()
}

func TestNotifierSuppressedInView(t *testing.T) {
	h := newHarness(t)
	h.views.Set("window", View{Team: team(t, "T01"), Conv: conv(t, "D01"), Focused: true})
	h.n.Publish(h.message("D01", "1700000010.000000", "seen"))
	h.settle()
}

func TestNotifierConfig(t *testing.T) {
	h := newHarness(t)
	cfg := DefaultConfig()
	cfg.Muted = []Target{{ID: conv(t, "D01")}}
	h.n.SetConfig(cfg)
	if h.n.Config().Muted == nil {
		t.Fatal("the config was not replaced")
	}
	h.n.Publish(h.message("D01", "1700000010.000000", "muted"))
	h.settle()
}

func TestNotifierClick(t *testing.T) {
	h := newHarness(t)
	cfg := DefaultConfig()
	cfg.Open = OpenTUI
	h.n.SetConfig(cfg)
	reply := h.message("D01", "1700000012.000000", "in a thread")
	reply.Message.ThreadTS = ts(t, "1700000011.000000")
	h.n.Publish(reply)
	h.expect("Bob (thread)")
	h.n.Clicked(99) // not ours
	h.n.Clicked(1)
	select {
	case o := <-h.opens:
		want := Focus{Team: team(t, "T01"), Conv: conv(t, "D01"), TS: ts(t, "1700000011.000000")}
		if o.target != want || o.open != OpenTUI {
			t.Errorf("opened %+v, want %+v in the TUI", o, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the click opened nothing")
	}
	h.n.Closed(1)
	h.n.Clicked(1)
	select {
	case o := <-h.opens:
		t.Errorf("a closed notification opened %+v", o)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestNotifierSendFails(t *testing.T) {
	h := newHarness(t)
	h.n.Publish(h.message("D01", "1700000010.000000", "unsendable"))
	h.settle()
	h.n.Clicked(0) // the failed one was never given an ID
	select {
	case o := <-h.opens:
		t.Errorf("opened %+v", o)
	default:
	}
}

func TestNotifierPublishNeverBlocks(t *testing.T) {
	n := New(Options{Sender: &fakeSender{}, Lookup: nil, Queue: 1})
	m := domain.MessageCreated{}
	done := make(chan struct{})
	go func() {
		for range 10 {
			n.Publish(m)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked without Run")
	}
}

func user(t *testing.T, s string) domain.UserID {
	t.Helper()
	id, err := domain.ParseUserID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
