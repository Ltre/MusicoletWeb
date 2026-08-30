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
	if !s.agentOK(r) {
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
	if !s.agentOK(r) {
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
	start, _ := strconv.ParseInt(r.Header.Get("X-Agent-Start"), 10, 64)
	end, _ := strconv.ParseInt(r.Header.Get("X-Agent-End"), 10, 64)
	size, _ := strconv.ParseInt(r.Header.Get("X-Agent-Size"), 10, 64)
	if !s.Hub.Deliver(id, agenthub.Result{Data: b, Err: errText, Start: start, End: end, Size: size}) {
		http.Error(w, "request not found", 404)
		return
	}
	writeOK(w)
}

func (s *Server) agentOK(r *http.Request) bool {
	token, err := s.Secure.Get(r.Context(), "agent_token")
	if err != nil {
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func (s *Server) agentTokenRotate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if readJSON(w, r, &in) != nil {
		return
	}
	if len(strings.TrimSpace(in.Token)) < 24 {
		writeJSON(w, 400, map[string]string{"error": "token must be at least 24 characters"})
		return
	}
	if err := s.Secure.Set(r.Context(), "agent_token", in.Token); err != nil {
		fail(w, err)
		return
	}
	writeOK(w)
}

type byteRange struct {
	Start, End int64
	Requested  bool
}

func parseRange(h string) (byteRange, error) {
	h = strings.TrimSpace(h)
	if h == "" {
		return byteRange{Start: 0, End: -1, Requested: false}, nil
	}
	if !strings.HasPrefix(h, "bytes=") || strings.Contains(strings.TrimPrefix(h, "bytes="), ",") {
		return byteRange{}, fmt.Errorf("unsupported Range")
	}
	p := strings.SplitN(strings.TrimPrefix(h, "bytes="), "-", 2)
	if len(p) != 2 || p[0] == "" {
		return byteRange{}, fmt.Errorf("suffix ranges are not supported")
	}
	a, e := strconv.ParseInt(p[0], 10, 64)
	if e != nil || a < 0 {
		return byteRange{}, fmt.Errorf("invalid Range")
	}
	b := int64(-1)
	if p[1] != "" {
		b, e = strconv.ParseInt(p[1], 10, 64)
		if e != nil || b < a {
			return byteRange{}, fmt.Errorf("invalid Range")
		}
	}
	return byteRange{Start: a, End: b, Requested: true}, nil
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
func randomToken(n int) string { b := make([]byte, n); _, _ = rand.Read(b); return hex.EncodeToString(b) }
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
