package slackwire

import (
	"context"
	"errors"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

func TestDNDIsTheSnoozeDNDInfoReports(t *testing.T) {
	cases := map[string]struct {
		answer string
		want   time.Time
	}{
		"snoozed": {`{"ok":true,"dnd_enabled":true,"next_dnd_start_ts":1700050000,"next_dnd_end_ts":1700080000,
			"snooze_enabled":true,"snooze_endtime":1700003600,"snooze_remaining":3600}`, time.Unix(1700003600, 0)},
		// A schedule alone (dnd_enabled) is not a snooze.
		"off":                 {`{"ok":true,"dnd_enabled":true,"next_dnd_start_ts":1700050000,"next_dnd_end_ts":1700080000,"snooze_enabled":false}`, time.Time{}},
		"enabled with no end": {`{"ok":true,"snooze_enabled":true}`, time.Time{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			api, s, _ := fakeSlack(t, map[string]map[string]string{"dnd.info": {"": tc.answer}})
			got, err := api.DND(context.Background())
			if err != nil || !got.Equal(tc.want) {
				t.Fatalf("DND() = %v, %v; want %v", got, err, tc.want)
			}
			if f := s.form(0); f.Has("user") {
				t.Errorf("form = %v, want no user: the signed-in user's own DND", f)
			}
		})
	}
}

func TestSnoozeDNDReturnsTheEndSlackSet(t *testing.T) {
	api, _, _ := fakeSlack(t, map[string]map[string]string{"dnd.setSnooze": {"": `{"ok":true,"snooze_enabled":true,"snooze_endtime":1700001800,"snooze_remaining":1800}`}})

	got, err := api.SnoozeDND(context.Background(), 30)
	if err != nil || !got.Equal(time.Unix(1700001800, 0)) {
		t.Fatalf("SnoozeDND() = %v, %v; want Slack's snooze_endtime", got, err)
	}
}

func TestSnoozeDNDWithoutAnEndCountsFromNow(t *testing.T) {
	api, _, _ := fakeSlack(t, map[string]map[string]string{"dnd.setSnooze": {"": `{"ok":true}`}})
	api.now = func() time.Time { return time.Unix(1700000000, 0) }

	got, err := api.SnoozeDND(context.Background(), 30)
	if err != nil || !got.Equal(time.Unix(1700001800, 0)) {
		t.Fatalf("SnoozeDND() = %v, %v; want now + 30 minutes", got, err)
	}
}

func TestDNDReportsSlackErrors(t *testing.T) {
	api, _, _ := fakeSlack(t, map[string]map[string]string{"dnd.info": {"": `{"ok":false,"error":"token_revoked"}`}})

	if _, err := api.DND(context.Background()); !errors.Is(err, provider.ErrAuthExpired) {
		t.Errorf("DND() err = %v, want ErrAuthExpired", err)
	}
}

func TestPresenceIsTheSignedInUsersOwn(t *testing.T) {
	for answer, want := range map[string]domain.Presence{
		`{"ok":true,"presence":"active","online":true,"auto_away":false,"manual_away":false}`: domain.Active,
		`{"ok":true,"presence":"away","online":true,"manual_away":true}`:                      domain.Away,
	} {
		api, s, _ := fakeSlack(t, map[string]map[string]string{"users.getPresence": {"": answer}})
		got, err := api.Presence(context.Background())
		if err != nil || got != want {
			t.Errorf("Presence() = %v, %v; want %v", got, err, want)
		}
		if f := s.form(0); f.Has("user") {
			t.Errorf("form = %v, want no user", f)
		}
	}
}

func TestPresenceRejectsAnUnknownValue(t *testing.T) {
	api, _, _ := fakeSlack(t, map[string]map[string]string{"users.getPresence": {"": `{"ok":true,"presence":"busy"}`}})

	if _, err := api.Presence(context.Background()); err == nil {
		t.Error("Presence() err = nil, want an error for an unknown presence")
	}
}

func TestParsePresence(t *testing.T) {
	for s, want := range map[string]domain.Presence{"active": domain.Active, "away": domain.Away} {
		if got, err := ParsePresence(s); err != nil || got != want {
			t.Errorf("ParsePresence(%q) = %v, %v", s, got, err)
		}
	}
	if _, err := ParsePresence(""); err == nil {
		t.Error(`ParsePresence("") err = nil`)
	}
}
