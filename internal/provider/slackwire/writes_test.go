package slackwire

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

func TestEditReturnsTheMessageAsSlackStoredIt(t *testing.T) {
	api, s, _ := fakeSlack(t, map[string]map[string]string{"chat.update": {"": `{"ok":true,"channel":"C0001",
		"ts":"1700000100.000100","text":"fixed",
		"message":{"user":"U0001","text":"fixed","thread_ts":"1700000050.000050","edited":{"user":"U0001","ts":"1700000900.000000"}}}`}})
	conv, ts := must(t, domain.ParseConversationID, "C0001"), must(t, domain.ParseTS, "1700000100.000100")

	got, err := api.Edit(context.Background(), conv, ts, "fixed")
	if err != nil {
		t.Fatal(err)
	}

	want := domain.Message{
		TS: ts, ThreadTS: must(t, domain.ParseTS, "1700000050.000050"), Author: must(t, domain.ParseAuthor, "U0001"),
		Text: "fixed", Edited: must(t, domain.ParseTS, "1700000900.000000"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Edit() = %+v, want %+v", got, want)
	}
	wantForm := url.Values{"channel": {"C0001"}, "ts": {"1700000100.000100"}, "text": {"fixed"}, "token": {"secret"}}
	if f := s.form(0); !reflect.DeepEqual(f, wantForm) {
		t.Errorf("form = %v, want %v", f, wantForm)
	}
}

func TestEditFillsWhatSlackLeavesOutOfTheMessage(t *testing.T) {
	// The documented chat.update answer: the message has no ts and no edited.
	api, _, _ := fakeSlack(t, map[string]map[string]string{"chat.update": {"": `{"ok":true,"channel":"C0001",
		"ts":"1700000100.000100","text":"fixed","message":{"user":"U0001","text":"fixed"}}`}})
	api.now = func() time.Time { return time.Unix(1700000999, 123456789) }

	got, err := api.Edit(context.Background(), must(t, domain.ParseConversationID, "C0001"), must(t, domain.ParseTS, "1700000100.000100"), "fixed")
	if err != nil {
		t.Fatal(err)
	}

	if got.TS != must(t, domain.ParseTS, "1700000100.000100") || got.Edited != must(t, domain.ParseTS, "1700000999.123456") {
		t.Errorf("Edit() = %+v, want the request's ts and the local clock as edited", got)
	}
}

func TestEditFailsWhenTheStoredMessageDoesNotParse(t *testing.T) {
	api, _, _ := fakeSlack(t, map[string]map[string]string{"chat.update": {"": `{"ok":true,"ts":"1700000100.000100","message":{"text":"no author"}}`}})

	_, err := api.Edit(context.Background(), must(t, domain.ParseConversationID, "C0001"), must(t, domain.ParseTS, "1700000100.000100"), "x")
	if err == nil || !strings.HasPrefix(err.Error(), "chat.update: ") {
		t.Fatalf("Edit() err = %v, want a chat.update error", err)
	}
}

func TestWritesSendTheirArguments(t *testing.T) {
	ok := map[string]string{"": `{"ok":true}`}
	api, s, _ := fakeSlack(t, map[string]map[string]string{
		"chat.delete": ok, "reactions.add": ok, "reactions.remove": ok, "dnd.setSnooze": ok, "dnd.endSnooze": ok,
	})
	ctx, conv, ts := context.Background(), must(t, domain.ParseConversationID, "C0001"), must(t, domain.ParseTS, "1700000100.000100")
	cases := []struct {
		call func() error
		want url.Values
	}{
		{func() error { return api.Delete(ctx, conv, ts) }, url.Values{"channel": {"C0001"}, "ts": {"1700000100.000100"}}},
		{func() error { return api.AddReaction(ctx, conv, ts, "+1") }, url.Values{"channel": {"C0001"}, "timestamp": {"1700000100.000100"}, "name": {"+1"}}},
		{func() error { return api.RemoveReaction(ctx, conv, ts, "smile::skin-tone-2") }, url.Values{"channel": {"C0001"}, "timestamp": {"1700000100.000100"}, "name": {"smile::skin-tone-2"}}},
		{func() error { return second(api.SnoozeDND(ctx, 30)) }, url.Values{"num_minutes": {"30"}}},
		{func() error { return api.EndDND(ctx) }, url.Values{}},
	}
	for i, tc := range cases {
		if err := tc.call(); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		tc.want.Set("token", "secret")
		if f := s.form(i); !reflect.DeepEqual(f, tc.want) {
			t.Errorf("call %d form = %v, want %v", i, f, tc.want)
		}
	}
}

func TestWritesThatAreAlreadyDoneSucceed(t *testing.T) {
	api, _, _ := fakeSlack(t, map[string]map[string]string{
		"reactions.add":    {"": `{"ok":false,"error":"already_reacted"}`},
		"reactions.remove": {"": `{"ok":false,"error":"no_reaction"}`},
		"dnd.endSnooze":    {"": `{"ok":false,"error":"snooze_not_active"}`},
	})
	ctx, conv, ts := context.Background(), must(t, domain.ParseConversationID, "C0001"), must(t, domain.ParseTS, "1700000100.000100")

	for name, err := range map[string]error{
		"AddReaction":    api.AddReaction(ctx, conv, ts, "+1"),
		"RemoveReaction": api.RemoveReaction(ctx, conv, ts, "+1"),
		"EndDND":         api.EndDND(ctx),
	} {
		if err != nil {
			t.Errorf("%s() = %v, want nil", name, err)
		}
	}
}

func TestWritesReportSlackErrors(t *testing.T) {
	notFound := map[string]string{"": `{"ok":false,"error":"message_not_found"}`}
	revoked := map[string]string{"": `{"ok":false,"error":"token_revoked"}`}
	api, _, _ := fakeSlack(t, map[string]map[string]string{
		"chat.update": notFound, "chat.delete": notFound, "reactions.add": notFound, "reactions.remove": notFound,
		"dnd.setSnooze": revoked, "dnd.endSnooze": revoked,
	})
	ctx, conv, ts := context.Background(), must(t, domain.ParseConversationID, "C0001"), must(t, domain.ParseTS, "1700000100.000100")

	plain := map[string]error{
		"chat.update":      second(api.Edit(ctx, conv, ts, "x")),
		"chat.delete":      api.Delete(ctx, conv, ts),
		"reactions.add":    api.AddReaction(ctx, conv, ts, "+1"),
		"reactions.remove": api.RemoveReaction(ctx, conv, ts, "+1"),
	}
	for method, err := range plain {
		if !HasCode(err, "message_not_found") || errors.Is(err, provider.ErrAuthExpired) || !strings.HasPrefix(err.Error(), method+": ") {
			t.Errorf("%s err = %v, want a plain message_not_found", method, err)
		}
	}
	for method, err := range map[string]error{"dnd.setSnooze": second(api.SnoozeDND(ctx, 5)), "dnd.endSnooze": api.EndDND(ctx)} {
		if !errors.Is(err, provider.ErrAuthExpired) {
			t.Errorf("%s err = %v, want ErrAuthExpired", method, err)
		}
	}
}

func TestSnoozeDNDRejectsANonPositiveLengthWithoutCallingSlack(t *testing.T) {
	api, s, _ := fakeSlack(t, nil)

	for _, minutes := range []int{0, -5} {
		if _, err := api.SnoozeDND(context.Background(), minutes); err == nil {
			t.Errorf("SnoozeDND(%d) = nil, want an error", minutes)
		}
	}
	if len(s.forms) != 0 {
		t.Errorf("%d requests, want none", len(s.forms))
	}
}

// second returns the error of a two-value call.
func second[T any](_ T, err error) error { return err }
