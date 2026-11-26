package sqlite_test

import (
	"context"
	"testing"
)

func TestNoDraftIsEmpty(t *testing.T) {
	got, err := openStore(t).Draft(context.Background(), conv(t, "C01"), zeroTS)
	if err != nil || got != "" {
		t.Errorf("Draft = %q, %v; want empty", got, err)
	}
}

func TestDraftsArePerConversationAndThread(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	thread := ts(t, "1700000000.000100")
	mustSetDraft(t, s, "C01", zeroTS, "channel draft")
	mustSetDraft(t, s, "C01", thread, "thread draft")
	mustSetDraft(t, s, "D02", zeroTS, "dm draft")

	for _, c := range []struct {
		conv string
		want string
		root bool
	}{
		{"C01", "channel draft", false},
		{"C01", "thread draft", true},
		{"D02", "dm draft", false},
	} {
		th := zeroTS
		if c.root {
			th = thread
		}
		got, err := s.Draft(ctx, conv(t, c.conv), th)
		if err != nil || got != c.want {
			t.Errorf("Draft(%s, thread=%v) = %q, %v; want %q", c.conv, c.root, got, err, c.want)
		}
	}
}

func TestSetDraftOverwrites(t *testing.T) {
	s := openStore(t)
	mustSetDraft(t, s, "C01", zeroTS, "first")
	mustSetDraft(t, s, "C01", zeroTS, "second")
	got, _ := s.Draft(context.Background(), conv(t, "C01"), zeroTS)
	if got != "second" {
		t.Errorf("Draft = %q, want second", got)
	}
}

func TestEmptyDraftDeletesIt(t *testing.T) {
	s := openStore(t)
	thread := ts(t, "1700000000.000100")
	mustSetDraft(t, s, "C01", thread, "text")
	mustSetDraft(t, s, "C01", zeroTS, "keep me")
	mustSetDraft(t, s, "C01", thread, "")
	mustSetDraft(t, s, "C01", thread, "") // deleting twice is fine

	ctx := context.Background()
	if got, _ := s.Draft(ctx, conv(t, "C01"), thread); got != "" {
		t.Errorf("thread draft = %q after delete, want empty", got)
	}
	if got, _ := s.Draft(ctx, conv(t, "C01"), zeroTS); got != "keep me" {
		t.Errorf("channel draft = %q, want it untouched", got)
	}
}
