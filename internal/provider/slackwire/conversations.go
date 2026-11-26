package slackwire

import (
	"context"
	"encoding/json"
	"errors"
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
// conversations.info send it. users.conversations carries no read state.
// conversations.info carries last_read and, for some kinds (Slack
// documents them for DMs and group DMs), the latest message and the
// unread counts; for channels it may leave them out.
type Conversation struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsIM      bool   `json:"is_im"`
	IsMPIM    bool   `json:"is_mpim"`
	IsPrivate bool   `json:"is_private"`
	LastRead  string `json:"last_read"`
	// Latest is the latest message object, or only its ts in some
	// legacy shapes; parsed by latestTS.
	Latest json.RawMessage `json:"latest"`
	// UnreadDisplay is unread_count_display: the unread messages that
	// matter to the user (join and leave messages left out).
	UnreadDisplay int `json:"unread_count_display"`
}

// errOddLatest reports a latest field that is neither a message, a ts
// nor null.
var errOddLatest = errors.New("latest is neither a message nor a ts")

// Parse checks w and builds its domain.Conversation, with its ID, kind,
// name and, when w carries them, read marker and latest message. Slack's
// never-read marker ("0000000000.000000") is the zero timestamp. In a DM
// or group DM every unread message is for the user, as Slack's own badge
// counts them, so unread_count_display there is the mention count; in a
// channel it is not, and Mentions stays zero (unknown).
func (w Conversation) Parse() (domain.Conversation, error) {
	id, err := domain.ParseConversationID(w.ID)
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("conversation id: %w", err)
	}
	lastRead, errRead := ParseOptionalTS(w.LastRead)
	latest, errLatest := w.latestTS()
	if err := errors.Join(errRead, errLatest); err != nil {
		return domain.Conversation{}, fmt.Errorf("conversation %s read state: %w", w.ID, err)
	}
	c := domain.Conversation{ID: id, Kind: w.kind(), Name: w.Name, Latest: latest, LastRead: lastRead}
	if c.Kind == domain.DirectMessage || c.Kind == domain.GroupMessage {
		c.Mentions = w.UnreadDisplay
	}
	return c, nil
}

// latestTS reads the latest field: absent or null is zero (unknown or
// empty), a message object gives its ts, and a string is the ts itself.
func (w Conversation) latestTS() (domain.TS, error) {
	var msg struct {
		TS string `json:"ts"`
	}
	switch {
	case len(w.Latest) == 0 || string(w.Latest) == "null":
		return domain.TS{}, nil
	case json.Unmarshal(w.Latest, &msg.TS) == nil:
	case json.Unmarshal(w.Latest, &msg) == nil:
	default:
		return domain.TS{}, errOddLatest
	}
	return ParseOptionalTS(msg.TS)
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
