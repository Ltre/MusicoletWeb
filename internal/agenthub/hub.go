package agenthub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

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

type Hub struct {
	mu         sync.Mutex
	ch         chan Request
	wait       map[string]chan Result
	online     bool
	generation uint64
}

func New() *Hub { return &Hub{wait: map[string]chan Result{}} }

// Connect replaces any prior agent session. The returned disconnect callback only
// marks the hub offline if it still belongs to the newest generation.
func (h *Hub) Connect() (<-chan Request, func()) {
	h.mu.Lock()
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
	id := rid()
	c := make(chan Result, 1)
	h.wait[id] = c
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.wait, id); h.mu.Unlock() }()
	select {
	case ch <- Request{id, path, start, end}:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	select {
	case r := <-c:
		if r.Err != "" {
			return Result{}, errors.New(r.Err)
		}
		return r, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-time.After(30 * time.Second):
		return Result{}, errors.New("agent timeout")
	}
}

func (h *Hub) Deliver(id string, r Result) bool {
	h.mu.Lock()
	c := h.wait[id]
	h.mu.Unlock()
	if c == nil {
		return false
	}
	select {
	case c <- r:
		return true
	default:
		return false
	}
}

func rid() string { var b [12]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }
