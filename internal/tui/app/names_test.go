package app

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/tui/client"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

func TestUsersInfoIsChunkedAndRetried(t *testing.T) {
	var convs []wire.Conversation
	for i := range 150 {
		convs = append(convs, wire.Conversation{ID: fmt.Sprintf("D%03d", i), Kind: "im", User: fmt.Sprintf("U%03d", i)})
	}
	h := newHarness(t, daemon(map[string]client.Handler{
		"conversations.list": client.Result(convs),
		"users.info":         client.Result([]wire.User{}), // the daemon skipped them this time
	}), 120, 40)
	var sizes []int
	for _, c := range h.f.Calls() {
		if c.Method == "users.info" {
			var p struct{ IDs []string }
			if err := json.Unmarshal(c.Params, &p); err != nil {
				t.Fatal(err)
			}
			sizes = append(sizes, len(p.IDs))
		}
	}
	if len(sizes) != 2 || sizes[0]+sizes[1] != 150 || max(sizes[0], sizes[1]) != usersPerCall {
		t.Fatalf("users.info batches %v", sizes)
	}
	h.event("typing", wire.TypingEvent{Team: "T1", Conv: "D000", User: "U000"})
	if n := h.count("users.info"); n != 2 {
		t.Fatalf("asked again at once: %d calls", n)
	}
	later := clock.Add(usersRetry)
	h.m.now = func() time.Time { return later }
	h.event("typing", wire.TypingEvent{Team: "T1", Conv: "D000", User: "U000"})
	if n := h.count("users.info"); n != 4 {
		t.Fatalf("not asked again after %v: %d calls", usersRetry, n)
	}
}

func TestTypingAndNameFailuresAreQuiet(t *testing.T) {
	h := newHarness(t, daemon(map[string]client.Handler{
		"typing.send": failing("socket reconnecting"),
		"users.info":  failing("slow"),
	}), 120, 40)
	h.keys("j", "enter", "i", "x")
	if h.m.status != "" || h.count("typing.send") != 1 {
		t.Fatalf("status %q, typing.send %d", h.m.status, h.count("typing.send"))
	}
}
