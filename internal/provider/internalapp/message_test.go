package internalapp

import (
	"context"
	"errors"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

func TestWireMessagesWithABadFieldAreRejected(t *testing.T) {
	good := wireMessage{User: "U0001", Text: "hi", TS: "1700000100.000100"}
	cases := map[string]struct {
		edit    func(*wireMessage)
		wantErr error
	}{
		"missing ts":    {func(w *wireMessage) { w.TS = "" }, domain.ErrInvalidTS},
		"bad ts":        {func(w *wireMessage) { w.TS = "yesterday" }, domain.ErrInvalidTS},
		"no user (bot)": {func(w *wireMessage) { w.User = "" }, domain.ErrInvalidID},
		"bad user":      {func(w *wireMessage) { w.User = "C0001" }, domain.ErrInvalidID},
		"bad thread_ts": {func(w *wireMessage) { w.ThreadTS = "1.2" }, domain.ErrInvalidTS},
		"bad edited.ts": {func(w *wireMessage) { w.Edited.TS = "x" }, domain.ErrInvalidTS},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := good
			tc.edit(&w)
			if _, err := w.parse(); !errors.Is(err, tc.wantErr) {
				t.Fatalf("parse() err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestAnInvalidAPIURLIsAnError(t *testing.T) {
	p := New(mustTeam(t, "T0001"), testCreds(), WithAPIURL("http://bad\x7f/"))
	if _, err := p.History(context.Background(), mustConv(t, "C0001"), domain.TS{}, 1); err == nil {
		t.Fatal("err = nil, want a request error")
	}
}
