package notify

import (
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

func TestViews(t *testing.T) {
	t1, c1, c2 := team(t, "T01"), conv(t, "C01"), conv(t, "C02")
	root := ts(t, "1700000001.000000")
	top := domain.Message{TS: ts(t, "1700000002.000000")}
	reply := domain.Message{TS: ts(t, "1700000003.000000"), ThreadTS: root}
	other := domain.Message{TS: ts(t, "1700000004.000000"), ThreadTS: ts(t, "1700000000.500000")}

	var v Views
	if v.Shows(t1, c1, top) {
		t.Error("nobody views anything yet")
	}
	v.Set("tui", View{Team: t1, Conv: c1, Focused: true})
	check := func(label string, conv domain.ConversationID, m domain.Message, want bool) {
		t.Helper()
		if got := v.Shows(t1, conv, m); got != want {
			t.Errorf("%s: Shows = %v, want %v", label, got, want)
		}
	}
	check("conversation, top level", c1, top, true)
	check("conversation, a reply", c1, reply, false)
	check("another conversation", c2, top, false)
	if v.Shows(team(t, "T02"), c1, top) {
		t.Error("another workspace")
	}

	v.Set("tui", View{Team: t1, Conv: c1, Thread: root, Focused: true})
	check("thread, its reply", c1, reply, true)
	check("thread, the conversation beside it", c1, top, true)
	check("thread, another thread", c1, other, false)

	v.Set("tui", View{Team: t1, Conv: c1})
	check("not focused", c1, top, false)

	v.Set("window", View{Team: t1, Conv: c2, Focused: true})
	v.Set("tui", View{Team: t1, Conv: c1, Focused: true})
	check("any connection", c2, top, true)
	v.Set("tui", View{})
	check("cleared", c1, top, false)
	v.Drop("window")
	check("dropped", c2, top, false)
	v.Drop("never set")
}
