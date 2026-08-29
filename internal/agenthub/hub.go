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
	Data []byte
	Err  string
}
type Hub struct {
	mu     sync.Mutex
	ch     chan Request
	wait   map[string]chan Result
	online bool
}

func New() *Hub { return &Hub{ch: make(chan Request, 16), wait: map[string]chan Result{}} }
func (h *Hub) Connect() (<-chan Request, func()) {
	h.mu.Lock()
	h.online = true
	h.mu.Unlock()
	return h.ch, func() { h.mu.Lock(); h.online = false; h.mu.Unlock() }
}
func (h *Hub) Online() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.online }
func (h *Hub) Request(ctx context.Context, path string, start, end int64) ([]byte, error) {
	h.mu.Lock()
	if !h.online {
		h.mu.Unlock()
		return nil, errors.New("agent offline")
	}
	id := rid()
	c := make(chan Result, 1)
	h.wait[id] = c
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.wait, id); h.mu.Unlock() }()
	select {
	case h.ch <- Request{id, path, start, end}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case r := <-c:
		if r.Err != "" {
			return nil, errors.New(r.Err)
		}
		return r.Data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		return nil, errors.New("agent timeout")
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
func rid() string { var b [12]byte; rand.Read(b[:]); return hex.EncodeToString(b[:]) }
