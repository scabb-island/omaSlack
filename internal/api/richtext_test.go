package api

import (
	"context"
	"encoding/json"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

const refs = "<@U0BOB> <#C0RANDOM>"

// Message events render with what reads learned: nobody before the first
// read, the people and channels it named after.
func TestEventsRenderWithTheNamesReadsLearned(t *testing.T) {
	f := newFixture()
	acme := f.dir[0]
	before := `<a href="omaslack://user/T0ACME/U0BOB">@U0BOB</a> <a href="omaslack://conv/T0ACME/C0RANDOM">#C0RANDOM</a>`
	if got := acme.HTML(refs); got != before {
		t.Fatalf("before any read: %q", got)
	}
	params := json.RawMessage(`{"team":"T0ACME","conv":"C0GENERAL","threadTS":"1700000200.000000"}`)
	if _, err := Methods(f.dir, f.focus).Handle(context.Background(), "messages.replies", params); err != nil {
		t.Fatal(err)
	}
	after := `<a href="omaslack://user/T0ACME/U0BOB">@Bob Stone</a> <a href="omaslack://conv/T0ACME/C0RANDOM">#random</a>`
	if got := acme.HTML(refs); got != after {
		t.Errorf("after a read: %q", got)
	}
}

// Rich text never fails a read: names that cannot be learned stay IDs.
func TestNamesThatCannotBeLearnedStayIDs(t *testing.T) {
	s := acmeStore()
	s.listErr = errDisk
	w := Workspace{Team: team("T0X"), Store: s, Engine: &fakeEngine{store: s, err: errExpired}, Names: NewNames()}
	want := `<a href="omaslack://user/T0X/U0BOB">@U0BOB</a> <a href="omaslack://conv/T0X/C0RANDOM">#C0RANDOM</a>`
	if got := w.named(context.Background(), domain.Message{Text: refs})(refs); got != want {
		t.Errorf("got %q", got)
	}
}

// Without Names (a workspace built by hand) nothing is asked.
func TestWithoutNamesNothingIsAsked(t *testing.T) {
	e := &fakeEngine{}
	w := Workspace{Team: team("T0X"), Engine: e}
	if got := w.named(context.Background(), domain.Message{Text: "<@U0BOB>"})("*hi*"); got != "<b>hi</b>" {
		t.Errorf("got %q", got)
	}
	if calls := e.recorded(); len(calls) != 0 {
		t.Errorf("engine asked %v", calls)
	}
}
