package events

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"

	"gitlab.com/scabb-island/omaslack/internal/rpc"
)

var (
	errNoConn    = errors.New("events: events.subscribe was called outside a connection")
	errConnEnded = errors.New("events: the connection has ended")
)

// peer is the part of an rpc.Conn the hub uses.
type peer interface {
	Notify(method string, params any) error
	Close() error
	Done() <-chan struct{}
}

// subscriber is one connection's topics and queue. Its own goroutine
// (drain) is the only writer of notifications to the connection, so they
// leave in the order they were queued.
type subscriber struct {
	topics map[string]bool // guarded by Hub.mu
	queue  *queue
	// summary is the last unread summary queued for it, guarded by
	// Hub.mu: a coalesced one equal to it is not sent again.
	summary any
}

type subscribeParams struct {
	Topics []string `json:"topics"`
}

type subscribeResult struct {
	Topics []string `json:"topics"`
}

// parseTopics checks the events.subscribe params and returns the topics
// without repeats, in the order given.
func parseTopics(params json.RawMessage) ([]string, error) {
	var p subscribeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: "events.subscribe needs {topics: [string]}"}
	}
	topics := make([]string, 0, len(p.Topics))
	for _, t := range p.Topics {
		if !slices.Contains(Topics, t) {
			return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: "unknown topic " + t}
		}
		if !slices.Contains(topics, t) {
			topics = append(topics, t)
		}
	}
	return topics, nil
}

// subscribe sets p's topics, replacing any it had.
func (h *Hub) subscribe(p peer, topics []string) error {
	set := make(map[string]bool, len(topics))
	for _, t := range topics {
		set[t] = true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if ended(p) {
		return errConnEnded
	}
	s, ok := h.subs[p]
	if !ok {
		s = &subscriber{queue: newQueue(h.opts.QueueSize)}
		h.subs[p] = s
		go h.drain(p, s)
	}
	s.topics = set
	return nil
}

// drain writes s's queue to p until the connection ends, then forgets it.
// A failed write means the connection is ending, which Done reports next.
func (h *Hub) drain(p peer, s *subscriber) {
	defer h.forget(p, s)
	for {
		select {
		case <-p.Done():
			return
		case <-s.queue.wake:
		}
		if !flush(p, s.queue) {
			return
		}
	}
}

// flush writes q to p until q is empty, or reports false if the
// connection ends first.
func flush(p peer, q *queue) bool {
	for n, ok := q.pop(); ok; n, ok = q.pop() {
		if ended(p) {
			return false
		}
		_ = p.Notify(n.method, n.params)
	}
	return true
}

func (h *Hub) forget(p peer, s *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[p] == s {
		delete(h.subs, p)
	}
}

// fanOut queues one notification of topic, coalesced by k (see queue), for
// each of its subscribers and returns how many it reached. payload is only
// built when someone wants it.
func (h *Hub) fanOut(topic string, k key, payload func() any) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	var n note
	reached := 0
	for p, s := range h.subs {
		if !s.topics[topic] || !h.live(p) {
			continue
		}
		if n.method == "" {
			n = note{method: topic, params: payload(), key: k}
		}
		if h.enqueue(p, s, n) {
			reached++
		}
	}
	return reached
}

// live forgets p if its connection has ended. Callers hold h.mu.
func (h *Hub) live(p peer) bool {
	if ended(p) {
		delete(h.subs, p)
		return false
	}
	return true
}

// enqueue queues n for s without blocking. A full queue means the client
// stopped keeping up: it is disconnected and forgotten, and reloads when it
// reconnects. Callers hold h.mu.
func (h *Hub) enqueue(p peer, s *subscriber, n note) bool {
	if s.queue.push(n) {
		return true
	}
	delete(h.subs, p)
	_ = p.Close()
	h.opts.Log.Warn("events: disconnected a slow subscriber", "queued", h.opts.QueueSize, "topic", n.method)
	return false
}

// wants reports whether anyone is subscribed to topic.
func (h *Hub) wants(topic string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.subs {
		if s.topics[topic] {
			return true
		}
	}
	return false
}

// sendSummary sends the current summary to every unread.summary
// subscriber that has not been sent it already, computing it only when
// there is one.
func (h *Hub) sendSummary(ctx context.Context) {
	if !h.wants(TopicUnreadSummary) {
		return
	}
	sum, ok := h.summary(ctx)
	if !ok {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for p, s := range h.subs {
		if s.topics[TopicUnreadSummary] && h.live(p) && !reflect.DeepEqual(s.summary, sum) {
			h.enqueueSummary(p, s, sum)
		}
	}
}

// sendSummaryTo sends the current summary to p alone: a new subscriber
// must not wait for the next change to show the counts.
func (h *Hub) sendSummaryTo(ctx context.Context, p peer) {
	sum, ok := h.summary(ctx)
	if !ok {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.subs[p]; ok {
		h.enqueueSummary(p, s, sum)
	}
}

// enqueueSummary queues sum for s and remembers it. Callers hold h.mu.
func (h *Hub) enqueueSummary(p peer, s *subscriber, sum any) {
	if h.enqueue(p, s, note{method: TopicUnreadSummary, params: sum, key: summaryKey}) {
		s.summary = sum
	}
}

func (h *Hub) summary(ctx context.Context) (any, bool) {
	sum, err := h.opts.Summary(ctx)
	if err != nil {
		h.opts.Log.Error("events: compute the unread summary", "err", err)
		return nil, false
	}
	return sum, true
}

func ended(p peer) bool {
	select {
	case <-p.Done():
		return true
	default:
		return false
	}
}
