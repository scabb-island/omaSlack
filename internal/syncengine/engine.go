package syncengine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
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
	PageSize int              // messages per History call; default 100
	Log      *slog.Logger     // default: discard

	// ReadPollInterval is how often read markers are polled when the
	// provider implements ReadMarkerReader; default one minute.
	ReadPollInterval time.Duration
}

func (c Config) withDefaults() Config {
	if c.Clock == nil {
		c.Clock = ratebudget.SystemClock{}
	}
	if c.Rand == nil {
		c.Rand = rand.Float64
	}
	if c.PageSize <= 0 {
		c.PageSize = 100
	}
	if c.Log == nil {
		c.Log = slog.New(slog.DiscardHandler)
	}
	if c.ReadPollInterval <= 0 {
		c.ReadPollInterval = time.Minute
	}
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
	wg         sync.WaitGroup // every goroutine Run started

	// wasUp is whether a stream ever reported connected. Only the connect
	// loop's goroutine touches it.
	wasUp bool
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
	if _, ok := e.cfg.Provider.(ReadMarkerReader); ok {
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
	r := request{op: o, done: make(chan error, 1)}
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
		connected, err := e.session(ctx)
		if err != nil {
			return err
		}
		if connected {
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

// session runs one connection. It reports whether the stream ever said it
// was connected, and returns an error only when the engine must stop.
func (e *Engine) session(ctx context.Context) (connected bool, err error) {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := e.connect(sctx)
	if err != nil {
		return false, e.fatal(ctx, err)
	}
	return e.consume(ctx, stream)
}

// consume handles stream until it closes or the engine must stop.
func (e *Engine) consume(ctx context.Context, stream <-chan domain.Event) (connected bool, err error) {
	for {
		select {
		case ev, ok := <-stream:
			if !ok {
				return connected, ctx.Err()
			}
			up, err := e.handle(ctx, ev)
			connected = connected || up
			if err != nil {
				return connected, err
			}
		case <-e.expired:
			return connected, errAuthExpired
		case <-ctx.Done():
			return connected, ctx.Err()
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

// handle stores and publishes one stream event, and reports whether it
// said the connection is up. Its error means the engine must stop.
func (e *Engine) handle(ctx context.Context, ev domain.Event) (up bool, err error) {
	if err := e.store(ctx, ev); err != nil {
		return false, err
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
func (e *Engine) react(ctx context.Context, ev domain.Event) (up bool, err error) {
	cc, ok := ev.(domain.ConnectionChanged)
	switch {
	case !ok:
		return false, nil
	case cc.State == domain.AuthExpired:
		e.expire(ctx)
		return false, errAuthExpired
	case cc.State == domain.Connected:
		e.connected(ctx)
		return true, nil
	default:
		return false, nil
	}
}

// connected starts catching up after every connection but the first:
// before that there was no outage to miss anything in.
func (e *Engine) connected(ctx context.Context) {
	if e.wasUp {
		e.wg.Go(func() { e.reconcile(ctx) })
	}
	e.wasUp = true
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
