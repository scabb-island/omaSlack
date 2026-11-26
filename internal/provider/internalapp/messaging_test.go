package internalapp

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

// pagedReplies serves page1 first and page2 when asked for page1's cursor.
func pagedReplies(t *testing.T, page2 http.HandlerFunc) http.HandlerFunc {
	page1 := okJSON(readFixture(t, "conversations.replies.page1.json"))
	return func(w http.ResponseWriter, r *http.Request) {
		if r.PostForm.Get("cursor") == "bmV4dF90czoxNzAwMDAwMzAw" {
			page2(w, r)
			return
		}
		page1(w, r)
	}
}

func TestRepliesFollowsEveryPageAndLeavesOutTheRoot(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"conversations.replies": pagedReplies(t, okJSON(readFixture(t, "conversations.replies.page2.json"))),
	})
	p, _ := newTestProvider(t, f)
	root := mustTS(t, "1700000100.000100")

	got, err := p.Replies(context.Background(), mustConv(t, "C0001"), root)
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.Message{
		{TS: mustTS(t, "1700000200.000200"), ThreadTS: root, Author: mustAuthor(t, "U0002"), Text: "first reply"},
		{TS: mustTS(t, "1700000300.000300"), ThreadTS: root, Author: mustAuthor(t, "U0002"), Text: "second reply"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Replies() =\n%+v\nwant\n%+v", got, want)
	}
	calls := f.recorded()
	if len(calls) != 2 {
		t.Fatalf("%d calls, want 2", len(calls))
	}
	if calls[0].Form.Get("channel") != "C0001" || calls[0].Form.Get("ts") != "1700000100.000100" || calls[0].Form.Get("limit") != "200" || calls[0].Form.Has("cursor") {
		t.Errorf("first call form = %v", calls[0].Form)
	}
}

func TestRepliesFailsWhenAnyPageFails(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"conversations.replies": pagedReplies(t, okJSON([]byte(`{"ok":false,"error":"token_revoked"}`))),
	})
	p, _ := newTestProvider(t, f)

	_, err := p.Replies(context.Background(), mustConv(t, "C0001"), mustTS(t, "1700000100.000100"))
	if !errors.Is(err, provider.ErrAuthExpired) {
		t.Fatalf("err = %v, want ErrAuthExpired", err)
	}
}

func TestPostReturnsTheMessageAsSlackStoredIt(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"chat.postMessage": okJSON(readFixture(t, "chat.postMessage.json")),
	})
	p, _ := newTestProvider(t, f)
	thread := mustTS(t, "1700000100.000100")

	got, err := p.Post(context.Background(), mustConv(t, "C0001"), thread, "hi there")
	if err != nil {
		t.Fatal(err)
	}

	want := domain.Message{TS: mustTS(t, "1700000500.000500"), ThreadTS: thread, Author: mustAuthor(t, "U0001"), Text: "hi there"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Post() = %+v, want %+v", got, want)
	}
	form := f.recorded()[0].Form
	if form.Get("channel") != "C0001" || form.Get("text") != "hi there" || form.Get("thread_ts") != "1700000100.000100" {
		t.Errorf("form = %v", form)
	}
}

func TestPostOutsideAThreadSendsNoThreadTS(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"chat.postMessage": okJSON(readFixture(t, "chat.postMessage.json")),
	})
	p, _ := newTestProvider(t, f)

	if _, err := p.Post(context.Background(), mustConv(t, "C0001"), domain.TS{}, "hi there"); err != nil {
		t.Fatal(err)
	}
	if form := f.recorded()[0].Form; form.Has("thread_ts") {
		t.Errorf("form = %v, want no thread_ts", form)
	}
}

func TestPostFailsOnSlackErrorsAndMalformedMessages(t *testing.T) {
	for name, body := range map[string]string{
		"slack error":       `{"ok":false,"error":"not_in_channel"}`,
		"malformed message": `{"ok":true,"ts":"1700000500.000500","message":{"text":"hi"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeSlack(t, map[string]http.HandlerFunc{"chat.postMessage": okJSON([]byte(body))})
			p, _ := newTestProvider(t, f)

			if _, err := p.Post(context.Background(), mustConv(t, "C0001"), domain.TS{}, "hi"); err == nil {
				t.Fatal("err = nil, want an error")
			}
		})
	}
}

func TestMarkReadMovesTheMarker(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"conversations.mark": okJSON([]byte(`{"ok":true}`)),
	})
	p, _ := newTestProvider(t, f)

	if err := p.MarkRead(context.Background(), mustConv(t, "D0001"), mustTS(t, "1700000300.000300")); err != nil {
		t.Fatal(err)
	}
	form := f.recorded()[0].Form
	if form.Get("channel") != "D0001" || form.Get("ts") != "1700000300.000300" {
		t.Errorf("form = %v", form)
	}
}

func TestMarkReadReportsSlackErrors(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"conversations.mark": okJSON([]byte(`{"ok":false,"error":"invalid_auth"}`)),
	})
	p, _ := newTestProvider(t, f)

	err := p.MarkRead(context.Background(), mustConv(t, "D0001"), mustTS(t, "1700000300.000300"))
	if !errors.Is(err, provider.ErrAuthExpired) {
		t.Fatalf("err = %v, want ErrAuthExpired", err)
	}
}
