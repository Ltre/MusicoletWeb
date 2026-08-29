package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/Ltre/MusicoletWeb/internal/agenthub"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func (s *Server) agentConnect(w http.ResponseWriter, r *http.Request) {
	if !agentOK(r, s.Cfg.AgentToken) {
		http.Error(w, "unauthorized", 401)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch, done := s.Hub.Connect()
	defer done()
	fmt.Fprintf(w, "event: ready\ndata: {}\n\n")
	fl.Flush()
	tick := time.NewTicker(45 * time.Second)
	defer tick.Stop()
	for {
		select {
		case req := <-ch:
			b, _ := json.Marshal(req)
			fmt.Fprintf(w, "event: read\ndata: %s\n\n", b)
			fl.Flush()
		case <-tick.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) agentResult(w http.ResponseWriter, r *http.Request) {
	if !agentOK(r, s.Cfg.AgentToken) {
		http.Error(w, "unauthorized", 401)
		return
	}
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	b, e := io.ReadAll(r.Body)
	if e != nil {
		fail(w, e)
		return
	}
	errText := r.Header.Get("X-Agent-Error")
	if !s.Hub.Deliver(id, agenthub.Result{Data: b, Err: errText}) {
		http.Error(w, "request not found", 404)
		return
	}
	writeOK(w)
}

func agentOK(r *http.Request, token string) bool {
	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func parseRange(h string) (int64, int64) {
	if !strings.HasPrefix(h, "bytes=") {
		return 0, 4<<20 - 1
	}
	p := strings.Split(strings.TrimPrefix(h, "bytes="), "-")
	a, _ := strconv.ParseInt(p[0], 10, 64)
	b := a + (4 << 20) - 1
	if len(p) > 1 && p[1] != "" {
		if x, e := strconv.ParseInt(p[1], 10, 64); e == nil && x >= a && x-a < 16<<20 {
			b = x
		}
	}
	return a, b
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		writeJSON(w, 400, map[string]string{"error": e.Error()})
		return e
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeOK(w http.ResponseWriter) { writeJSON(w, 200, map[string]bool{"ok": true}) }

func fail(w http.ResponseWriter, e error) { writeJSON(w, 400, map[string]string{"error": e.Error()}) }

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func FileExists(p string) bool { _, e := os.Stat(p); return e == nil }
