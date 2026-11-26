package providertest_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
)

var zeroTS domain.TS

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

var (
	team = must(domain.ParseTeamID("T01"))
	conv = must(domain.ParseConversationID("C01"))
	ts1  = must(domain.ParseTS("1700000001.000000"))
	ts2  = must(domain.ParseTS("1700000002.000000"))
	ts3  = must(domain.ParseTS("1700000003.000000"))
)

func msg(at domain.TS) domain.Message { return domain.Message{TS: at, Text: at.String()} }

func created(at domain.TS) domain.Event {
	return domain.MessageCreated{Team: team, Conversation: conv, Message: msg(at)}
}

// drain reads stream until it closes, failing after a real-time limit so a
// bug cannot hang the suite.
func drain(t *testing.T, stream <-chan domain.Event) []domain.Event {
	t.Helper()
	var got []domain.Event
	for {
		select {
		case ev, ok := <-stream:
			if !ok {
				return got
			}
			got = append(got, ev)
		case <-time.After(5 * time.Second):
			t.Fatal("stream did not close")
		}
	}
}

func TestIdentity(t *testing.T) {
	caps := domain.NewCapabilities(domain.Realtime)
	f := providertest.New(team, caps)
	if f.Team() != team || f.Capabilities() != caps {
		t.Fatalf("Team/Capabilities = %v/%v", f.Team(), f.Capabilities())
	}
}

func TestSessionsReplayInOrderAndCloseTheStream(t *testing.T) {
	f := providertest.New(team, domain.Capabilities{})
	f.Script(
		providertest.Session{Events: []domain.Event{created(ts1), created(ts2)}},
		providertest.Session{Err: provider.ErrAuthExpired},
	)
	ctx := context.Background()

	stream, err := f.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := drain(t, stream); !slices.Equal(got, []domain.Event{created(ts1), created(ts2)}) {
		t.Fatalf("first session = %v", got)
	}
	if _, err := f.Connect(ctx); !errors.Is(err, provider.ErrAuthExpired) {
		t.Fatalf("second Connect err = %v, want ErrAuthExpired", err)
	}
	want := []providertest.Call{{Method: providertest.MethodConnect}, {Method: providertest.MethodConnect}}
	if got := f.Calls(); !slices.Equal(got, want) {
		t.Fatalf("Calls = %v", got)
	}
}

func TestLiveEventsFollowTheScriptedOnesUntilTheFeedCloses(t *testing.T) {
	live := make(chan domain.Event, 1)
	f := providertest.New(team, domain.Capabilities{})
	f.Script(providertest.Session{Events: []domain.Event{created(ts1)}, Live: live})

	stream, err := f.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	live <- created(ts2)
	close(live)
	if got := drain(t, stream); !slices.Equal(got, []domain.Event{created(ts1), created(ts2)}) {
		t.Fatalf("stream = %v", got)
	}
}

func TestWithoutAScriptTheStreamStaysOpenUntilCtxEnds(t *testing.T) {
	f := providertest.New(team, domain.Capabilities{})
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := f.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-stream:
		t.Fatalf("idle stream delivered %v", ev)
	default:
	}
	cancel()
	if got := drain(t, stream); len(got) != 0 {
		t.Fatalf("idle stream delivered %v", got)
	}
}

func TestAnEndedCtxStopsDelivery(t *testing.T) {
	f := providertest.New(team, domain.Capabilities{})
	f.Script(providertest.Session{Events: []domain.Event{created(ts1), created(ts2)}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stream, err := f.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := drain(t, stream); len(got) != 0 {
		t.Fatalf("delivered %v after ctx ended", got)
	}
}

func TestHistoryPagesNewestFirstBeforeACursor(t *testing.T) {
	f := providertest.New(team, domain.Capabilities{})
	f.SetHistory(conv, msg(ts2), msg(ts1), msg(ts3))
	ctx := context.Background()

	page, err := f.History(ctx, conv, zeroTS, 2)
	if err != nil || !slices.Equal(page, []domain.Message{msg(ts3), msg(ts2)}) {
		t.Fatalf("newest page = %v, %v", page, err)
	}
	page, err = f.History(ctx, conv, ts2, 2)
	if err != nil || !slices.Equal(page, []domain.Message{msg(ts1)}) {
		t.Fatalf("older page = %v, %v", page, err)
	}
	want := []providertest.Call{
		{Method: providertest.MethodHistory, Conv: conv},
		{Method: providertest.MethodHistory, Conv: conv, TS: ts2},
	}
	if got := f.Calls(); !slices.Equal(got, want) {
		t.Fatalf("Calls = %v", got)
	}
}

func TestRepliesReturnTheScriptedThread(t *testing.T) {
	f := providertest.New(team, domain.Capabilities{})
	reply := domain.Message{TS: ts2, ThreadTS: ts1}
	f.SetReplies(conv, ts1, reply)

	got, err := f.Replies(context.Background(), conv, ts1)
	if err != nil || !slices.Equal(got, []domain.Message{reply}) {
		t.Fatalf("Replies = %v, %v", got, err)
	}
	if calls := f.Calls(); !slices.Equal(calls, []providertest.Call{{Method: providertest.MethodReplies, Conv: conv, TS: ts1}}) {
		t.Fatalf("Calls = %v", calls)
	}
}

func TestScriptedFailuresComeFirstThenTheCallSucceeds(t *testing.T) {
	f := providertest.New(team, domain.Capabilities{})
	f.SetHistory(conv, msg(ts1))
	limited := &provider.RateLimitedError{Method: providertest.MethodHistory, RetryAfter: 30 * time.Second}
	f.Fail(providertest.MethodHistory, limited, provider.ErrAuthExpired)
	ctx := context.Background()

	if _, err := f.History(ctx, conv, zeroTS, 10); !errors.Is(err, limited) {
		t.Fatalf("first err = %v", err)
	}
	if _, err := f.History(ctx, conv, zeroTS, 10); !errors.Is(err, provider.ErrAuthExpired) {
		t.Fatalf("second err = %v", err)
	}
	if page, err := f.History(ctx, conv, zeroTS, 10); err != nil || len(page) != 1 {
		t.Fatalf("third = %v, %v", page, err)
	}
	if n := len(f.Calls()); n != 3 {
		t.Fatalf("recorded %d calls, want 3", n)
	}
}

func TestFailuresArePerMethod(t *testing.T) {
	f := providertest.New(team, domain.Capabilities{})
	boom := errors.New("boom")
	f.Fail(providertest.MethodReplies, boom)
	ctx := context.Background()

	if _, err := f.History(ctx, conv, zeroTS, 10); err != nil {
		t.Fatalf("History err = %v", err)
	}
	if _, err := f.Replies(ctx, conv, ts1); !errors.Is(err, boom) {
		t.Fatalf("Replies err = %v", err)
	}
}

func TestPostAndMarkReadAreRecorded(t *testing.T) {
	f := providertest.New(team, domain.Capabilities{})
	ctx := context.Background()

	m, err := f.Post(ctx, conv, ts1, "hi")
	if err != nil || m.Text != "hi" || m.ThreadTS != ts1 {
		t.Fatalf("Post = %+v, %v", m, err)
	}
	boom := errors.New("boom")
	f.Fail(providertest.MethodMarkRead, boom)
	if err := f.MarkRead(ctx, conv, ts2); !errors.Is(err, boom) {
		t.Fatalf("MarkRead err = %v", err)
	}
	f.Fail(providertest.MethodPost, boom)
	if _, err := f.Post(ctx, conv, zeroTS, "again"); !errors.Is(err, boom) {
		t.Fatalf("Post err = %v", err)
	}
	want := []providertest.Call{
		{Method: providertest.MethodPost, Conv: conv, TS: ts1},
		{Method: providertest.MethodMarkRead, Conv: conv, TS: ts2},
		{Method: providertest.MethodPost, Conv: conv},
	}
	if got := f.Calls(); !slices.Equal(got, want) {
		t.Fatalf("Calls = %v", got)
	}
}

func TestReadMarkersServeTheScriptedMarker(t *testing.T) {
	f := providertest.New(team, domain.Capabilities{})
	r := f.WithReadMarkers()
	f.SetLastRead(conv, ts2)
	ctx := context.Background()

	if got, err := r.LastRead(ctx, conv); err != nil || got != ts2 {
		t.Fatalf("LastRead = %v, %v", got, err)
	}
	boom := errors.New("boom")
	f.Fail(providertest.MethodReadMarker, boom)
	if _, err := r.LastRead(ctx, conv); !errors.Is(err, boom) {
		t.Fatalf("LastRead err = %v", err)
	}
	if got := f.Calls(); len(got) != 2 || got[0] != (providertest.Call{Method: providertest.MethodReadMarker, Conv: conv}) {
		t.Fatalf("Calls = %v", got)
	}
	var _ provider.Provider = r // still a full provider
}
