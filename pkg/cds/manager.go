package cds

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Logger is the subset of the Grafana SDK logger the manager uses.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

const (
	// subscriptionBuffer is how many undelivered batches a slow subscriber
	// may queue before new batches are dropped for it.
	subscriptionBuffer = 64
	// resubscribeOverlap is how many seconds of back data are requested for
	// already-streaming fields when the subscription is re-sent (field set
	// changed or reconnect). Points at or before the last delivered
	// timestamp are dropped, so the overlap only closes gaps.
	resubscribeOverlap = 10
	dialTimeout        = 10 * time.Second
	minStallTimeout    = 10 * time.Second
	minBackoff         = 1 * time.Second
	maxBackoff         = 30 * time.Second
)

// Subscription delivers batches of new points for one field. C is closed
// when the subscription or the manager is closed.
type Subscription struct {
	Field string
	C     <-chan []Point

	c       chan []Point
	m       *Manager
	once    sync.Once
	dropped int
}

// Close stops delivery and releases the subscription.
func (s *Subscription) Close() {
	s.once.Do(func() { s.m.unsubscribe(s) })
}

// Manager multiplexes any number of per-field subscriptions over a single
// CDS WebSocket connection. The connection is opened when the first field is
// subscribed, re-subscribed whenever the set of fields changes, closed when
// the last subscription goes away, and re-established with backoff if it
// fails.
type Manager struct {
	opts     Options
	interval time.Duration
	log      Logger

	mu      sync.Mutex
	subs    map[string]map[*Subscription]struct{}
	lastTs  map[string]float64 // last delivered timestamp per active field
	last    map[string]Point   // last delivered point per active field
	closed  bool
	started bool

	changed chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewManager creates a manager. interval is the CDS subscription interval,
// i.e. the target time between batches.
func NewManager(opts Options, interval time.Duration, logger Logger) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		opts:     opts,
		interval: interval,
		log:      logger,
		subs:     map[string]map[*Subscription]struct{}{},
		lastTs:   map[string]float64{},
		last:     map[string]Point{},
		changed:  make(chan struct{}, 1),
		ctx:      ctx,
		cancel:   cancel,
		done:     make(chan struct{}),
	}
}

// Subscribe starts delivering new points for field.
func (m *Manager) Subscribe(field string) (*Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("CDS connection manager is closed")
	}
	c := make(chan []Point, subscriptionBuffer)
	s := &Subscription{Field: field, C: c, c: c, m: m}
	set, ok := m.subs[field]
	if !ok {
		set = map[*Subscription]struct{}{}
		m.subs[field] = set
	}
	set[s] = struct{}{}
	if !m.started {
		m.started = true
		go m.run()
	}
	if !ok {
		m.signal()
	}
	return s, nil
}

// Latest returns the last point delivered for a field that is currently
// being streamed, if any.
func (m *Manager) Latest(field string) (Point, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.last[field]
	return p, ok
}

// Close shuts down the connection and closes all subscriptions.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	started := m.started
	m.cancel()
	m.mu.Unlock()
	if started {
		<-m.done
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, set := range m.subs {
		for s := range set {
			close(s.c)
		}
	}
	m.subs = map[string]map[*Subscription]struct{}{}
}

func (m *Manager) unsubscribe(s *Subscription) {
	m.mu.Lock()
	defer m.mu.Unlock()
	set, ok := m.subs[s.Field]
	if !ok {
		return
	}
	if _, ok := set[s]; !ok {
		return
	}
	delete(set, s)
	close(s.c)
	if len(set) == 0 {
		delete(m.subs, s.Field)
		delete(m.lastTs, s.Field)
		delete(m.last, s.Field)
		m.signal()
	}
}

// signal notes that the field set changed. Callers hold m.mu.
func (m *Manager) signal() {
	select {
	case m.changed <- struct{}{}:
	default:
	}
}

// subscriptionSpecs builds the subscribe request for the current field set.
// Fields that have already delivered data get a short overlap so that no
// points are missed across a re-subscribe; new fields start from their most
// recent cached value.
func (m *Manager) subscriptionSpecs() map[string]FieldSpec {
	m.mu.Lock()
	defer m.mu.Unlock()
	specs := make(map[string]FieldSpec, len(m.subs))
	for f := range m.subs {
		if _, ok := m.lastTs[f]; ok {
			specs[f] = FieldSpec{Seconds: resubscribeOverlap}
		} else {
			specs[f] = FieldSpec{Seconds: -1}
		}
	}
	return specs
}

// dispatch fans a data batch out to subscribers, dropping points already
// delivered.
func (m *Manager) dispatch(data map[string][]Point) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for field, pts := range data {
		set, ok := m.subs[field]
		if !ok || len(pts) == 0 {
			continue
		}
		last, seen := m.lastTs[field]
		fresh := pts[:0:0]
		for _, p := range pts {
			if !seen || p.Time > last {
				fresh = append(fresh, p)
				last, seen = p.Time, true
			}
		}
		if len(fresh) == 0 {
			continue
		}
		m.lastTs[field] = last
		m.last[field] = fresh[len(fresh)-1]
		for s := range set {
			select {
			case s.c <- fresh:
			default:
				s.dropped++
				if s.dropped == 1 || s.dropped%100 == 0 {
					m.log.Warn("Dropping CDS data for slow stream", "field", field, "dropped", s.dropped)
				}
			}
		}
	}
}

func (m *Manager) hasFields() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.subs) > 0
}

// run owns the connection lifecycle.
func (m *Manager) run() {
	defer close(m.done)
	backoff := minBackoff
	for {
		if m.ctx.Err() != nil {
			return
		}
		if !m.hasFields() {
			select {
			case <-m.ctx.Done():
				return
			case <-m.changed:
				continue
			}
		}
		start := time.Now()
		err := m.session()
		if m.ctx.Err() != nil {
			return
		}
		if err == nil {
			// Clean exit: no fields left.
			backoff = minBackoff
			continue
		}
		if time.Since(start) > maxBackoff {
			backoff = minBackoff
		}
		m.log.Warn("CDS connection lost; reconnecting", "error", err, "retryIn", backoff.String())
		select {
		case <-m.ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

type recvResult struct {
	msg *Message
	err error
}

// session runs one connection until it fails (returns an error) or there
// are no fields left to stream (returns nil).
func (m *Manager) session() error {
	ctx, cancel := context.WithCancel(m.ctx)
	defer cancel()

	dialCtx, dialCancel := context.WithTimeout(ctx, dialTimeout)
	conn, err := Dial(dialCtx, m.opts)
	dialCancel()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	m.log.Info("Connected to CDS", "url", m.opts.URL, "interval", m.interval.String())

	msgs := make(chan recvResult, 1)
	go func() {
		for {
			msg, err := conn.Recv(ctx)
			select {
			case msgs <- recvResult{msg, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	subscribe := func() error {
		specs := m.subscriptionSpecs()
		if len(specs) == 0 {
			return nil
		}
		m.log.Debug("Subscribing to CDS fields", "count", len(specs))
		return conn.Send(ctx, map[string]any{
			"type":     "subscribe",
			"interval": m.interval.Seconds(),
			"fields":   specs,
		})
	}
	ready := func() error { return conn.Send(ctx, map[string]any{"type": "ready"}) }

	// Drain any stale change signal; the subscription below covers it.
	select {
	case <-m.changed:
	default:
	}
	if err := subscribe(); err != nil {
		return err
	}
	if err := ready(); err != nil {
		return err
	}

	stallTimeout := 20 * m.interval
	if stallTimeout < minStallTimeout {
		stallTimeout = minStallTimeout
	}
	stall := time.NewTimer(stallTimeout)
	defer stall.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-m.changed:
			if !m.hasFields() {
				m.log.Info("No fields to stream; closing CDS connection")
				return nil
			}
			if err := subscribe(); err != nil {
				return err
			}

		case <-stall.C:
			return fmt.Errorf("no response from CDS for %s", stallTimeout)

		case r := <-msgs:
			if r.err != nil {
				return r.err
			}
			if !stall.Stop() {
				select {
				case <-stall.C:
				default:
				}
			}
			stall.Reset(stallTimeout)

			switch r.msg.Type {
			case "data":
				data, err := DecodeData(r.msg.Data)
				if err != nil {
					m.log.Warn("Ignoring undecodable CDS data message", "error", err)
				} else {
					m.dispatch(data)
				}
				if err := ready(); err != nil {
					return err
				}
			case "ready":
				// "client ready, but no matching fields found (yet)": the
				// server pauses on its side, so just ask again.
				if err := ready(); err != nil {
					return err
				}
			case "subscribe":
				if r.msg.Status != 200 {
					return fmt.Errorf("CDS rejected subscription (status %d): %s", r.msg.Status, r.msg.ErrorText())
				}
			case "error":
				// From the OpenRVDAS API proxy, e.g. 503 "CachedDataServer
				// unavailable"; it closes the connection next.
				return fmt.Errorf("server error (status %d): %s", r.msg.Status, r.msg.ErrorText())
			default:
				if r.msg.Status != 200 {
					m.log.Warn("CDS error message", "status", r.msg.Status, "error", r.msg.ErrorText())
				}
			}
		}
	}
}
