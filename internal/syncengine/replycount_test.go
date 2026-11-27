package syncengine_test

import (
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
)

// A reply that arrives in real time counts on its cached root, and
// clients hear of the root's new count. Slack's echo of the same reply
// is not a second one.
func TestALiveReplyCountsOnItsRoot(t *testing.T) {
	r := newRig(t)
	root := msg(ts1)
	root.ThreadTS, root.ReplyCount, root.LatestReply = ts1, 1, ts2
	r.cache(t, convA, root)
	live := make(chan domain.Event)
	r.fake.Script(providertest.Session{Events: []domain.Event{state(domain.Connected)}, Live: live})
	r.start(t)
	r.pub.expect(t, state(domain.Connecting), state(domain.Connected))

	reply := domain.MessageCreated{Team: team, Conversation: convA, Message: domain.Message{TS: ts3, ThreadTS: ts1, Text: "reply"}}
	live <- reply
	counted := root
	counted.ReplyCount, counted.LatestReply = 2, ts3
	r.pub.expect(t, reply, domain.MessageUpdated{Team: team, Conversation: convA, Message: counted})

	live <- reply
	typing := domain.UserTyping{Team: team, Conversation: convA, User: me}
	live <- typing
	r.pub.expect(t, reply, typing)
}

// A reply whose root is not cached changes nothing else.
func TestALiveReplyWithoutItsRootIsOnlyStored(t *testing.T) {
	r := newRig(t)
	live := make(chan domain.Event)
	r.fake.Script(providertest.Session{Events: []domain.Event{state(domain.Connected)}, Live: live})
	r.start(t)
	r.pub.expect(t, state(domain.Connecting), state(domain.Connected))

	reply := domain.MessageCreated{Team: team, Conversation: convA, Message: domain.Message{TS: ts3, ThreadTS: ts1, Text: "reply"}}
	live <- reply
	typing := domain.UserTyping{Team: team, Conversation: convA, User: me}
	live <- typing
	r.pub.expect(t, reply, typing)
}
