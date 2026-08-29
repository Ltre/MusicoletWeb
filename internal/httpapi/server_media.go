package httpapi

import (
	"context"
	"mime"
	"net/http"
	"path/filepath"
	"time"
)

func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	s.serveMedia(w, r, path)
}

func (s *Server) publicNow(w http.ResponseWriter, r *http.Request) {
	v, e := s.App.Playback(r.Context())
	if e != nil {
		fail(w, e)
		return
	}
	out := map[string]any{"playing": v.Playing, "position_ms": v.PositionMS, "queue_name": v.QueueName, "speed": v.Speed}
	if v.Song != nil {
		out["song"] = map[string]any{"title": v.Song.Title, "artist": v.Song.Artist, "album": v.Song.Album, "duration_ms": v.Song.DurationMS}
	}
	writeJSON(w, 200, out)
}

func (s *Server) publicMedia(w http.ResponseWriter, r *http.Request) {
	v, e := s.App.Playback(r.Context())
	if e != nil || v.Path == "" {
		http.Error(w, "no current song", 404)
		return
	}
	s.serveMedia(w, r, v.Path)
}

func (s *Server) serveMedia(w http.ResponseWriter, r *http.Request, path string) {
	if path == "" {
		http.Error(w, "path required", 400)
		return
	}
	start, end := parseRange(r.Header.Get("Range"))
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	b, e := s.Hub.Request(ctx, path, start, end)
	if e != nil {
		http.Error(w, e.Error(), 503)
		return
	}
	ct := mime.TypeByExtension(filepath.Ext(path))
	if ct == "" {
		ct = http.DetectContentType(b)
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Accept-Ranges", "bytes")
	if start > 0 || end >= 0 {
		w.WriteHeader(http.StatusPartialContent)
	}
	_, _ = w.Write(b)
}
