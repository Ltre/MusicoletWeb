package agenthub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

const defaultRequestTimeout = 30 * time.Second

type Request struct {
	ID, Path   string
	Start, End int64
}

type Result struct {
	Data       []byte
	Err        string
	Start, End int64
	Size       int64
}

type pendingRequest struct {
	ch         chan Result
	generation uint64
}

type Hub struct {
	mu             sync.Mutex
	ch             chan Request
	wait           map[string]pendingRequest
	online         bool
	generation     uint64
	requestTimeout time.Duration
}

func New() *Hub { return NewWithTimeout(defaultRequestTimeout) }

// NewWithTimeout is primarily useful for deterministic tests and deployments
// that need a stricter upper bound than the default 30-second Agent response timeout.
func NewWithTimeout(timeout time.Duration) *Hub {
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	return &Hub{wait: map[string]pendingRequest{}, requestTimeout: timeout}
}

// Connect replaces any prior agent session. Requests already dispatched to the
// previous generation cannot be safely replayed against the replacement Agent,
// so they fail immediately instead of waiting for the normal request timeout.
// The returned disconnect callback only marks the hub offline if it still
// belongs to the newest generation.
func (h *Hub) Connect() (<-chan Request, func()) {
	h.mu.Lock()
	h.failPendingLocked(0, "agent disconnected")
	h.generation++
	gen := h.generation
	ch := make(chan Request, 32)
	h.ch = ch
	h.online = true
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if h.generation == gen {
			h.online = false
			h.ch = nil
			h.failPendingLocked(gen, "agent disconnected")
		}
		h.mu.Unlock()
	}
}

func (h *Hub) Online() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.online }

func (h *Hub) Request(ctx context.Context, path string, start, end int64) (Result, error) {
	h.mu.Lock()
	if !h.online || h.ch == nil {
		h.mu.Unlock()
		return Result{}, errors.New("agent offline")
	}
	ch := h.ch
	gen := h.generation
	timeout := h.requestTimeout
	id := rid()
	c := make(chan Result, 1)
	h.wait[id] = pendingRequest{ch: c, generation: gen}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.wait, id)
		h.mu.Unlock()
	}()

	req := Request{id, path, start, end}
	select {
	case ch <- req:
	case r := <-c:
		return resultOrError(r)
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-c:
		return resultOrError(r)
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-timer.C:
		return Result{}, errors.New("agent timeout")
	}
}

func (h *Hub) Deliver(id string, r Result) bool {
	h.mu.Lock()
	p, ok := h.wait[id]
	h.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case p.ch <- r:
		return true
	default:
		return false
	}
}

func (h *Hub) failPendingLocked(generation uint64, message string) {
	for id, p := range h.wait {
		if generation != 0 && p.generation != generation {
			continue
		}
		select {
		case p.ch <- Result{Err: message}:
		default:
		}
		delete(h.wait, id)
	}
}

func resultOrError(r Result) (Result, error) {
	if r.Err != "" {
		return Result{}, errors.New(r.Err)
	}
	return r, nil
}

func rid() string { var b [12]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }
