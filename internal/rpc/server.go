package rpc

import (
	"context"
	"log/slog"
	"net"
	"os"
	"sync"
)

// MaxLineSize is the longest NDJSON line a client may send. A longer one
// gets a parse error and the connection is closed.
const MaxLineSize = 4 << 20

// DefaultMaxCalls is how many calls one connection may have running at
// once (spec daemon-api, "Bounded concurrency per connection").
const DefaultMaxCalls = 16

// Options configures a Server. Every field is optional.
type Options struct {
	Hello  HelloFunc    // extra hello fields; none when nil
	Logger *slog.Logger // discarded when nil

	// MaxCalls caps the calls running at once on one connection; zero or
	// less means DefaultMaxCalls. At the cap the server stops reading that
	// connection until a call finishes: backpressure, not an error.
	MaxCalls int
}

// Server answers JSON-RPC 2.0 over NDJSON on a Listener. Each connection is
// served on its own goroutine, and each call after hello on its own too.
type Server struct {
	handler Handler
	hello   HelloFunc
	log     *slog.Logger
	owner   uint32 // the only UID allowed to connect
	maxLine int
	maxCall int             // calls running at once per connection
	ctx     context.Context // ends on Close; every connection derives from it
	cancel  context.CancelFunc

	mu       sync.Mutex
	closed   bool
	listener *Listener
	conns    map[*Conn]struct{}
	wg       sync.WaitGroup // Serve and every connection
}

// NewServer returns a Server that sends calls made after hello to h.
func NewServer(h Handler, opts Options) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		handler: h,
		hello:   opts.Hello,
		log:     opts.Logger,
		owner:   uint32(os.Getuid()), //nolint:gosec // a UID always fits in uint32 on Linux
		maxLine: MaxLineSize,
		maxCall: opts.MaxCalls,
		ctx:     ctx,
		cancel:  cancel,
		conns:   make(map[*Conn]struct{}),
	}
	if s.hello == nil {
		s.hello = noExtras
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.maxCall <= 0 {
		s.maxCall = DefaultMaxCalls
	}
	return s
}

// Serve accepts connections on l until Close, then returns nil. Any other
// accept failure is returned as is.
func (s *Server) Serve(l *Listener) error {
	if !s.start(l) {
		return l.Close()
	}
	defer s.wg.Done()
	for {
		nc, err := l.AcceptUnix()
		if err != nil {
			if s.isClosed() {
				return nil
			}
			return err
		}
		s.wg.Add(1)
		go s.serveConn(nc)
	}
}

// Close stops accepting, ends every connection, removes the socket file
// and waits until every connection and call has finished.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	l := s.listener
	for c := range s.conns {
		_ = c.nc.Close()
	}
	s.mu.Unlock()

	s.cancel()
	var err error
	if l != nil {
		err = l.Close()
	}
	s.wg.Wait()
	return err
}

func (s *Server) start(l *Listener) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.listener = l
	s.wg.Add(1)
	return true
}

func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Server) serveConn(nc *net.UnixConn) {
	defer s.wg.Done()
	if err := checkPeer(nc, s.owner); err != nil {
		s.log.Warn("rpc: rejected connection", "err", err)
		_ = nc.Close()
		return
	}
	c := newConn(s, nc)
	if !s.track(c) {
		_ = nc.Close()
		return
	}
	defer s.untrack(c)
	c.run()
}

func (s *Server) track(c *Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) untrack(c *Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, c)
}
