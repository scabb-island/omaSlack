package notify

import (
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// fixture builds the Candidate and Moment of one decision; each case
// changes what it tests.
type fixture struct {
	t    *testing.T
	cfg  Config
	mo   Moment
	cand Candidate
}

var now = time.Unix(1700000100, 0)

func newFixture(t *testing.T, kind domain.ConversationKind, text string) *fixture {
	t.Helper()
	self, _ := domain.ParseUserID("U0SELF")
	other, _ := domain.ParseUserID("U0BOB")
	return &fixture{
		t:   t,
		cfg: DefaultConfig(),
		mo:  Moment{Started: ts(t, "1700000000.000000"), Now: now},
		cand: Candidate{
			Team: team(t, "T01"), Self: self,
			Conversation: domain.Conversation{ID: conv(t, "C0GEN"), Kind: kind, Name: "general"},
			Message:      domain.Message{TS: ts(t, "1700000050.000100"), Author: domain.UserAuthor(other), Text: text},
		},
	}
}

func (f *fixture) decide() Reason { return Decide(f.cfg, f.mo, f.cand) }

func (f *fixture) want(r Reason) {
	f.t.Helper()
	if got := f.decide(); got != r {
		f.t.Errorf("Decide = %v, want %v", got, r)
	}
}

func TestDecideNotifies(t *testing.T) {
	newFixture(t, domain.DirectMessage, "hi").want(DirectMessage)
	newFixture(t, domain.GroupMessage, "hi").want(GroupMessage)
	newFixture(t, domain.Channel, "hey <@U0SELF>, look").want(Mention)
	newFixture(t, domain.PrivateChannel, "<!here> standup").want(Mention)

	group := newFixture(t, domain.Channel, "<!subteam^S0DEV|@devs> deploy")
	group.cand.Groups = []string{"S0DEV"}
	group.want(Mention)

	kw := newFixture(t, domain.Channel, "the *Deploy* is done")
	kw.cfg.Keywords = []string{"deploy"}
	kw.want(Keyword)

	ch := newFixture(t, domain.Channel, "anything")
	ch.cfg.Channels = []Target{{Name: "general"}}
	ch.want(Channel)

	thread := newFixture(t, domain.Channel, "a reply")
	thread.cand.Message.ThreadTS = ts(t, "1700000001.000000")
	thread.cand.Following = true
	thread.want(Thread)
}

func TestDecideDoesNotNotify(t *testing.T) {
	newFixture(t, domain.Channel, "chatter").want(Skip)
	newFixture(t, domain.Channel, "<@U0BOB> not me").want(Skip)
	newFixture(t, domain.Channel, "<!subteam^S0DEV> not my group").want(Skip)

	word := newFixture(t, domain.Channel, "redeployed")
	word.cfg.Keywords = []string{"deploy"}
	word.want(Skip)

	root := newFixture(t, domain.Channel, "a root the user follows is not a reply")
	root.cand.Following = true
	root.want(Skip)
}

func TestDecideGates(t *testing.T) {
	gates := map[string]func(f *fixture){
		"disabled": func(f *fixture) { f.cfg.Enabled = false },
		"own message": func(f *fixture) {
			f.cand.Message.Author = domain.UserAuthor(f.cand.Self)
		},
		"before the daemon started": func(f *fixture) { f.cand.Message.TS = ts(f.t, "1699999999.000000") },
		"do not disturb":            func(f *fixture) { f.mo.DND = now.Add(time.Minute) },
		"in view":                   func(f *fixture) { f.mo.Viewed = true },
		"muted by name":             func(f *fixture) { f.cfg.Muted = []Target{{Name: "general"}} },
		"muted by id": func(f *fixture) {
			f.cfg.Muted = []Target{{ID: f.cand.Conversation.ID}}
		},
	}
	for name, gate := range gates {
		f := newFixture(t, domain.DirectMessage, "<@U0SELF> deploy")
		f.cfg.Keywords = []string{"deploy"}
		f.cfg.Channels = []Target{{Name: "general"}}
		gate(f)
		if got := f.decide(); got != Skip {
			t.Errorf("%s: Decide = %v, want Skip", name, got)
		}
	}
}

func TestDecideDNDOver(t *testing.T) {
	f := newFixture(t, domain.DirectMessage, "hi")
	f.mo.DND = now.Add(-time.Second)
	f.want(DirectMessage)
	f.mo.DND = now
	f.want(DirectMessage)
}

func TestDecideStartedAtTheMessage(t *testing.T) {
	f := newFixture(t, domain.DirectMessage, "hi")
	f.mo.Started = f.cand.Message.TS
	f.want(DirectMessage)
}

func TestReasonString(t *testing.T) {
	for r, want := range map[Reason]string{
		Skip: "skip", DirectMessage: "dm", GroupMessage: "group dm", Mention: "mention",
		Thread: "thread", Keyword: "keyword", Channel: "channel", Reason(99): "",
	} {
		if r.String() != want {
			t.Errorf("%d: %q, want %q", r, r.String(), want)
		}
	}
}

func TestHasKeyword(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"deploy", true},
		{"DEPLOY now", true},
		{"after the deploy.", true},
		{"pre-deploy", true},
		{"deployed", false},
		{"redeploy", false},
		{"the on call rota", true},
		{"no match here", false},
	}
	kws := []string{"deploy", "on call"}
	for _, c := range cases {
		if got := hasKeyword(c.text, kws); got != c.want {
			t.Errorf("%q: %v", c.text, got)
		}
	}
}

func ts(t *testing.T, s string) domain.TS {
	t.Helper()
	v, err := domain.ParseTS(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
