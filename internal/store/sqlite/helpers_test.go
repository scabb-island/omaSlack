package sqlite_test

import (
	"context"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

var zeroTS domain.TS

func conv(t *testing.T, s string) domain.ConversationID {
	t.Helper()
	id, err := domain.ParseConversationID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func user(t *testing.T, s string) domain.UserID {
	t.Helper()
	id, err := domain.ParseUserID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustSetDraft(t *testing.T, s store.Store, c string, thread domain.TS, text string) {
	t.Helper()
	if err := s.SetDraft(context.Background(), conv(t, c), thread, text); err != nil {
		t.Fatalf("SetDraft: %v", err)
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

func author(t *testing.T, s string) domain.Author {
	t.Helper()
	a, err := domain.ParseAuthor(s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
