package syncengine_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

func entry(c domain.Conversation) domain.Event {
	return domain.ConversationUpdated{Team: team, Conversation: c}
}

func (r *rig) listed(t *testing.T) []domain.Conversation {
	t.Helper()
	got, err := r.store.Conversations(bg)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTheListIsStoredAndEveryEntryPublished(t *testing.T) {
	r := newRig(t)
	direct := domain.Conversation{ID: dm, Kind: domain.DirectMessage, Latest: ts1}
	general := domain.Conversation{ID: convA, Kind: domain.Channel, Name: "general", Latest: ts3, LastRead: ts2, Mentions: 1}
	r.fake.SetConversations(direct, general)
	r.firstSession(t)

	// Markers first, then the entries in the Store's order.
	r.pub.expect(t, readTo(convA, ts2), entry(general), entry(direct))
	if got := r.listed(t); !reflect.DeepEqual(got, []domain.Conversation{general, direct}) {
		t.Fatalf("stored list = %+v", got)
	}
}

func TestOnlyTheEntriesThatChangedArePublishedAgain(t *testing.T) {
	r := newRig(t)
	general := domain.Conversation{ID: convA, Kind: domain.Channel, Latest: ts1, LastRead: ts1}
	direct := domain.Conversation{ID: dm, Kind: domain.DirectMessage, Latest: ts1, LastRead: ts1}
	r.fake.SetConversations(general, direct)
	live, _ := r.firstSession(t)
	r.pub.expect(t, readTo(convA, ts1), readTo(dm, ts1), entry(general), entry(direct))

	direct.Name, direct.Latest = "renamed", ts2
	r.fake.SetConversations(general, direct)
	r.dropAndReconnect(t, live)
	// convA sorts first: had it been published, it would come first.
	r.pub.expect(t, entry(direct))
}

func TestARestartPublishesOnlyWhatChangedSinceTheStoredList(t *testing.T) {
	r := newRig(t)
	general := domain.Conversation{ID: convA, Kind: domain.Channel, Latest: ts1}
	direct := domain.Conversation{ID: dm, Kind: domain.DirectMessage, Latest: ts1}
	if err := r.store.PutConversations(bg, []domain.Conversation{general, direct}); err != nil {
		t.Fatal(err)
	}
	direct.Mentions = 1
	r.fake.SetConversations(general, direct)
	r.firstSession(t)
	r.pub.expect(t, entry(direct))
}

func TestLiveEventsPublishTheEntryTheyChange(t *testing.T) {
	r := newRig(t)
	general := domain.Conversation{ID: convA, Kind: domain.Channel, Latest: ts1, LastRead: ts1}
	r.fake.SetConversations(general)
	live, _ := r.firstSession(t)
	r.pub.expect(t, readTo(convA, ts1), entry(general))

	live <- created(convA, ts2)
	general.Latest = ts2
	r.pub.expect(t, created(convA, ts2), entry(general))

	edit := domain.MessageUpdated{Team: team, Conversation: convA, Message: domain.Message{TS: ts2, Text: "edited", Edited: ts3}}
	live <- edit
	live <- readTo(convA, ts2)
	general.LastRead = ts2
	r.pub.expect(t, edit, readTo(convA, ts2), entry(general))

	gone := domain.MessageDeleted{Team: team, Conversation: convA, TS: ts2}
	live <- gone
	general.Latest = ts1 // back to the list's
	r.pub.expect(t, gone, entry(general))

	live <- created(convB, ts3) // not in the list: no entry to publish
	live <- created(convA, ts3)
	general.Latest = ts3
	r.pub.expect(t, created(convB, ts3), created(convA, ts3), entry(general))
}

func TestACatchUpPublishesTheEntryItChanges(t *testing.T) {
	r := newRig(t)
	general := domain.Conversation{ID: convA, Kind: domain.Channel, Latest: ts1, LastRead: ts1}
	r.fake.SetConversations(general)
	r.cache(t, convA, msg(ts1))
	r.firstSession(t)
	r.pub.expect(t, readTo(convA, ts1), entry(general))

	r.fake.SetHistory(convA, msg(ts1), msg(ts2))
	if err := r.engine.OpenConversation(bg, convA); err != nil {
		t.Fatal(err)
	}
	general.Latest = ts2
	r.pub.expect(t, created(convA, ts2), entry(general))
}

// putListFails rejects every conversation list.
type putListFails struct{ store.Store }

func (putListFails) PutConversations(context.Context, []domain.Conversation) error {
	return errors.New("disk full")
}

func TestAListTheStoreRejectsIsLoggedAndItsMarkersStillMove(t *testing.T) {
	r := newRigFrom(t, nil, func(s store.Store) store.Store { return putListFails{s} })
	r.fake.SetConversations(domain.Conversation{ID: convA, Kind: domain.Channel, Latest: ts2, LastRead: ts1})
	r.firstSession(t)
	r.pub.expect(t, readTo(convA, ts1))
	r.waitForLog(t, "could not store the conversation list", 1)
	if logs := r.logs.String(); !strings.Contains(logs, "disk full") {
		t.Fatalf("log = %q", logs)
	}
}

// listReadFails cannot read the list back.
type listReadFails struct{ store.Store }

func (listReadFails) Conversations(context.Context) ([]domain.Conversation, error) {
	return nil, errors.New("disk on fire")
}

func TestAListThatCannotBeReadBackIsLoggedAndPublishesNothing(t *testing.T) {
	r := newRigFrom(t, nil, func(s store.Store) store.Store { return listReadFails{s} })
	r.fake.SetConversations(domain.Conversation{ID: convA, Kind: domain.Channel, Latest: ts2})
	live, _ := r.firstSession(t)
	r.waitForLog(t, "could not read the conversation list", 2) // before and after storing it

	live <- created(convB, ts1)
	r.pub.expect(t, created(convB, ts1))
	r.pub.expectNothing(t)
}
