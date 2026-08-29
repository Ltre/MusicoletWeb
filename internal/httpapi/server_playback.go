package httpapi

import "net/http"

func (s *Server) library(w http.ResponseWriter, r *http.Request) {
	snap, e := s.App.WorkingSnapshot(r.Context())
	if e != nil {
		fail(w, e)
		return
	}
	writeJSON(w, 200, snap)
}

func (s *Server) playback(w http.ResponseWriter, r *http.Request) {
	v, e := s.App.Playback(r.Context())
	if e != nil {
		fail(w, e)
		return
	}
	writeJSON(w, 200, v)
}

func (s *Server) playbackSet(w http.ResponseWriter, r *http.Request) {
	var in struct {
		QueueID    int64  `json:"queue_id"`
		Path       string `json:"path"`
		PositionMS int64  `json:"position_ms"`
		Playing    bool   `json:"playing"`
	}
	if readJSON(w, r, &in) != nil {
		return
	}
	if e := s.App.SetPlayback(r.Context(), in.QueueID, in.Path, in.PositionMS, in.Playing); e != nil {
		fail(w, e)
		return
	}
	writeOK(w)
}

func (s *Server) playbackMode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Shuffle bool    `json:"shuffle"`
		Loop    string  `json:"loop"`
		Speed   float64 `json:"speed"`
	}
	if readJSON(w, r, &in) != nil {
		return
	}
	if e := s.App.SetPlaybackMode(r.Context(), in.Shuffle, in.Loop, in.Speed); e != nil {
		fail(w, e)
		return
	}
	writeOK(w)
}

func (s *Server) stopTarget(w http.ResponseWriter, r *http.Request) {
	var in struct {
		QueueID int64  `json:"queue_id"`
		Path    string `json:"path"`
	}
	if readJSON(w, r, &in) != nil {
		return
	}
	if e := s.App.SetStopTarget(r.Context(), in.QueueID, in.Path); e != nil {
		fail(w, e)
		return
	}
	writeOK(w)
}
