// Adapted from github.com/gammons/slk internal/slack/client.go (MIT, Copyright (c) 2026 Grant Ammons).

package desktopsession

import (
	"context"
	"fmt"
	"net/url"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider/slackwire"
)

// Conversations lists the conversations the user is a member of, the way
// the web client builds its sidebar: users.conversations gives each one's
// kind and name, and client.counts (the web client's own unread source)
// gives its latest message, read marker and mention count.
//
// What client.counts' mention_count means for DMs is not documented: slk
// believes Slack counts every unread message there, as the official badge
// does, but no capture proves it. It is passed through unchanged.
//
// A conversation client.counts leaves out, or whose entry does not
// parse (it is logged), keeps zero read state: unknown, never guessed.
// Entries for conversations the user is not a member of are ignored.
func (p *Provider) Conversations(ctx context.Context) ([]domain.Conversation, error) {
	convs, err := p.UserConversations(ctx)
	if err != nil {
		return nil, err
	}
	states, err := p.readStates(ctx)
	if err != nil {
		return nil, err
	}
	for i, c := range convs {
		if s, ok := states[c.ID]; ok {
			convs[i].Latest, convs[i].LastRead, convs[i].Mentions = s.Latest, s.LastRead, s.Mentions
		}
	}
	return convs, nil
}

// countsEntry is one conversation in client.counts.
type countsEntry struct {
	ID           string `json:"id"`
	LastRead     string `json:"last_read"`
	Latest       string `json:"latest"`
	MentionCount int    `json:"mention_count"`
}

// parse builds the read state of e: a Conversation with only ID, Latest,
// LastRead and Mentions set.
func (e countsEntry) parse() (domain.Conversation, error) {
	id, err := domain.ParseConversationID(e.ID)
	if err != nil {
		return domain.Conversation{}, err
	}
	latest, err := slackwire.ParseOptionalTS(e.Latest)
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("%s latest: %w", e.ID, err)
	}
	lastRead, err := slackwire.ParseOptionalTS(e.LastRead)
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("%s last_read: %w", e.ID, err)
	}
	return domain.Conversation{ID: id, Latest: latest, LastRead: lastRead, Mentions: e.MentionCount}, nil
}

// countsResponse is client.counts. It splits conversations into channels
// (public and private), mpims and ims; the kinds come from
// users.conversations instead, so the three are read alike.
type countsResponse struct {
	slackwire.Envelope
	Channels []countsEntry `json:"channels"`
	MPIMs    []countsEntry `json:"mpims"`
	IMs      []countsEntry `json:"ims"`
}

// readStates calls client.counts and returns the read state of every
// conversation in it that parses.
func (p *Provider) readStates(ctx context.Context) (map[domain.ConversationID]domain.Conversation, error) {
	var resp countsResponse
	if err := p.Call(ctx, "client.counts", url.Values{}, &resp); err != nil {
		return nil, err
	}
	states := map[domain.ConversationID]domain.Conversation{}
	for _, group := range [][]countsEntry{resp.Channels, resp.MPIMs, resp.IMs} {
		for _, e := range group {
			s, err := e.parse()
			if err != nil {
				p.log.Warn("desktop-session: dropped a client.counts entry", "team", p.team, "err", err)
				continue
			}
			states[s.ID] = s
		}
	}
	return states, nil
}
