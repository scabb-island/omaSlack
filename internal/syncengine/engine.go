package syncengine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/ratebudget"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

// Publisher hands published events to subscribed clients. The engine
// calls it from one goroutine, after the Store holds the event. It must
// not block: the RPC layer (task 2.4) queues per client.
type Publisher interface {
	Publish(ev domain.Event)
}

// ErrStopped reports a call made while Run is not running.
var ErrStopped = errors.New("syncengine: engine is not running")

// errAuthExpired is what Run returns once Slack rejected the credentials.
var errAuthExpired = fmt.Errorf("syncengine: %w", provider.ErrAuthExpired)

// methodConnect is the budget key of Provider.Connect. The contract does
// not say which Slack method a provider's Connect calls (rtm.connect,
// apps.connections.open), so every provider shares this one.
const methodConnect = "connect"

// Config is what one workspace's engine is built from. Provider, Store,
// Budget and Publisher are required; the rest have defaults.
type Config struct {
	Provider  provider.Provider
	Store     store.Store
	Budget    ratebudget.Budget
	Publisher Publisher

	Clock    ratebudget.Clock // default: the wall clock
	Rand     func() float64   // in [0, 1), for jitter; default: math/rand
	PageSize int              // messages per History call; zero means 100
	Log      *slog.Logger     // default: discard

	// ReadPollInterval is how often read markers are polled when the
	// provider implements provider.ReadMarkerReader; zero means one minute.
	ReadPollInterval time.Duration
}

func (c Config) withDefaults() Config {
	if c.Clock == nil {
		c.Clock = ratebudget.SystemClock{}
	}
	if c.Rand == nil {
		c.Rand = rand.Float64
	}
	if c.Log == nil {
		c.Log = slog.New(slog.DiscardHandler)
	}
	c.PageSize = cmp.Or(c.PageSize, 100)
	c.ReadPollInterval = cmp.Or(c.ReadPollInterval, time.Minute)
	return c
}

// Engine syncs one workspace. Build it with New and call Run once; the
// other methods are safe from any goroutine while Run runs.
type Engine struct {
	cfg Config
	w   *writer

	ops        chan request
	stopped    chan struct{} // closed when Run stops serving ops
	expired    chan struct{} // closed when Slack rejected the credentials
	expireOnce sync.Once
	listFailed atomic.Bool    // the conversation list is failing (logged when it changes)
	wg         sync.WaitGroup // every goroutine Run started

	// Only the connect loop's goroutine touches these: whether a stream
	// ever reported connected, and whether the current one did.
	wasUp, upNow bool
}

// New builds an engine from cfg.
func New(cfg Config) *Engine {
	cfg = cfg.withDefaults()
	return &Engine{
		cfg:     cfg,
		w:       newWriter(cfg),
		ops:     make(chan request),
		stopped: make(chan struct{}),
		expired: make(chan struct{}),
	}
}

// Run keeps the workspace connected until ctx ends or Slack rejects the
// credentials, and returns why it stopped: ctx's error, or an error
// wrapping provider.ErrAuthExpired. The calling goroutine is the Store's
// only writer; Run returns once every goroutine it started is done.
func (e *Engine) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	e.wg.Go(func() {
		defer cancel()
		result <- e.connectLoop(ctx)
	})
	if _, ok := e.cfg.Provider.(provider.ReadMarkerReader); ok {
		e.wg.Go(func() { e.pollReads(ctx) })
	}
	e.w.serve(ctx, e.ops)
	close(e.stopped)
	e.wg.Wait()
	return <-result
}

// do runs o on the writer's goroutine and returns its result. Once ctx has
// ended, that is the error, even if Run stopped at the same moment.
func (e *Engine) do(ctx context.Context, o op) error {
	r := request{op: o, done: make(chan error)}
	select {
	case e.ops <- r:
		return <-r.done
	case <-ctx.Done():
	case <-e.stopped:
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrStopped
}

// connectLoop connects, consumes the stream and reconnects with backoff
// until ctx ends or the credentials are rejected.
func (e *Engine) connectLoop(ctx context.Context) error {
	for attempt := 0; ; attempt++ {
		if err := e.do(ctx, linkOp(domain.Connecting)); err != nil {
			return err
		}
		e.upNow = false
		if err := e.session(ctx); err != nil {
			return err
		}
		if e.upNow {
			attempt = 0
		}
		if err := e.do(ctx, linkOp(domain.Disconnected)); err != nil {
			return err
		}
		if err := e.sleep(ctx, backoff(attempt, e.cfg.Rand())); err != nil {
			return err
		}
	}
}

// session runs one connection. Its error means the engine must stop.
func (e *Engine) session(ctx context.Context) error {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := e.connect(sctx)
	if err != nil {
		return e.fatal(ctx, err)
	}
	return e.consume(ctx, stream)
}

// consume handles stream until it closes or the engine must stop.
func (e *Engine) consume(ctx context.Context, stream <-chan domain.Event) error {
	for {
		select {
		case ev, ok := <-stream:
			if !ok {
				return ctx.Err()
			}
			if err := e.handle(ctx, ev); err != nil {
				return err
			}
		case <-e.expired:
			return errAuthExpired
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (e *Engine) connect(ctx context.Context) (<-chan domain.Event, error) {
	var stream <-chan domain.Event
	err := e.call(ctx, methodConnect, ratebudget.Background, func(ctx context.Context) error {
		var err error
		stream, err = e.cfg.Provider.Connect(ctx)
		return err
	})
	return stream, err
}

// handle stores and publishes one stream event. Its error means the
// engine must stop.
func (e *Engine) handle(ctx context.Context, ev domain.Event) error {
	if err := e.store(ctx, ev); err != nil {
		return err
	}
	return e.react(ctx, ev)
}

// store hands ev to the writer. An event the Store rejects is logged and
// skipped; only an ended ctx is an error.
func (e *Engine) store(ctx context.Context, ev domain.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.do(ctx, applyOp(ev)); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		e.cfg.Log.Warn("sync: could not store an event", "team", e.w.team, "topic", ev.Topic(), "err", err)
	}
	return nil
}

// react acts on a connection state the stream reported: catching up when
// it is up, stopping when the credentials were rejected.
func (e *Engine) react(ctx context.Context, ev domain.Event) error {
	// Any other event leaves cc zero, whose state (connecting) needs
	// nothing done.
	cc, _ := ev.(domain.ConnectionChanged)
	switch cc.State {
	case domain.AuthExpired:
		e.expire(ctx)
		return errAuthExpired
	case domain.Connected:
		e.connected(ctx)
	}
	return nil
}

// connected resets the backoff and reconciles. Only after the first
// connection is there no outage to catch up on.
func (e *Engine) connected(ctx context.Context) {
	outage := e.wasUp
	e.wg.Go(func() { e.reconcile(ctx, outage) })
	e.wasUp, e.upNow = true, true
}

// fatal sorts an error from a provider call: rejected credentials and an
// ended ctx stop the engine; anything else is logged and retried.
func (e *Engine) fatal(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, provider.ErrAuthExpired):
		return errAuthExpired
	case ctx.Err() != nil:
		return ctx.Err()
	default:
		e.cfg.Log.Warn("sync: connection failed", "team", e.w.team, "err", err)
		return nil
	}
}

// call runs fn under the rate budget. A 429 penalizes method and is
// retried once the budget allows; clients see rate_limited meanwhile.
// Rejected credentials stop the engine.
func (e *Engine) call(ctx context.Context, method string, p ratebudget.Priority, fn func(context.Context) error) error {
	limited := false
	defer func() {
		if limited {
			_ = e.do(context.WithoutCancel(ctx), limitOp(-1))
		}
	}()
	for {
		if err := e.cfg.Budget.Wait(ctx, method, p); err != nil {
			return err
		}
		err := fn(ctx)
		var rl *provider.RateLimitedError
		if !errors.As(err, &rl) {
			e.expireOn(ctx, err)
			return err
		}
		e.cfg.Budget.Penalize(method, rl.RetryAfter)
		if !limited {
			// Counted only once the writer took it, so the deferred -1
			// always has a writer to reach or a stopped Run to return.
			limited = e.do(ctx, limitOp(1)) == nil
		}
	}
}

// expireOn stops the engine when err means Slack rejected the credentials.
func (e *Engine) expireOn(ctx context.Context, err error) {
	if errors.Is(err, provider.ErrAuthExpired) {
		e.expire(ctx)
	}
}

// expire tells clients the credentials are gone and stops Run.
func (e *Engine) expire(ctx context.Context) {
	_ = e.do(context.WithoutCancel(ctx), linkOp(domain.AuthExpired))
	e.expireOnce.Do(func() { close(e.expired) })
}

// sleep waits d on the engine's clock, unless ctx ends or the credentials
// are rejected first.
func (e *Engine) sleep(ctx context.Context, d time.Duration) error {
	t := e.cfg.Clock.NewTimer(e.cfg.Clock.Now().Add(d))
	defer t.Stop()
	select {
	case <-t.C():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-e.expired:
		return errAuthExpired
	}
}
