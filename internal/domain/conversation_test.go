package domain

import "testing"

func TestConversationUnread(t *testing.T) {
	older, _ := ParseTS("1700000001.000100")
	newer, _ := ParseTS("1700000002.000200")
	cases := []struct {
		name string
		conv Conversation
		want bool
	}{
		{"never read, has messages", Conversation{Latest: newer}, true},
		{"read up to an older message", Conversation{Latest: newer, LastRead: older}, true},
		{"read up to the latest", Conversation{Latest: newer, LastRead: newer}, false},
		{"marker past the latest", Conversation{Latest: older, LastRead: newer}, false},
		{"empty conversation", Conversation{}, false},
	}
	for _, c := range cases {
		if got := c.conv.Unread(); got != c.want {
			t.Errorf("%s: Unread() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestConversationKindNames(t *testing.T) {
	want := map[ConversationKind]string{
		Channel: "channel", PrivateChannel: "private", DirectMessage: "im", GroupMessage: "mpim",
	}
	for k, name := range want {
		if k.String() != name {
			t.Errorf("kind %d is %q, want %q", k, k.String(), name)
		}
	}
	for _, k := range []ConversationKind{0, conversationKindEnd, 200} {
		if k.String() != "" {
			t.Errorf("kind %d is %q, want no name", k, k.String())
		}
	}
}
