package agenthub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Ltre/MusicoletWeb/internal/agentproto"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type connection struct {
	ws        *websocket.Conn
	write     sync.Mutex
	pending   map[string]chan agentproto.Message
	mu        sync.Mutex
	connected time.Time
	version   string
}
type Hub struct {
	token  string
	mu     sync.RWMutex
	active *connection
}

func New(token string) *Hub { return &Hub{token: token} }
func (h *Hub) Online() bool { h.mu.RLock(); defer h.mu.RUnlock(); return h.active != nil }
func (h *Hub) Status() map[string]any {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.active == nil {
		return map[string]any{"online": false}
	}
	h.active.mu.Lock()
	version := h.active.version
	h.active.mu.Unlock()
	return map[string]any{"online": true, "connectedAt": h.active.connected, "version": version}
}

func (h *Hub) ServeConnect(w http.ResponseWriter, r *http.Request) {
	if !constantToken(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), h.token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	ws.SetReadLimit(2 << 20)
	c := &connection{ws: ws, pending: map[string]chan agentproto.Message{}, connected: time.Now()}
	h.mu.Lock()
	old := h.active
	h.active = c
	h.mu.Unlock()
	if old != nil {
		_ = old.ws.Close(websocket.StatusNormalClosure, "replaced by newer agent")
	}
	defer func() {
		h.mu.Lock()
		if h.active == c {
			h.active = nil
		}
		h.mu.Unlock()
		c.failAll()
		_ = ws.CloseNow()
	}()
	for {
		var msg agentproto.Message
		if err = wsjson.Read(r.Context(), ws, &msg); err != nil {
			return
		}
		if msg.Type == "hello" {
			c.mu.Lock()
			c.version = msg.Version
			c.mu.Unlock()
			continue
		}
		c.mu.Lock()
		ch := c.pending[msg.ID]
		if ch != nil {
			delete(c.pending, msg.ID)
		}
		c.mu.Unlock()
		if ch != nil {
			ch <- msg
			close(ch)
		}
	}
}

func (h *Hub) Read(ctx context.Context, path string, offset int64, length int) (agentproto.Message, error) {
	if length <= 0 || length > 1<<20 {
		length = 1 << 20
	}
	h.mu.RLock()
	c := h.active
	h.mu.RUnlock()
	if c == nil {
		return agentproto.Message{}, errors.New("phone agent is offline")
	}
	id := randomID()
	ch := make(chan agentproto.Message, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	req := agentproto.Message{Type: "read", ID: id, Path: path, Offset: offset, Length: length}
	c.write.Lock()
	err := wsjson.Write(ctx, c.ws, req)
	c.write.Unlock()
	if err != nil {
		c.remove(id)
		return agentproto.Message{}, err
	}
	select {
	case msg, ok := <-ch:
		if !ok {
			return agentproto.Message{}, errors.New("agent disconnected")
		}
		if msg.Error != "" {
			return msg, errors.New(msg.Error)
		}
		return msg, nil
	case <-ctx.Done():
		c.remove(id)
		return agentproto.Message{}, ctx.Err()
	}
}

func (c *connection) remove(id string) { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }
func (c *connection) failAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, ch := range c.pending {
		delete(c.pending, id)
		close(ch)
	}
}
func randomID() string { var b [12]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }
func constantToken(got, want string) bool {
	if len(got) != len(want) || want == "" {
		return false
	}
	var diff byte
	for i := range got {
		diff |= got[i] ^ want[i]
	}
	return diff == 0
}
