package httpapi

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Ltre/MusicoletWeb/internal/agenthub"
)

const mediaChunk = int64(4 << 20)

func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	s.serveMedia(w, r, r.URL.Query().Get("path"))
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

func validAgentChunk(r agenthub.Result, wantStart, maxEnd, wantSize int64) bool {
	if r.Size <= 0 || r.Start != wantStart || r.End < r.Start || r.End > maxEnd || r.End >= r.Size {
		return false
	}
	if wantSize > 0 && r.Size != wantSize {
		return false
	}
	return int64(len(r.Data)) == r.End-r.Start+1
}

func (s *Server) serveMedia(w http.ResponseWriter, r *http.Request, path string) {
	if path == "" {
		http.Error(w, "path required", 400)
		return
	}
	br, err := parseRange(r.Header.Get("Range"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusRequestedRangeNotSatisfiable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	firstEnd := br.End
	if firstEnd < 0 || firstEnd-br.Start+1 > mediaChunk {
		firstEnd = br.Start + mediaChunk - 1
	}
	first, err := s.Hub.Request(ctx, path, br.Start, firstEnd)
	if err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	if first.Size <= 0 || br.Start >= first.Size {
		http.Error(w, "range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if !validAgentChunk(first, br.Start, firstEnd, 0) {
		http.Error(w, "invalid Agent media chunk", http.StatusBadGateway)
		return
	}
	end := br.End
	if end < 0 || end >= first.Size {
		end = first.Size - 1
	}
	if end < br.Start {
		http.Error(w, "range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	ct := mime.TypeByExtension(filepath.Ext(path))
	if ct == "" {
		ct = http.DetectContentType(first.Data)
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Accept-Ranges", "bytes")
	length := end - br.Start + 1
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	if br.Requested {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", br.Start, end, first.Size))
		w.WriteHeader(http.StatusPartialContent)
	}
	if r.Method == http.MethodHead {
		return
	}
	writeChunk := func(b []byte) bool {
		_, e := w.Write(b)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return e == nil
	}
	if !writeChunk(first.Data) {
		return
	}
	next := first.End + 1
	for next <= end {
		chunkEnd := next + mediaChunk - 1
		if chunkEnd > end {
			chunkEnd = end
		}
		rr, e := s.Hub.Request(ctx, path, next, chunkEnd)
		if e != nil {
			return
		}
		if !validAgentChunk(rr, next, chunkEnd, first.Size) {
			return
		}
		if !writeChunk(rr.Data) {
			return
		}
		next = rr.End + 1
	}
}
