package api

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/store"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// go test ./internal/api -update rewrites each golden file's expected
// output (calls, result, error) from what the methods answer now. The
// names and params are written by hand and kept.
var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

// fixture is a fresh daemon state for one call: three workspaces, each
// with its own store and engine.
//
//   - T0ACME (desktop-session) can do everything and has history,
//     a thread, reactions and drafts in C0GENERAL.
//   - T0BETA (internal-app) lacks search and presenceWrite.
//   - T0EXPIRED's credentials were rejected: every engine action fails
//     with provider.ErrAuthExpired.
type fixture struct {
	dir     fakeDirectory
	engines map[string]*fakeEngine
	focus   *fakeFocus
}

func newFixture() *fixture {
	f := &fixture{engines: map[string]*fakeEngine{}, focus: &fakeFocus{}}
	all := domain.NewCapabilities(domain.AllCapabilities()...)
	f.add("T0ACME", "Acme", "desktop-session", all, acmeStore(), acmeOlder(), nil)
	f.add("T0BETA", "Beta", "internal-app", domain.NewCapabilities(domain.Realtime, domain.Files), betaStore(), nil, nil)
	f.add("T0EXPIRED", "Expired", "desktop-session", all, newFakeStore(), nil, errExpired)
	return f
}

func (f *fixture) add(id, name, prov string, caps domain.Capabilities, s *fakeStore, older map[domain.ConversationID][]domain.Message, err error) {
	e := &fakeEngine{store: s, older: older, err: err}
	f.engines[id] = e
	f.dir = append(f.dir, Workspace{Team: team(id), Name: name, Provider: prov, Capabilities: caps, Store: s, Engine: e})
}

func acmeStore() *fakeStore {
	s := newFakeStore()
	s.convs = []domain.Conversation{
		{ID: conv("C0GENERAL"), Kind: domain.Channel, Name: "general", Latest: ts("1700000300.000000"), LastRead: ts("1700000100.000000"), Mentions: 1},
		{ID: conv("C0RANDOM"), Kind: domain.Channel, Name: "random", Latest: ts("1700000050.000000"), LastRead: ts("1700000050.000000")},
		{ID: conv("D0ANA"), Kind: domain.DirectMessage, Latest: ts("1700000400.000000"), LastRead: ts("1700000390.000000")},
		{ID: conv("G0TEAM"), Kind: domain.GroupMessage, Latest: ts("1700000500.000000"), LastRead: ts("1700000500.000000")},
	}
	general := conv("C0GENERAL")
	root := ts("1700000200.000000")
	s.add(general,
		domain.Message{TS: ts("1700000100.000000"), Author: author("U0ANA"), Text: "hello"},
		domain.Message{TS: root, ThreadTS: root, Author: author("U0BOB"), Text: "thread root"},
		domain.Message{TS: ts("1700000210.000000"), ThreadTS: root, Author: author("U0ANA"), Text: "first reply"},
		domain.Message{TS: ts("1700000220.000000"), ThreadTS: root, Author: author("U0BOB"), Text: "second reply"},
		domain.Message{TS: ts("1700000300.000000"), Author: author("U0ANA"), Text: "latest", Edited: ts("1700000310.000000")},
	)
	s.reactions[key(general, ts("1700000300.000000"))] = []store.Reaction{{Emoji: "thumbsup", Users: []domain.UserID{user("U0ANA"), user("U0BOB")}}}
	s.reactions[key(general, ts("1700000210.000000"))] = []store.Reaction{{Emoji: "eyes", Users: []domain.UserID{user("U0BOB")}}}
	s.drafts[key(general, domain.TS{})] = "half a thought"
	s.drafts[key(general, root)] = "a reply in progress"
	return s
}

// acmeOlder is the page Slack still has before C0GENERAL's cache.
func acmeOlder() map[domain.ConversationID][]domain.Message {
	return map[domain.ConversationID][]domain.Message{conv("C0GENERAL"): {
		{TS: ts("1700000010.000000"), Author: author("U0BOB"), Text: "older one"},
		{TS: ts("1700000020.000000"), Author: author("U0ANA"), Text: "older two"},
	}}
}

func betaStore() *fakeStore {
	s := newFakeStore()
	s.convs = []domain.Conversation{
		{ID: conv("C0ALERTS"), Kind: domain.PrivateChannel, Name: "alerts", Latest: ts("1700000600.000000"), Mentions: 3},
	}
	return s
}

// goldenCase is one call: name and params are written by hand; calls
// (what the engines were asked to do), result and error are the output.
type goldenCase struct {
	Name   string            `json:"name"`
	Params json.RawMessage   `json:"params,omitempty"`
	Calls  []string          `json:"calls,omitempty"`
	Result json.RawMessage   `json:"result,omitempty"`
	Error  *rpc.Error        `json:"error,omitempty"`
	Focus  []wire.FocusEvent `json:"focus,omitempty"`
}

func (f *fixture) run(t *testing.T, method string, c goldenCase) goldenCase {
	t.Helper()
	out := goldenCase{Name: c.Name, Params: c.Params}
	result, err := Methods(f.dir, f.focus).Handle(context.Background(), method, c.Params)
	if err != nil {
		out.Error = rpc.FromError(err)
	} else if out.Result, err = json.Marshal(result); err != nil {
		t.Fatalf("%s: result does not marshal: %v", c.Name, err)
	}
	for _, d := range f.dir {
		for _, call := range f.engines[d.Team.String()].recorded() {
			out.Calls = append(out.Calls, d.Team.String()+" "+call)
		}
	}
	out.Focus = f.focus.sent
	return out
}

func goldenPath(method string) string { return filepath.Join("testdata", method+".json") }

func TestMethodsMatchTheirGoldenFiles(t *testing.T) {
	for _, method := range methodNames() {
		t.Run(method, func(t *testing.T) {
			path := goldenPath(method)
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var cases []goldenCase
			if err := json.Unmarshal(want, &cases); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			got := make([]goldenCase, len(cases))
			for i, c := range cases {
				got[i] = newFixture().run(t, method, c)
			}
			compareGolden(t, path, want, got)
		})
	}
}

func compareGolden(t *testing.T, path string, want []byte, got []goldenCase) {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(got); err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("%s does not match; got:\n%s\n(run go test ./internal/api -update and review the diff)", path, buf.Bytes())
	}
}

// methodNames is every method the package doc comment of Methods lists.
func methodNames() []string {
	return []string{
		"workspaces.list",
		"conversations.list", "conversations.open", "conversations.close", "conversations.markRead",
		"messages.history", "messages.replies", "messages.send", "messages.edit", "messages.delete",
		"reactions.add", "reactions.remove",
		"drafts.get", "drafts.set",
		"search.messages", "users.search",
		"presence.set", "dnd.snooze", "dnd.end",
		"unread.summary", "ui.focus",
	}
}

func TestMethodsAnswersExactlyTheDocumentedMethods(t *testing.T) {
	var got []string
	for name := range Methods(fakeDirectory{}, &fakeFocus{}) {
		got = append(got, name)
	}
	slices.Sort(got)
	want := slices.Sorted(slices.Values(methodNames()))
	if !slices.Equal(got, want) {
		t.Fatalf("methods = %v\nwant %v", got, want)
	}
}

func TestEveryGoldenFileIsAMethod(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		if !slices.Contains(methodNames(), name) {
			t.Errorf("%s has no method", f)
		}
	}
}
