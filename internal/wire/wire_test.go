package wire

import (
	"encoding/json"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

func must[T any](t *testing.T, parse func(string) (T, error), s string) T {
	t.Helper()
	v, err := parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func check(t *testing.T, name string, v any, want string) {
	t.Helper()
	if got := jsonOf(t, v); got != want {
		t.Errorf("%s:\n got %s\nwant %s", name, got, want)
	}
}

func TestShapes(t *testing.T) {
	ts := func(s string) domain.TS { return must(t, domain.ParseTS, s) }
	user := must(t, domain.ParseUserID, "U0001")
	conv := must(t, domain.ParseConversationID, "C0001")

	root := domain.Message{TS: ts("1700000100.000100"), Author: domain.UserAuthor(user), Text: "hi"}
	check(t, "plain message", FromMessage(root, nil),
		`{"ts":"1700000100.000100","author":"U0001","text":"hi"}`)

	reply := domain.Message{TS: ts("1700000200.000200"), ThreadTS: root.TS, Author: domain.BotAuthor(must(t, domain.ParseBotID, "B0001")),
		Text: "ok", Edited: ts("1700000300.000300")}
	check(t, "reply with reactions", FromMessage(reply, []store.Reaction{{Emoji: "+1", Users: []domain.UserID{user}}}),
		`{"ts":"1700000200.000200","threadTS":"1700000100.000100","author":"B0001","text":"ok","edited":"1700000300.000300","reactions":[{"emoji":"+1","users":["U0001"]}]}`)

	check(t, "conversation", FromConversation(domain.Conversation{ID: conv, Kind: domain.Channel, Name: "general",
		Latest: ts("1700000200.000200"), LastRead: ts("1700000100.000100"), Mentions: 1}),
		`{"id":"C0001","kind":"channel","name":"general","latest":"1700000200.000200","lastRead":"1700000100.000100","mentions":1,"unread":true}`)
	check(t, "unknown read state", FromConversation(domain.Conversation{ID: conv, Kind: domain.Channel}),
		`{"id":"C0001","kind":"channel","mentions":0,"unread":false}`)

	check(t, "workspace", FromWorkspace(must(t, domain.ParseTeamID, "T0001"), "Acme", "internal-app",
		domain.NewCapabilities(domain.Search, domain.Realtime), domain.Connected),
		`{"team":"T0001","name":"Acme","provider":"internal-app","capabilities":["realtime","search"],"state":"connected"}`)
	check(t, "workspace without capabilities", FromWorkspace(must(t, domain.ParseTeamID, "T0001"), "Acme", "desktop-session",
		domain.NewCapabilities(), domain.Connecting),
		`{"team":"T0001","name":"Acme","provider":"desktop-session","capabilities":[],"state":"connecting"}`)

	check(t, "user", FromUser(domain.User{ID: user, Name: "ana", RealName: "Ana Pérez"}),
		`{"id":"U0001","name":"ana","realName":"Ana Pérez"}`) // non-ASCII on purpose
	check(t, "search hit", FromSearchHit(domain.SearchHit{Conversation: conv, Message: root}),
		`{"conv":"C0001","message":{"ts":"1700000100.000100","author":"U0001","text":"hi"}}`)
}

func TestSummarize(t *testing.T) {
	ts := func(s string) domain.TS { return must(t, domain.ParseTS, s) }
	id := func(s string) domain.ConversationID { return must(t, domain.ParseConversationID, s) }
	old, newer := ts("1700000100.000100"), ts("1700000200.000200")
	unread := func(c string, k domain.ConversationKind, mentions int) domain.Conversation {
		return domain.Conversation{ID: id(c), Kind: k, Latest: newer, LastRead: old, Mentions: mentions}
	}
	read := domain.Conversation{ID: id("C0009"), Kind: domain.Channel, Latest: old, LastRead: old}

	got := Summarize(map[domain.TeamID][]domain.Conversation{
		must(t, domain.ParseTeamID, "T0001"): {
			unread("D0001", domain.DirectMessage, 1),
			unread("G0001", domain.GroupMessage, 0),
			unread("C0001", domain.Channel, 2),
			unread("C0002", domain.PrivateChannel, 0),
			read,
		},
		must(t, domain.ParseTeamID, "T0002"): {unread("C0003", domain.Channel, 0)},
		must(t, domain.ParseTeamID, "T0003"): nil,
	})
	check(t, "summary", got,
		`{"workspaces":{"T0001":{"dms":2,"mentions":3,"channels":2,"threads":0},"T0002":{"dms":0,"mentions":0,"channels":1,"threads":0},"T0003":{"dms":0,"mentions":0,"channels":0,"threads":0}},"total":{"dms":2,"mentions":3,"channels":3,"threads":0}}`)

	check(t, "no workspaces", Summarize(nil), `{"workspaces":{},"total":{"dms":0,"mentions":0,"channels":0,"threads":0}}`)
}

func TestFromEventCoversEveryEvent(t *testing.T) {
	ts := func(s string) domain.TS { return must(t, domain.ParseTS, s) }
	team := must(t, domain.ParseTeamID, "T0001")
	conv := must(t, domain.ParseConversationID, "C0001")
	user := must(t, domain.ParseUserID, "U0001")
	msg := domain.Message{TS: ts("1700000100.000100"), Author: domain.UserAuthor(user), Text: "hi"}
	m := `{"ts":"1700000100.000100","author":"U0001","text":"hi"}`

	for _, tc := range []struct {
		ev   domain.Event
		want string
	}{
		{domain.MessageCreated{Team: team, Conversation: conv, Message: msg}, `{"team":"T0001","conv":"C0001","message":` + m + `}`},
		{domain.MessageUpdated{Team: team, Conversation: conv, Message: msg}, `{"team":"T0001","conv":"C0001","message":` + m + `}`},
		{domain.MessageDeleted{Team: team, Conversation: conv, TS: msg.TS}, `{"team":"T0001","conv":"C0001","ts":"1700000100.000100"}`},
		{domain.ReactionChanged{Team: team, Conversation: conv, TS: msg.TS, User: user, Emoji: "smile", Added: true},
			`{"team":"T0001","conv":"C0001","ts":"1700000100.000100","user":"U0001","emoji":"smile","added":true}`},
		{domain.ReadChanged{Team: team, Conversation: conv, LastRead: msg.TS}, `{"team":"T0001","conv":"C0001","lastRead":"1700000100.000100"}`},
		{domain.PresenceChanged{Team: team, User: user, Presence: domain.Active}, `{"team":"T0001","user":"U0001","presence":"active"}`},
		{domain.PresenceChanged{Team: team, User: user, Presence: domain.Away}, `{"team":"T0001","user":"U0001","presence":"away"}`},
		{domain.UserTyping{Team: team, Conversation: conv, User: user}, `{"team":"T0001","conv":"C0001","user":"U0001"}`},
		{domain.ConnectionChanged{Team: team, State: domain.RateLimited}, `{"team":"T0001","state":"rate_limited"}`},
		{domain.ConversationUpdated{Team: team, Conversation: domain.Conversation{ID: conv, Kind: domain.DirectMessage}},
			`{"team":"T0001","conversation":{"id":"C0001","kind":"im","mentions":0,"unread":false}}`},
		{domain.DraftChanged{Team: team, Conversation: conv, Thread: msg.TS, Text: "wip"},
			`{"team":"T0001","conv":"C0001","threadTS":"1700000100.000100","text":"wip"}`},
		{domain.DraftChanged{Team: team, Conversation: conv}, `{"team":"T0001","conv":"C0001","text":""}`},
	} {
		check(t, tc.ev.Topic(), FromEvent(tc.ev), tc.want)
	}
}
