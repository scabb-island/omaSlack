package app

import (
	"strings"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/tui/client"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// The user's own Do Not Disturb and presence, and other people's
// presence in DMs (task 5.3).

// ownDaemon is the test daemon signed in as U0, which also answers the
// own-state methods: snoozed for 42 more minutes, and away.
func ownDaemon(t *testing.T, handlers map[string]client.Handler) *client.Fake {
	t.Helper()
	h := map[string]client.Handler{
		"dnd.info":     client.Result(wire.DNDEvent{Team: "T1", Snoozed: true, Until: clock.Add(42 * time.Minute).Unix()}),
		"presence.get": client.Result(map[string]string{"presence": "away"}),
		"presence.set": client.Result(nil),
		"dnd.snooze":   client.Result(nil),
		"dnd.end":      client.Result(nil),
	}
	for k, v := range handlers {
		h[k] = v
	}
	f := daemon(h)
	me := acme()
	me.Self = "U0"
	f.HelloResult = raw(t, hello(me, wire.Workspace{Team: "T2", Name: "Beta", State: "connected"}))
	return f
}

func statusLine(h *harness) string {
	lines := strings.Split(strings.TrimRight(h.screen(), "\n"), "\n")
	return lines[len(lines)-1]
}

func TestTheStatusBarShowsDNDAndAway(t *testing.T) {
	h := newHarness(t, ownDaemon(t, nil), 120, 40)
	h.event("presence.changed", wire.PresenceEvent{Team: "T1", User: "U1", Presence: "active"})
	h.event("presence.changed", wire.PresenceEvent{Team: "T1", User: "U2", Presence: "away"})
	h.golden("own_state_120x40")
	if s := statusLine(h); !strings.Contains(s, "DND 42m") || !strings.Contains(s, "away") {
		t.Fatalf("status bar %q, want DND with the time left and away", s)
	}
}

func TestOwnStateFollowsTheDaemonsEvents(t *testing.T) {
	h := newHarness(t, ownDaemon(t, map[string]client.Handler{
		"dnd.info":     client.Result(wire.DNDEvent{Team: "T1"}),
		"presence.get": client.Result(map[string]string{"presence": "active"}),
	}), 120, 40)
	if s := statusLine(h); strings.Contains(s, "DND") || strings.Contains(s, "away") {
		t.Fatalf("status bar %q, want neither DND nor away", s)
	}

	h.event("dnd.changed", wire.DNDEvent{Team: "T1", Snoozed: true, Until: clock.Add(90 * time.Minute).Unix()})
	h.event("presence.changed", wire.PresenceEvent{Team: "T1", User: "U0", Presence: "away"})
	if s := statusLine(h); !strings.Contains(s, "DND 1h30m") || !strings.Contains(s, "away") {
		t.Fatalf("status bar %q, want DND 1h30m and away", s)
	}
	h.event("dnd.changed", wire.DNDEvent{Team: "T2", Snoozed: true, Until: clock.Add(time.Hour).Unix()}) // another workspace
	h.event("dnd.changed", wire.DNDEvent{Team: "T1"})
	h.event("presence.changed", wire.PresenceEvent{Team: "T1", User: "U0", Presence: "active"})
	if s := statusLine(h); strings.Contains(s, "DND") || strings.Contains(s, "away") {
		t.Fatalf("status bar %q, want neither once ended", s)
	}
}

func TestAnEndedSnoozeIsNotShown(t *testing.T) {
	h := newHarness(t, ownDaemon(t, map[string]client.Handler{
		"dnd.info": client.Result(wire.DNDEvent{Team: "T1", Snoozed: true, Until: clock.Add(-time.Minute).Unix()}),
	}), 120, 40)
	if s := statusLine(h); strings.Contains(s, "DND") {
		t.Fatalf("status bar %q, want no DND: the snooze is over", s)
	}
}

func TestADaemonWithoutOwnStateIsFine(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40) // no dnd.info, no presence.get
	if h.m.status != "" || strings.Contains(statusLine(h), "DND") {
		t.Fatalf("status %q, line %q", h.m.status, statusLine(h))
	}
}

func TestPresenceCommands(t *testing.T) {
	h := newHarness(t, ownDaemon(t, nil), 120, 40)
	for _, tc := range []struct{ line, want string }{{"away", "away"}, {"back", "auto"}} {
		h.keys(":")
		h.typeText(tc.line)
		h.keys("enter")
		if p := h.lastCall("presence.set"); p["team"] != "T1" || p["presence"] != tc.want {
			t.Fatalf(":%s sent %v", tc.line, p)
		}
	}
}

func TestDNDCommands(t *testing.T) {
	cases := map[string]float64{
		"30m": 30, "1h": 60, "2h": 120, "90m": 90,
		"tomorrow": 640, // 22:20 UTC to 09:00 the next day
	}
	for arg, want := range cases {
		h := newHarness(t, ownDaemon(t, nil), 120, 40)
		h.keys(":")
		h.typeText("dnd " + arg)
		h.keys("enter")
		if p := h.lastCall("dnd.snooze"); p["team"] != "T1" || p["minutes"] != want {
			t.Errorf(":dnd %s sent %v, want %v minutes", arg, p, want)
		}
	}
	h := newHarness(t, ownDaemon(t, nil), 120, 40)
	h.keys(":")
	h.typeText("dnd off")
	h.keys("enter")
	if p := h.lastCall("dnd.end"); p["team"] != "T1" {
		t.Fatalf(":dnd off sent %v", p)
	}
}

func TestBadDNDArgumentsSayHow(t *testing.T) {
	for _, arg := range []string{"", "soon", "0m", "-1h", "30s"} {
		h := newHarness(t, ownDaemon(t, nil), 120, 40)
		h.keys(":")
		h.typeText(strings.TrimSpace("dnd " + arg))
		h.keys("enter")
		if !h.m.statusIsError || !strings.Contains(h.m.status, ":dnd 30m|1h|2h|tomorrow|off") || h.count("dnd.snooze") != 0 {
			t.Errorf(":dnd %q: status %q, %d snoozes", arg, h.m.status, h.count("dnd.snooze"))
		}
	}
}

func TestSnoozeMinutesUntilTomorrowUseTheLocalZone(t *testing.T) {
	loc := time.FixedZone("ART", -3*3600)
	now := time.Date(2026, 11, 27, 23, 30, 0, 0, loc)
	if got, err := snoozeMinutes("tomorrow", now); err != nil || got != 570 {
		t.Fatalf("snoozeMinutes(tomorrow) = %d, %v; want 570 (to 09:00 local)", got, err)
	}
}

func TestDNDLeft(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second: "1m", 42 * time.Minute: "42m", 60 * time.Minute: "1h",
		90 * time.Minute: "1h30m", 10*time.Hour + 40*time.Minute: "10h40m",
	} {
		if got := dndLeft(d); got != want {
			t.Errorf("dndLeft(%v) = %q, want %q", d, got, want)
		}
	}
}
