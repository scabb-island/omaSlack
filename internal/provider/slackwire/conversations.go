package slackwire

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// conversationsPageSize is how many conversations users.conversations
// returns per page; 200 is the most Slack recommends.
const conversationsPageSize = 200

// conversationTypes are the kinds of conversation the user can be in, as
// users.conversations names them.
const conversationTypes = "public_channel,private_channel,mpim,im"

// Conversation is a conversation object as users.conversations and
// conversations.info send it. Of the read state, only conversations.info
// carries last_read; neither carries the latest message's ts or the
// user's unread mentions.
type Conversation struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsIM      bool   `json:"is_im"`
	IsMPIM    bool   `json:"is_mpim"`
	IsPrivate bool   `json:"is_private"`
	LastRead  string `json:"last_read"`
}

// Parse checks w and builds its domain.Conversation, with its ID, kind,
// name and, when w carries one, read marker. Slack's never-read marker
// ("0000000000.000000") is the zero timestamp.
func (w Conversation) Parse() (domain.Conversation, error) {
	id, err := domain.ParseConversationID(w.ID)
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("conversation id: %w", err)
	}
	lastRead, err := ParseOptionalTS(w.LastRead)
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("conversation %s last_read: %w", w.ID, err)
	}
	return domain.Conversation{ID: id, Kind: w.kind(), Name: w.Name, LastRead: lastRead}, nil
}

// kind reads Slack's flags. A group DM is also private, so is_mpim is
// checked before is_private.
func (w Conversation) kind() domain.ConversationKind {
	switch {
	case w.IsIM:
		return domain.DirectMessage
	case w.IsMPIM:
		return domain.GroupMessage
	case w.IsPrivate:
		return domain.PrivateChannel
	default:
		return domain.Channel
	}
}

type conversationsResponse struct {
	Envelope
	Channels []Conversation `json:"channels"`
	Metadata struct {
		NextCursor string `json:"next_cursor"`
	} `json:"response_metadata"`
}

// UserConversations calls users.conversations until the last page and
// returns every unarchived conversation the user is a member of, of every
// kind. Its entries carry no latest message, read marker or mention count:
// those stay zero. Entries that do not parse are logged and dropped, so
// one odd conversation does not hide the list.
func (a *WebAPI) UserConversations(ctx context.Context) ([]domain.Conversation, error) {
	form := url.Values{
		"types": {conversationTypes}, "exclude_archived": {"true"}, "limit": {strconv.Itoa(conversationsPageSize)},
	}
	var convs []domain.Conversation
	for {
		var resp conversationsResponse
		if err := a.Call(ctx, "users.conversations", form, &resp); err != nil {
			return nil, err
		}
		for _, w := range resp.Channels {
			c, err := w.Parse()
			if err != nil {
				a.log.Warn("dropped a conversation", "err", err)
				continue
			}
			convs = append(convs, c)
		}
		if resp.Metadata.NextCursor == "" {
			return convs, nil
		}
		form.Set("cursor", resp.Metadata.NextCursor)
	}
}
