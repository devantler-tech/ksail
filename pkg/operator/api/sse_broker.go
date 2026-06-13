package api

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// clusterBroker runs a single cluster-list discovery loop per Server process and fans its results out
// to every open SSE connection, so the backend cost of the events stream is O(1) in the number of
// connected clients instead of O(connections) — N browser tabs plus the desktop window no longer mean
// N independent provider-discovery loops (each a Docker enumeration plus Hetzner/Omni/EKS cloud
// round-trips on the local backend).
//
// The loop starts when the first subscriber connects and stops when the last one disconnects, so the
// broker idles with zero subscribers (the same Server type runs in the operator, where the events
// stream may never be opened). Each tick the loop calls Service.List once, serializes the cluster
// list, and broadcasts a brokerTick to every subscriber; per-connection concerns (the initial
// snapshot, change-only emission, heartbeats, and per-tick session re-validation) stay in handleEvents.
type clusterBroker struct {
	server *Server

	mu          sync.Mutex
	subscribers map[chan brokerTick]struct{}
	cancel      context.CancelFunc
}

// brokerTick is the result of one discovery tick broadcast to subscribers. payload is the serialized
// full cluster list when List succeeded and errEvent is empty; when List failed, errEvent carries the
// stream-error payload and payload is empty. Exactly one of payload/errEvent is meaningful per tick.
type brokerTick struct {
	// payload is the JSON-serialized fullClusterList for a successful tick (empty on a failed tick).
	payload string
	// errEvent is the stream-error JSON for a failed List tick (empty on a successful tick).
	errEvent string
}

// newClusterBroker builds a broker bound to the server whose Service it polls and whose interval it
// honours.
func newClusterBroker(server *Server) *clusterBroker {
	return &clusterBroker{
		server:      server,
		subscribers: map[chan brokerTick]struct{}{},
	}
}

// subscribe registers a new subscriber and returns its tick channel. The first subscriber starts the
// shared discovery loop (rooted at a process-lifetime context derived from baseCtx without
// cancellation, so one connection's disconnect cannot cancel the loop other connections still use).
// The returned channel is buffered by one and the broadcaster drops rather than blocks, so a slow
// consumer never stalls the shared loop or the other subscribers.
func (b *clusterBroker) subscribe(baseCtx context.Context) chan brokerTick {
	subscriber := make(chan brokerTick, 1)

	b.mu.Lock()
	defer b.mu.Unlock()

	b.subscribers[subscriber] = struct{}{}

	if b.cancel == nil {
		// Detach from the request context so the loop spans connections; it is cancelled only when the
		// last subscriber leaves (see unsubscribe).
		loopCtx, cancel := context.WithCancel(context.WithoutCancel(baseCtx))
		b.cancel = cancel

		go b.run(loopCtx)
	}

	return subscriber
}

// unsubscribe removes a subscriber and stops the shared discovery loop once the last one leaves, so
// the broker consumes no resources while idle.
func (b *clusterBroker) unsubscribe(subscriber chan brokerTick) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.subscribers[subscriber]; !ok {
		return
	}

	delete(b.subscribers, subscriber)

	if len(b.subscribers) == 0 && b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
}

// run is the single shared discovery loop. It ticks on the server's events interval, calls List once
// per tick, and broadcasts the result to every subscriber until the context is cancelled (last
// subscriber gone or server shutdown).
func (b *clusterBroker) run(ctx context.Context) {
	ticker := time.NewTicker(b.server.eventsInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.broadcast(ctx, b.poll(ctx))
		}
	}
}

// poll runs one List and reduces it to a brokerTick: a serialized cluster list on success, or a
// stream-error payload on failure (so a transient discovery error reaches subscribers as a
// stream-error event rather than ending any stream).
func (b *clusterBroker) poll(ctx context.Context) brokerTick {
	list, err := b.server.Service.List(ctx)
	if err != nil {
		return brokerTick{errEvent: errorPayload(err)}
	}

	payload, err := json.Marshal(toFullClusterList(list))
	if err != nil {
		// A marshal failure is treated as "no change": keep the subscribers' last-known-good list rather
		// than emitting an internal error. Mirrors writeClusterEvent's pre-broker behaviour.
		return brokerTick{}
	}

	return brokerTick{payload: string(payload)}
}

// broadcast delivers a tick to every subscriber without blocking on a slow consumer (the channel is
// buffered by one and a full buffer is skipped — the next tick supersedes it).
func (b *clusterBroker) broadcast(ctx context.Context, tick brokerTick) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for subscriber := range b.subscribers {
		select {
		case <-ctx.Done():
			return
		case subscriber <- tick:
		default:
		}
	}
}

// subscriberCount reports the number of active subscribers; used by tests to assert the broker idles
// (drops to zero, stopping its loop) once every connection disconnects.
func (b *clusterBroker) subscriberCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	return len(b.subscribers)
}
