// Adapted from github.com/gammons/slk internal/slack/client.go (MIT, Copyright (c) 2026 Grant Ammons).

package desktopsession

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

// Frames the provider writes to the web client's socket: presence_sub
// (provider.PresenceSubscriber) and typing (provider.TypingSender). Both
// go over the socket Connect opened, so they work only while a stream is
// live; otherwise they fail with ErrNotConnected and send nothing. The
// caller decides whether to retry after the next Connected.

var (
	_ provider.PresenceSubscriber = (*Provider)(nil)
	_ provider.TypingSender       = (*Provider)(nil)
)

// ErrNotConnected reports a socket write attempted while no Connect
// stream has a live socket: before the first Connect, between
// reconnects, or after the stream closed.
var ErrNotConnected = errors.New("desktop-session socket is not open")

// writeTimeout bounds one frame write, so a stalled connection cannot
// hold the socket's lock for ever.
const writeTimeout = 10 * time.Second

// liveSocket is the socket of the live Connect stream, if any. Its mutex
// makes every write go through one writer at a time, which is all
// gorilla/websocket allows; the read loop only reads, and gorilla's
// answers to pings use WriteControl, which is safe alongside a write.
type liveSocket struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (s *liveSocket) set(conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conn = conn
}

// clear forgets conn, unless a newer stream already replaced it.
func (s *liveSocket) clear(conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == conn {
		s.conn = nil
	}
}

// write sends v as one JSON frame, unless ctx has already ended. A frame
// is a few bytes, so it is not cancelled halfway: writeTimeout bounds it.
func (s *liveSocket) write(ctx context.Context, v any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return ErrNotConnected
	}
	_ = s.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	return s.conn.WriteJSON(v)
}

type presenceSubFrame struct {
	Type string   `json:"type"`
	IDs  []string `json:"ids"`
}

// SubscribePresence asks Slack for presence_change frames about users,
// and only them: each call replaces the previous subscription, so an
// empty users clears it.
func (p *Provider) SubscribePresence(ctx context.Context, users []domain.UserID) error {
	ids := make([]string, len(users))
	for i, u := range users {
		ids[i] = u.String()
	}
	if err := p.live.write(ctx, presenceSubFrame{Type: "presence_sub", IDs: ids}); err != nil {
		return fmt.Errorf("presence_sub: %w", err)
	}
	return nil
}

type typingFrame struct {
	Type    string `json:"type"`
	Channel string `json:"channel"`
}

// SendTyping tells the other people in conv that the user is typing.
// Slack shows it for a few seconds, so callers repeat it while the user
// keeps typing.
func (p *Provider) SendTyping(ctx context.Context, conv domain.ConversationID) error {
	if err := p.live.write(ctx, typingFrame{Type: "typing", Channel: conv.String()}); err != nil {
		return fmt.Errorf("typing: %w", err)
	}
	return nil
}
