package domain

// ConversationKind is what kind of conversation a Slack conversation is.
// The zero value is not a kind.
type ConversationKind uint8

const (
	_              ConversationKind = iota
	Channel                         // public channel (C…)
	PrivateChannel                  // private channel (C… or G…)
	DirectMessage                   // one-to-one DM (D…)
	GroupMessage                    // group DM (C… or G…)
	conversationKindEnd
)

var conversationKindNames = [conversationKindEnd]string{
	Channel:        "channel",
	PrivateChannel: "private",
	DirectMessage:  "im",
	GroupMessage:   "mpim",
}

func (k ConversationKind) String() string {
	if k >= conversationKindEnd {
		return ""
	}
	return conversationKindNames[k]
}

// Conversation is one entry of a workspace's conversation list, with what
// the sync engine needs to know whether it has something unread (spec
// sync-engine, "Reconciliation after reconnecting") and the clients need
// for their sidebars.
type Conversation struct {
	ID       ConversationID
	Kind     ConversationKind
	Name     string // channel name; empty for DMs, whose name is the other person
	Latest   TS     // newest message, zero when unknown or empty
	LastRead TS     // the user's read marker, zero when never read
	Mentions int    // unread mentions of the user, when the provider knows them
}

// Unread reports whether the conversation has a message newer than the
// user's read marker.
func (c Conversation) Unread() bool { return c.LastRead.Before(c.Latest) }
