package slackwire

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// emojiTTL is how long a loaded emoji.list answers. People add custom
// emoji rarely; a new one shows up within this long.
const emojiTTL = time.Hour

type emojiResponse struct {
	Envelope
	Emoji map[string]string `json:"emoji"`
}

// Emoji returns the workspace's custom emoji from emoji.list (provider.
// EmojiLister): name (no colons) to its image URL, or "alias:<name>". The
// list is kept for emojiTTL; each call gets its own copy. It needs the
// custom emoji capability.
func (a *WebAPI) Emoji(ctx context.Context) (map[string]string, error) {
	if err := a.caps.Require(domain.CustomEmoji); err != nil {
		return nil, fmt.Errorf("emoji.list: %w", err)
	}
	emoji, err := a.emoji.get(a.now(), emojiTTL, func() (map[string]string, error) {
		var resp emojiResponse
		if err := a.Call(ctx, "emoji.list", url.Values{}, &resp); err != nil {
			return nil, err
		}
		return resp.Emoji, nil
	})
	if err != nil {
		return nil, err
	}
	out := maps.Clone(emoji)
	if out == nil {
		out = map[string]string{}
	}
	return out, nil
}
